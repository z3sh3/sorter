// API contract types mirroring the Go backend DTOs.

export interface Invoice {
  id: number
  file_name: string
  invoice_number: string
  invoice_date: string
  amount: number // yuan
  currency: string
  seller_name: string
  buyer_name: string
  drawer: string
  category: string
  status: "processing" | "done" | "failed"
  error_message: string
  raw_response: string
  notes: string
  created_at: string
  updated_at: string
}

export interface ListResponse {
  items: Invoice[]
  total: number
  page: number
  page_size: number
}

export interface StatItem {
  period: string
  count: number
  total: number
}

export interface Summary {
  total_count: number
  total_amount: number
  month_count: number
  month_amount: number
}

export interface UploadResult {
  file_name: string
  invoice_id?: number
  status: "queued" | "duplicate" | "error"
  error?: string
}

export interface UpdateInvoicePayload {
  invoice_number?: string
  invoice_date?: string
  amount?: number
  seller_name?: string
  buyer_name?: string
  drawer?: string
  category?: string
  notes?: string
}

export interface ListParams {
  page?: number
  page_size?: number
  category?: string
  seller?: string
  status?: string
  keyword?: string
  start_date?: string
  end_date?: string
  sort_by?: string
  sort_order?: string
}
