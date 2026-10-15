package batch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/opsbl/virtualsms/internal/metrics"
	"github.com/opsbl/virtualsms/internal/provider"
)

type captureProvider struct {
	calls    chan provider.Batch
	err      error
	outcomes map[string]error
}

func (p captureProvider) SendBatch(_ context.Context, b provider.Batch) []error {
	p.calls <- b
	results := make([]error, len(b.To))
	for i, to := range b.To {
		results[i] = p.err
		if p.outcomes != nil {
			results[i] = p.outcomes[to]
		}
	}
	return results
}

func TestPartialFailureReturnsPerRecipientResult(t *testing.T) {
	p := captureProvider{calls: make(chan provider.Batch, 1), outcomes: map[string]error{"222": errors.New("rejected")}}
	b := New(context.Background(), map[string]provider.Provider{"p": p}, 50*time.Millisecond, "sent", metrics.New())
	defer b.Close()
	type result struct {
		to  string
		err error
	}
	results := make(chan result, 2)
	for _, to := range []string{"111", "222"} {
		go func() {
			_, err := b.For("p").Send(context.Background(), provider.Message{To: to, Text: "same"})
			results <- result{to, err}
		}()
	}
	for i := 0; i < 2; i++ {
		r := <-results
		if r.to == "111" && r.err != nil {
			t.Errorf("111 should succeed: %v", r.err)
		}
		if r.to == "222" && (r.err == nil || r.err.Error() != "rejected") {
			t.Errorf("222 should fail: %v", r.err)
		}
	}
	if len((<-p.calls).To) != 2 {
		t.Fatal("requests were not batched")
	}
}

func TestQueuedModeAllowsSequentialSubmissions(t *testing.T) {
	p := captureProvider{calls: make(chan provider.Batch, 1)}
	b := New(context.Background(), map[string]provider.Provider{"p": p}, 80*time.Millisecond, "queued", metrics.New())
	defer b.Close()
	for _, to := range []string{"111", "222", "333"} {
		ack, err := b.For("p").Send(context.Background(), provider.Message{To: to, Text: "same"})
		if err != nil || !ack.Queued {
			t.Fatalf("send %s: ack=%+v err=%v", to, ack, err)
		}
	}
	select {
	case sent := <-p.calls:
		if len(sent.To) != 3 {
			t.Fatalf("batch has %d recipients, want 3", len(sent.To))
		}
	case <-time.After(time.Second):
		t.Fatal("batch was not submitted")
	}
}

func TestDrainSubmitsPendingBatch(t *testing.T) {
	p := captureProvider{calls: make(chan provider.Batch, 1)}
	b := New(context.Background(), map[string]provider.Provider{"p": p}, time.Hour, "queued", metrics.New())
	if _, err := b.For("p").Send(context.Background(), provider.Message{To: "111", Text: "pending"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case sent := <-p.calls:
		if len(sent.To) != 1 || sent.To[0] != "111" {
			t.Fatalf("unexpected batch: %+v", sent)
		}
	default:
		t.Fatal("pending batch was lost during drain")
	}
}

func TestGroupsSameTextByInstance(t *testing.T) {
	first := captureProvider{calls: make(chan provider.Batch, 3)}
	second := captureProvider{calls: make(chan provider.Batch, 1)}
	b := New(context.Background(), map[string]provider.Provider{"first": first, "second": second}, 80*time.Millisecond, "sent", metrics.New())
	defer b.Close()
	var wg sync.WaitGroup
	for _, item := range []struct{ name, to, text string }{
		{"first", "111", "same"}, {"first", "222", "same"}, {"first", "333", "same"},
		{"first", "444", "different"}, {"second", "555", "same"},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := b.For(item.name).Send(context.Background(), provider.Message{To: item.to, Text: item.text}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(first.calls) != 2 || len(second.calls) != 1 {
		t.Fatalf("batch counts: first=%d second=%d", len(first.calls), len(second.calls))
	}
	var same provider.Batch
	for i := 0; i < 2; i++ {
		batch := <-first.calls
		if batch.Text == "same" {
			same = batch
		}
	}
	if len(same.To) != 3 {
		t.Fatalf("same-text batch has %d recipients, want 3", len(same.To))
	}
	if (<-second.calls).To[0] != "555" {
		t.Fatal("cross-provider merge")
	}
}

func TestBatchFailureReachesEverySubmitter(t *testing.T) {
	p := captureProvider{calls: make(chan provider.Batch, 1), err: errors.New("gateway failed")}
	b := New(context.Background(), map[string]provider.Provider{"p": p}, 50*time.Millisecond, "sent", metrics.New())
	defer b.Close()
	results := make(chan error, 2)
	for _, to := range []string{"111", "222"} {
		go func() {
			_, err := b.For("p").Send(context.Background(), provider.Message{To: to, Text: "same"})
			results <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil || err.Error() != "gateway failed" {
			t.Fatalf("unexpected result: %v", err)
		}
	}
	if len((<-p.calls).To) != 2 {
		t.Fatal("requests were not batched")
	}
}
