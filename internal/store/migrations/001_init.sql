CREATE TABLE IF NOT EXISTS invoices (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    file_name      TEXT NOT NULL,
    file_path      TEXT NOT NULL,
    file_hash      TEXT NOT NULL UNIQUE,
    invoice_number TEXT,
    invoice_date   TEXT,
    amount         INTEGER NOT NULL DEFAULT 0,
    currency       TEXT NOT NULL DEFAULT 'CNY',
    seller_name    TEXT,
    buyer_name     TEXT,
    drawer         TEXT,
    category       TEXT NOT NULL DEFAULT 'other',
    status         TEXT NOT NULL DEFAULT 'processing',
    error_message  TEXT,
    raw_response   TEXT,
    notes          TEXT,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_invoices_date ON invoices(invoice_date);
CREATE INDEX IF NOT EXISTS idx_invoices_category ON invoices(category);
CREATE INDEX IF NOT EXISTS idx_invoices_seller ON invoices(seller_name);
CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status);
