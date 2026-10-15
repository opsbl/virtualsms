package modem

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"github.com/opsbl/virtualsms/internal/config"
	"github.com/opsbl/virtualsms/internal/metrics"
	"github.com/opsbl/virtualsms/internal/provider"
	"golang.org/x/sys/unix"
)

type Modem struct {
	device        config.DeviceConfig
	master, slave *os.File
	sender        Sender
	metrics       *metrics.Metrics
	echo          bool
	textMode      bool
	input         []byte
	message       []byte
	to            string
	sequence      atomic.Uint64
}

type Sender interface {
	Send(context.Context, provider.Message) (provider.Ack, error)
}

func Open(d config.DeviceConfig, sender Sender, m *metrics.Metrics) (*Modem, error) {
	master, slave, err := pty.Open()
	if err != nil {
		return nil, err
	}
	cleanup := func() { master.Close(); slave.Close() }
	term, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		cleanup()
		return nil, err
	}
	term.Iflag = 0
	term.Oflag = 0
	term.Lflag = 0
	term.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL
	term.Cc[unix.VMIN] = 1
	term.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(int(slave.Fd()), unix.TCSETS, term); err != nil {
		cleanup()
		return nil, err
	}
	mode := os.FileMode(d.Mode)
	if mode == 0 {
		mode = 0660
	}
	if err := slave.Chmod(mode); err != nil {
		cleanup()
		return nil, err
	}
	if d.Group != "" {
		g, err := user.LookupGroup(d.Group)
		if err != nil {
			cleanup()
			return nil, err
		}
		gid, err := strconv.Atoi(g.Gid)
		if err != nil {
			cleanup()
			return nil, err
		}
		if err := slave.Chown(-1, gid); err != nil {
			cleanup()
			return nil, err
		}
	}
	if err := installLink(d.Path, slave.Name()); err != nil {
		cleanup()
		return nil, err
	}
	return &Modem{device: d, master: master, slave: slave, sender: sender, metrics: m, echo: true, textMode: true}, nil
}

func installLink(path, target string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s exists and is not a symlink", path)
		}
		old, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(old, "/dev/pts/") {
			return fmt.Errorf("%s is an unrelated symlink", path)
		}
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already points to a live PTY", path)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, path)
}

func (m *Modem) Close() {
	if target, err := os.Readlink(m.device.Path); err == nil && target == m.slave.Name() {
		os.Remove(m.device.Path)
	}
	m.master.Close()
	m.slave.Close()
	slog.Info("virtual modem closed", "device", m.device.Path)
}

func (m *Modem) Serve(ctx context.Context) error {
	defer m.Close()
	buf := make([]byte, 1024)
	for {
		if ctx.Err() != nil {
			return nil
		}
		fds := []unix.PollFd{{Fd: int32(m.master.Fd()), Events: unix.POLLIN}}
		ready, err := unix.Poll(fds, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if ready == 0 {
			continue
		}
		n, err := unix.Read(int(m.master.Fd()), buf)
		if err == unix.EINTR || err == unix.EAGAIN {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.EOF
		}
		for _, b := range buf[:n] {
			m.consume(ctx, b)
		}
	}
}

func (m *Modem) SlaveName() string { return m.slave.Name() }

func (m *Modem) write(s string) {
	if _, err := io.WriteString(m.master, s); err != nil {
		slog.Warn("modem write failed", "device", m.device.Path, "error", err)
	}
}
func (m *Modem) ok()   { m.write("\r\nOK\r\n") }
func (m *Modem) fail() { m.write("\r\nERROR\r\n") }

func (m *Modem) consume(ctx context.Context, b byte) {
	if b == 0x1b {
		m.message = nil
		m.to = ""
		m.input = nil
		return
	}
	if m.to != "" {
		if b == 0x1a {
			msg := provider.Message{To: m.to, Text: string(m.message)}
			m.to = ""
			m.message = nil
			if msg.Text == "" {
				slog.Warn("empty SMS rejected", "device", m.device.Path, "to", msg.To)
				m.fail()
				return
			}
			start := time.Now()
			slog.Info("SMS received from AT client", "device", m.device.Path, "instance", m.device.Instance, "to", msg.To, "message_bytes", len(msg.Text))
			ack, err := m.sender.Send(ctx, msg)
			m.metrics.Record(m.device.Path, m.device.Instance, ack.Queued, err, time.Since(start))
			if err != nil {
				slog.Error("SMS delivery failed", "device", m.device.Path, "instance", m.device.Instance, "to", msg.To, "elapsed_ms", time.Since(start).Milliseconds(), "error", err)
				m.write("\r\n+CMS ERROR: 500\r\n")
				return
			}
			m.write(fmt.Sprintf("\r\n+CMGS: %d\r\n\r\nOK\r\n", m.sequence.Add(1)))
			slog.Info("SMS acknowledged to AT client", "device", m.device.Path, "instance", m.device.Instance, "to", msg.To, "queued", ack.Queued, "elapsed_ms", time.Since(start).Milliseconds())
			return
		}
		if len(m.message) >= 65535 {
			m.to = ""
			m.message = nil
			m.fail()
			return
		}
		m.message = append(m.message, b)
		return
	}
	if b == '\n' {
		return
	}
	if b != '\r' {
		if len(m.input) >= 512 {
			m.input = nil
			m.fail()
			return
		}
		m.input = append(m.input, b)
		if m.echo {
			m.write(string(b))
		}
		return
	}
	cmd := strings.ToUpper(strings.TrimSpace(string(m.input)))
	m.input = nil
	if m.echo {
		m.write("\r")
	}
	switch {
	case cmd == "AT", cmd == "AT+CMEE=2", cmd == "AT+CMGF=1", cmd == "AT+CSCS=\"GSM\"", cmd == "AT+CSCS=\"IRA\"":
		m.textMode = true
		m.ok()
	case cmd == "ATE0":
		m.echo = false
		m.ok()
	case cmd == "ATE1":
		m.echo = true
		m.ok()
	case cmd == "AT+CMGF?":
		m.write("\r\n+CMGF: 1\r\n")
		m.ok()
	case cmd == "AT+CPIN?":
		m.write("\r\n+CPIN: READY\r\n")
		m.ok()
	case cmd == "AT+CSQ":
		m.write("\r\n+CSQ: 20,0\r\n")
		m.ok()
	case cmd == "AT+CREG?":
		m.write("\r\n+CREG: 0,1\r\n")
		m.ok()
	case strings.HasPrefix(cmd, "AT+CMGS=\"") && strings.HasSuffix(cmd, "\"") && m.textMode:
		to := string(maybeOriginalPhone(cmd))
		if !provider.ValidPhone(to) {
			slog.Warn("invalid SMS destination rejected", "device", m.device.Path, "to", to)
			m.fail()
			return
		}
		m.to = to
		m.message = nil
		m.write("\r\n> ")
	default:
		slog.Debug("unsupported AT command", "device", m.device.Path, "command", cmd)
		m.fail()
	}
}

func maybeOriginalPhone(cmd string) []byte { return []byte(cmd[len("AT+CMGS=\"") : len(cmd)-1]) }
