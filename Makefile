BINARY := sorter
DIST   := internal/server/dist

.PHONY: all build frontend backend test check clean dev dev-backend dev-frontend

all: build

# One-shot build: frontend -> embed -> single binary.
# The order-only prerequisite keeps `make -j build` correct.
build: backend
backend: | frontend
	go build -ldflags "-s -w" -o "$(BINARY)" ./cmd/sorter

# Vite output is copied into the embed directory; .gitkeep stays tracked.
frontend:
	cd web && pnpm install --frozen-lockfile && pnpm run build
	rm -rf "$(DIST)/assets" "$(DIST)/index.html"
	cp -a web/dist/. "$(DIST)/"

test:
	go test ./...

check: test
	go vet ./...
	cd web && pnpm exec tsc --noEmit -p tsconfig.json

# Development helpers (backend serves the embedded assets from the last build).
dev-backend:
	go run ./cmd/sorter -config config.yaml

dev-frontend:
	cd web && pnpm run dev

clean:
	rm -f "$(BINARY)"
	rm -rf web/dist web/node_modules "$(DIST)/assets" "$(DIST)/index.html"

# Cross-compile examples (pure Go, no CGO needed). Each needs the frontend
# assets embedded into the binary, so the frontend is built first.
build-linux-arm64: | frontend
	GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o "$(BINARY)-linux-arm64" ./cmd/sorter

build-windows: | frontend
	GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o "$(BINARY)-windows-amd64.exe" ./cmd/sorter
