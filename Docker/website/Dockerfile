FROM golang:alpine AS builder

WORKDIR /src

# Copy module definition
COPY Website/go.mod Website/go.sum* ./
RUN go mod download || true

# Copy source code
COPY Website/ ./
RUN go mod tidy || true
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/website ./cmd

# Final minimal runtime image
FROM alpine:3.20

WORKDIR /app

# Install ca-certificates and curl/wget for healthchecks
RUN apk --no-cache add ca-certificates wget

# Copy binary and UI assets
COPY --from=builder /bin/website /app/website
COPY Website/templates /app/templates
COPY Website/static /app/static

ENV WEBSITE_PORT=3000
ENV API_BASE_URL=http://api:8080

EXPOSE 3000

HEALTHCHECK --interval=5s --timeout=3s --retries=3 CMD wget -q -O - http://localhost:3000/health || exit 1

ENTRYPOINT ["/app/website"]
