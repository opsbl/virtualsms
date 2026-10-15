package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opsbl/virtualsms/internal/config"
)

func TestHTTPProvider(t *testing.T) {
	var got Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected HTTP request: %s %v", r.Method, r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	p, err := New(map[string]config.ProviderConfig{"gateway": {Type: "http", URL: server.URL, Token: "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	msg := Message{To: "+15551234567", Text: "hello"}
	if err := p["gateway"].SendBatch(context.Background(), Batch{To: []string{msg.To}, Text: msg.Text})[0]; err != nil {
		t.Fatal(err)
	}
	if got != msg {
		t.Fatalf("got %+v, want %+v", got, msg)
	}
}
