package extractor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"sorter/internal/config"
	"sorter/internal/invoice"
)

// Result is the structured extraction output of the LLM.
type Result struct {
	InvoiceNumber string  `json:"invoice_number"`
	InvoiceDate   string  `json:"invoice_date"`
	TotalAmount   float64 `json:"total_amount"`
	Currency      string  `json:"currency"`
	SellerName    string  `json:"seller_name"`
	BuyerName     string  `json:"buyer_name"`
	Drawer        string  `json:"drawer"`
	Category      string  `json:"category"`
	Confidence    float64 `json:"confidence"`
	Reason        string  `json:"reason"`
}

// Service performs invoice extraction with a multimodal LLM.
type Service struct {
	client *Client
}

// NewService builds a Service from configuration.
func NewService(cfg config.LLMConfig) *Service {
	return &Service{client: NewClient(cfg)}
}

// apiError marks HTTP failures; status drives retry decisions.
type apiError struct {
	status int
	body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("llm api returned %d: %s", e.status, e.body)
}

// ExtractFromFile extracts invoice data from an image or PDF file.
// It renders PDFs to PNG first, then calls the LLM with bounded retries.
// Returns the normalized result plus the raw LLM response for auditability.
func (s *Service) ExtractFromFile(ctx context.Context, path string) (*Result, string, error) {
	imgBytes, mimeType, err := loadAsImage(path)
	if err != nil {
		return nil, "", err
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		result, raw, err := s.client.Extract(ctx, imgBytes, mimeType)
		if err == nil {
			normalize(result)
			return result, raw, nil
		}
		lastErr = err
		var ae *apiError
		if ok := errorAs(err, &ae); ok && ae.status != 0 && ae.status != http.StatusTooManyRequests && ae.status < 500 {
			break // 4xx (except 429): retrying will not help
		}
		if attempt < 3 {
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
			case <-time.After(time.Duration(attempt*2) * time.Second):
			}
		}
	}
	return nil, "", fmt.Errorf("extraction failed after retries: %w", lastErr)
}

// errorAs is a small local wrapper to avoid importing errors just for As.
func errorAs(err error, target **apiError) bool {
	for err != nil {
		if ae, ok := err.(*apiError); ok {
			*target = ae
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// loadAsImage returns image bytes ready for the LLM. PDF files are rendered
// to PNG via the WebAssembly PDFium renderer.
func loadAsImage(path string) ([]byte, string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".pdf" {
		renderer, err := SharedRenderer()
		if err != nil {
			return nil, "", fmt.Errorf("init pdf renderer: %w", err)
		}
		data, err := readFileLimit(path, 64<<20)
		if err != nil {
			return nil, "", err
		}
		png, err := renderer.RenderFirstPagePNG(data)
		if err != nil {
			return nil, "", err
		}
		return png, "image/png", nil
	}

	data, err := readFileLimit(path, 20<<20)
	if err != nil {
		return nil, "", err
	}
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	return data, mimeType, nil
}

func readFileLimit(path string, limit int64) ([]byte, error) {
	f, err := openFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file %s exceeds size limit of %d bytes", path, limit)
	}
	return data, nil
}

// normalize fixes up common LLM output quirks and validates fields.
func normalize(r *Result) {
	r.InvoiceNumber = strings.TrimSpace(r.InvoiceNumber)
	r.SellerName = strings.TrimSpace(r.SellerName)
	r.BuyerName = strings.TrimSpace(r.BuyerName)
	r.Drawer = strings.TrimSpace(r.Drawer)
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	if r.Currency == "" {
		r.Currency = "CNY"
	}
	// Normalize common date separators to YYYY-MM-DD.
	r.InvoiceDate = normalizeDate(strings.TrimSpace(r.InvoiceDate))
	if !invoice.IsValidCategory(r.Category) {
		r.Category = invoice.CategoryOther
	}
}

func normalizeDate(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ".", "-")
	for _, layout := range []string{"2006-01-02", "2006-1-2", "2006年1月2日", "20060102"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return s
}

// Client is an OpenAI-compatible chat completions client.
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// NewClient builds a Client from configuration.
func NewClient(cfg config.LLMConfig) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		http:    &http.Client{Timeout: cfg.Timeout},
	}
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
	Temperature    float64        `json:"temperature"`
	MaxTokens      int            `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	ImageURL map[string]any `json:"image_url,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Extract sends one image to the multimodal LLM and parses the JSON reply.
func (c *Client) Extract(ctx context.Context, img []byte, mimeType string) (*Result, string, error) {
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(img)
	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: []contentPart{{Type: "text", Text: systemPrompt}}},
			{Role: "user", Content: []contentPart{
				{Type: "text", Text: userPrompt},
				{Type: "image_url", ImageURL: map[string]any{"url": dataURL}},
			}},
		},
		ResponseFormat: map[string]any{"type": "json_object"},
		Temperature:    0.1,
		MaxTokens:      1024,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", &apiError{status: resp.StatusCode, body: truncate(string(body), 300)}
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, "", fmt.Errorf("decode llm response: %w", err)
	}
	if cr.Error != nil {
		return nil, "", fmt.Errorf("llm error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 || cr.Choices[0].Message.Content == "" {
		return nil, "", fmt.Errorf("llm returned no content")
	}

	raw := strings.TrimSpace(cr.Choices[0].Message.Content)
	cleaned := stripCodeFences(raw)

	// Detect the {"error": "not an invoice"} sentinel first.
	var probe struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(cleaned), &probe); err != nil {
		return nil, raw, fmt.Errorf("llm returned invalid json: %w", err)
	}
	if probe.Error != "" {
		return nil, raw, fmt.Errorf("llm rejected input: %s", probe.Error)
	}

	var result Result
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, raw, fmt.Errorf("parse extraction result: %w", err)
	}
	return &result, raw, nil
}

// stripCodeFences removes optional ```json fences some models add.
func stripCodeFences(s string) string {
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
