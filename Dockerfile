# syntax=docker/dockerfile:1

# Multi-stage build for himanshurajaman.com: one static, CGO-free Go binary that
# embeds the built Vite SPA. Build order matters — the frontend (web/dist) must
# exist before `go build`, because web/embed.go embeds it with //go:embed
# all:dist. Render builds linux/amd64, so the Go stage targets amd64.

# ---- Stage 1: build the React/Vite frontend -> web/dist ---------------------
FROM node:22-slim AS web
WORKDIR /app/web
# Install dependencies against the lockfile first for better layer caching.
COPY web/package.json web/package-lock.json ./
RUN npm ci
# Then the frontend sources, and build (tsc + vite build -> /app/web/dist).
COPY web/ ./
RUN npm run build

# ---- Stage 2: build the static Go server binary ----------------------------
FROM golang:1.26-bookworm AS build
WORKDIR /src
# Module graph first (cached unless go.mod/go.sum change).
COPY go.mod go.sum ./
RUN go mod download
# Server sources (web/ brings in embed.go + the Vite project sources; its local
# dist/ and node_modules are excluded by .dockerignore and overlaid next).
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/ ./web/
# Overlay the built SPA so //go:embed all:dist finds it at compile time.
COPY --from=web /app/web/dist ./web/dist
# CGO-free static binary targeting the amd64 runtime Render provides. -trimpath
# and -ldflags "-s -w" keep the binary small and reproducible.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- Stage 3: minimal non-root runtime -------------------------------------
# distroless/static ships CA certificates (needed for HTTPS to GitHub / Google /
# email) and a nonroot user (uid 65532). No shell, no package manager, tiny image.
FROM gcr.io/distroless/static-debian12:nonroot
# The app honors $PORT (Render injects it; default 8080). EXPOSE is documentation
# only — the port is NOT hardcoded anywhere in the image.
EXPOSE 8080
COPY --from=build /out/server /server
USER nonroot:nonroot
ENTRYPOINT ["/server"]
