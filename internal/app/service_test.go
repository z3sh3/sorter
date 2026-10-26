package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sorter/internal/invoice"
)

type fakeRepo struct {
	created   []*invoice.Invoice
	get       *invoice.Invoice
	saveErr   error
	createErr error
	deleted   bool
}

func (r *fakeRepo) CreateInvoice(_ context.Context, inv *invoice.Invoice) (int64, error) {
	if r.createErr != nil {
		return 0, r.createErr
	}
	r.created = append(r.created, inv)
	return int64(len(r.created)), nil
}
func (r *fakeRepo) GetInvoice(context.Context, int64) (*invoice.Invoice, error) {
	if r.get == nil {
		return nil, invoice.ErrNotFound
	}
	return r.get, nil
}
func (r *fakeRepo) SaveInvoice(_ context.Context, inv *invoice.Invoice) error {
	r.get = inv
	return r.saveErr
}
func (r *fakeRepo) MarkProcessing(context.Context, int64, int64) error { return nil }
func (r *fakeRepo) DeleteInvoice(context.Context, int64, int64) error  { r.deleted = true; return nil }

type fakeFiles struct {
	saved   []string
	removed []string
	err     error
}

func (f *fakeFiles) Save(_ context.Context, _ string, _ io.Reader) (string, string, error) {
	if f.err != nil {
		return "", "", f.err
	}
	path := "/uploads/unique-" + string(rune('a'+len(f.saved)))
	f.saved = append(f.saved, path)
	return path, "hash", nil
}
func (f *fakeFiles) Remove(path string) error { f.removed = append(f.removed, path); return nil }

type fakeQueue struct{ notified int }

func (q *fakeQueue) Notify() { q.notified++ }

// A failed insert must delete only the file this request created.
func TestImportCleansUpOwnFileOnDuplicate(t *testing.T) {
	files := &fakeFiles{}
	repo := &fakeRepo{createErr: invoice.ErrDuplicate}
	queue := &fakeQueue{}
	svc := New(repo, files, queue)

	if _, err := svc.Import(context.Background(), "a.png", strings.NewReader("x")); !errors.Is(err, invoice.ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
	if len(files.removed) != 1 || files.removed[0] != files.saved[0] {
		t.Fatalf("removed = %v, saved = %v", files.removed, files.saved)
	}
	if queue.notified != 0 {
		t.Fatal("duplicate import must not notify the queue")
	}
}

func TestImportNotifiesQueue(t *testing.T) {
	queue := &fakeQueue{}
	svc := New(&fakeRepo{}, &fakeFiles{}, queue)
	id, err := svc.Import(context.Background(), "a.png", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if id != 1 || queue.notified != 1 {
		t.Fatalf("id = %d, notified = %d", id, queue.notified)
	}
}

func TestUpdateMapsValidationErrors(t *testing.T) {
	repo := &fakeRepo{get: &invoice.Invoice{
		ID: 1, Version: 1, Status: invoice.StatusNeedsReview, Currency: "CNY",
		Category: invoice.CategoryOther, InvoiceDate: "2025-01-01",
	}}
	svc := New(repo, &fakeFiles{}, &fakeQueue{})

	badDate := "2025-13-01"
	_, err := svc.Update(context.Background(), 1, invoice.Update{Version: 1, InvoiceDate: &badDate})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	_, err = svc.Update(context.Background(), 1, invoice.Update{Version: 9})
	if !errors.Is(err, invoice.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestUpdatePersistsConfirmedInvoice(t *testing.T) {
	repo := &fakeRepo{get: &invoice.Invoice{
		ID: 1, Version: 1, Status: invoice.StatusNeedsReview, Currency: "CNY",
		Category: invoice.CategoryOther, InvoiceDate: "2025-01-01",
		InvoiceNumber: "N1", SellerName: "ACME",
	}}
	svc := New(repo, &fakeFiles{}, &fakeQueue{})
	amount := invoice.Money(1234)

	updated, err := svc.Update(context.Background(), 1, invoice.Update{Version: 1, Amount: &amount, Confirm: true})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != invoice.StatusDone || updated.AmountCents != 1234 {
		t.Fatalf("updated = status %s amount %d", updated.Status, updated.AmountCents)
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	repo := &fakeRepo{get: &invoice.Invoice{ID: 1, Version: 2, FilePath: "/uploads/one.png"}}
	files := &fakeFiles{}
	svc := New(repo, files, &fakeQueue{})

	if err := svc.Delete(context.Background(), 1, 2); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !repo.deleted {
		t.Fatal("record was not deleted")
	}
	if len(files.removed) != 1 || files.removed[0] != "/uploads/one.png" {
		t.Fatalf("removed = %v", files.removed)
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func newFiles(t *testing.T) *LocalFiles {
	t.Helper()
	files, err := NewLocalFiles(filepath.Join(t.TempDir(), "uploads"))
	if err != nil {
		t.Fatalf("new local files: %v", err)
	}
	return files
}

// Two uploads of identical content must never share a path: deleting one
// request must not be able to remove another request's stored file.
func TestSaveAssignsUniquePaths(t *testing.T) {
	files := newFiles(t)
	data := tinyPNG(t)

	first, firstHash, err := files.Save(context.Background(), "a.png", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("save first: %v", err)
	}
	second, secondHash, err := files.Save(context.Background(), "b.png", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("save second: %v", err)
	}
	if first == second {
		t.Fatalf("both uploads stored at %q", first)
	}
	if firstHash != secondHash {
		t.Fatalf("hashes differ: %q vs %q", firstHash, secondHash)
	}
	if err := files.Remove(first); err != nil {
		t.Fatalf("remove first: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("second file removed by first cleanup: %v", err)
	}
}

func TestSaveRejectsContentTypeMismatch(t *testing.T) {
	files := newFiles(t)
	if _, _, err := files.Save(context.Background(), "fake.png", strings.NewReader("not an image")); !errors.Is(err, ErrInvalidFile) {
		t.Fatalf("err = %v, want ErrInvalidFile", err)
	}
	entries, err := os.ReadDir(files.Dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected upload left %d files behind", len(entries))
	}
}

func TestSaveRejectsUnsupportedExtension(t *testing.T) {
	files := newFiles(t)
	if _, _, err := files.Save(context.Background(), "invoice.exe", strings.NewReader("x")); !errors.Is(err, ErrInvalidFile) {
		t.Fatalf("err = %v, want ErrInvalidFile", err)
	}
}

func TestRemoveRefusesPathsOutsideUploadDir(t *testing.T) {
	files := newFiles(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := files.Remove(outside); err == nil {
		t.Fatal("expected removal outside the upload directory to be refused")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("file was removed: %v", err)
	}
}
