package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opsbl/virtualsms/internal/atclient"
	"github.com/opsbl/virtualsms/internal/config"
	"github.com/opsbl/virtualsms/internal/provider"
)

func TestShutdownDrainsQueuedSMS(t *testing.T) {
	received := make(chan provider.Message, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg provider.Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Error(err)
		}
		received <- msg
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "ttySMS0")
	cfg := config.Config{
		Metrics:   config.MetricsConfig{Listen: "127.0.0.1:0"},
		Batch:     config.BatchConfig{Wait: "1h", AckMode: "queued"},
		Instances: map[string]config.InstanceConfig{"gateway": {Provider: "http", Options: map[string]any{"url": server.URL}}},
		Devices:   []config.DeviceConfig{{Path: path, Instance: "gateway"}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()
	defer cancel()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Lstat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("device did not appear")
		}
		time.Sleep(10 * time.Millisecond)
	}
	sendCtx, stopSend := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopSend()
	if err := atclient.Send(sendCtx, path, "111", "queued message"); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("service did not stop")
	}
	select {
	case msg := <-received:
		if msg.To != "111" || msg.Text != "queued message" {
			t.Fatalf("unexpected message: %+v", msg)
		}
	default:
		t.Fatal("queued SMS was not delivered on shutdown")
	}
}
