package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/opsbl/virtualsms/internal/config"
)

// Message is the provider-neutral operation submitted by a modem.
type Message struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

// Batch contains requests with the same text collected within one time window.
type Batch struct {
	To   []string
	Text string
}

type Ack struct{ Queued bool }

func ValidPhone(s string) bool {
	if len(s) < 1 || len(s) > 20 {
		return false
	}
	for i, b := range []byte(s) {
		if b == '+' && i == 0 {
			continue
		}
		if b < '0' || b > '9' {
			return false
		}
	}
	return s != "+"
}

// Provider implementations may be added without changing the AT modem.
type Provider interface {
	// Results align with Batch.To, allowing individual AT requests to be acknowledged accurately.
	SendBatch(context.Context, Batch) []error
}

type logProvider struct{ name string }

func (p logProvider) SendBatch(_ context.Context, b Batch) []error {
	slog.Info("SMS batch accepted by log provider", "provider", p.name, "recipients", b.To, "text", b.Text)
	return make([]error, len(b.To))
}

type httpProvider struct {
	url, token string
	client     *http.Client
}

func (p httpProvider) SendBatch(ctx context.Context, b Batch) []error {
	results := make([]error, len(b.To))
	for i, to := range b.To {
		results[i] = p.sendOne(ctx, Message{To: to, Text: b.Text})
	}
	return results
}

func (p httpProvider) sendOne(ctx context.Context, m Message) error {
	data, _ := json.Marshal(m)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP provider returned %s", resp.Status)
	}
	return nil
}

func New(configs map[string]config.ProviderConfig) (map[string]Provider, error) {
	return NewWithPlugins(configs, "")
}

func NewWithPlugins(configs map[string]config.ProviderConfig, pluginDir string) (map[string]Provider, error) {
	available, err := discoverPlugins(pluginDir, configs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Provider, len(configs))
	for name, p := range configs {
		if p.Plugin != "" {
			path, ok := available[p.Plugin]
			if !ok {
				return nil, fmt.Errorf("provider %q: plugin %q not found in %q", name, p.Plugin, pluginDir)
			}
			instance, err := loadPlugin(path, name, p.Options)
			if err != nil {
				return nil, fmt.Errorf("provider %q: %w", name, err)
			}
			out[name] = instance
			continue
		}
		timeout := 30 * time.Second
		if p.Timeout != "" {
			var err error
			timeout, err = time.ParseDuration(p.Timeout)
			if err != nil || timeout <= 0 {
				return nil, fmt.Errorf("provider %q: invalid timeout %q", name, p.Timeout)
			}
		}
		switch p.Type {
		case "log":
			out[name] = logProvider{name: name}
		case "http":
			out[name] = httpProvider{url: p.URL, token: p.Token, client: &http.Client{Timeout: timeout}}
		default:
			return nil, fmt.Errorf("provider %q has unsupported type %q", name, p.Type)
		}
	}
	return out, nil
}
