package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/opsbl/virtualsms/pkg/pluginapi"
)

func TestPluginBatchAndLogging(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("unexpected request: %s %v", r.Method, r.Header)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		want := map[string]string{
			"Username": "account", "Password": "secret", "Telephone": "111;222;333",
			"Message": "测试 + &", "UserDefineNo": "00000", "Hex": "", "Sender": "",
		}
		for key, value := range want {
			if got := r.PostForm.Get(key); got != value {
				t.Errorf("%s: got %q, want %q", key, got, value)
			}
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil)).With("provider", "hk")
	p, err := New(pluginapi.Config{Name: "hk", OptionsTOML: []byte(fmt.Sprintf("url = %q\nusername = 'account'\npassword = 'secret'\n", server.URL)), Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	batch := pluginapi.Batch{To: []string{"111", "222", "333"}, Text: "测试 + &"}
	results := p.SendBatch(context.Background(), batch)
	if len(results) != 3 || calls.Load() != 1 {
		t.Fatalf("results = %v, calls = %d", results, calls.Load())
	}
	for _, err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	status.Store(http.StatusBadRequest)
	results = p.SendBatch(context.Background(), batch)
	if calls.Load() != 2 || results[0] == nil || results[1] == nil || results[2] == nil {
		t.Fatalf("failure results = %v, calls = %d", results, calls.Load())
	}
	if !strings.Contains(logs.String(), "AbleMobile batch submitted") || !strings.Contains(logs.String(), "AbleMobile batch failed") || strings.Contains(logs.String(), "secret") || strings.Contains(logs.String(), batch.Text) {
		t.Fatalf("unexpected plugin logs: %s", logs.String())
	}
}

func TestPluginRejectsUnknownOption(t *testing.T) {
	_, err := New(pluginapi.Config{OptionsTOML: []byte("username='u'\npassword='p'\nunknown='x'\n")})
	if err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("expected unknown option error, got %v", err)
	}
}
