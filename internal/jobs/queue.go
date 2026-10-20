package jobs

import (
	"context"
	"log"
	"sync"
	"time"

	"sorter/internal/extractor"
	"sorter/internal/invoice"
	"sorter/internal/store"
)

// Queue is an in-process worker pool that runs LLM extraction for
// uploaded invoices in the background.
type Queue struct {
	store   *store.Store
	service *extractor.Service
	ch      chan int64
	wg      sync.WaitGroup
}

// NewQueue creates a queue with the given worker count.
func NewQueue(st *store.Store, svc *extractor.Service, workers int) *Queue {
	if workers < 1 {
		workers = 1
	}
	q := &Queue{store: st, service: svc, ch: make(chan int64, 256)}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	return q
}

// Stop drains the channel and waits for workers to finish in-flight jobs.
func (q *Queue) Stop() {
	close(q.ch)
	q.wg.Wait()
}

// Enqueue schedules an invoice for extraction.
func (q *Queue) Enqueue(id int64) {
	select {
	case q.ch <- id:
	default:
		// Channel full: hand off to a helper goroutine so the HTTP handler
		// never blocks; the record stays in "processing" either way.
		go func() { q.ch <- id }()
	}
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for id := range q.ch {
		q.process(id)
	}
}

func (q *Queue) process(id int64) {
	inv, err := q.store.GetInvoice(id)
	if err != nil {
		log.Printf("job: load invoice %d: %v", id, err)
		return
	}
	if inv == nil || inv.Status != invoice.StatusProcessing {
		return // deleted or already handled
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	result, raw, err := q.service.ExtractFromFile(ctx, inv.FilePath)
	if err != nil {
		log.Printf("job: extract invoice %d failed: %v", id, err)
		if e := q.store.FailExtraction(id, err.Error()); e != nil {
			log.Printf("job: mark invoice %d failed: %v", id, e)
		}
		return
	}

	done := &invoice.Invoice{
		InvoiceNumber: result.InvoiceNumber,
		InvoiceDate:   result.InvoiceDate,
		AmountCents:   invoice.YuanToCents(result.TotalAmount),
		Currency:      result.Currency,
		SellerName:    result.SellerName,
		BuyerName:     result.BuyerName,
		Drawer:        result.Drawer,
		Category:      result.Category,
		RawResponse:   raw,
	}
	if err := q.store.CompleteExtraction(id, done); err != nil {
		log.Printf("job: save extraction for invoice %d: %v", id, err)
		return
	}
	log.Printf("job: invoice %d extracted (category=%s amount=%.2f %s)", id, done.Category, float64(done.AmountCents)/100, done.Currency)
}
