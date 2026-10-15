package modem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opsbl/virtualsms/internal/config"
	"github.com/opsbl/virtualsms/internal/metrics"
	"github.com/opsbl/virtualsms/internal/provider"
)

type captureProvider struct {
	messages chan provider.Message
	err      error
}

func (p captureProvider) Send(_ context.Context, m provider.Message) (provider.Ack, error) {
	p.messages <- m
	return provider.Ack{}, p.err
}

func TestZabbixSerialSequence(t *testing.T) {
	for _, providerErr := range []error{nil, errors.New("gateway unavailable")} {
		t.Run(fmt.Sprint(providerErr), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ttySMS0")
			p := captureProvider{messages: make(chan provider.Message, 1), err: providerErr}
			m, err := Open(config.DeviceConfig{Path: path, Provider: "test"}, p, metrics.New())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- m.Serve(ctx) }()
			defer func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(time.Second):
					t.Error("modem did not stop")
				}
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Errorf("device link not removed: %v", err)
				}
			}()
			serial, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer serial.Close()
			for _, step := range []struct{ write, want string }{
				{"\x1bAT+CMEE=2\r", "OK"},
				{"ATE0\r", "OK"},
				{"AT\r", "OK"},
				{"AT+CMGF=1\r", "OK"},
				{"AT+CMGS=\"+1234567890\"\r", "> "},
			} {
				if _, err := io.WriteString(serial, step.write); err != nil {
					t.Fatal(err)
				}
				readUntil(t, serial, step.want)
			}
			if _, err := io.WriteString(serial, "hello from zabbix\x1a"); err != nil {
				t.Fatal(err)
			}
			if providerErr == nil {
				readUntil(t, serial, "+CMGS: 1\r\n\r\nOK")
			} else {
				readUntil(t, serial, "+CMS ERROR: 500")
			}
			select {
			case msg := <-p.messages:
				if msg.To != "+1234567890" || msg.Text != "hello from zabbix" {
					t.Errorf("unexpected message: %+v", msg)
				}
			case <-time.After(time.Second):
				t.Fatal("provider was not called")
			}
		})
	}
}

func readUntil(t *testing.T, f *os.File, want string) {
	t.Helper()
	result := make(chan string, 1)
	go func() {
		var out strings.Builder
		buf := make([]byte, 128)
		for {
			n, err := f.Read(buf)
			out.Write(buf[:n])
			if strings.Contains(out.String(), want) || err != nil {
				result <- out.String()
				return
			}
		}
	}()
	select {
	case got := <-result:
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q, got %q", want, got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %q", want)
	}
}
