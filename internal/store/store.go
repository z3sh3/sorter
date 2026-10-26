package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"modernc.org/sqlite"
	"sorter/internal/invoice"
)

//go:embed migrations
var migrationsFS embed.FS

type Store struct{ db *sql.DB }

func Open(dbPath string) (*Store, error) {
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err = db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(name TEXT PRIMARY KEY, applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		name := entry.Name()
		var count int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE name=?", name).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(string(body)); err == nil {
			_, err = tx.Exec("INSERT INTO schema_migrations(name) VALUES(?)", name)
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const invoiceColumns = `id,file_name,file_path,file_hash,invoice_number,invoice_date,amount,currency,seller_name,buyer_name,drawer,category,status,error_message,raw_response,notes,created_at,updated_at,version,extraction_token,confidence,review_reason`

func scanInvoice(row interface{ Scan(...any) error }) (*invoice.Invoice, error) {
	var i invoice.Invoice
	var number, date, seller, buyer, drawer, message, raw, notes sql.NullString
	err := row.Scan(&i.ID, &i.FileName, &i.FilePath, &i.FileHash, &number, &date, &i.AmountCents, &i.Currency, &seller, &buyer, &drawer, &i.Category, &i.Status, &message, &raw, &notes, &i.CreatedAt, &i.UpdatedAt, &i.Version, &i.ExtractionToken, &i.Confidence, &i.ReviewReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, invoice.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	i.InvoiceNumber = number.String
	i.InvoiceDate = date.String
	i.SellerName = seller.String
	i.BuyerName = buyer.String
	i.Drawer = drawer.String
	i.ErrorMessage = message.String
	i.RawResponse = raw.String
	i.Notes = notes.String
	return &i, nil
}
func (s *Store) CreateInvoice(ctx context.Context, i *invoice.Invoice) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO invoices(file_name,file_path,file_hash,status,category) VALUES(?,?,?,?,?)`, i.FileName, i.FilePath, i.FileHash, invoice.StatusProcessing, invoice.CategoryOther)
	if err != nil {
		var se *sqlite.Error
		if errors.As(err, &se) && se.Code() == 2067 {
			return 0, invoice.ErrDuplicate
		}
		return 0, err
	}
	return res.LastInsertId()
}
func (s *Store) GetInvoice(ctx context.Context, id int64) (*invoice.Invoice, error) {
	return scanInvoice(s.db.QueryRowContext(ctx, "SELECT "+invoiceColumns+" FROM invoices WHERE id=?", id))
}

// RecoverPending is called once before workers start. Only one server may own a database.
func (s *Store) RecoverPending(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE invoices SET extraction_token='', version=version+1 WHERE status=?`, invoice.StatusProcessing)
	return err
}

// ClaimNext atomically claims one durable pending record.
func (s *Store) ClaimNext(ctx context.Context) (*invoice.Invoice, error) {
	token := rand.Text()
	return scanInvoice(s.db.QueryRowContext(ctx, `UPDATE invoices SET extraction_token=?,updated_at=? WHERE id=(SELECT id FROM invoices WHERE status=? AND extraction_token='' ORDER BY id LIMIT 1) AND extraction_token='' RETURNING `+invoiceColumns, token, time.Now(), invoice.StatusProcessing))
}
func (s *Store) Release(ctx context.Context, id int64, token string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE invoices SET extraction_token='' WHERE id=? AND extraction_token=? AND status=?`, id, token, invoice.StatusProcessing)
	return err
}
func changed(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return invoice.ErrConflict
	}
	return nil
}
func (s *Store) MarkProcessing(ctx context.Context, id, version int64) error {
	return changed(s.db.ExecContext(ctx, `UPDATE invoices SET status=?,extraction_token='',error_message=NULL,version=version+1,updated_at=? WHERE id=? AND version=? AND status<>?`, invoice.StatusProcessing, time.Now(), id, version, invoice.StatusProcessing))
}
func (s *Store) CompleteExtraction(ctx context.Context, id int64, token string, i *invoice.Invoice) error {
	return changed(s.db.ExecContext(ctx, `UPDATE invoices SET invoice_number=?,invoice_date=?,amount=?,currency=?,seller_name=?,buyer_name=?,drawer=?,category=?,status=?,error_message=NULL,raw_response=?,confidence=?,review_reason=?,extraction_token='',version=version+1,updated_at=? WHERE id=? AND extraction_token=? AND status=?`, i.InvoiceNumber, i.InvoiceDate, i.AmountCents, i.Currency, i.SellerName, i.BuyerName, i.Drawer, i.Category, i.Status, i.RawResponse, i.Confidence, i.ReviewReason, time.Now(), id, token, invoice.StatusProcessing))
}
func (s *Store) FailExtraction(ctx context.Context, id int64, token, message, raw string) error {
	return changed(s.db.ExecContext(ctx, `UPDATE invoices SET status=?,error_message=?,raw_response=?,extraction_token='',version=version+1,updated_at=? WHERE id=? AND extraction_token=? AND status=?`, invoice.StatusFailed, message, raw, time.Now(), id, token, invoice.StatusProcessing))
}
func (s *Store) SaveInvoice(ctx context.Context, i *invoice.Invoice) error {
	return changed(s.db.ExecContext(ctx, `UPDATE invoices SET invoice_number=?,invoice_date=?,amount=?,currency=?,seller_name=?,buyer_name=?,drawer=?,category=?,notes=?,status=?,review_reason=?,error_message=?,version=version+1,updated_at=? WHERE id=? AND version=? AND status<>?`, i.InvoiceNumber, i.InvoiceDate, i.AmountCents, i.Currency, i.SellerName, i.BuyerName, i.Drawer, i.Category, i.Notes, i.Status, i.ReviewReason, i.ErrorMessage, time.Now(), i.ID, i.Version, invoice.StatusProcessing))
}
func (s *Store) DeleteInvoice(ctx context.Context, id, version int64) error {
	return changed(s.db.ExecContext(ctx, `DELETE FROM invoices WHERE id=? AND version=? AND status<>?`, id, version, invoice.StatusProcessing))
}

// FilePaths supports read-only orphan reconciliation at startup.
func (s *Store) FilePaths(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT file_path FROM invoices")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	paths := map[string]bool{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths[path] = true
	}
	return paths, rows.Err()
}

type ListFilter struct {
	Page, PageSize                                                           int
	Category, Seller, Status, Keyword, StartDate, EndDate, SortBy, SortOrder string
}

func (f *ListFilter) Normalize() error {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Page > 1_000_000 {
		return fmt.Errorf("page exceeds limit")
	}
	if f.PageSize < 1 || f.PageSize > 200 {
		f.PageSize = 20
	}
	if f.Category != "" && !invoice.IsValidCategory(f.Category) {
		return fmt.Errorf("invalid category")
	}
	if f.Status != "" && f.Status != invoice.StatusProcessing && f.Status != invoice.StatusDone && f.Status != invoice.StatusFailed && f.Status != invoice.StatusNeedsReview {
		return fmt.Errorf("invalid status")
	}
	if len(f.Keyword) > 256 || len(f.Seller) > 4096 {
		return fmt.Errorf("filter exceeds length limit")
	}
	return invoice.ValidateDateRange(f.StartDate, f.EndDate)
}
func (s *Store) ListInvoices(ctx context.Context, f ListFilter) ([]*invoice.Invoice, int, error) {
	if err := f.Normalize(); err != nil {
		return nil, 0, err
	}
	var where []string
	var args []any
	for _, p := range []struct{ column, value string }{{"category", f.Category}, {"seller_name", f.Seller}, {"status", f.Status}} {
		if p.value != "" {
			where = append(where, p.column+"=?")
			args = append(args, p.value)
		}
	}
	if f.StartDate != "" {
		where = append(where, "invoice_date>=?")
		args = append(args, f.StartDate)
	}
	if f.EndDate != "" {
		where = append(where, "invoice_date<=?")
		args = append(args, f.EndDate)
	}
	if f.Keyword != "" {
		where = append(where, `(invoice_number LIKE ? OR seller_name LIKE ? OR buyer_name LIKE ? OR drawer LIKE ? OR notes LIKE ?)`)
		for range 5 {
			args = append(args, "%"+f.Keyword+"%")
		}
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM invoices"+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	col := map[string]string{"invoice_date": "invoice_date", "amount": "amount", "created_at": "created_at"}[f.SortBy]
	if col == "" {
		col = "invoice_date"
	}
	order := "DESC"
	if f.SortOrder == "asc" {
		order = "ASC"
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+invoiceColumns+" FROM invoices"+clause+fmt.Sprintf(" ORDER BY %s %s,id %s LIMIT ? OFFSET ?", col, order, order), append(args, f.PageSize, (f.Page-1)*f.PageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*invoice.Invoice, 0)
	for rows.Next() {
		i, err := scanInvoice(rows)
		if err != nil {
			return nil, 0, err
		}
		i.RawResponse = ""
		items = append(items, i)
	}
	return items, total, rows.Err()
}

type StatRow struct {
	Period     string
	Count      int
	TotalCents int64
}

func (s *Store) Stats(ctx context.Context, group, start, end, currency string) ([]StatRow, error) {
	if err := invoice.ValidateDateRange(start, end); err != nil {
		return nil, err
	}
	if !invoice.ValidCurrency(currency) {
		return nil, fmt.Errorf("unsupported currency")
	}
	expression := map[string]string{"day": "invoice_date", "month": "substr(invoice_date,1,7)", "year": "substr(invoice_date,1,4)", "seller": "COALESCE(NULLIF(seller_name,''),'(unknown)')", "category": "category"}[group]
	if expression == "" {
		return nil, fmt.Errorf("invalid group_by")
	}
	order := "period ASC"
	if group == "seller" {
		order = "total DESC LIMIT 20"
	}
	if group == "category" {
		order = "total DESC"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+expression+` AS period,COUNT(*),SUM(amount) AS total FROM invoices WHERE status=? AND currency=? AND invoice_date<>'' AND (?='' OR invoice_date>=?) AND (?='' OR invoice_date<=?) GROUP BY `+expression+` ORDER BY `+order, invoice.StatusDone, currency, start, start, end, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]StatRow, 0)
	for rows.Next() {
		var r StatRow
		if err := rows.Scan(&r.Period, &r.Count, &r.TotalCents); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type CurrencySummary struct {
	Currency    string  `json:"currency"`
	TotalCount  int     `json:"total_count"`
	TotalAmount float64 `json:"total_amount"`
	MonthCount  int     `json:"month_count"`
	MonthAmount float64 `json:"month_amount"`
	ReviewCount int     `json:"review_count"`
}

// Summary aggregates each currency separately: amounts from different
// currencies are never added together.
func (s *Store) Summary(ctx context.Context) ([]CurrencySummary, error) {
	month := time.Now().Format("2006-01")
	rows, err := s.db.QueryContext(ctx, `SELECT currency,
		COALESCE(SUM(CASE WHEN status='done' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='done' THEN amount ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='done' AND substr(invoice_date,1,7)=? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='done' AND substr(invoice_date,1,7)=? THEN amount ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status=? THEN 1 ELSE 0 END),0)
		FROM invoices WHERE status IN (?,?) GROUP BY currency ORDER BY currency`,
		month, month, invoice.StatusNeedsReview, invoice.StatusDone, invoice.StatusNeedsReview)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]CurrencySummary, 0)
	for rows.Next() {
		var r CurrencySummary
		var total, monthTotal int64
		if err := rows.Scan(&r.Currency, &r.TotalCount, &total, &r.MonthCount, &monthTotal, &r.ReviewCount); err != nil {
			return nil, err
		}
		r.TotalAmount = float64(total) / 100
		r.MonthAmount = float64(monthTotal) / 100
		result = append(result, r)
	}
	return result, rows.Err()
}
