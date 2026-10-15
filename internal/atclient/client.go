package atclient

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"
)

// Send submits one message through a running virtual serial modem.
func Send(ctx context.Context, path, to, message string) error {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return err
	}
	raw := *old
	raw.Iflag = 0
	raw.Oflag = 0
	raw.Lflag = 0
	raw.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return err
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, old)
	for _, step := range []struct{ send, want string }{
		{"\x1bAT+CMEE=2\r", "\r\nOK\r\n"},
		{"ATE0\r", "\r\nOK\r\n"},
		{"AT\r", "\r\nOK\r\n"},
		{"AT+CMGF=1\r", "\r\nOK\r\n"},
		{fmt.Sprintf("AT+CMGS=\"%s\"\r", to), "> "},
	} {
		if err := write(ctx, fd, step.send); err != nil {
			return err
		}
		if _, err := readUntil(ctx, fd, step.want); err != nil {
			return err
		}
	}
	if err := write(ctx, fd, message+"\x1a"); err != nil {
		return err
	}
	response, err := readUntil(ctx, fd, "\r\nOK\r\n")
	if err != nil {
		return err
	}
	if !strings.Contains(response, "+CMGS:") {
		return fmt.Errorf("modem did not confirm SMS submission: %q", response)
	}
	return nil
}

func write(ctx context.Context, fd int, data string) error {
	buf := []byte(data)
	for len(buf) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := unix.Write(fd, buf)
		if err == unix.EINTR {
			continue
		}
		if err == unix.EAGAIN {
			if err := poll(ctx, fd, unix.POLLOUT); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		buf = buf[n:]
	}
	return nil
}

func readUntil(ctx context.Context, fd int, want string) (string, error) {
	var out strings.Builder
	buf := make([]byte, 256)
	for out.Len() < 4096 {
		if err := poll(ctx, fd, unix.POLLIN); err != nil {
			return out.String(), err
		}
		n, err := unix.Read(fd, buf)
		if err == unix.EINTR || err == unix.EAGAIN {
			continue
		}
		if err != nil {
			return out.String(), err
		}
		if n == 0 {
			return out.String(), fmt.Errorf("modem closed")
		}
		out.Write(buf[:n])
		response := out.String()
		if strings.Contains(response, "+CMS ERROR:") || strings.Contains(response, "\r\nERROR\r\n") {
			return response, fmt.Errorf("modem rejected SMS: %q", response)
		}
		if strings.Contains(response, want) {
			return response, nil
		}
	}
	return out.String(), fmt.Errorf("modem response exceeded 4096 bytes")
}

func poll(ctx context.Context, fd int, event int16) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: event}}
		ready, err := unix.Poll(fds, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if ready > 0 {
			return nil
		}
	}
}
