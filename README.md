# HookRelay

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16+-336791?style=flat&logo=postgresql)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7+-DC382D?style=flat&logo=redis)](https://redis.io/)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat&logo=docker)](https://www.docker.com/)

**HookRelay** is a production-grade, high-throughput webhook delivery and event routing engine written in Go. It implements the **Transactional Outbox Pattern** over PostgreSQL, an asynchronous **Redis-backed Worker Pool**, **HMAC-SHA256 signature verification**, and intelligent **exponential backoff with jitter** to guarantee at-least-once webhook delivery without data loss.

---

## Table of Contents

- [Key Features](#key-features)
- [System Architecture](#system-architecture)
- [Tech Stack](#tech-stack)
- [Project Layout](#project-layout)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Method 1: Docker Compose (Quickest)](#method-1-docker-compose-quickest)
  - [Method 2: Local Native Setup](#method-2-local-native-setup)
- [Configuration Reference](#configuration-reference)
- [API Reference](#api-reference)
  - [Health Check](#health-check)
  - [Subscriptions API](#subscriptions-api)
  - [Events API](#events-api)
  - [Deliveries API](#deliveries-api)
- [Webhook Security & Signature Verification](#webhook-security--signature-verification)
- [Retry Policy & Dead Lettering](#retry-policy--dead-lettering)
- [Database Migrations](#database-migrations)

---

## Key Features

- **Transactional Outbox Pattern**: Ingested events, matched subscriptions, deliveries, and outbox entries are committed atomically in a single PostgreSQL transaction. Even if the network or process crashes immediately after ingestion, zero events are lost.
- **Decoupled Architecture**:
  - `api`: Fast HTTP service handling event ingestion, subscription management, and delivery queries.
  - `dispatcher`: Lightweight poller that streams pending deliveries from PostgreSQL outbox into Redis.
  - `worker`: Concurrent worker pool pulling delivery jobs from Redis, signing payloads, and executing HTTP requests.
- **HMAC-SHA256 Signatures**: Every outgoing webhook request includes a timestamped cryptographic signature (`X-HookRelay-Signature: t=<timestamp>,v1=<sig>`) to prevent payload tampering and replay attacks.
- **Exponential Backoff with Full Jitter**: Automatic retries for transient failures (HTTP 408, 429, 5xx, or network drops) up to 8 attempts, with randomized jitter to prevent thundering herds against destination servers.
- **Dead Letter Queue (DLQ)**: Non-retryable client errors (HTTP 4xx except 408/429) or deliveries that exhaust all retries are flagged as `dead_letter` for manual inspection.
- **Full Delivery & Attempt Audit Trail**: Every attempt logs the HTTP response status, duration in milliseconds, timestamp, and error details, queryable via the REST API.
- **Idempotency Key Support**: Optional `Idempotency-Key` header on event publishing to prevent duplicate event ingestion.
- **Security Hardened Containers**: Multi-stage Alpine container runs as a non-privileged `app` user with a read-only root filesystem and `no-new-privileges`.

---

## System Architecture

```mermaid
sequenceDiagram
    autonumber
    actor Client as Event Producer
    participant API as HookRelay API
    participant DB as PostgreSQL
    participant Dispatcher as HookRelay Dispatcher
    participant Redis as Redis Queue
    participant Worker as HookRelay Worker
    actor Webhook as Subscriber Endpoint

    %% 1. Ingestion
    Client->>API: POST /api/v1/events (type, data)
    Note over API,DB: Transactional Outbox (Single ACID Tx)
    API->>DB: INSERT events<br/>MATCH active subscriptions<br/>INSERT deliveries (status='pending')<br/>INSERT outbox
    API-->>Client: 201 Created (event_id)

    %% 2. Dispatching
    loop Every 2s Polling
        Dispatcher->>DB: SELECT unpublished FROM outbox LIMIT 100
        Dispatcher->>Redis: RPUSH deliveries (delivery_id)
        Dispatcher->>DB: UPDATE outbox SET published_at = NOW()
    end

    %% 3. Worker Delivery
    Worker->>Redis: BLPOP deliveries
    Worker->>DB: UPDATE deliveries SET status='processing'
    Worker->>DB: Fetch event payload & subscriber secret
    Worker->>Worker: Compute HMAC-SHA256 signature
    Worker->>Webhook: POST subscriber_url (with X-HookRelay-Signature)

    %% 4. Response & Retries
    alt HTTP 2xx Success
        Worker->>DB: Record attempt & UPDATE deliveries (status='delivered')
    else HTTP 5xx / 429 / Network Failure (Attempts < 8)
        Worker->>DB: Record attempt & UPDATE deliveries (status='retry_waiting', next_attempt_at)
    else Fatal Error or Max Attempts Reached
        Worker->>DB: Record attempt & UPDATE deliveries (status='dead_letter')
    end
```

---

## Tech Stack

| Component | Technology | Description |
| :--- | :--- | :--- |
| **Language** | [Go 1.22+](https://go.dev/) | Clean architecture, standard library `net/http` and `log/slog` |
| **Database** | [PostgreSQL 16+](https://www.postgresql.org/) | ACID transactions, JSONB payloads, and indexing using `pgx/v5` |
| **Queue / Broker** | [Redis 7+](https://redis.io/) | In-memory FIFO queue for workers via `go-redis/v9` |
| **Migrations** | [golang-migrate](https://github.com/golang-migrate/migrate) | Versioned SQL up/down migrations |
| **Containers** | [Docker](https://www.docker.com/) & Docker Compose | Multi-stage builder, minimal Alpine runtime, read-only rootfs |
| **Observability** | `log/slog` | Structured JSON and human-readable logging with file rotation |

---

## Project Layout

```text
hookrelay/
├── cmd/
│   ├── api/              # API HTTP server entrypoint
│   ├── dispatcher/       # Outbox publisher service entrypoint
│   └── worker/           # Background delivery worker pool entrypoint
├── internal/
│   ├── adapters/
│   │   ├── httpclient/   # Outbound webhook sender & HMAC signing logic
│   │   ├── postgres/     # PostgreSQL repository implementations (pgx/v5)
│   │   └── redis/        # Redis queue adapter (go-redis/v9)
│   ├── application/
│   │   ├── deliveries/   # Webhook delivery orchestration & retry logic
│   │   ├── events/       # Event ingestion & validation service
│   │   └── subscriptions/# Subscriber lifecycle service
│   ├── config/           # Environment configuration loader
│   ├── dispatcher/       # Outbox polling and Redis publishing
│   ├── domain/           # Core domain models (Event, Subscription, Delivery)
│   ├── observability/    # Structured logger initialization (log/slog)
│   ├── ports/            # Repository and driver interfaces (Hexagonal architecture)
│   ├── transport/
│   │   └── httpapi/      # HTTP handlers, middleware, request routing
│   └── worker/           # Worker pool implementation
├── migrations/           # Raw SQL database migrations (001-005)
├── compose.yaml          # Multi-container local orchestration
├── Dockerfile            # Multi-stage production container build
├── .env.example          # Local environment variables template
└── .docker.env.example   # Docker Compose environment variables template
```

---

## Getting Started

### Prerequisites

- **Docker** (version 24.0+) & **Docker Compose** (version 2.20+)
  *or for local native execution:*
- **Go** (version 1.22+)
- **PostgreSQL** (version 15+)
- **Redis** (version 7+)
- **golang-migrate** CLI (optional, if running migrations manually)

---

### Method 1: Docker Compose (Quickest)

Docker Compose starts PostgreSQL, Redis, runs all database migrations automatically, and starts `api`, `dispatcher`, and `worker`.

1. **Clone the repository:**
   ```bash
   git clone https://github.com/purangoswami2001/hookrelay.git
   cd hookrelay
   ```

2. **Create the environment file:**
   ```bash
   cp .docker.env.example docker.env
   ```

3. **Start all services:**
   ```bash
   docker compose up -d --build
   ```

4. **Verify running containers:**
   ```bash
   docker compose ps
   ```
   You should see:
   - `postgres` (healthy on `127.0.0.1:5434`)
   - `redis` (healthy on `127.0.0.1:6379`)
   - `migrate` (completed successfully)
   - `api` (healthy on `0.0.0.0:8080`)
   - `dispatcher` (running)
   - `worker` (running)

5. **Test the health endpoint:**
   ```bash
   curl -i http://localhost:8080/health
   ```
   Expected response:
   ```http
   HTTP/1.1 200 OK
   Content-Type: application/json
   {"status":"ok"}
   ```

6. **View live logs:**
   ```bash
   docker compose logs -f api worker dispatcher
   ```

7. **Tear down:**
   ```bash
   docker compose down
   # To also erase database & redis volumes:
   docker compose down -v
   ```

---

### Method 2: Local Native Setup

If you prefer developing directly on your host machine:

1. **Start backing services (PostgreSQL & Redis):**
   You can run local instances or spin up only the databases via Docker:
   ```bash
   docker compose up -d postgres redis
   ```

2. **Configure your `.env`:**
   ```bash
   cp .env.example .env
   ```
   Ensure `DATABASE_URL` and `REDIS_ADDR` match your local instances:
   ```env
   APP_ENV=development
   LOG_LEVEL=DEBUG
   LOG_FILE=logs/hookrelay.log
   HTTP_ADDR=:8080
   WORKER_COUNT=5
   DATABASE_URL=postgres://hookrelay:hookrelay_dev_password@localhost:5434/hookrelay?sslmode=disable
   REDIS_ADDR=localhost:6379
   ```

3. **Apply database migrations:**
   Using the `golang-migrate` CLI:
   ```bash
   migrate -path ./migrations -database "$DATABASE_URL" up
   ```
   *(Or using Docker migrate container: `docker compose up migrate`)*

4. **Run the services in separate terminal windows:**
   - **Terminal 1 (API Server):**
     ```bash
     export $(cat .env | xargs) && go run ./cmd/api
     ```
   - **Terminal 2 (Outbox Dispatcher):**
     ```bash
     export $(cat .env | xargs) && go run ./cmd/dispatcher
     ```
   - **Terminal 3 (Worker Pool):**
     ```bash
     export $(cat .env | xargs) && go run ./cmd/worker
     ```

---

## Configuration Reference

HookRelay is configured entirely via environment variables:

| Variable | Default Value | Description |
| :--- | :--- | :--- |
| `APP_ENV` | `development` | Environment mode (`development`, `production`, `test`) |
| `LOG_LEVEL` | `DEBUG` | Logging level (`DEBUG`, `INFO`, `WARN`, `ERROR`) |
| `LOG_FILE` | `logs/hookrelay.log` | Path to persistent log file (leave blank to log only to stdout) |
| `HTTP_ADDR` | `:8080` | Bind address and port for the API server |
| `WORKER_COUNT` | `5` | Number of concurrent delivery workers per worker process |
| `DATABASE_URL` | *Required* | PostgreSQL connection string (`postgres://user:pass@host:port/dbname?sslmode=disable`) |
| `REDIS_ADDR` | `localhost:6379` | Host and port of the Redis broker instance |

---

## API Reference

Base URL: `http://localhost:8080`

### Authentication & Multi-Tenancy

Protected endpoints require an API key passed in the request header via either:
- `Authorization: Bearer <your_api_key>`
- `X-API-Key: <your_api_key>`

Each user's subscriptions, events, and deliveries are strictly isolated and scoped to their account.

---

### Users & Authentication API

#### 1. Register a New User / Customer
Registers a new customer account and generates a high-entropy API key.
```http
POST /api/v1/users/register
Content-Type: application/json
```
**Request Body:**
```json
{
  "email": "customer@company.com",
  "name": "Acme Corp"
}
```
**Response (201 Created):**
> [!IMPORTANT]
> The `api_key` is displayed **only once** upon registration or regeneration. Store it securely in your secrets manager or environment configuration.
```json
{
  "user": {
    "id": "e3b0c442-98fc-1c14-9afb-4c7c66424400",
    "email": "customer@company.com",
    "name": "Acme Corp",
    "api_key_prefix": "hr_live_9f8b7c6d...",
    "created_at": "2026-10-20T17:00:00Z",
    "updated_at": "2026-10-20T17:00:00Z"
  },
  "api_key": "hr_live_9f8b7c6d5e4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c",
  "message": "Store your API key securely. It will never be shown again."
}
```

#### 2. Get Current User Profile
Retrieves the authenticated user's profile and key prefix.
```http
GET /api/v1/users/me
Authorization: Bearer hr_live_...
```
**Response (200 OK):**
```json
{
  "user": {
    "id": "e3b0c442-98fc-1c14-9afb-4c7c66424400",
    "email": "customer@company.com",
    "name": "Acme Corp",
    "api_key_prefix": "hr_live_9f8b7c6d...",
    "created_at": "2026-10-20T17:00:00Z",
    "updated_at": "2026-10-20T17:00:00Z"
  }
}
```

#### 3. Regenerate API Key
Immediately terminates the current API key and issues a new one. The old key is instantly revoked and can no longer authenticate.
```http
POST /api/v1/users/me/regenerate-key
Authorization: Bearer hr_live_...
```
**Response (200 OK):**
```json
{
  "message": "API key regenerated successfully. Previous key has been terminated.",
  "api_key": "hr_live_new_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "api_key_prefix": "hr_live_01234567..."
}
```

---

### Health Check

#### Check system health (Public)
```http
GET /health
```
**Response (200 OK):**
```json
{
  "status": "ok",
  "service": "hookrelay"
}
```

---

### Subscriptions API

Subscriptions represent destination endpoints interested in specific event types.

#### 1. Create a Subscription
```http
POST /api/v1/subscriptions
Content-Type: application/json
```
**Request Body:**
```json
{
  "url": "https://webhook.site/your-custom-uuid",
  "event_types": ["payment.succeeded", "invoice.created"]
}
```
**Response (201 Created):**
> [!IMPORTANT]
> The `secret` is generated randomly (32 cryptographically secure bytes, base64url encoded) and returned **only once** upon creation. Store it securely to verify signatures.
```json
{
  "subscription": {
    "id": "7ca64731-0306-4447-909d-cbbe6a048705",
    "url": "https://webhook.site/your-custom-uuid",
    "event_types": ["payment.succeeded", "invoice.created"],
    "active": true,
    "created_at": "2026-10-17T11:40:00Z",
    "updated_at": "2026-10-17T11:40:00Z"
  },
  "secret": "G3l4Yy9wXv7K1bB2cN3dE4fG5hJ6kL7mN8pQ9rS0tU"
}
```

#### 2. List Subscriptions
```http
GET /api/v1/subscriptions
```
**Response (200 OK):**
```json
{
  "data": [
    {
      "id": "7ca64731-0306-4447-909d-cbbe6a048705",
      "url": "https://webhook.site/your-custom-uuid",
      "event_types": ["payment.succeeded", "invoice.created"],
      "active": true,
      "created_at": "2026-10-17T11:40:00Z",
      "updated_at": "2026-10-17T11:40:00Z"
    }
  ]
}
```

#### 3. Get Subscription by ID
```http
GET /api/v1/subscriptions/{id}
```
**Response (200 OK):**
```json
{
  "id": "7ca64731-0306-4447-909d-cbbe6a048705",
  "url": "https://webhook.site/your-custom-uuid",
  "event_types": ["payment.succeeded", "invoice.created"],
  "active": true,
  "created_at": "2026-10-17T11:40:00Z",
  "updated_at": "2026-10-17T11:40:00Z"
}
```

#### 4. Update Subscription
```http
PATCH /api/v1/subscriptions/{id}
Content-Type: application/json
```
**Request Body (all fields optional):**
```json
{
  "url": "https://webhook.site/new-endpoint",
  "event_types": ["payment.*"],
  "active": false
}
```
**Response (200 OK):**
```json
{
  "id": "7ca64731-0306-4447-909d-cbbe6a048705",
  "url": "https://webhook.site/new-endpoint",
  "event_types": ["payment.*"],
  "active": false,
  "created_at": "2026-10-17T11:40:00Z",
  "updated_at": "2026-10-17T11:48:12Z"
}
```

#### 5. Delete Subscription
```http
DELETE /api/v1/subscriptions/{id}
```
**Response:** `204 No Content`

---

### Events API

Events represent system actions that trigger webhook deliveries.

#### 1. Publish an Event
```http
POST /api/v1/events
Content-Type: application/json
Idempotency-Key: pay_req_992144 (optional)
```
**Request Body:**
```json
{
  "type": "payment.succeeded",
  "data": {
    "order_id": "ord_10293",
    "amount": 99.50,
    "currency": "USD",
    "customer": {
      "id": "cust_882",
      "email": "customer@example.com"
    }
  }
}
```
**Response (201 Created):**
```json
{
  "id": "b3e09887-b1ef-4fe1-ba55-fc4294b05537",
  "event_type": "payment.succeeded",
  "payload": {
    "order_id": "ord_10293",
    "amount": 99.50,
    "currency": "USD",
    "customer": {
      "id": "cust_882",
      "email": "customer@example.com"
    }
  },
  "idempotency_key": "pay_req_992144",
  "created_at": "2026-10-17T11:50:00Z"
}
```

#### 2. List Events
```http
GET /api/v1/events?limit=50&offset=0
Authorization: Bearer hr_live_...
```
**Response (200 OK):**
```json
{
  "data": [
    {
      "id": "b3e09887-b1ef-4fe1-ba55-fc4294b05537",
      "user_id": "e3b0c442-98fc-1c14-9afb-4c7c66424400",
      "type": "payment.succeeded",
      "data": {
        "order_id": "ord_10293",
        "amount": 99.50,
        "currency": "USD"
      },
      "created_at": "2026-10-17T11:50:00Z"
    }
  ]
}
```

#### 3. Get Event by ID
```http
GET /api/v1/events/{id}
Authorization: Bearer hr_live_...
```
**Response (200 OK):**
```json
{
  "id": "b3e09887-b1ef-4fe1-ba55-fc4294b05537",
  "event_type": "payment.succeeded",
  "payload": {
    "order_id": "ord_10293",
    "amount": 99.50,
    "currency": "USD"
  },
  "idempotency_key": "pay_req_992144",
  "created_at": "2026-10-17T11:50:00Z"
}
```

---

### Deliveries API

Deliveries track the state and execution attempts of each webhook dispatched to a subscriber (scoped to the authenticated user).

#### 1. List Deliveries
Supports optional query parameters for filtering and pagination:
- `event_id`: Filter deliveries generated by a specific event UUID.
- `subscription_id`: Filter deliveries targeted to a specific subscription UUID.
- `limit`: Number of records to return (default: `50`, maximum: `100`).
- `offset`: Offset index for pagination (default: `0`).

```http
GET /api/v1/deliveries?limit=10&offset=0
GET /api/v1/deliveries?event_id=b3e09887-b1ef-4fe1-ba55-fc4294b05537
GET /api/v1/deliveries?subscription_id=7ca64731-0306-4447-909d-cbbe6a048705
Authorization: Bearer hr_live_...
```
**Response (200 OK):**
```json
{
  "data": [
    {
      "id": "c71a3964-b054-46f9-b883-ec7d2cebb918",
      "user_id": "e3b0c442-98fc-1c14-9afb-4c7c66424400",
      "event_id": "b3e09887-b1ef-4fe1-ba55-fc4294b05537",
      "subscription_id": "7ca64731-0306-4447-909d-cbbe6a048705",
      "status": "delivered",
      "attempt_count": 1,
      "next_attempt_at": null,
      "last_http_status": 200,
      "last_error": "",
      "created_at": "2026-10-17T11:50:01Z",
      "updated_at": "2026-10-17T11:50:02Z"
    }
  ]
}
```

#### 2. Get Delivery Details and Attempt History
```http
GET /api/v1/deliveries/{id}
Authorization: Bearer hr_live_...
```
**Response (200 OK):**
```json
{
  "id": "c71a3964-b054-46f9-b883-ec7d2cebb918",
  "event_id": "b3e09887-b1ef-4fe1-ba55-fc4294b05537",
  "subscription_id": "7ca64731-0306-4447-909d-cbbe6a048705",
  "status": "delivered",
  "attempt_count": 2,
  "next_attempt_at": null,
  "last_http_status": 200,
  "last_error": "",
  "created_at": "2026-10-17T11:50:01Z",
  "updated_at": "2026-10-17T11:50:08Z",
  "attempts": [
    {
      "id": "e91244e8-8a8b-4c55-b040-57f9ea11e134",
      "delivery_id": "c71a3964-b054-46f9-b883-ec7d2cebb918",
      "attempt_number": 1,
      "http_status": 503,
      "duration_ms": 320,
      "error": "HTTP status 503",
      "created_at": "2026-10-17T11:50:02Z"
    },
    {
      "id": "f03451c2-9b9c-4d66-c151-68a0fb22f245",
      "delivery_id": "c71a3964-b054-46f9-b883-ec7d2cebb918",
      "attempt_number": 2,
      "http_status": 200,
      "duration_ms": 115,
      "error": "",
      "created_at": "2026-10-17T11:50:08Z"
    }
  ]
}
```

---

## Webhook Security & Signature Verification

Every webhook request sent by HookRelay includes the following custom headers:

| Header | Description | Example |
| :--- | :--- | :--- |
| `Content-Type` | MIME payload type | `application/json` |
| `User-Agent` | Client identifier | `HookRelay/1.0` |
| `X-HookRelay-Event-ID` | ID of the event being relayed | `b3e09887-b1ef-4fe1-ba55-fc4294b05537` |
| `X-HookRelay-Delivery` | ID of this delivery execution | `c71a3964-b054-46f9-b883-ec7d2cebb918` |
| `X-HookRelay-Timestamp`| Unix timestamp in seconds | `1792237800` |
| `X-HookRelay-Signature`| Signature format `t=<timestamp>,v1=<signature>` | `t=1792237800,v1=9f83ab...` |

### How It Works
The signature is created using **HMAC-SHA256**:
1. Take the timestamp string from `X-HookRelay-Timestamp` (e.g. `"1792237800"`).
2. Append a period `.` and the raw request body bytes: `"{timestamp}.{raw_body}"`.
3. Compute the HMAC-SHA256 hash using the subscription's `secret`.
4. Hex-encode the resulting bytes and format as `t={timestamp},v1={hex_hash}`.

### Verification Examples

#### Go
```go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

func VerifySignature(secret string, payload []byte, sigHeader string) error {
	var timestamp, expectedSig string
	for _, part := range strings.Split(sigHeader, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			switch kv[0] {
			case "t":
				timestamp = kv[1]
			case "v1":
				expectedSig = kv[1]
			}
		}
	}

	if timestamp == "" || expectedSig == "" {
		return errors.New("missing timestamp or signature in header")
	}

	message := timestamp + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	calculatedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(calculatedSig), []byte(expectedSig)) {
		return errors.New("invalid signature")
	}

	return nil
}
```

#### Node.js / Express
```javascript
const crypto = require('crypto');

function verifyWebhook(secret, rawBody, signatureHeader) {
  const parts = signatureHeader.split(',');
  const timestamp = parts.find(p => p.startsWith('t='))?.split('=')[1];
  const receivedSig = parts.find(p => p.startsWith('v1='))?.split('=')[1];

  if (!timestamp || !receivedSig) {
    throw new Error('Malformed signature header');
  }

  const message = `${timestamp}.${rawBody}`;
  const calculatedSig = crypto
    .createHmac('sha256', secret)
    .update(message)
    .digest('hex');

  const trusted = Buffer.from(calculatedSig, 'hex');
  const untrusted = Buffer.from(receivedSig, 'hex');

  if (trusted.length !== untrusted.length || !crypto.timingSafeEqual(trusted, untrusted)) {
    throw new Error('Invalid signature');
  }

  return true;
}
```

#### Python
```python
import hmac
import hashlib

def verify_webhook(secret: str, raw_body: bytes, sig_header: str) -> bool:
    pairs = dict(item.split("=", 1) for item in sig_header.split(","))
    timestamp = pairs.get("t")
    received_sig = pairs.get("v1")

    if not timestamp or not received_sig:
        return False

    message = f"{timestamp}.".encode("utf-8") + raw_body
    computed_sig = hmac.new(
        secret.encode("utf-8"),
        message,
        hashlib.sha256
    ).hexdigest()

    return hmac.compare_digest(computed_sig, received_sig)
```

---

## Retry Policy & Dead Lettering

### Retry Schedule
HookRelay automatically retries failed deliveries using exponential backoff with full jitter:
- **Max Attempts**: `8`
- **Base Delay**: `2 seconds`
- **Max Delay**: `30 minutes`
- **Backoff Formula**: `delay = min(BaseDelay * 2^(attempt-1), MaxDelay)` with randomized equal jitter.

### Retry Rules
| Response / Condition | Behavior | Reason |
| :--- | :--- | :--- |
| **HTTP 2xx** | Marked `delivered` | Success |
| **HTTP 408 / 429** | Scheduled for retry | Request Timeout / Rate Limited |
| **HTTP 5xx** | Scheduled for retry | Destination Server Error |
| **Network Failure / Timeout** | Scheduled for retry | Connection reset, DNS failure, TCP dial timeout |
| **HTTP 4xx** (except 408/429) | Moved to `dead_letter` | Bad Request, Not Found, Forbidden (fatal) |
| **Exhausted 8 attempts** | Moved to `dead_letter` | Max retry limit reached |

---

## Database Migrations

Database schema changes are managed with standard SQL files located in `migrations/`:

| Version | Migration File | Description |
| :--- | :--- | :--- |
| **001** | `001_create_subscriptions` | Webhook endpoints, subscribed topics/event types, secrets |
| **002** | `002_create_events` | Raw events, JSONB payload, idempotency keys |
| **003** | `003_create_deliveries` | Delivery tracking, execution state, retry timestamps |
| **004** | `004_create_delivery_attempts`| Detailed log for every HTTP attempt, status, and duration |
| **005** | `005_create_outbox` | Transactional outbox table for reliable Redis dispatching |

To execute migrations with `golang-migrate`:
```bash
# Run all pending migrations
migrate -path ./migrations -database "$DATABASE_URL" up

# Roll back the most recent migration
migrate -path ./migrations -database "$DATABASE_URL" down 1
```
