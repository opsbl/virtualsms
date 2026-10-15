package batch

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/opsbl/virtualsms/internal/metrics"
	"github.com/opsbl/virtualsms/internal/provider"
)

type key struct{ instance, text string }
type pending struct {
	id      uint64
	created time.Time
	to      []string
	waiters []chan error
	timer   *time.Timer
}

// Batcher groups identical messages for the same instance from the first arrival.
type Batcher struct {
	ctx       context.Context
	instances map[string]provider.Provider
	wait      time.Duration
	ackMode   string
	metrics   *metrics.Metrics
	mu        sync.Mutex
	workers   sync.WaitGroup
	closed    bool
	groups    map[key]*pending
	sequence  atomic.Uint64
}

func New(ctx context.Context, instances map[string]provider.Provider, wait time.Duration, ackMode string, m *metrics.Metrics) *Batcher {
	return &Batcher{ctx: ctx, instances: instances, wait: wait, ackMode: ackMode, metrics: m, groups: make(map[key]*pending)}
}

type Sender struct {
	batcher  *Batcher
	instance string
}

func (b *Batcher) For(name string) Sender { return Sender{batcher: b, instance: name} }
func (s Sender) Send(ctx context.Context, msg provider.Message) (provider.Ack, error) {
	return s.batcher.submit(ctx, s.instance, msg)
}

func (b *Batcher) submit(ctx context.Context, name string, msg provider.Message) (provider.Ack, error) {
	if err := ctx.Err(); err != nil {
		return provider.Ack{}, err
	}
	if err := b.ctx.Err(); err != nil {
		return provider.Ack{}, err
	}
	k := key{instance: name, text: msg.Text}
	waiter := make(chan error, 1)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return provider.Ack{}, fmt.Errorf("batcher is closed")
	}
	p := b.groups[k]
	if p == nil {
		p = &pending{id: b.sequence.Add(1), created: time.Now()}
		b.workers.Add(1)
		b.groups[k] = p
		p.timer = time.AfterFunc(b.wait, func() { b.flush(k, p) })
	}
	p.to = append(p.to, msg.To)
	if b.ackMode == "sent" {
		p.waiters = append(p.waiters, waiter)
	}
	count := len(p.to)
	b.mu.Unlock()
	slog.Info("SMS queued", "batch_id", p.id, "instance", name, "to", msg.To, "recipients", count, "content_hash", hash(msg.Text))
	if b.ackMode != "sent" {
		return provider.Ack{Queued: true}, nil
	}
	select {
	case err := <-waiter:
		return provider.Ack{}, err
	case <-ctx.Done():
		return provider.Ack{}, ctx.Err()
	case <-b.ctx.Done():
		return provider.Ack{}, b.ctx.Err()
	}
}

func (b *Batcher) flush(k key, p *pending) {
	b.mu.Lock()
	if b.groups[k] != p {
		b.mu.Unlock()
		return
	}
	delete(b.groups, k)
	b.mu.Unlock()
	b.deliver(b.ctx, k, p)
}

func (b *Batcher) deliver(ctx context.Context, k key, p *pending) {
	defer b.workers.Done()
	to := p.to
	slog.Info("SMS batch submitting", "batch_id", p.id, "instance", k.instance, "recipients", len(to), "wait_ms", time.Since(p.created).Milliseconds(), "content_hash", hash(k.text))
	start := time.Now()
	results := b.instances[k.instance].SendBatch(ctx, provider.Batch{To: to, Text: k.text})
	if len(results) != len(to) {
		results = make([]error, len(to))
		for i := range results {
			results[i] = fmt.Errorf("provider returned wrong number of batch results")
		}
	}
	failed := 0
	for i, result := range results {
		b.metrics.RecordDelivery(k.instance, result)
		if result != nil {
			failed++
			slog.Error("SMS recipient delivery failed", "batch_id", p.id, "instance", k.instance, "to", to[i], "error", result)
		}
	}
	b.metrics.RecordBatch(k.instance, len(to), failed, time.Since(start))
	if failed > 0 {
		slog.Error("SMS batch completed with failures", "batch_id", p.id, "instance", k.instance, "recipients", len(to), "failed", failed, "duration_ms", time.Since(start).Milliseconds())
	} else {
		slog.Info("SMS batch accepted", "batch_id", p.id, "instance", k.instance, "recipients", len(to), "duration_ms", time.Since(start).Milliseconds())
	}
	for i, waiter := range p.waiters {
		waiter <- results[i]
	}
}

func (b *Batcher) Drain(ctx context.Context) error {
	b.mu.Lock()
	b.closed = true
	groups := b.groups
	b.groups = make(map[key]*pending)
	b.mu.Unlock()
	for k, p := range groups {
		p.timer.Stop()
		go b.deliver(ctx, k, p)
	}
	done := make(chan struct{})
	go func() { b.workers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Batcher) Close() { _ = b.Drain(context.Background()) }

func hash(text string) string { sum := sha256.Sum256([]byte(text)); return fmt.Sprintf("%x", sum[:6]) }
