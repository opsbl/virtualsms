package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/opsbl/virtualsms/pkg/pluginapi"
)

type ableMobileProvider struct {
	options options
	client  *http.Client
	logger  *slog.Logger
}

func (p *ableMobileProvider) SendBatch(ctx context.Context, b pluginapi.Batch) []error {
	results := make([]error, len(b.To))
	if len(b.To) == 0 {
		return results
	}
	start := time.Now()
	err := p.send(ctx, strings.Join(b.To, ";"), b.Text)
	if err != nil {
		p.logger.Error("AbleMobile batch failed", "recipients", len(b.To), "duration_ms", time.Since(start).Milliseconds(), "error", err)
	} else {
		p.logger.Info("AbleMobile batch submitted", "recipients", len(b.To), "duration_ms", time.Since(start).Milliseconds())
	}
	for i := range results {
		results[i] = err
	}
	return results
}

func (p *ableMobileProvider) send(ctx context.Context, telephone, message string) error {
	form := url.Values{
		"Username":     {p.options.Username},
		"Password":     {p.options.Password},
		"Telephone":    {telephone},
		"Message":      {message},
		"UserDefineNo": {p.options.UserDefineNo},
		"Hex":          {p.options.Hex},
		"Sender":       {p.options.Sender},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.options.URL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 4096)); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("AbleMobile returned %s", resp.Status)
	}
	return nil
}
