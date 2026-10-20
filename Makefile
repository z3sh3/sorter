BINARY := sorter
DIST   := internal/server/dist

.PHONY: all build frontend backend clean dev

all: build

# One-shot build: frontend -> embed -> single binary
build: frontend backend

frontend:
	cd web && pnpm install && pnpm run build
	rm -rf $(DIST)
	cp -r web/dist $(DIST)

backend:
	go build -ldflags "-s -w" -o $(BINARY) ./cmd/sorter

# Development helpers (backend uses the placeholder dist)
dev-backend:
	go run ./cmd/sorter -config config.yaml

dev-frontend:
	cd web && pnpm run dev

clean:
	rm -f $(BINARY)
	rm -rf web/dist web/node_modules
# Cross-compile examples (pure Go, no CGO needed)
build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o $(BINARY)-linux-arm64 ./cmd/sorter

build-windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o $(BINARY)-windows-amd64.exe ./cmd/sorter
