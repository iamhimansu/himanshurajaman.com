.PHONY: dev build server run admin clean

# Dev: Go API on :8080 (local sample content) + Vite dev server on :5173.
# Vite proxies /api and /images to :8080 (see web/vite.config.ts), so the React
# app uses the same relative URLs it will use in production.
dev:
	@trap 'kill 0' INT TERM EXIT; \
	CONTENT_SOURCE=local CONTENT_LOCAL_DIR=./sample-content go run ./cmd/server & \
	( cd web && npm run dev ) & \
	wait

# Production build. ORDER MATTERS: build the frontend FIRST so web/dist exists,
# then `go build` embeds it via //go:embed all:dist in web/embed.go.
build:
	cd web && npm ci && npm run build
	CGO_ENABLED=0 go build ./...

# Build a single deployable server binary (frontend first) to bin/server.
server:
	cd web && npm ci && npm run build
	CGO_ENABLED=0 go build -o bin/server ./cmd/server

# Run the server against the local sample content. Requires web/dist to exist
# (run `make build` or `cd web && npm run build` first) because go:embed reads
# it at compile time.
run:
	CONTENT_SOURCE=local CONTENT_LOCAL_DIR=./sample-content go run ./cmd/server

clean:
	rm -rf web/dist web/node_modules bin

# Admin dev: load secrets from the gitignored .env.local (copy from
# .env.local.example), then run with the /admin CMS enabled. Requires the
# frontend built first (make build), since go:embed reads web/dist.
admin:
	@test -f .env.local || { echo "Missing .env.local — copy .env.local.example to .env.local and fill it in"; exit 1; } && set -a && . ./.env.local && set +a && go run ./cmd/server
