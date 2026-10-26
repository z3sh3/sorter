ALTER TABLE invoices ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE invoices ADD COLUMN extraction_token TEXT NOT NULL DEFAULT '';
ALTER TABLE invoices ADD COLUMN confidence REAL NOT NULL DEFAULT 0;
ALTER TABLE invoices ADD COLUMN review_reason TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_invoices_pending ON invoices(status, extraction_token, id);
