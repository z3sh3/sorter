package store

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"sorter/internal/invoice"
)

//go:embed migrations
var migrationsFS embed.FS

// Store wraps the SQLite database handle.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at dbPath,
// sets pragmas and applies pending migrations.
func Open(dbPath string) (*Store, error) {
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single connection keeps WAL-mode writes serialized and avoids
	// SQLITE_BUSY errors under the small worker pool.
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %q: %w", pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists int
		if err := s.db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE name = ?`, name).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
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
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const invoiceColumns = `id, file_name, file_path, file_hash, invoice_number, invoice_date,
	amount, currency, seller_name, buyer_name, drawer, category, status,
	error_message, raw_response, notes, created_at, updated_at`

func scanInvoice(row interface{ Scan(...any) error }) (*invoice.Invoice, error) {
	var inv invoice.Invoice
	var num, date, seller, buyer, drawer, errMsg, raw, notes sql.NullString
	err := row.Scan(&inv.ID, &inv.FileName, &inv.FilePath, &inv.FileHash,
		&num, &date, &inv.AmountCents, &inv.Currency, &seller, &buyer, &drawer,
		&inv.Category, &inv.Status, &errMsg, &raw, &notes, &inv.CreatedAt, &inv.UpdatedAt)
	if err != nil {
		return nil, err
	}
	inv.InvoiceNumber, inv.InvoiceDate = num.String, date.String
	inv.SellerName, inv.BuyerName, inv.Drawer = seller.String, buyer.String, drawer.String
	inv.ErrorMessage, inv.RawResponse, inv.Notes = errMsg.String, raw.String, notes.String
	return &inv, nil
}

// CreateInvoice inserts a new invoice record (typically in "processing" status).
func (s *Store) CreateInvoice(inv *invoice.Invoice) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO invoices
		(file_name, file_path, file_hash, status, category) VALUES (?, ?, ?, ?, ?)`,
		inv.FileName, inv.FilePath, inv.FileHash, inv.Status, inv.Category)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetInvoice returns one invoice by ID.
func (s *Store) GetInvoice(id int64) (*invoice.Invoice, error) {
	row := s.db.QueryRow(`SELECT `+invoiceColumns+` FROM invoices WHERE id = ?`, id)
	inv, err := scanInvoice(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return inv, err
}

// ProcessingIDs returns IDs of invoices stuck in "processing" status,
// used to resume extraction after a restart.
func (s *Store) ProcessingIDs() ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM invoices WHERE status = ?`, invoice.StatusProcessing)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FileHashExists reports whether a file with this hash was already imported.
func (s *Store) FileHashExists(hash string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM invoices WHERE file_hash = ?`, hash).Scan(&n)
	return n > 0, err
}

// DeleteInvoice removes the record and returns its file path for cleanup.
func (s *Store) DeleteInvoice(id int64) (string, error) {
	inv, err := s.GetInvoice(id)
	if err != nil || inv == nil {
		return "", err
	}
	_, err = s.db.Exec(`DELETE FROM invoices WHERE id = ?`, id)
	return inv.FilePath, err
}

// MarkProcessing resets an invoice back to the processing state.
func (s *Store) MarkProcessing(id int64) error {
	_, err := s.db.Exec(`UPDATE invoices SET status = ?, error_message = NULL, updated_at = ? WHERE id = ?`,
		invoice.StatusProcessing, time.Now(), id)
	return err
}

// CompleteExtraction fills the extracted fields and marks the invoice done.
func (s *Store) CompleteExtraction(id int64, inv *invoice.Invoice) error {
	_, err := s.db.Exec(`UPDATE invoices SET
		invoice_number = ?, invoice_date = ?, amount = ?, currency = ?,
		seller_name = ?, buyer_name = ?, drawer = ?, category = ?,
		status = ?, error_message = NULL, raw_response = ?, updated_at = ?
		WHERE id = ?`,
		inv.InvoiceNumber, inv.InvoiceDate, inv.AmountCents, inv.Currency,
		inv.SellerName, inv.BuyerName, inv.Drawer, inv.Category,
		invoice.StatusDone, inv.RawResponse, time.Now(), id)
	return err
}

// FailExtraction marks the invoice failed with an error message.
func (s *Store) FailExtraction(id int64, errMsg string) error {
	_, err := s.db.Exec(`UPDATE invoices SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`,
		invoice.StatusFailed, errMsg, time.Now(), id)
	return err
}

// UpdateFields applies a partial update from the given column->value map.
// Column names are validated by the caller (handler whitelist).
func (s *Store) UpdateFields(id int64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	type pair struct {
		col string
		val any
	}
	pairs := make([]pair, 0, len(fields))
	for col, val := range fields {
		pairs = append(pairs, pair{col, val})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].col < pairs[j].col })
	sets := make([]string, 0, len(pairs)+1)
	args := make([]any, 0, len(pairs)+2)
	for _, p := range pairs {
		sets = append(sets, p.col+" = ?")
		args = append(args, p.val)
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now(), id)
	_, err := s.db.Exec(`UPDATE invoices SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

// ListFilter controls ListInvoices queries.
type ListFilter struct {
	Page      int
	PageSize  int
	Category  string
	Seller    string
	Status    string
	Keyword   string
	StartDate string // YYYY-MM-DD, inclusive
	EndDate   string // YYYY-MM-DD, inclusive
	SortBy    string // invoice_date | amount | created_at
	SortOrder string // asc | desc
}

// sortableColumns whitelists ORDER BY columns to prevent SQL injection.
var sortableColumns = map[string]string{
	"invoice_date": "invoice_date",
	"amount":       "amount",
	"created_at":   "created_at",
}

// ListInvoices returns one page plus the total row count.
func (s *Store) ListInvoices(f ListFilter) ([]*invoice.Invoice, int, error) {
	var where []string
	var args []any
	if f.Category != "" {
		where = append(where, "category = ?")
		args = append(args, f.Category)
	}
	if f.Seller != "" {
		where = append(where, "seller_name = ?")
		args = append(args, f.Seller)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	if f.StartDate != "" {
		where = append(where, "invoice_date >= ?")
		args = append(args, f.StartDate)
	}
	if f.EndDate != "" {
		where = append(where, "invoice_date <= ?")
		args = append(args, f.EndDate)
	}
	if f.Keyword != "" {
		where = append(where, `(invoice_number LIKE ? OR seller_name LIKE ? OR buyer_name LIKE ? OR drawer LIKE ? OR notes LIKE ?)`)
		like := "%" + f.Keyword + "%"
		args = append(args, like, like, like, like, like)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM invoices`+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	col := sortableColumns["invoice_date"]
	if c, ok := sortableColumns[f.SortBy]; ok {
		col = c
	}
	order := "DESC"
	if strings.EqualFold(f.SortOrder, "asc") {
		order = "ASC"
	}

	page := f.Page
	if page < 1 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	rows, err := s.db.Query(`SELECT `+invoiceColumns+` FROM invoices`+whereSQL+
		fmt.Sprintf(` ORDER BY %s %s, id %s LIMIT ? OFFSET ?`, col, order, order),
		append(args, pageSize, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []*invoice.Invoice
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, inv)
	}
	return items, total, rows.Err()
}

// StatRow is one aggregated bucket.
type StatRow struct {
	Period     string `json:"period"`
	Count      int    `json:"count"`
	TotalCents int64  `json:"total_cents"`
}

// statsDateFilter is the shared constant WHERE clause. Date bounds are
// nullable parameters: an empty string disables the bound.
const statsDateFilter = `status = 'done' AND invoice_date IS NOT NULL AND invoice_date != ''
	AND (? = '' OR invoice_date >= ?) AND (? = '' OR invoice_date <= ?)`

// Stats aggregates done invoices by the given dimension within a date range.
// Every branch uses a fully constant query string; all user input is bound
// as parameters.
func (s *Store) Stats(groupBy, startDate, endDate string) ([]StatRow, error) {
	args := []any{startDate, startDate, endDate, endDate}
	var rows *sql.Rows
	var err error
	switch groupBy {
	case "day":
		rows, err = s.db.Query(`SELECT invoice_date AS period, COUNT(1) AS count, SUM(amount) AS total FROM invoices WHERE `+statsDateFilter+` GROUP BY invoice_date ORDER BY period ASC`, args...)
	case "month":
		rows, err = s.db.Query(`SELECT substr(invoice_date, 1, 7) AS period, COUNT(1) AS count, SUM(amount) AS total FROM invoices WHERE `+statsDateFilter+` GROUP BY substr(invoice_date, 1, 7) ORDER BY period ASC`, args...)
	case "year":
		rows, err = s.db.Query(`SELECT substr(invoice_date, 1, 4) AS period, COUNT(1) AS count, SUM(amount) AS total FROM invoices WHERE `+statsDateFilter+` GROUP BY substr(invoice_date, 1, 4) ORDER BY period ASC`, args...)
	case "seller":
		rows, err = s.db.Query(`SELECT COALESCE(NULLIF(seller_name, ''), '(未知)') AS period, COUNT(1) AS count, SUM(amount) AS total FROM invoices WHERE `+statsDateFilter+` GROUP BY seller_name ORDER BY total DESC LIMIT 20`, args...)
	case "category":
		rows, err = s.db.Query(`SELECT category AS period, COUNT(1) AS count, SUM(amount) AS total FROM invoices WHERE `+statsDateFilter+` GROUP BY category ORDER BY total DESC`, args...)
	default:
		return nil, fmt.Errorf("invalid group_by: %q", groupBy)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StatRow
	for rows.Next() {
		var r StatRow
		if err := rows.Scan(&r.Period, &r.Count, &r.TotalCents); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Summary returns headline numbers for the dashboard.
func (s *Store) Summary() (*StatRow, *StatRow, error) {
	var total StatRow
	err := s.db.QueryRow(`SELECT 'all', COUNT(1), COALESCE(SUM(amount), 0)
		FROM invoices WHERE status = 'done'`).Scan(&total.Period, &total.Count, &total.TotalCents)
	if err != nil {
		return nil, nil, err
	}
	month := time.Now().Format("2006-01")
	var m StatRow
	err = s.db.QueryRow(`SELECT 'month', COUNT(1), COALESCE(SUM(amount), 0)
		FROM invoices WHERE status = 'done' AND substr(invoice_date, 1, 7) = ?`, month).
		Scan(&m.Period, &m.Count, &m.TotalCents)
	if err != nil {
		return nil, nil, err
	}
	return &total, &m, nil
}
