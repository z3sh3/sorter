package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sorter/internal/app"
	"sorter/internal/auth"
	"sorter/internal/config"
	"sorter/internal/store"
)

type noopQueue struct{}

func (noopQueue) Notify() {}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// newTestServer wires the HTTP layer without starting background workers.
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	files, err := app.NewLocalFiles(filepath.Join(dir, "uploads"))
	if err != nil {
		t.Fatalf("new files: %v", err)
	}
	am, err := auth.NewManager(tokenHash("access"), time.Hour)
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	jwtToken, _, err := am.Issue()
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return &Server{
		cfg:   &config.Config{},
		store: st,
		auth:  am,
		app:   app.New(st, files, noopQueue{}),
	}, jwtToken
}

func do(t *testing.T, handler http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestProtectedRoutesRequireToken(t *testing.T) {
	s, _ := newTestServer(t)
	router := s.router()

	for _, target := range []string{"/api/invoices", "/api/stats", "/api/stats/summary"} {
		rec := do(t, router, http.MethodGet, target, "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without token = %d, want 401", target, rec.Code)
		}
		rec = do(t, router, http.MethodGet, target, "bogus-token", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s with bogus token = %d, want 401", target, rec.Code)
		}
	}
}

func TestProtectedRoutesAcceptValidToken(t *testing.T) {
	s, token := newTestServer(t)
	router := s.router()

	rec := do(t, router, http.MethodGet, "/api/invoices", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d (%s)", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items    []any `json:"items"`
		Total    int   `json:"total"`
		Page     int   `json:"page"`
		PageSize int   `json:"page_size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Total != 0 || payload.Page != 1 || payload.PageSize != 20 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestListRejectsInvalidFilters(t *testing.T) {
	s, token := newTestServer(t)
	router := s.router()

	for _, query := range []string{"?page=abc", "?category=travel", "?status=unknown", "?start_date=2025-05-01&end_date=2025-01-01"} {
		rec := do(t, router, http.MethodGet, "/api/invoices"+query, token, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", query, rec.Code)
		}
	}
}

// Mutations must carry the version they were read at; otherwise a stale tab
// could overwrite a newer extraction.
func TestMutationsRequireVersion(t *testing.T) {
	s, token := newTestServer(t)
	router := s.router()

	rec := do(t, router, http.MethodDelete, "/api/invoices/1", token, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete without version = %d, want 400", rec.Code)
	}
	rec = do(t, router, http.MethodPut, "/api/invoices/1", token, `{"amount":"10.00"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update without version = %d, want 400", rec.Code)
	}
	rec = do(t, router, http.MethodPost, "/api/invoices/1/re-extract", token, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("re-extract without version = %d, want 400", rec.Code)
	}
}

func TestInvalidIDIsRejected(t *testing.T) {
	s, token := newTestServer(t)
	router := s.router()

	rec := do(t, router, http.MethodGet, "/api/invoices/abc", token, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMissingInvoiceReturns404(t *testing.T) {
	s, token := newTestServer(t)
	router := s.router()

	rec := do(t, router, http.MethodGet, "/api/invoices/42", token, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateRejectsInvalidAmountFormat(t *testing.T) {
	s, token := newTestServer(t)
	router := s.router()

	rec := do(t, router, http.MethodPut, "/api/invoices/1", token, `{"version":1,"amount":"1.234"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
}

func TestUnknownAPIRouteReturnsJSON404(t *testing.T) {
	s, _ := newTestServer(t)
	router := s.router()

	rec := do(t, router, http.MethodGet, "/api/does-not-exist", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("content type = %q", contentType)
	}
}

// Placeholder assets keep the SPA handler from panicking on a clean checkout.
func TestSPAHandlerReportsMissingAssets(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s.router(), http.MethodGet, "/", "", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}
