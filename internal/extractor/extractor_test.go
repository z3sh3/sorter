package extractor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sorter/internal/config"
	"sorter/internal/invoice"
)

func money(t *testing.T, value string) *invoice.Money {
	t.Helper()
	amount, err := invoice.ParseMoney(value)
	if err != nil {
		t.Fatalf("parse money: %v", err)
	}
	return &amount
}

func validResult(t *testing.T) *Result {
	return &Result{
		InvoiceNumber: "N-1",
		InvoiceDate:   "2025-02-01",
		TotalAmount:   money(t, "199.00"),
		Currency:      "CNY",
		SellerName:    "ACME",
		Category:      invoice.CategoryDining,
		Confidence:    0.95,
	}
}

func TestResultInvoiceAcceptsCompleteExtraction(t *testing.T) {
	got := validResult(t).Invoice("raw")
	if got.Status != invoice.StatusDone {
		t.Fatalf("status = %s, want done (reason %q)", got.Status, got.ReviewReason)
	}
	if got.AmountCents != 19900 {
		t.Fatalf("amount = %d, want 19900", got.AmountCents)
	}
	if got.RawResponse != "raw" {
		t.Fatalf("raw response not preserved: %q", got.RawResponse)
	}
}

func TestResultInvoiceRequiresReview(t *testing.T) {
	cases := map[string]func(*Result){
		"missing amount":   func(r *Result) { r.TotalAmount = nil },
		"missing number":   func(r *Result) { r.InvoiceNumber = "" },
		"missing seller":   func(r *Result) { r.SellerName = "" },
		"low confidence":   func(r *Result) { r.Confidence = 0.4 },
		"invalid date":     func(r *Result) { r.InvoiceDate = "2025-02-30" },
		"bad currency":     func(r *Result) { r.Currency = "XXX" },
		"invalid category": func(r *Result) { r.Category = "travel" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			result := validResult(t)
			mutate(result)
			got := result.Invoice("raw")
			if got.Status != invoice.StatusNeedsReview {
				t.Fatalf("status = %s, want needs_review", got.Status)
			}
			if got.ReviewReason == "" {
				t.Fatal("missing review reason")
			}
		})
	}
}

func TestNormalizeDate(t *testing.T) {
	cases := map[string]string{
		"2025/02/01": "2025-02-01",
		"2025.2.1":   "2025-02-01",
		"20250201":   "2025-02-01",
		"2025年1月2日":  "2025-01-02",
		"not a date": "not a date",
	}
	for in, want := range cases {
		if got := normalizeDate(in); got != want {
			t.Errorf("normalizeDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeFillsDefaults(t *testing.T) {
	r := &Result{SellerName: "  ACME  ", Category: "unknown"}
	normalize(r)
	if r.SellerName != "ACME" {
		t.Fatalf("seller = %q", r.SellerName)
	}
	if r.Currency != "CNY" {
		t.Fatalf("currency = %q, want CNY", r.Currency)
	}
	if r.Category != invoice.CategoryOther {
		t.Fatalf("category = %q, want other", r.Category)
	}
}

func TestStripCodeFences(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```": "{\"a\":1}",
		"{\"a\":1}":               "{\"a\":1}",
		"```\n{\"a\":1}```":       "{\"a\":1}",
	}
	for in, want := range cases {
		if got := stripCodeFences(in); got != want {
			t.Errorf("stripCodeFences(%q) = %q, want %q", in, got, want)
		}
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(config.LLMConfig{BaseURL: server.URL, APIKey: "key", Model: "model", Timeout: 5 * time.Second})
}

func reply(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
	})
}

func TestClientExtractParsesFencedJSON(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Errorf("authorization = %q", got)
		}
		reply(w, "```json\n{\"invoice_number\":\"N-1\",\"total_amount\":12.5,\"currency\":\"CNY\"}\n```")
	})
	result, raw, err := client.Extract(context.Background(), []byte("img"), "image/png")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if result.InvoiceNumber != "N-1" || result.TotalAmount == nil || int64(*result.TotalAmount) != 1250 {
		t.Fatalf("result = %+v", result)
	}
	if raw == "" {
		t.Fatal("raw response not returned")
	}
}

func TestClientExtractDetectsNotInvoice(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, `{"error":"not an invoice"}`)
	})
	if _, _, err := client.Extract(context.Background(), []byte("img"), "image/png"); err == nil {
		t.Fatal("non-invoice response was accepted")
	}
}

func TestClientExtractRejectsNull(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, "null")
	})
	if _, _, err := client.Extract(context.Background(), []byte("img"), "image/png"); err == nil {
		t.Fatal("null response was accepted")
	}
}

// The retry loop depends on 4xx responses being distinguishable from 5xx.
func TestClientExtractReportsHTTPStatus(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	})
	_, _, err := client.Extract(context.Background(), []byte("img"), "image/png")
	apiErr, ok := err.(*apiError)
	if !ok {
		t.Fatalf("err = %T, want *apiError", err)
	}
	if apiErr.status != http.StatusBadRequest {
		t.Fatalf("status = %d", apiErr.status)
	}
}
