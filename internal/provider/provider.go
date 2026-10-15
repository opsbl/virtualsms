package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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
	slog.Info("SMS batch accepted by log provider", "instance", p.name, "provider", "log", "recipients", b.To, "text", b.Text)
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

func New(configs map[string]config.InstanceConfig) (map[string]Provider, error) {
	return NewWithPlugins(configs, "")
}

func NewWithPlugins(configs map[string]config.InstanceConfig, pluginDir string) (map[string]Provider, error) {
	available, err := discoverPlugins(pluginDir, configs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Provider, len(configs))
	for name, cfg := range configs {
		switch cfg.Provider {
		case "log":
			if len(cfg.Options) != 0 {
				return nil, fmt.Errorf("instance %q: log has no options", name)
			}
			out[name] = logProvider{name: name}
		case "http":
			for key := range cfg.Options {
				if key != "url" && key != "token" && key != "timeout" {
					return nil, fmt.Errorf("instance %q: unknown HTTP option %q", name, key)
				}
			}
			urlValue, err := stringOption(name, cfg.Options, "url")
			if err != nil {
				return nil, err
			}
			u, err := url.Parse(urlValue)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, fmt.Errorf("instance %q: HTTP url must be HTTP(S)", name)
			}
			token, err := stringOption(name, cfg.Options, "token")
			if err != nil {
				return nil, err
			}
			timeoutValue, err := stringOption(name, cfg.Options, "timeout")
			if err != nil {
				return nil, err
			}
			timeout := 30 * time.Second
			if timeoutValue != "" {
				timeout, err = time.ParseDuration(timeoutValue)
				if err != nil || timeout <= 0 {
					return nil, fmt.Errorf("instance %q: invalid timeout %q", name, timeoutValue)
				}
			}
			out[name] = httpProvider{url: urlValue, token: token, client: &http.Client{Timeout: timeout}}
		default:
			def, ok := available[cfg.Provider]
			if !ok {
				return nil, fmt.Errorf("instance %q: provider %q not found in %q", name, cfg.Provider, pluginDir)
			}
			instance, err := newPlugin(def, cfg.Provider, name, cfg.Options)
			if err != nil {
				return nil, err
			}
			out[name] = instance
		}
	}
	return out, nil
}

func stringOption(instance string, options map[string]any, key string) (string, error) {
	value, ok := options[key]
	if !ok {
		return "", nil
	}
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("instance %q: option %q must be a string", instance, key)
	}
	return s, nil
}
