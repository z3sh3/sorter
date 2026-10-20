package invoice

import (
	"slices"
	"time"
)

// Invoice categories.
const (
	CategoryTransport     = "transport"     // 交通
	CategoryDining        = "dining"        // 餐饮
	CategoryAccommodation = "accommodation" // 住宿
	CategoryTechService   = "tech_service"  // 技术服务费
	CategoryOther         = "other"         // 其他
)

// Categories lists all valid category values.
var Categories = []string{
	CategoryTransport,
	CategoryDining,
	CategoryAccommodation,
	CategoryTechService,
	CategoryOther,
}

// IsValidCategory reports whether c is a known category.
func IsValidCategory(c string) bool {
	return slices.Contains(Categories, c)
}

// Processing statuses.
const (
	StatusProcessing = "processing"
	StatusDone       = "done"
	StatusFailed     = "failed"
)

// Invoice is the core domain model. Amount is stored in cents to avoid
// floating point rounding issues.
type Invoice struct {
	ID            int64     `json:"id"`
	FileName      string    `json:"file_name"`
	FilePath      string    `json:"-"`
	FileHash      string    `json:"-"`
	InvoiceNumber string    `json:"invoice_number"`
	InvoiceDate   string    `json:"invoice_date"` // YYYY-MM-DD
	AmountCents   int64     `json:"-"`
	Currency      string    `json:"currency"`
	SellerName    string    `json:"seller_name"`
	BuyerName     string    `json:"buyer_name"`
	Drawer        string    `json:"drawer"` // 开票人
	Category      string    `json:"category"`
	Status        string    `json:"status"`
	ErrorMessage  string    `json:"error_message"`
	RawResponse   string    `json:"raw_response"`
	Notes         string    `json:"notes"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// AmountYuan returns the amount in yuan (CNY major unit).
func (i *Invoice) AmountYuan() float64 {
	return float64(i.AmountCents) / 100
}

// YuanToCents converts yuan to cents with rounding.
func YuanToCents(yuan float64) int64 {
	return int64(yuan*100 + 0.5)
}
