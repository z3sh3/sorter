package invoice

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	CategoryTransport           = "transport"
	CategoryDining              = "dining"
	CategoryAccommodation       = "accommodation"
	CategoryTechService         = "tech_service"
	CategoryOther               = "other"
	StatusProcessing            = "processing"
	StatusDone                  = "done"
	StatusFailed                = "failed"
	StatusNeedsReview           = "needs_review"
	MaxFileBytes          int64 = 32 << 20
	MaxUploadBytes        int64 = 128 << 20
	MaxUploadFiles              = 20
	MaxAmountCents        int64 = 100_000_000_000_000
)

var (
	ErrNotFound  = errors.New("invoice not found")
	ErrConflict  = errors.New("invoice changed or is processing; refresh and try again")
	ErrDuplicate = errors.New("same file already imported")
	Categories   = []string{CategoryTransport, CategoryDining, CategoryAccommodation, CategoryTechService, CategoryOther}
	moneyPattern = regexp.MustCompile(`^-?\d+(\.\d{1,2})?$`)
)

func IsValidCategory(c string) bool { return slices.Contains(Categories, c) }

// Money is an exact amount in hundredths of the currency's major unit.
// JSON input accepts decimal strings or numbers; scientific notation is rejected.
type Money int64

func ParseMoney(s string) (Money, error) {
	s = strings.TrimSpace(s)
	if len(s) > 32 || !moneyPattern.MatchString(s) {
		return 0, fmt.Errorf("amount must have at most two decimal places")
	}
	value, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, fmt.Errorf("invalid amount")
	}
	value.Mul(value, big.NewRat(100, 1))
	if !value.IsInt() || !value.Num().IsInt64() {
		return 0, fmt.Errorf("amount out of range")
	}
	cents := value.Num().Int64()
	if cents < -MaxAmountCents || cents > MaxAmountCents {
		return 0, fmt.Errorf("amount out of range")
	}
	return Money(cents), nil
}

func (m *Money) UnmarshalJSON(data []byte) error {
	s := string(data)
	if strings.HasPrefix(s, `"`) {
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
	}
	value, err := ParseMoney(s)
	if err == nil {
		*m = value
	}
	return err
}

// Currency validation is deliberately limited to supported currencies.
func ValidCurrency(s string) bool {
	return slices.Contains([]string{"CNY", "USD", "EUR", "GBP", "HKD", "JPY", "CAD", "AUD", "SGD", "CHF", "KRW", "NZD"}, s)
}

func ValidateFields(date string, amount int64, currency, category string) error {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return fmt.Errorf("invoice_date must be a valid YYYY-MM-DD date")
	}
	if amount < -MaxAmountCents || amount > MaxAmountCents {
		return fmt.Errorf("amount out of range")
	}
	if !ValidCurrency(currency) {
		return fmt.Errorf("unsupported currency")
	}
	if (currency == "JPY" || currency == "KRW") && amount%100 != 0 {
		return fmt.Errorf("this currency requires a whole-unit amount")
	}
	if !IsValidCategory(category) {
		return fmt.Errorf("invalid category")
	}
	return nil
}

func ValidateDateRange(start, end string) error {
	for _, v := range []string{start, end} {
		if v != "" {
			if _, err := time.Parse("2006-01-02", v); err != nil {
				return fmt.Errorf("date filters must use valid YYYY-MM-DD dates")
			}
		}
	}
	if start != "" && end != "" && start > end {
		return fmt.Errorf("start_date must not exceed end_date")
	}
	return nil
}

type Invoice struct {
	ID              int64
	FileName        string
	FilePath        string
	FileHash        string
	InvoiceNumber   string
	InvoiceDate     string
	AmountCents     int64
	Currency        string
	SellerName      string
	BuyerName       string
	Drawer          string
	Category        string
	Status          string
	ErrorMessage    string
	RawResponse     string
	Notes           string
	Confidence      float64
	ReviewReason    string
	Version         int64
	ExtractionToken string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (i *Invoice) AmountYuan() float64 { return float64(i.AmountCents) / 100 }

// Update is a typed patch. Version is mandatory for optimistic concurrency.
type Update struct {
	Version       int64   `json:"version"`
	InvoiceNumber *string `json:"invoice_number"`
	InvoiceDate   *string `json:"invoice_date"`
	Amount        *Money  `json:"amount"`
	Currency      *string `json:"currency"`
	SellerName    *string `json:"seller_name"`
	BuyerName     *string `json:"buyer_name"`
	Drawer        *string `json:"drawer"`
	Category      *string `json:"category"`
	Notes         *string `json:"notes"`
	Confirm       bool    `json:"confirm"`
}

func (u Update) Apply(inv *Invoice) error {
	if inv.Status == StatusProcessing || inv.Version != u.Version {
		return ErrConflict
	}
	for _, pair := range []struct {
		source *string
		target *string
	}{
		{u.InvoiceNumber, &inv.InvoiceNumber}, {u.InvoiceDate, &inv.InvoiceDate}, {u.Currency, &inv.Currency},
		{u.SellerName, &inv.SellerName}, {u.BuyerName, &inv.BuyerName}, {u.Drawer, &inv.Drawer},
		{u.Category, &inv.Category}, {u.Notes, &inv.Notes},
	} {
		if pair.source != nil {
			if len(*pair.source) > 4096 {
				return fmt.Errorf("field exceeds 4096 bytes")
			}
			*pair.target = strings.TrimSpace(*pair.source)
		}
	}
	if u.Amount != nil {
		inv.AmountCents = int64(*u.Amount)
	}
	inv.Currency = strings.ToUpper(inv.Currency)
	if err := ValidateFields(inv.InvoiceDate, inv.AmountCents, inv.Currency, inv.Category); err != nil {
		return err
	}
	if u.Confirm {
		if inv.InvoiceNumber == "" || inv.SellerName == "" {
			return fmt.Errorf("invoice number and seller are required for confirmation")
		}
		if inv.Status != StatusDone && u.Amount == nil {
			return fmt.Errorf("explicit amount is required for confirmation")
		}
		inv.Status = StatusDone
		inv.ReviewReason = ""
		inv.ErrorMessage = ""
	}
	return nil
}
