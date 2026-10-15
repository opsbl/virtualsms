package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opsbl/virtualsms/internal/batch"
	"github.com/opsbl/virtualsms/internal/config"
	"github.com/opsbl/virtualsms/internal/metrics"
	"github.com/opsbl/virtualsms/internal/modem"
	"github.com/opsbl/virtualsms/internal/provider"
)

type testSender struct{ messages chan provider.Message }

type batchCapture struct{ calls chan provider.Batch }

func (p batchCapture) SendBatch(_ context.Context, b provider.Batch) []error {
	p.calls <- b
	return make([]error, len(b.To))
}

func (s testSender) Send(_ context.Context, m provider.Message) (provider.Ack, error) {
	s.messages <- m
	return provider.Ack{}, nil
}

func TestSerialTestCommandNeedsNoConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ttySMS0")
	sender := testSender{messages: make(chan provider.Message, 1)}
	m, err := modem.Open(config.DeviceConfig{Path: path, Instance: "test"}, sender, metrics.New())
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
	cmd := command()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"test", "-d", path, "-t", "111", "-m", "hello", "-T", "2s"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "accepted") {
		t.Fatalf("unexpected output: %q", output.String())
	}
	select {
	case msg := <-sender.messages:
		if msg.To != "111" || msg.Text != "hello" {
			t.Fatalf("unexpected message: %+v", msg)
		}
	default:
		t.Fatal("modem did not receive test message")
	}
}

func TestSerialTestBatchesSequentialRecipients(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ttySMS0")
	p := batchCapture{calls: make(chan provider.Batch, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	meter := metrics.New()
	batcher := batch.New(ctx, map[string]provider.Provider{"test": p}, time.Second, "queued", meter)
	defer batcher.Close()
	m, err := modem.Open(config.DeviceConfig{Path: path, Instance: "test"}, batcher.For("test"), meter)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.Serve(ctx) }()
	defer func() { cancel(); <-done }()
	for _, to := range []string{"111", "222", "333"} {
		cmd := command()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetArgs([]string{"test", "-d", path, "-t", to, "-m", "same text"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case sent := <-p.calls:
		if sent.Text != "same text" || len(sent.To) != 3 {
			t.Fatalf("unexpected batch: %+v", sent)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not receive batch")
	}
}

func TestDirectTestShortFlags(t *testing.T) {
	device := filepath.Join(t.TempDir(), "ttySMS0")
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[instances.dryrun]\nprovider = \"log\"\n[[devices]]\npath = \"" + device + "\"\ninstance = \"dryrun\"\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := command()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"-c", path, "test", "-D", "-d", device, "-t", "111", "-m", "hello", "-T", "2s"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "accepted by provider") {
		t.Fatalf("unexpected output: %q", output.String())
	}
}
