
# Build stage
FROM golang:1.27.1-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" \
    -o /out/api ./cmd/api \
    && go build -trimpath -ldflags="-s -w" \
    -o /out/dispatcher ./cmd/dispatcher \
    && go build -trimpath -ldflags="-s -w" \
    -o /out/worker ./cmd/worker

# Runtime stage
FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S app \
    && adduser -S -G app app

WORKDIR /app

COPY --from=builder --chown=app:app /out/api /app/api
COPY --from=builder --chown=app:app /out/dispatcher /app/dispatcher
COPY --from=builder --chown=app:app /out/worker /app/worker

USER app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/app/api"]
