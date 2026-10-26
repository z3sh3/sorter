package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"sorter/internal/extractor"
	"sorter/internal/invoice"
)

// fakeRepo models the durable queue: one claimable row at a time.
type fakeRepo struct {
	mu             sync.Mutex
	pending        *invoice.Invoice
	claimed        *invoice.Invoice
	completed      *invoice.Invoice
	completedToken string
	failed         string
	released       int
}

func (r *fakeRepo) ClaimNext(context.Context) (*invoice.Invoice, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil || r.claimed != nil {
		return nil, invoice.ErrNotFound
	}
	claimed := *r.pending
	claimed.ExtractionToken = "token"
	r.claimed = &claimed
	r.pending = nil
	return &claimed, nil
}
func (r *fakeRepo) Release(_ context.Context, _ int64, token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimed != nil && r.claimed.ExtractionToken == token {
		r.pending = r.claimed
		r.claimed = nil
		r.released++
	}
	return nil
}
func (r *fakeRepo) CompleteExtraction(_ context.Context, _ int64, token string, inv *invoice.Invoice) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completed = inv
	r.completedToken = token
	r.claimed = nil
	return nil
}
func (r *fakeRepo) FailExtraction(_ context.Context, _ int64, _ string, message, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = message
	r.claimed = nil
	return nil
}

// snapshot reads worker-written fields under the same mutex.
func (r *fakeRepo) snapshot() (completed *invoice.Invoice, token, failed string, released int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.completed, r.completedToken, r.failed, r.released
}

type fakeExtractor struct {
	result *extractor.Result
	err    error
	block  chan struct{}
}

func (f *fakeExtractor) ExtractFromFile(ctx context.Context, _ string) (*extractor.Result, string, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	if f.err != nil {
		return nil, "raw", f.err
	}
	return f.result, "raw", nil
}

func pendingInvoice() *invoice.Invoice {
	return &invoice.Invoice{
		ID: 7, FilePath: "/uploads/one.png", Status: invoice.StatusProcessing, Version: 1,
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}

func TestQueueCompletesClaimedJob(t *testing.T) {
	repo := &fakeRepo{pending: pendingInvoice()}
	amount := invoice.Money(1990)
	svc := &fakeExtractor{result: &extractor.Result{
		InvoiceDate: "2025-02-01", Currency: "CNY", Category: invoice.CategoryDining,
		SellerName: "ACME", InvoiceNumber: "N-1", Confidence: 0.95, TotalAmount: &amount,
	}}
	q := NewQueue(repo, svc, 1)
	t.Cleanup(func() {
		if err := q.Stop(context.Background()); err != nil {
			t.Errorf("stop: %v", err)
		}
	})

	waitFor(t, func() bool {
		completed, token, _, _ := repo.snapshot()
		return completed != nil && token == "token"
	})
	completed, _, _, _ := repo.snapshot()
	if completed.Status != invoice.StatusDone {
		t.Fatalf("completed = %+v", completed)
	}
	if completed.AmountCents != 1990 {
		t.Fatalf("amount = %d, want 1990", completed.AmountCents)
	}
	if completed.ReviewReason != "" {
		t.Fatalf("unexpected review reason: %q", completed.ReviewReason)
	}
}

func TestQueueFlagsLowConfidenceForReview(t *testing.T) {
	repo := &fakeRepo{pending: pendingInvoice()}
	amount := invoice.Money(1990)
	svc := &fakeExtractor{result: &extractor.Result{
		InvoiceDate: "2025-02-01", Currency: "CNY", Category: invoice.CategoryDining,
		SellerName: "ACME", InvoiceNumber: "N-1", Confidence: 0.3, TotalAmount: &amount,
	}}
	q := NewQueue(repo, svc, 1)
	t.Cleanup(func() {
		if err := q.Stop(context.Background()); err != nil {
			t.Errorf("stop: %v", err)
		}
	})

	waitFor(t, func() bool {
		completed, _, _, _ := repo.snapshot()
		return completed != nil
	})
	completed, _, _, _ := repo.snapshot()
	if completed.Status != invoice.StatusNeedsReview {
		t.Fatalf("status = %s, want needs_review", completed.Status)
	}
	if completed.ReviewReason == "" {
		t.Fatal("missing review reason")
	}
}

func TestQueueMarksFailure(t *testing.T) {
	repo := &fakeRepo{pending: pendingInvoice()}
	svc := &fakeExtractor{err: errors.New("llm down")}
	q := NewQueue(repo, svc, 1)
	t.Cleanup(func() {
		if err := q.Stop(context.Background()); err != nil {
			t.Errorf("stop: %v", err)
		}
	})

	waitFor(t, func() bool {
		_, _, failed, _ := repo.snapshot()
		return failed != ""
	})
	completed, _, _, _ := repo.snapshot()
	if completed != nil {
		t.Fatal("failed job must not complete")
	}
}

// A stopping queue must not strand the row: it has to be released back.
func TestQueueReleasesJobWhenStopped(t *testing.T) {
	block := make(chan struct{})
	repo := &fakeRepo{pending: pendingInvoice()}
	svc := &fakeExtractor{block: block}
	q := NewQueue(repo, svc, 1)

	waitFor(t, func() bool { return len(block) == 0 && repo.claimedJob() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := q.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if repo.released == 0 {
		t.Fatal("interrupted job was not released")
	}
}

// claimedJob reports whether a worker has taken the pending row.
func (r *fakeRepo) claimedJob() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.claimed != nil
}

func TestStopIsIdempotent(t *testing.T) {
	q := NewQueue(&fakeRepo{}, &fakeExtractor{result: &extractor.Result{}}, 1)
	if err := q.Stop(context.Background()); err != nil {
		t.Fatalf("first stop: %v", err)
	}
	if err := q.Stop(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestNotifyNeverBlocks(t *testing.T) {
	q := NewQueue(&fakeRepo{}, &fakeExtractor{result: &extractor.Result{}}, 1)
	t.Cleanup(func() {
		if err := q.Stop(context.Background()); err != nil {
			t.Errorf("stop: %v", err)
		}
	})
	for i := 0; i < 1000; i++ {
		q.Notify()
	}
}
