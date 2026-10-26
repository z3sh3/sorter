package jobs

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"sorter/internal/extractor"
	"sorter/internal/invoice"
)

// Repository owns durable scheduling and conditional task completion.
type Repository interface {
	ClaimNext(context.Context) (*invoice.Invoice, error)
	Release(context.Context, int64, string) error
	CompleteExtraction(context.Context, int64, string, *invoice.Invoice) error
	FailExtraction(context.Context, int64, string, string, string) error
}
type Extractor interface {
	ExtractFromFile(context.Context, string) (*extractor.Result, string, error)
}

// Queue uses SQLite as the durable queue. Notifications are coalesced, never closed.
type Queue struct {
	store   Repository
	service Extractor
	wake    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	done    chan struct{}
}

func NewQueue(st Repository, svc Extractor, workers int) *Queue {
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{store: st, service: svc, wake: make(chan struct{}, workers), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	for range workers {
		q.wg.Add(1)
		go q.worker()
	}
	go func() { q.wg.Wait(); close(q.done) }()
	return q
}
func (q *Queue) Notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
func (q *Queue) Stop(ctx context.Context) error {
	q.cancel()
	select {
	case <-q.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (q *Queue) worker() {
	defer q.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if q.ctx.Err() != nil {
			return
		}
		inv, err := q.store.ClaimNext(q.ctx)
		if err == nil {
			q.process(inv)
			continue
		}
		if !errors.Is(err, invoice.ErrNotFound) && q.ctx.Err() == nil {
			log.Printf("job claim failed: %v", err)
		}
		select {
		case <-q.ctx.Done():
			return
		case <-q.wake:
		case <-ticker.C:
		}
	}
}
func (q *Queue) process(inv *invoice.Invoice) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(q.ctx, 3*time.Minute)
	result, raw, err := q.service.ExtractFromFile(ctx, inv.FilePath)
	cancel()
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer saveCancel()
	// A canceled process or failed database write must not strand the durable job.
	defer func() {
		if e := q.store.Release(saveCtx, inv.ID, inv.ExtractionToken); e != nil {
			log.Printf("job release id=%d: %v", inv.ID, e)
		}
	}()
	if q.ctx.Err() != nil {
		return
	}
	if err != nil {
		log.Printf("job failed id=%d duration=%s: %v", inv.ID, time.Since(started), err)
		if e := q.store.FailExtraction(saveCtx, inv.ID, inv.ExtractionToken, "Extraction failed; retry or enter the invoice manually.", raw); e != nil {
			log.Printf("job save failure id=%d: %v", inv.ID, e)
		}
		return
	}
	done := result.Invoice(raw)
	if err := q.store.CompleteExtraction(saveCtx, inv.ID, inv.ExtractionToken, done); err != nil {
		log.Printf("job save id=%d: %v", inv.ID, err)
		return
	}
	log.Printf("job completed id=%d status=%s duration=%s", inv.ID, done.Status, time.Since(started))
}
