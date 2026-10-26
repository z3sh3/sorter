package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sorter/internal/invoice"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return st
}

func mustCreate(t *testing.T, st *Store, hash string) int64 {
	t.Helper()
	id, err := st.CreateInvoice(context.Background(), &invoice.Invoice{
		FileName: "invoice.png",
		FilePath: "/tmp/invoice.png",
		FileHash: hash,
	})
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}
	return id
}

func mustClaim(t *testing.T, st *Store) *invoice.Invoice {
	t.Helper()
	inv, err := st.ClaimNext(context.Background())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	return inv
}

func complete(t *testing.T, st *Store, inv *invoice.Invoice, done *invoice.Invoice) {
	t.Helper()
	if err := st.CompleteExtraction(context.Background(), inv.ID, inv.ExtractionToken, done); err != nil {
		t.Fatalf("complete: %v", err)
	}
}

func TestCreateInvoiceRejectsDuplicateHash(t *testing.T) {
	st := newTestStore(t)
	mustCreate(t, st, "hash-1")
	_, err := st.CreateInvoice(context.Background(), &invoice.Invoice{
		FileName: "dup.png", FilePath: "/tmp/dup.png", FileHash: "hash-1",
	})
	if !errors.Is(err, invoice.ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
}

// A claimed job must not be handed to a second worker.
func TestClaimIsExclusive(t *testing.T) {
	st := newTestStore(t)
	id := mustCreate(t, st, "hash-1")

	claimed := mustClaim(t, st)
	if claimed.ID != id {
		t.Fatalf("claimed id = %d, want %d", claimed.ID, id)
	}
	if claimed.ExtractionToken == "" {
		t.Fatal("claim did not set an extraction token")
	}
	if _, err := st.ClaimNext(context.Background()); !errors.Is(err, invoice.ErrNotFound) {
		t.Fatalf("second claim err = %v, want ErrNotFound", err)
	}

	// Releasing makes the row claimable again (used when work is interrupted).
	if err := st.Release(context.Background(), claimed.ID, claimed.ExtractionToken); err != nil {
		t.Fatalf("release: %v", err)
	}
	if again := mustClaim(t, st); again.ID != id {
		t.Fatalf("re-claimed id = %d, want %d", again.ID, id)
	}
}

// A late result from a superseded attempt must not overwrite newer state.
func TestCompleteRejectsStaleToken(t *testing.T) {
	st := newTestStore(t)
	mustCreate(t, st, "hash-1")
	claimed := mustClaim(t, st)

	if err := st.Release(context.Background(), claimed.ID, claimed.ExtractionToken); err != nil {
		t.Fatalf("release: %v", err)
	}
	fresh := mustClaim(t, st)

	err := st.CompleteExtraction(context.Background(), claimed.ID, claimed.ExtractionToken, &invoice.Invoice{
		Category: invoice.CategoryOther, Status: invoice.StatusDone,
	})
	if !errors.Is(err, invoice.ErrConflict) {
		t.Fatalf("stale token err = %v, want ErrConflict", err)
	}

	complete(t, st, fresh, &invoice.Invoice{
		InvoiceDate: "2025-03-01", AmountCents: 4200, Currency: "CNY",
		Category: invoice.CategoryDining, Status: invoice.StatusDone,
	})
	saved, err := st.GetInvoice(context.Background(), claimed.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if saved.Status != invoice.StatusDone || saved.AmountCents != 4200 {
		t.Fatalf("saved = status %s amount %d", saved.Status, saved.AmountCents)
	}
	if saved.ExtractionToken != "" {
		t.Fatal("completion must clear the extraction token")
	}
	if saved.Version != claimed.Version+1 {
		t.Fatalf("version = %d, want %d", saved.Version, claimed.Version+1)
	}
}

func TestMarkProcessingRequiresCurrentVersion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id := mustCreate(t, st, "hash-1")
	claimed := mustClaim(t, st)
	complete(t, st, claimed, &invoice.Invoice{
		InvoiceDate: "2025-01-01", Currency: "CNY", Category: invoice.CategoryOther,
		Status: invoice.StatusDone,
	})
	inv, err := st.GetInvoice(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if err := st.MarkProcessing(ctx, id, inv.Version+5); !errors.Is(err, invoice.ErrConflict) {
		t.Fatalf("stale version err = %v, want ErrConflict", err)
	}
	if err := st.MarkProcessing(ctx, id, inv.Version); err != nil {
		t.Fatalf("mark processing: %v", err)
	}
	// A row already being processed must not be duplicated into the queue.
	if err := st.MarkProcessing(ctx, id, inv.Version); !errors.Is(err, invoice.ErrConflict) {
		t.Fatalf("processing row err = %v, want ErrConflict", err)
	}
}

func TestDeleteRequiresVersion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id := mustCreate(t, st, "hash-1")
	claimed := mustClaim(t, st)
	complete(t, st, claimed, &invoice.Invoice{
		InvoiceDate: "2025-01-01", Currency: "CNY", Category: invoice.CategoryOther,
		Status: invoice.StatusDone,
	})

	if err := st.DeleteInvoice(ctx, id, 99); !errors.Is(err, invoice.ErrConflict) {
		t.Fatalf("delete with wrong version err = %v, want ErrConflict", err)
	}
	if err := st.DeleteInvoice(ctx, id, claimed.Version+1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetInvoice(ctx, id); !errors.Is(err, invoice.ErrNotFound) {
		t.Fatalf("get after delete err = %v, want ErrNotFound", err)
	}
}

// Deleting an in-flight row is rejected so the worker cannot resurrect it.
func TestDeleteRejectsProcessingInvoice(t *testing.T) {
	st := newTestStore(t)
	id := mustCreate(t, st, "hash-1")
	if err := st.DeleteInvoice(context.Background(), id, 1); !errors.Is(err, invoice.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestStatsAndSummaryKeepCurrenciesSeparate(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	addDone := func(hash, currency, date string, amount int64, status string) {
		mustCreate(t, st, hash)
		claimed := mustClaim(t, st)
		complete(t, st, claimed, &invoice.Invoice{
			InvoiceDate: date, AmountCents: amount, Currency: currency,
			SellerName: "ACME", Category: invoice.CategoryTransport, Status: status,
		})
	}
	addDone("h1", "CNY", "2025-01-10", 10000, invoice.StatusDone)
	addDone("h2", "CNY", "2025-02-10", 5000, invoice.StatusDone)
	addDone("h3", "USD", "2025-01-11", 7000, invoice.StatusDone)
	addDone("h4", "CNY", "2025-01-12", 999, invoice.StatusNeedsReview)

	cn, err := st.Stats(ctx, "month", "", "", "CNY")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(cn) != 2 {
		t.Fatalf("CNY buckets = %d, want 2", len(cn))
	}
	var total int64
	for _, row := range cn {
		total += row.TotalCents
	}
	if total != 15000 {
		t.Fatalf("CNY total = %d, want 15000 (needs_review excluded)", total)
	}

	usd, err := st.Stats(ctx, "month", "", "", "USD")
	if err != nil {
		t.Fatalf("stats usd: %v", err)
	}
	if len(usd) != 1 || usd[0].TotalCents != 7000 {
		t.Fatalf("USD stats = %+v", usd)
	}

	summary, err := st.Summary(ctx)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	byCurrency := map[string]CurrencySummary{}
	for _, item := range summary {
		byCurrency[item.Currency] = item
	}
	if got := byCurrency["CNY"]; got.TotalAmount != 150 || got.TotalCount != 2 || got.ReviewCount != 1 {
		t.Fatalf("CNY summary = %+v", got)
	}
	if got := byCurrency["USD"]; got.TotalAmount != 70 || got.TotalCount != 1 {
		t.Fatalf("USD summary = %+v", got)
	}

	if _, err := st.Stats(ctx, "month", "", "", "XXX"); err == nil {
		t.Fatal("expected unsupported currency to be rejected")
	}
	if _, err := st.Stats(ctx, "week", "", "", "CNY"); err == nil {
		t.Fatal("expected invalid group_by to be rejected")
	}
}

func TestListFilterValidation(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if _, _, err := st.ListInvoices(ctx, ListFilter{Category: "nope"}); err == nil {
		t.Fatal("expected invalid category to be rejected")
	}
	if _, _, err := st.ListInvoices(ctx, ListFilter{Status: "nope"}); err == nil {
		t.Fatal("expected invalid status to be rejected")
	}
	if _, _, err := st.ListInvoices(ctx, ListFilter{StartDate: "2025-05-01", EndDate: "2025-01-01"}); err == nil {
		t.Fatal("expected inverted date range to be rejected")
	}

	items, total, err := st.ListInvoices(ctx, ListFilter{Page: 1, PageSize: 5})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 || total != 0 {
		t.Fatalf("empty list = %d items, total %d", len(items), total)
	}
}

func TestRecoverPendingReleasesInterruptedWork(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id := mustCreate(t, st, "hash-1")
	claimed := mustClaim(t, st)

	if err := st.RecoverPending(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	recovered := mustClaim(t, st)
	if recovered.ID != id {
		t.Fatalf("recovered id = %d, want %d", recovered.ID, id)
	}
	if recovered.Version != claimed.Version+1 {
		t.Fatalf("version = %d, want %d", recovered.Version, claimed.Version+1)
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for i := 0; i < 2; i++ {
		st, err := Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("close #%d: %v", i, err)
		}
	}
}
