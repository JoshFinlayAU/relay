# --- web build stage ---
FROM node:22-bookworm AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- go build stage ---
# Must match go.mod (go 1.25); the 1.23 image cannot build this module.
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Bring in the SPA built above so go:embed finds it.
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /relayd ./cmd/relayd

# --- runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /relayd /relayd
USER nonroot:nonroot
EXPOSE 8080 80 443 587 465 25
ENTRYPOINT ["/relayd"]
