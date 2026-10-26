# Sorter · Invoice Management

A single-binary invoice management system: upload invoices (images / PDFs), let a
multimodal LLM extract amount, issue date, issuer, seller and other fields, and
categorize them automatically, then explore the data with multi-dimensional stats.

## Features

- **Smart import**: one multimodal LLM call (OpenAI-compatible API) handles both OCR
  extraction and auto-categorization (transport / dining / accommodation /
  tech service / other); PDFs are rendered with an embedded PDFium (WebAssembly)
- **Review before counting**: extraction results that miss an amount, invoice number
  or seller, or that come back with low confidence, land in `needs_review` and stay
  out of the statistics until a human confirms them
- **Multi-dimensional stats**: aggregate by day / month / year / seller / category,
  visualized with charts on the dashboard; every figure is scoped to a single
  currency (amounts are never converted or summed across currencies)
- **Invoice management**: keyword search, date-range, category and status filters,
  manual corrections, retry on failure, original file preview
- **Safe concurrency**: uploads are stored per request (no shared file paths), edits
  carry the record version so stale tabs cannot overwrite newer data, and queued
  extraction work is persisted in SQLite and reclaimed after a crash
- **Token auth**: no usernames or passwords; a pre-shared hashed token is verified and
  exchanged for a JWT (7 days)
- **One-step deployment**: the frontend is embedded into the Go binary, no CGO
  (modernc.org/sqlite + wazero), cross-compilation works out of the box

## Tech stack

| Layer | Technology | Version in use |
| --- | --- | --- |
| Backend | Go · Gin · golang-jwt · yaml.v3 | Go 1.27.2 · Gin 1.12.0 · jwt/v5 5.3.1 · yaml.v3 3.0.1 |
| Database | modernc.org/sqlite (pure Go, no CGO, WAL) | 1.60.1 |
| PDF rendering | go-pdfium + wazero (WebAssembly, no system PDFium library) | go-pdfium 1.21.3 · wazero 1.12.0 |
| Frontend | React · react-router-dom · TypeScript | React 19.3 · react-router-dom 7.18 · TypeScript 7.0 |
| Build tooling | Vite (Rolldown core) · @vitejs/plugin-react | Vite 8.3 · plugin-react 6.1 |
| Styling / UI | Tailwind CSS v4 (@tailwindcss/vite) · Radix UI primitives + class-variance-authority (shadcn-style) · lucide-react · sonner · tw-animate-css | tailwindcss 4.3 · Radix UI 1.x/2.x · cva 0.7 |
| Charts | recharts (split into its own chunk at build time) | 3.10 |
| Storage | SQLite (WAL) + local file directory | — |
| Toolchain | Make · pnpm (`packageManager`-pinned) | pnpm 12.6.0 |
| LLM | OpenAI-compatible multimodal endpoint (GPT-4o / Qwen-VL / Doubao Vision, etc.) | — |

> Versions are taken from `go.mod` and `web/package.json`; verify with
> `go list -m all` / `pnpm list`.

## Quick start

### 1. Build (produces a single binary)

Prerequisites: Go 1.27+ (builds currently use 1.27.2), Node.js 20+ (currently 24.22),
and pnpm (`npm install -g pnpm`; the repo pins 12.6.0 via `packageManager`).

```bash
make build        # = frontend build + embed + Go compile → ./sorter
```

### 2. Generate the access token hash

```bash
./sorter hash-token your-access-token
# Output: sha256:xxxx...  ← put it into auth.token_hash in config.yaml
```

### 3. Configure

```bash
cp config.example.yaml config.yaml
# Edit config.yaml: set token_hash and the LLM base_url / api_key / model
```

### 4. Run

```bash
./sorter -config config.yaml
# Listens on :8080; open http://localhost:8080 in a browser
```

Deploying to a server only requires copying two files: `sorter` (the binary) and
`config.yaml`. Data (SQLite database + uploaded files) is written to `./data/` by
default.

> Run exactly one instance per database. The in-process worker pool owns the
> queue, and startup reclaims rows that were interrupted mid-extraction; two
> processes sharing one database would compete for the same work.

Settings can also be overridden with environment variables: `SORTER_ADDR`,
`SORTER_TOKEN_HASH`, `SORTER_LLM_BASE_URL`, `SORTER_LLM_API_KEY`,
`SORTER_LLM_MODEL`, and more. Unknown YAML keys are rejected so typos fail at
startup instead of silently falling back to defaults.

### Limits and data rules

| Rule | Value |
| --- | --- |
| Max file size | 32 MiB per file |
| Max request size | 128 MiB, up to 20 files per upload |
| Accepted types | JPG / JPEG / PNG / WebP / single-page PDF (content and dimensions are verified) |
| Amounts | stored in minor units; max two decimals; negative values are allowed for red invoices |
| Currencies | CNY, USD, EUR, GBP, HKD, JPY, CAD, AUD, SGD, CHF, KRW, NZD |
| Sessions | invalidated when the process restarts or `auth.token_hash` changes |

## Development

The frontend uses **pnpm** for dependency management (`packageManager` pins
pnpm@12.6.0). If it is not installed yet:

```bash
npm install -g pnpm     # or: corepack enable pnpm
```

```bash
# Terminal 1: backend (serves the placeholder frontend page)
make dev-backend    # go run ./cmd/sorter -config config.yaml

# Terminal 2: frontend hot reload (Vite 8 / Rolldown proxies /api to :8080)
make dev-frontend   # cd web && pnpm run dev
```

## Testing

```bash
make test     # go test ./...
make check    # go test + go vet + TypeScript typecheck
```

CI runs the same checks (`.github/workflows/ci.yml`): `go vet`, `go test -race`,
a clean `go build`, and the frontend typecheck plus production build.

Covered regression areas: per-request upload paths and duplicate cleanup, stale
version rejection, exclusive job claiming and release on shutdown, currency-scoped
statistics, review-state transitions, JWT handling, and config validation.

## Cross-compilation (no CGO required)

```bash
make build-linux-arm64   # Linux ARM64
make build-windows       # Windows amd64
```
