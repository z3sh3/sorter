import type {
  Invoice,
  ListParams,
  ListResponse,
  StatItem,
  Summary,
  UpdateInvoicePayload,
  UploadResult,
} from "./types"

const TOKEN_KEY = "sorter_token"

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request(path: string, options: RequestInit = {}): Promise<Response> {
  const headers = new Headers(options.headers)
  const token = getToken()
  if (token) {
    headers.set("Authorization", `Bearer ${token}`)
  }
  if (options.body && !(options.body instanceof FormData)) {
    headers.set("Content-Type", "application/json")
  }
  const res = await fetch("/api" + path, { ...options, headers })
  if (res.status === 401) {
    clearToken()
    // Force re-login on any unauthorized response. The target is a
    // compile-time constant route, never user-controlled input.
    if (!window.location.pathname.startsWith("/login")) {
      window.location.assign("/login")
    }
    throw new ApiError(401, "unauthorized")
  }
  if (!res.ok) {
    let message = res.statusText
    try {
      const body = await res.json()
      if (body?.error) message = body.error
    } catch {
      // keep statusText
    }
    throw new ApiError(res.status, message)
  }
  return res
}

async function json<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await request(path, options)
  return res.json() as Promise<T>
}

function toQuery(params: Record<string, string | number | undefined>): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== "") q.set(k, String(v))
  }
  const s = q.toString()
  return s ? "?" + s : ""
}

export const api = {
  login: (token: string) =>
    json<{ token: string; expires_at: string }>("/login", {
      method: "POST",
      body: JSON.stringify({ token }),
    }),

  listInvoices: (params: ListParams) =>
    json<ListResponse>("/invoices" + toQuery({ ...params })),

  getInvoice: (id: number) => json<Invoice>(`/invoices/${id}`),

  updateInvoice: (id: number, payload: UpdateInvoicePayload) =>
    json<Invoice>(`/invoices/${id}`, { method: "PUT", body: JSON.stringify(payload) }),

  deleteInvoice: (id: number) =>
    json<{ deleted: boolean }>(`/invoices/${id}`, { method: "DELETE" }),

  reExtract: (id: number) =>
    json<{ status: string }>(`/invoices/${id}/re-extract`, { method: "POST" }),

  upload: (files: File[]) => {
    const form = new FormData()
    files.forEach((f) => form.append("files", f))
    return json<{ results: UploadResult[] }>("/invoices", { method: "POST", body: form })
  },

  stats: (groupBy: string, startDate = "", endDate = "") =>
    json<{ items: StatItem[] }>("/stats" + toQuery({ group_by: groupBy, start_date: startDate, end_date: endDate })),

  summary: () => json<Summary>("/stats/summary"),

  // fetchFileBlob loads the original invoice file with auth headers for preview.
  fetchFileBlob: async (id: number): Promise<Blob> => {
    const res = await request(`/invoices/${id}/file`)
    return res.blob()
  },
}
