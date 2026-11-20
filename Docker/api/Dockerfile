FROM golang:1.22-alpine AS builder

WORKDIR /src

# Copy module definition using bracket JSON array syntax for paths with spaces
COPY ["API + MongoDB/go.mod", "API + MongoDB/go.sum", "./"]
RUN go mod download || true

# Copy source code
COPY ["API + MongoDB/", "./"]
RUN go mod tidy || true
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/api ./cmd

# Final minimal runtime image with OpenSSH client and rsync
FROM alpine:3.20

WORKDIR /app

RUN apk --no-cache add ca-certificates wget rsync openssh-client bash

COPY --from=builder /bin/api /app/api

# Create staging directory for uploads
RUN mkdir -p /var/lib/mission1/staging && chmod 700 /var/lib/mission1/staging

ENV API_PORT=8080
ENV API_ENV=production
ENV MONGO_URI="mongodb://mongo:27017/cdn_metadata?replicaSet=rs0&directConnection=true"
ENV MONGO_DB=mini_cdn
ENV SSH_KEY_PATH=/keys/storage_test_key
ENV KNOWN_HOSTS_PATH=/app/cdn_known_hosts
ENV STAGING_DIR=/var/lib/mission1/staging
ENV ADMIN_API_KEY=admin_dev_secret_key_123

EXPOSE 8080

HEALTHCHECK --interval=5s --timeout=3s --retries=3 CMD wget -q -O - http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/api"]
