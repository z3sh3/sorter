package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"sorter/internal/invoice"
	"sorter/internal/store"
)

// invoiceDTO is the API representation; amount is exposed in yuan.
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
	RawResponse   string  `json:"raw_response"`
	Notes         string  `json:"notes"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

func toDTO(inv *invoice.Invoice) invoiceDTO {
	return invoiceDTO{
		ID:            inv.ID,
		FileName:      inv.FileName,
		InvoiceNumber: inv.InvoiceNumber,
		InvoiceDate:   inv.InvoiceDate,
		Amount:        inv.AmountYuan(),
		Currency:      inv.Currency,
		SellerName:    inv.SellerName,
		BuyerName:     inv.BuyerName,
		Drawer:        inv.Drawer,
		Category:      inv.Category,
		Status:        inv.Status,
		ErrorMessage:  inv.ErrorMessage,
		RawResponse:   inv.RawResponse,
		Notes:         inv.Notes,
		CreatedAt:     inv.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     inv.UpdatedAt.Format(time.RFC3339),
	}
}

func badRequest(c *gin.Context, msg string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": msg})
}

func notFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
}

// handleLogin verifies the access token and issues a JWT.
func (s *Server) handleLogin(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "invalid request body")
		return
	}
	if !s.auth.VerifyToken(req.Token) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
		return
	}
	jwtToken, expiresAt, err := s.auth.Issue()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "issue token failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": jwtToken, "expires_at": expiresAt.Format(time.RFC3339)})
}

// allowedUploadExts restricts imported file types.
var allowedUploadExts = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".pdf":  "application/pdf",
}

type uploadResult struct {
	FileName  string `json:"file_name"`
	InvoiceID int64  `json:"invoice_id,omitempty"`
	Status    string `json:"status"` // queued | duplicate | error
	Error     string `json:"error,omitempty"`
}

// handleUpload accepts multiple files (field name "files"), stores them,
// deduplicates by content hash and enqueues extraction.
func (s *Server) handleUpload(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		badRequest(c, "invalid multipart form")
		return
	}
	files := form.File["files"]
	if len(files) == 0 {
		badRequest(c, "no files provided (field name: files)")
		return
	}

	results := make([]uploadResult, 0, len(files))
	for _, fh := range files {
		res := uploadResult{FileName: fh.Filename}
		results = append(results, res) // placeholder, filled below by index
		r := &results[len(results)-1]

		ext := strings.ToLower(filepath.Ext(fh.Filename))
		if _, ok := allowedUploadExts[ext]; !ok {
			r.Status = "error"
			r.Error = "unsupported file type: " + ext
			continue
		}

		src, err := fh.Open()
		if err != nil {
			r.Status = "error"
			r.Error = err.Error()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(src, 32<<20+1))
		src.Close()
		if err != nil || len(data) > 32<<20 {
			r.Status = "error"
			r.Error = "read failed or file exceeds 32MB"
			continue
		}

		hash := sha256.Sum256(data)
		hashHex := hex.EncodeToString(hash[:])
		exists, err := s.store.FileHashExists(hashHex)
		if err != nil {
			r.Status = "error"
			r.Error = err.Error()
			continue
		}
		if exists {
			r.Status = "duplicate"
			r.Error = "same file already imported"
			continue
		}

		storedName := hashHex + ext
		storedPath := filepath.Join(s.cfg.Storage.UploadDir, storedName)
		if err := os.WriteFile(storedPath, data, 0o644); err != nil {
			r.Status = "error"
			r.Error = err.Error()
			continue
		}

		id, err := s.store.CreateInvoice(&invoice.Invoice{
			FileName: fh.Filename,
			FilePath: storedPath,
			FileHash: hashHex,
			Status:   invoice.StatusProcessing,
			Category: invoice.CategoryOther,
		})
		if err != nil {
			os.Remove(storedPath)
			r.Status = "error"
			r.Error = err.Error()
			continue
		}
		s.queue.Enqueue(id)
		r.Status = "queued"
		r.InvoiceID = id
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

// handleList returns a filtered, paginated invoice list.
func (s *Server) handleList(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	filter := store.ListFilter{
		Page:      page,
		PageSize:  pageSize,
		Category:  c.Query("category"),
		Seller:    c.Query("seller"),
		Status:    c.Query("status"),
		Keyword:   c.Query("keyword"),
		StartDate: c.Query("start_date"),
		EndDate:   c.Query("end_date"),
		SortBy:    c.Query("sort_by"),
		SortOrder: c.Query("sort_order"),
	}
	items, total, err := s.store.ListInvoices(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	dtos := make([]invoiceDTO, 0, len(items))
	for _, inv := range items {
		dtos = append(dtos, toDTO(inv))
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     dtos,
		"total":     total,
		"page":      filter.Page,
		"page_size": filter.PageSize,
	})
}

func (s *Server) mustGet(c *gin.Context) *invoice.Invoice {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		badRequest(c, "invalid id")
		return nil
	}
	inv, err := s.store.GetInvoice(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil
	}
	if inv == nil {
		notFound(c)
		return nil
	}
	return inv
}

func (s *Server) handleGet(c *gin.Context) {
	inv := s.mustGet(c)
	if inv == nil {
		return
	}
	c.JSON(http.StatusOK, toDTO(inv))
}

// updateInvoiceReq carries optional editable fields; amount is in yuan.
type updateInvoiceReq struct {
	InvoiceNumber *string  `json:"invoice_number"`
	InvoiceDate   *string  `json:"invoice_date"`
	Amount        *float64 `json:"amount"`
	SellerName    *string  `json:"seller_name"`
	BuyerName     *string  `json:"buyer_name"`
	Drawer        *string  `json:"drawer"`
	Category      *string  `json:"category"`
	Notes         *string  `json:"notes"`
}

func (s *Server) handleUpdate(c *gin.Context) {
	inv := s.mustGet(c)
	if inv == nil {
		return
	}
	var req updateInvoiceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "invalid request body")
		return
	}
	fields := map[string]any{}
	if req.InvoiceNumber != nil {
		fields["invoice_number"] = strings.TrimSpace(*req.InvoiceNumber)
	}
	if req.InvoiceDate != nil {
		fields["invoice_date"] = strings.TrimSpace(*req.InvoiceDate)
	}
	if req.Amount != nil {
		if *req.Amount < 0 {
			badRequest(c, "amount must be non-negative")
			return
		}
		fields["amount"] = invoice.YuanToCents(*req.Amount)
	}
	if req.SellerName != nil {
		fields["seller_name"] = strings.TrimSpace(*req.SellerName)
	}
	if req.BuyerName != nil {
		fields["buyer_name"] = strings.TrimSpace(*req.BuyerName)
	}
	if req.Drawer != nil {
		fields["drawer"] = strings.TrimSpace(*req.Drawer)
	}
	if req.Category != nil {
		if !invoice.IsValidCategory(*req.Category) {
			badRequest(c, "invalid category: "+*req.Category)
			return
		}
		fields["category"] = *req.Category
	}
	if req.Notes != nil {
		fields["notes"] = *req.Notes
	}
	if err := s.store.UpdateFields(inv.ID, fields); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	updated, _ := s.store.GetInvoice(inv.ID)
	c.JSON(http.StatusOK, toDTO(updated))
}

func (s *Server) handleDelete(c *gin.Context) {
	inv := s.mustGet(c)
	if inv == nil {
		return
	}
	path, err := s.store.DeleteInvoice(inv.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			c.Error(fmt.Errorf("remove file %s: %w", path, err))
		}
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// handleReExtract re-queues extraction for one invoice.
func (s *Server) handleReExtract(c *gin.Context) {
	inv := s.mustGet(c)
	if inv == nil {
		return
	}
	if err := s.store.MarkProcessing(inv.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	s.queue.Enqueue(inv.ID)
	c.JSON(http.StatusOK, gin.H{"status": "queued"})
}

// handleFile streams the original uploaded file for preview.
func (s *Server) handleFile(c *gin.Context) {
	inv := s.mustGet(c)
	if inv == nil {
		return
	}
	ext := strings.ToLower(filepath.Ext(inv.FilePath))
	contentType, ok := allowedUploadExts[ext]
	if !ok {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(inv.FileName))
	http.ServeFile(c.Writer, c.Request, inv.FilePath)
}

// handleStats aggregates by day/month/year/seller/category.
func (s *Server) handleStats(c *gin.Context) {
	rows, err := s.store.Stats(c.DefaultQuery("group_by", "month"), c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		badRequest(c, err.Error())
		return
	}
	type statDTO struct {
		Period string  `json:"period"`
		Count  int     `json:"count"`
		Total  float64 `json:"total"`
	}
	out := make([]statDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, statDTO{Period: r.Period, Count: r.Count, Total: float64(r.TotalCents) / 100})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// handleSummary returns headline numbers for the dashboard.
func (s *Server) handleSummary(c *gin.Context) {
	total, month, err := s.store.Summary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"total_count":  total.Count,
		"total_amount": float64(total.TotalCents) / 100,
		"month_count":  month.Count,
		"month_amount": float64(month.TotalCents) / 100,
	})
}
