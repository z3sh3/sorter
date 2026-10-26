package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
	"sorter/internal/invoice"
)

var ErrInvalidFile = errors.New("invalid file")
var ErrValidation = errors.New("invalid invoice")

func invalidFile(message string) error { return fmt.Errorf("%w: %s", ErrInvalidFile, message) }

// Repository is the persistence boundary used by invoice commands.
type Repository interface {
	CreateInvoice(context.Context, *invoice.Invoice) (int64, error)
	GetInvoice(context.Context, int64) (*invoice.Invoice, error)
	SaveInvoice(context.Context, *invoice.Invoice) error
	MarkProcessing(context.Context, int64, int64) error
	DeleteInvoice(context.Context, int64, int64) error
}
type Notifier interface{ Notify() }
type Files interface {
	Save(context.Context, string, io.Reader) (string, string, error)
	Remove(string) error
}
type Service struct {
	store Repository
	files Files
	queue Notifier
}

func New(st Repository, files Files, queue Notifier) *Service {
	return &Service{store: st, files: files, queue: queue}
}

func (s *Service) Import(ctx context.Context, name string, reader io.Reader) (int64, error) {
	path, hash, err := s.files.Save(ctx, name, reader)
	if err != nil {
		return 0, err
	}
	id, err := s.store.CreateInvoice(ctx, &invoice.Invoice{FileName: name, FilePath: path, FileHash: hash})
	if err != nil {
		// Save always returns a request-owned path, never a content-addressed shared path.
		if cleanupErr := s.files.Remove(path); cleanupErr != nil {
			log.Printf("upload cleanup failed: %v", cleanupErr)
		}
		return 0, err
	}
	s.queue.Notify()
	return id, nil
}
func (s *Service) Update(ctx context.Context, id int64, patch invoice.Update) (*invoice.Invoice, error) {
	inv, err := s.store.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if err = patch.Apply(inv); err != nil {
		if errors.Is(err, invoice.ErrConflict) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if err = s.store.SaveInvoice(ctx, inv); err != nil {
		return nil, err
	}
	return s.store.GetInvoice(ctx, id)
}
func (s *Service) ReExtract(ctx context.Context, id, version int64) error {
	if err := s.store.MarkProcessing(ctx, id, version); err != nil {
		return err
	}
	s.queue.Notify()
	return nil
}
func (s *Service) Delete(ctx context.Context, id, version int64) error {
	inv, err := s.store.GetInvoice(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.DeleteInvoice(ctx, id, version); err != nil {
		return err
	}
	if err := s.files.Remove(inv.FilePath); err != nil {
		log.Printf("deleted invoice %d has orphan file: %v", id, err)
	}
	return nil
}

var AllowedExtensions = map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp", ".pdf": "application/pdf"}

type LocalFiles struct{ Dir string }

// NewLocalFiles resolves paths once so future working-directory changes are harmless.
func NewLocalFiles(dir string) (*LocalFiles, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &LocalFiles{Dir: dir}, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func (f *LocalFiles) Save(ctx context.Context, name string, reader io.Reader) (path, hash string, err error) {
	ext := strings.ToLower(filepath.Ext(name))
	expected, ok := AllowedExtensions[ext]
	if !ok {
		return "", "", invalidFile("unsupported file type")
	}
	if len(name) > 255 {
		return "", "", invalidFile("file name exceeds 255 bytes")
	}
	file, err := os.CreateTemp(f.Dir, "upload-*"+ext)
	if err != nil {
		return "", "", err
	}
	path = file.Name()
	defer func() {
		file.Close()
		if err != nil {
			os.Remove(file.Name())
		}
	}()
	digest := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(contextReader{ctx, reader}, invoice.MaxFileBytes+1))
	if err != nil {
		return "", "", err
	}
	if n == 0 || n > invoice.MaxFileBytes {
		return "", "", invalidFile("file must contain 1 byte to 32 MiB")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return "", "", err
	}
	header := make([]byte, 512)
	count, readErr := file.Read(header)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return "", "", readErr
	}
	actual := http.DetectContentType(header[:count])
	if actual != expected {
		return "", "", invalidFile("file contents do not match extension")
	}
	if ext != ".pdf" {
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			return "", "", err
		}
		cfg, _, decodeErr := image.DecodeConfig(file)
		if decodeErr != nil {
			return "", "", invalidFile("cannot decode image header")
		}
		if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 10000 || cfg.Height > 10000 || int64(cfg.Width)*int64(cfg.Height) > 25_000_000 {
			return "", "", invalidFile("image dimensions exceed limit")
		}
	}
	if err = file.Sync(); err != nil {
		return "", "", err
	}
	if err = file.Close(); err != nil {
		return "", "", err
	}
	return path, hex.EncodeToString(digest.Sum(nil)), nil
}
func (f *LocalFiles) Remove(path string) error {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if filepath.Dir(resolved) != f.Dir {
		return fmt.Errorf("refusing to remove file outside upload directory")
	}
	err = os.Remove(resolved)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ReportOrphans never deletes data automatically: operators may need it for recovery.
func (f *LocalFiles) ReportOrphans(paths map[string]bool) error {
	live := map[string]bool{}
	for path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		live[absolute] = true
		if _, err := os.Stat(absolute); err != nil {
			log.Printf("invoice file unavailable path=%q: %v", absolute, err)
		}
	}
	entries, err := os.ReadDir(f.Dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			path := filepath.Join(f.Dir, entry.Name())
			if !live[path] {
				log.Printf("orphan upload retained for manual recovery: %s", path)
			}
		}
	}
	return nil
}
