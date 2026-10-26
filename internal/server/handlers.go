package server

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"sorter/internal/app"
	"sorter/internal/invoice"
	"sorter/internal/store"
)

type invoiceDTO struct {
	ID            int64   `json:"id"`
	FileName      string  `json:"file_name"`
	InvoiceNumber string  `json:"invoice_number"`
	InvoiceDate   string  `json:"invoice_date"`
	Amount        float64 `json:"amount"`
	Currency      string  `json:"currency"`
	SellerName    string  `json:"seller_name"`
	BuyerName     string  `json:"buyer_name"`
	Drawer        string  `json:"drawer"`
	Category      string  `json:"category"`
	Status        string  `json:"status"`
	ErrorMessage  string  `json:"error_message"`
	RawResponse   string  `json:"raw_response,omitempty"`
	Notes         string  `json:"notes"`
	Confidence    float64 `json:"confidence"`
	ReviewReason  string  `json:"review_reason"`
	Version       int64   `json:"version"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

func toDTO(i *invoice.Invoice) invoiceDTO {
	return invoiceDTO{ID: i.ID, FileName: i.FileName, InvoiceNumber: i.InvoiceNumber, InvoiceDate: i.InvoiceDate, Amount: i.AmountYuan(), Currency: i.Currency, SellerName: i.SellerName, BuyerName: i.BuyerName, Drawer: i.Drawer, Category: i.Category, Status: i.Status, ErrorMessage: i.ErrorMessage, RawResponse: i.RawResponse, Notes: i.Notes, Confidence: i.Confidence, ReviewReason: i.ReviewReason, Version: i.Version, CreatedAt: i.CreatedAt.Format(time.RFC3339), UpdatedAt: i.UpdatedAt.Format(time.RFC3339)}
}
func badRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "error": message})
}
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, invoice.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "error": "invoice not found"})
	case errors.Is(err, invoice.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"code": "conflict", "error": invoice.ErrConflict.Error()})
	default:
		log.Printf("request failed method=%s route=%s: %v", c.Request.Method, c.FullPath(), err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "error": "operation failed; please retry"})
	}
}
func bindJSON(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if err := c.ShouldBindJSON(target); err != nil {
		badRequest(c, "invalid request body")
		return false
	}
	return true
}
func (s *Server) handleLogin(c *gin.Context) {
	if !s.loginLimit.Allow() {
		c.Header("Retry-After", "3")
		c.JSON(http.StatusTooManyRequests, gin.H{"code": "rate_limited", "error": "too many login attempts"})
		return
	}
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if !s.auth.VerifyToken(req.Token) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
		return
	}
	token, expiry, err := s.auth.Issue()
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expires_at": expiry.Format(time.RFC3339)})
}

type uploadResult struct {
	FileName  string `json:"file_name"`
	InvoiceID int64  `json:"invoice_id,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func (s *Server) handleUpload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, invoice.MaxUploadBytes)
	// Bound temporary disk use before inspecting individual file parts.
	defer func() {
		if c.Request.MultipartForm != nil {
			c.Request.MultipartForm.RemoveAll()
		}
	}()
	form, err := c.MultipartForm()
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request exceeds 128 MiB"})
		} else {
			badRequest(c, "invalid multipart form")
		}
		return
	}
	files := form.File["files"]
	if len(files) < 1 || len(files) > invoice.MaxUploadFiles {
		badRequest(c, "provide between 1 and 20 files")
		return
	}
	results := make([]uploadResult, 0, len(files))
	for _, fh := range files {
		r := uploadResult{FileName: fh.Filename, Status: "error"}
		if fh.Size > invoice.MaxFileBytes {
			r.Error = "file exceeds 32 MiB"
			results = append(results, r)
			continue
		}
		src, err := fh.Open()
		if err != nil {
			log.Printf("open upload: %v", err)
			r.Error = "cannot read upload"
			results = append(results, r)
			continue
		}
		id, err := s.app.Import(c.Request.Context(), fh.Filename, src)
		src.Close()
		switch {
		case err == nil:
			r.Status = "queued"
			r.InvoiceID = id
		case errors.Is(err, invoice.ErrDuplicate):
			r.Status = "duplicate"
			r.Error = "same file already imported"
		case errors.Is(err, app.ErrInvalidFile):
			r.Error = err.Error()
		default:
			log.Printf("import failed: %v", err)
			r.Error = "import failed; please retry"
		}
		results = append(results, r)
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}
func (s *Server) handleList(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		badRequest(c, "invalid page")
		return
	}
	size, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil {
		badRequest(c, "invalid page_size")
		return
	}
	f := store.ListFilter{Page: page, PageSize: size, Category: c.Query("category"), Seller: c.Query("seller"), Status: c.Query("status"), Keyword: c.Query("keyword"), StartDate: c.Query("start_date"), EndDate: c.Query("end_date"), SortBy: c.Query("sort_by"), SortOrder: c.Query("sort_order")}
	if err := f.Normalize(); err != nil {
		badRequest(c, err.Error())
		return
	}
	items, total, err := s.store.ListInvoices(c.Request.Context(), f)
	if err != nil {
		fail(c, err)
		return
	}
	dtos := make([]invoiceDTO, 0, len(items))
	for _, i := range items {
		dtos = append(dtos, toDTO(i))
	}
	c.JSON(http.StatusOK, gin.H{"items": dtos, "total": total, "page": f.Page, "page_size": f.PageSize})
}
func requestID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		badRequest(c, "invalid invoice id")
		return 0, false
	}
	return id, true
}
func (s *Server) mustGet(c *gin.Context) *invoice.Invoice {
	id, ok := requestID(c)
	if !ok {
		return nil
	}
	i, err := s.store.GetInvoice(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return nil
	}
	return i
}
func (s *Server) handleGet(c *gin.Context) {
	if i := s.mustGet(c); i != nil {
		c.JSON(http.StatusOK, toDTO(i))
	}
}
func (s *Server) handleUpdate(c *gin.Context) {
	id, ok := requestID(c)
	if !ok {
		return
	}
	var patch invoice.Update
	if !bindJSON(c, &patch) {
		return
	}
	if patch.Version < 1 {
		badRequest(c, "version is required")
		return
	}
	updated, err := s.app.Update(c.Request.Context(), id, patch)
	if err != nil {
		if errors.Is(err, app.ErrValidation) {
			badRequest(c, err.Error())
		} else {
			fail(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, toDTO(updated))
}
func (s *Server) handleDelete(c *gin.Context) {
	id, ok := requestID(c)
	if !ok {
		return
	}
	version, err := strconv.ParseInt(c.Query("version"), 10, 64)
	if err != nil || version < 1 {
		badRequest(c, "version is required")
		return
	}
	if err := s.app.Delete(c.Request.Context(), id, version); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
func (s *Server) handleReExtract(c *gin.Context) {
	id, ok := requestID(c)
	if !ok {
		return
	}
	var req struct {
		Version int64 `json:"version"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if req.Version < 1 {
		badRequest(c, "version is required")
		return
	}
	if err := s.app.ReExtract(c.Request.Context(), id, req.Version); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "queued"})
}
func (s *Server) handleFile(c *gin.Context) {
	i := s.mustGet(c)
	if i == nil {
		return
	}
	kind := app.AllowedExtensions[strings.ToLower(filepath.Ext(i.FilePath))]
	if kind == "" {
		kind = "application/octet-stream"
	}
	c.Header("Content-Type", kind)
	c.Header("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(i.FileName))
	c.Header("Cache-Control", "private, no-store")
	http.ServeFile(c.Writer, c.Request, i.FilePath)
}
func (s *Server) handleStats(c *gin.Context) {
	currency := c.DefaultQuery("currency", "CNY")
	group := c.DefaultQuery("group_by", "month")
	if !invoice.ValidCurrency(currency) {
		badRequest(c, "unsupported currency")
		return
	}
	if err := invoice.ValidateDateRange(c.Query("start_date"), c.Query("end_date")); err != nil {
		badRequest(c, err.Error())
		return
	}
	switch group {
	case "day", "month", "year", "seller", "category":
	default:
		badRequest(c, "invalid group_by")
		return
	}
	rows, err := s.store.Stats(c.Request.Context(), group, c.Query("start_date"), c.Query("end_date"), currency)
	if err != nil {
		fail(c, err)
		return
	}
	type stat struct {
		Period string  `json:"period"`
		Count  int     `json:"count"`
		Total  float64 `json:"total"`
	}
	out := make([]stat, 0, len(rows))
	for _, r := range rows {
		out = append(out, stat{r.Period, r.Count, float64(r.TotalCents) / 100})
	}
	c.JSON(http.StatusOK, gin.H{"currency": currency, "items": out})
}
func (s *Server) handleSummary(c *gin.Context) {
	rows, err := s.store.Summary(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows})
}
