package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/opsbl/virtualsms/internal/batch"
	"github.com/opsbl/virtualsms/internal/config"
	"github.com/opsbl/virtualsms/internal/metrics"
	"github.com/opsbl/virtualsms/internal/modem"
	"github.com/opsbl/virtualsms/internal/provider"
)

func Run(parent context.Context, c config.Config) error {
	level := slog.LevelInfo
	switch c.Logging.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	providers, err := provider.NewWithPlugins(c.Providers, c.Plugins.Dir)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", c.Metrics.Listen)
	if err != nil {
		return fmt.Errorf("metrics listen: %w", err)
	}
	defer listener.Close()
	m := metrics.New()
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	batchCtx, cancelBatch := context.WithCancel(context.Background())
	defer cancelBatch()
	wait := 3 * time.Second
	if c.Batch.Wait != "" {
		wait, err = time.ParseDuration(c.Batch.Wait)
		if err != nil {
			return err
		}
	}
	batcher := batch.New(batchCtx, providers, wait, c.Batch.AckMode, m)
	slog.Info("VirtualSMS starting", "devices", len(c.Devices), "providers", len(providers), "batch_wait", wait.String(), "ack_mode", c.Batch.AckMode, "metrics_listen", c.Metrics.Listen)
	modems := make([]*modem.Modem, 0, len(c.Devices))
	for _, d := range c.Devices {
		md, err := modem.Open(d, batcher.For(d.Provider), m)
		if err != nil {
			slog.Error("virtual modem startup failed", "device", d.Path, "error", err)
			for _, previous := range modems {
				previous.Close()
			}
			return fmt.Errorf("device %s: %w", d.Path, err)
		}
		modems = append(modems, md)
		m.DeviceUp(d.Path, d.Provider, true)
		slog.Info("virtual modem ready", "device", d.Path, "pty", md.SlaveName(), "provider", d.Provider)
	}
	errCh := make(chan error, len(modems)+1)
	var workers sync.WaitGroup
	for i, md := range modems {
		d := c.Devices[i]
		workers.Add(1)
		go func() {
			defer workers.Done()
			err := md.Serve(ctx)
			m.DeviceUp(d.Path, d.Provider, false)
			if err != nil {
				errCh <- fmt.Errorf("device %s: %w", d.Path, err)
			}
		}()
	}
	server := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	slog.Info("metrics listening", "address", listener.Addr())
	select {
	case <-ctx.Done():
	case err = <-errCh:
		stop()
	}
	slog.Info("VirtualSMS stopping", "error", err)
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(shutdown)
	workers.Wait()
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 35*time.Second)
	if drainErr := batcher.Drain(drainCtx); drainErr != nil {
		slog.Error("SMS batch drain failed", "error", drainErr)
	}
	cancelDrain()
	cancelBatch()
	slog.Info("VirtualSMS stopped")
	return err
}
