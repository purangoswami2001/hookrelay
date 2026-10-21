# HookRelay

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16+-336791?style=flat&logo=postgresql)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7+-DC382D?style=flat&logo=redis)](https://redis.io/)
[![Prometheus](https://img.shields.io/badge/Prometheus-Metrics-E6522C?style=flat&logo=prometheus)](https://prometheus.io/)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![Security](https://img.shields.io/badge/Security-SSRF%20Hardened-green?style=flat&logo=shield)](https://owasp.org/www-community/attacks/Server_Side_Request_Forgery)

**HookRelay** is a production-ready, distributed webhook delivery engine and event routing system written in Go. Designed for platforms requiring guaranteed delivery, HookRelay pairs the **Transactional Outbox Pattern** on PostgreSQL with an asynchronous **Redis-backed Worker Pool**, **Socket-Level SSRF & DNS Rebinding Protection**, **Redis ZSET Delayed Scheduling**, **HMAC-SHA256 signature verification**, **Prometheus observability**, and operational **Dead-Letter Queue (DLQ) self-service replay APIs**.

---

## Table of Contents

- [Key Architecture Highlights](#key-architecture-highlights)
- [Core Features Deep-Dive](#core-features-deep-dive)
  - [1. SSRF & DNS Rebinding Mitigation](#1-ssrf--dns-rebinding-mitigation)
  - [2. Redis ZSET Delayed Retry Scheduling](#2-redis-zset-delayed-retry-scheduling)
  - [3. Operational DLQ Management (Retry & Batch Replay)](#3-operational-dlq-management-retry--batch-replay)
  - [4. Prometheus Observability & Metrics](#4-prometheus-observability--metrics)
  - [5. Official Client SDK: pkg/verifier](#5-official-client-sdk-pkgverifier)
- [System Architecture & Sequence Diagram](#system-architecture--sequence-diagram)
- [Tech Stack](#tech-stack)
- [Project Directory Tree](#project-directory-tree)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Method 1: Docker Compose (Recommended)](#method-1-docker-compose-recommended)
  - [Method 2: Local Native Execution](#method-2-local-native-execution)
- [Configuration Reference](#configuration-reference)
- [Complete API Reference](#complete-api-reference)
  - [Authentication & Multi-Tenancy](#authentication--multi-tenancy)
  - [Health & Metrics](#health--metrics)
  - [Users & Authentication API](#users--authentication-api)
  - [Subscriptions API](#subscriptions-api)
  - [Events API](#events-api)
  - [Deliveries & DLQ Management API](#deliveries--dlq-management-api)
- [Webhook Verification Guide](#webhook-verification-guide)
  - [Using Go (`pkg/verifier`)](#using-go-pkgverifier)
  - [Using Node.js / Express](#using-nodejs--express)
  - [Using Python](#using-python)
- [Retry Policy & Failure Modes](#retry-policy--failure-modes)
- [Database Migrations](#database-migrations)
- [Postman Collection](#postman-collection)

---

## Key Architecture Highlights

- **Zero Event Loss Guarantee (Transactional Outbox)**: Ingested events, matched subscriptions, deliveries, and outbox logs are committed atomically in a single PostgreSQL transaction. Even if the network drops or the process crashes mid-ingestion, zero events or webhooks are lost.
- **Kernel-Level SSRF & DNS Rebinding Defense**: Outbound webhook HTTP requests utilize custom `net.Dialer.Control` socket controls that block loopback, link-local, private RFC 1918 networks, and Cloud Instance Metadata Services (IMDS `169.254.169.254`), preventing Time-of-Check to Time-of-Use (TOCTOU) DNS rebinding attacks.
- **Automated Delayed Retries with Redis Sorted Sets (ZSET)**: Failed deliveries are scheduled directly into Redis ZSETs (`hookrelay:deliveries:delayed`) scored by Unix execution timestamp. An atomic Lua script promotes due retries into the active queue with zero polling overhead on PostgreSQL.
- **Self-Service DLQ Recovery**: Operational REST endpoints (`POST /api/v1/deliveries/{id}/retry` and `POST /api/v1/deliveries/replay`) empower customers and operators to instantly redeliver dead-lettered webhooks after downstream outage resolution.
- **Prometheus Observability**: Native `/metrics` endpoint exports event ingestion volume, delivery latency histograms, HTTP status codes, queue depth, and promotion metrics.
- **Multi-Tenant Isolation**: Cryptographic SHA-256 API key authentication guarantees strict isolation of events, endpoints, and audit logs across tenants.
- **HMAC-SHA256 Signatures with Anti-Replay Drift Tolerance**: Every webhook is signed with the subscriber's private secret and timestamped. Consumers can verify authenticity using the included `pkg/verifier` Go SDK.

---

## Core Features Deep-Dive

### 1. SSRF & DNS Rebinding Mitigation

Allowing users to register arbitrary webhook URLs introduces serious Server-Side Request Forgery (SSRF) vulnerabilities. Naive validation that checks hostnames or resolves DNS prior to the HTTP request is vulnerable to **DNS Rebinding (TOCTOU)**: an attacker's DNS server can return a public IP during registration, and immediately switch to `169.254.169.254` (AWS/GCP/Azure IMDS) or `127.0.0.1` upon webhook dispatch.

HookRelay implements **multi-tier defense-in-depth**:
1. **Registration-Time Pre-Validation**: Rejects invalid schemes (only `http` and `https`), prohibited hostnames (`localhost`), and literal private/loopback/IMDS IP addresses.
2. **Socket-Level Connection Filter (`net.Dialer.Control`)**: The outbound HTTP transport controls the socket *after* DNS resolution is complete and immediately *before* the TCP connection handshake occurs:
   ```go
   // internal/adapters/httpclient/ssrf.go
   Control: func(network, address string, c syscall.RawConn) error {
       host, _, _ := net.SplitHostPort(address)
       ip := net.ParseIP(host)
       if IsBlockedIP(ip) {
           return fmt.Errorf("SSRF protection: connection to restricted IP %s is blocked", ip)
       }
       return nil
   }
   ```
   **Blocked Subnets:**
   - Loopback: `127.0.0.0/8`, `::1/128`
   - Cloud IMDS: `169.254.0.0/16` (AWS, Google Cloud, Azure, DigitalOcean)
   - RFC 1918 Private: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`
   - IPv6 ULA / Link-Local: `fc00::/7`, `fe80::/10`
   - Carrier-Grade NAT & Multicast: `100.64.0.0/10`, `224.0.0.0/4`, `240.0.0.0/4`

---

### 2. Redis ZSET Delayed Retry Scheduling

When an outbound webhook fails with a retryable error (e.g. HTTP 500, 502, 503, 504, 429, or network drop), HookRelay schedules redelivery using exponential backoff with full jitter.

Instead of continuously polling the database for due retries, HookRelay uses **Redis Sorted Sets (ZSET)**:
1. **Enqueueing**: The delivery ID is inserted into `hookrelay:deliveries:delayed` with its scheduled execution Unix timestamp as its score (`ZADD hookrelay:deliveries:delayed <timestamp> <delivery_id>`).
2. **Atomic Promotion**: The dispatcher executes an atomic Lua script on each cycle:
   ```lua
   -- Atomically fetch items whose score <= current_timestamp, remove them, and push to active FIFO list
   local items = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
   if #items > 0 then
       for _, id in ipairs(items) do
           redis.call('ZREM', KEYS[1], id)
           redis.call('RPUSH', KEYS[2], id)
       end
       return #items
   end
   return 0
   ```
This provides sub-second retry precision with minimal database load.

---

### 3. Operational DLQ Management (Retry & Batch Replay)

When a webhook destination encounters fatal errors (HTTP 4xx client errors) or exhausts all 8 retry attempts, the delivery is transitioned to `status = 'dead_letter'`. HookRelay provides self-service REST endpoints for operators and customers:

- **Single Delivery Redelivery**:
  ```bash
  curl -X POST http://localhost:8080/api/v1/deliveries/c71a3964-b054-46f9-b883-ec7d2cebb918/retry \
    -H "Authorization: Bearer <api_key>"
  ```
  Atomically resets status to `pending`, clears error logs, and pushes the delivery directly to the active Redis queue.

- **Batch Dead-Letter Replay**:
  ```bash
  curl -X POST http://localhost:8080/api/v1/deliveries/replay \
    -H "Authorization: Bearer <api_key>" \
    -H "Content-Type: application/json" \
    -d '{
      "subscription_id": "7ca64731-0306-4447-909d-cbbe6a048705",
      "limit": 100
    }'
  ```
  Enables bulk replay of failed webhooks after downstream endpoint recovery.

---

### 4. Prometheus Observability & Metrics

HookRelay natively exposes standard Prometheus metrics at `GET /metrics`:

| Metric Name | Type | Description | Labels |
| :--- | :--- | :--- | :--- |
| `hookrelay_events_published_total` | Counter | Total events ingested & published | `event_type` |
| `hookrelay_deliveries_total` | Counter | Webhook deliveries by terminal state | `status` (`delivered`, `retry_waiting`, `dead_letter`) |
| `hookrelay_delivery_duration_seconds` | Histogram | Outbound HTTP request latency distribution | `status` (`success`, `failure`) |
| `hookrelay_delivery_attempts_total` | Counter | Individual HTTP attempt status codes | `status_code` (`200`, `500`, `429`, etc.) |
| `hookrelay_deliveries_replayed_total` | Counter | Count of manual DLQ retries & batch replays | None |
| `hookrelay_scheduled_promotions_total` | Counter | Delayed retries promoted from Redis ZSET to active list | None |

#### Prometheus & Grafana UI Access

Both Prometheus and Grafana are included and pre-configured in the Docker Compose stack:

- **Prometheus Expression Browser**: [http://localhost:9090](http://localhost:9090)
  - Scrapes HookRelay metrics at `http://api:8080/metrics` every 5 seconds.
  - View raw metrics and test PromQL queries.
- **Grafana Visualization Dashboard**: [http://localhost:3000](http://localhost:3000)
  - **Login**: Username `admin`, Password `password` (or custom configured)
  - **Pre-Built Dashboard**: Directly navigate to **[HookRelay - System Overview](http://localhost:3000/d/hookrelay-overview/hookrelay-system-overview)**.
  - Automatically provisioned with:
    - 📊 **Real-time Event Ingestion Throughput** (events/sec by type)
    - 🟢 **Deliveries by Terminal State** (Delivered vs. DLQ)
    - ⏱️ **Webhook Outbound Latency** (P50 Median & P95 Tail Latency)
    - 🔍 **Subscriber HTTP Response Status Codes**
    - 🔄 **Redis ZSET Retries & DLQ Replay Counters**

---

### 5. Official Client SDK: pkg/verifier

HookRelay includes an official, zero-dependency client verification package located at [`pkg/verifier`](file:///Users/apple/go/src/projects/hookrelay/pkg/verifier/verifier.go).

Webhook consumers can verify signatures, protect against payload tampering, and enforce timestamp drift windows to stop replay attacks:

```go
package main

import (
    "fmt"
    "net/http"
    "time"

    "github.com/purangoswami2001/hookrelay/pkg/verifier"
)

func webhookHandler(w http.ResponseWriter, r *http.Request) {
    secret := "your_subscription_secret"

    // Verifies X-HookRelay-Signature, constant-time HMAC, and enforces a 5-minute replay window
    payload, err := verifier.VerifyRequest(r, secret, 5*time.Minute)
    if err != nil {
        http.Error(w, fmt.Sprintf("Unauthorized: %v", err), http.StatusUnauthorized)
        return
    }

    // Process authenticated payload safely
    w.WriteHeader(http.StatusOK)
}
```

---

## System Architecture & Sequence Diagram

```mermaid
sequenceDiagram
    autonumber
    actor Client as Event Producer / API Client
    participant API as HookRelay API (:8080)
    participant DB as PostgreSQL 16+
    participant Dispatcher as HookRelay Dispatcher
    participant Redis as Redis (Active List & Delayed ZSET)
    participant Worker as HookRelay Worker Pool
    actor Webhook as Subscriber HTTP Endpoint

    %% 1. Ingestion
    Client->>API: POST /api/v1/events (type, data)
    Note over API,DB: Transactional Outbox (Single ACID Tx)
    API->>DB: INSERT events<br/>MATCH active subscriptions<br/>INSERT deliveries (status='pending')<br/>INSERT outbox
    API-->>Client: 201 Created (event_id)

    %% 2. Dispatching & ZSET Promotion
    loop Every 2 Seconds
        Dispatcher->>Redis: Lua: ZRANGEBYSCORE + ZREM + RPUSH (Promote Due Retries)
        Dispatcher->>DB: SELECT unpublished FROM outbox LIMIT 100
        Dispatcher->>Redis: RPUSH hookrelay:deliveries (delivery_id)
        Dispatcher->>DB: UPDATE outbox SET published_at = NOW()
    end

    %% 3. Worker Processing & SSRF Defense
    Worker->>Redis: BLPOP hookrelay:deliveries 2s
    Worker->>DB: UPDATE deliveries SET status='processing' (Optimistic Lock)
    Worker->>DB: Fetch Event Payload & Subscription Secret
    Worker->>Worker: Sign Payload: HMAC-SHA256 (t=<ts>,v1=<sig>)
    Worker->>Webhook: HTTP POST (via SafeTransport: net.Dialer.Control blocks SSRF/IMDS)

    %% 4. Response & DLQ / Retry Flow
    alt HTTP 2xx Success
        Worker->>DB: Record attempt & UPDATE deliveries (status='delivered')
    else Retryable Error (5xx / 429 / Timeout) & Attempts < 8
        Worker->>DB: Record attempt & UPDATE deliveries (status='retry_waiting', next_attempt_at)
        Worker->>Redis: ZADD hookrelay:deliveries:delayed <next_attempt_unix> <delivery_id>
    else Fatal Error (4xx except 408/429) or Exhausted 8 Attempts
        Worker->>DB: Record attempt & UPDATE deliveries (status='dead_letter')
    end

    %% 5. Self-Service DLQ Replay
    Client->>API: POST /api/v1/deliveries/{id}/retry
    API->>DB: UPDATE deliveries SET status='pending'
    API->>Redis: RPUSH hookrelay:deliveries (delivery_id)
    API-->>Client: 200 OK (Queued for Redelivery)
```

---

## Tech Stack

| Component | Technology | Role |
| :--- | :--- | :--- |
| **Language** | [Go 1.22+](https://go.dev/) | Clean Hexagonal Architecture, `net/http`, `log/slog` |
| **Primary Database** | [PostgreSQL 16+](https://www.postgresql.org/) | ACID transactions, outbox tables, JSONB indexing via `pgx/v5` |
| **Message Broker** | [Redis 7+](https://redis.io/) | FIFO delivery queue (`LPUSH`/`BLPOP`) and delayed retry ZSET via `go-redis/v9` |
| **Observability** | [Prometheus](https://prometheus.io/) | Native `/metrics` exporter using `client_golang` |
| **Security** | Kernel Socket Controls | `net.Dialer.Control` prevents SSRF & DNS rebinding to private IPs/IMDS |
| **Verification SDK** | `pkg/verifier` | Client library for constant-time HMAC-SHA256 verification |
| **Migrations** | [golang-migrate](https://github.com/golang-migrate/migrate) | Versioned SQL migrations (001–006) |
| **Containers** | Docker & Docker Compose | Multi-stage build, Alpine runtime, non-root `app` user, read-only rootfs |

---

## Project Directory Tree

```text
hookrelay/
├── cmd/
│   ├── api/                   # API HTTP server entrypoint
│   ├── dispatcher/            # Outbox publisher & ZSET retry promoter
│   └── worker/                # Concurrent webhook delivery worker pool
├── internal/
│   ├── adapters/
│   │   ├── httpclient/        # Outbound HTTP client, HMAC signer & SSRF SafeTransport
│   │   │   ├── sender.go      # HTTP request execution & retry evaluation
│   │   │   ├── signature.go   # HMAC-SHA256 payload signer
│   │   │   ├── ssrf.go        # net.Dialer.Control socket filter & CIDR blacklists
│   │   │   └── ssrf_test.go   # SSRF unit tests
│   │   ├── postgres/          # PostgreSQL repository implementations (pgx/v5)
│   │   └── redis/             # Redis FIFO queue and ZSET delayed scheduling
│   ├── application/
│   │   ├── deliveries/        # Webhook delivery orchestration, DLQ retry & replay
│   │   ├── events/            # Event ingestion & validation service
│   │   ├── subscriptions/     # Subscriber lifecycle & URL validation
│   │   └── users/             # Multi-tenant user registration & API key management
│   ├── config/                # Environment configuration loader
│   ├── dispatcher/            # Outbox dispatcher & ZSET promoter
│   ├── domain/                # Core domain models (User, Event, Subscription, Delivery)
│   ├── observability/         # Structured logger (log/slog) & Prometheus metrics
│   ├── ports/                 # Hexagonal interfaces for data access & queues
│   ├── transport/
│   │   └── httpapi/           # HTTP handlers, auth middleware, router & /metrics
│   └── worker/                # Delivery worker polling and lifecycle
├── pkg/
│   └── verifier/              # Public client SDK: webhook signature verification
│       ├── verifier.go        # HMAC-SHA256 constant-time verification & tolerance
│       └── verifier_test.go   # SDK unit tests
├── migrations/                # Versioned SQL migrations (001–006)
├── postman/                   # Postman Collection & Environment JSON
├── compose.yaml               # Docker Compose multi-service definition
├── Dockerfile                 # Multi-stage production container build
├── .env.example               # Local native environment variables template
└── .docker.env.example        # Docker Compose environment variables template
```

---

## Getting Started

### Prerequisites

- **Docker** (version 24.0+) & **Docker Compose** (version 2.20+)
  *or for local host execution:*
- **Go** (version 1.22+)
- **PostgreSQL** (version 15+)
- **Redis** (version 7+)

---

### Method 1: Docker Compose (Recommended)

Docker Compose starts PostgreSQL, Redis, automatically executes all database migrations, and boots `api`, `dispatcher`, and `worker`.

1. **Clone the repository:**
   ```bash
   git clone https://github.com/purangoswami2001/hookrelay.git
   cd hookrelay
   ```

2. **Configure environment:**
   ```bash
   cp .docker.env.example docker.env
   ```

3. **Start the stack:**
   ```bash
   docker compose up -d --build
   ```

4. **Verify running containers:**
   ```bash
   docker compose ps
   ```
   All 5 services (`postgres`, `redis`, `api`, `dispatcher`, `worker`) will report healthy/running.

5. **Test the health & metrics endpoints:**
   ```bash
   curl -i http://localhost:8080/health
   curl -i http://localhost:8080/metrics
   ```

6. **View live logs:**
   ```bash
   docker compose logs -f api worker dispatcher
   ```

7. **Stop the stack:**
   ```bash
   docker compose down
   # To purge volumes:
   docker compose down -v
   ```

---

### Method 2: Local Native Execution

1. **Start backing services:**
   ```bash
   docker compose up -d postgres redis
   ```

2. **Configure `.env`:**
   ```bash
   cp .env.example .env
   ```

3. **Apply migrations:**
   ```bash
   docker compose up migrate
   ```

4. **Run services in separate terminals:**
   - **Terminal 1 (API Server):**
     ```bash
     export $(cat .env | xargs) && go run ./cmd/api
     ```
   - **Terminal 2 (Outbox Dispatcher & ZSET Promoter):**
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
| `LOG_LEVEL` | `DEBUG` | Logging verbosity (`DEBUG`, `INFO`, `WARN`, `ERROR`) |
| `LOG_FILE` | `logs/hookrelay.log` | Persistent log file path (leave blank for stdout only) |
| `HTTP_ADDR` | `:8080` | Bind address and port for the API server |
| `WORKER_COUNT` | `5` | Number of concurrent delivery workers |
| `DATABASE_URL` | *Required* | PostgreSQL connection string (`postgres://user:pass@host:port/dbname?sslmode=disable`) |
| `REDIS_ADDR` | `localhost:6379` | Host and port of the Redis instance |

---

## Complete API Reference

Base URL: `http://localhost:8080`

### Authentication & Multi-Tenancy

Protected endpoints require an API key passed via:
- `Authorization: Bearer <your_api_key>`
- `X-API-Key: <your_api_key>`

Every user's subscriptions, events, and deliveries are strictly isolated and scoped to their tenant account.

---

### Health & Metrics

#### 1. System Health (Public)
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

#### 2. Prometheus Metrics (Public)
```http
GET /metrics
```
**Response (200 OK):** Standard Prometheus plain text scrape output containing `hookrelay_*` metrics.

---

### Users & Authentication API

#### 1. Register a New Customer Account
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
> The `api_key` is revealed **only once** upon registration or regeneration. Store it in a secure secrets store.
```json
{
  "user": {
    "id": "e3b0c442-98fc-1c14-9afb-4c7c66424400",
    "email": "customer@company.com",
    "name": "Acme Corp",
    "api_key_prefix": "hr_live_9f8b7c6d...",
    "created_at": "2026-10-21T17:00:00Z",
    "updated_at": "2026-10-21T17:00:00Z"
  },
  "api_key": "hr_live_9f8b7c6d5e4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c",
  "message": "Store your API key securely. It will never be shown again."
}
```

#### 2. Get Current User Profile
```http
GET /api/v1/users/me
Authorization: Bearer <api_key>
```
**Response (200 OK):**
```json
{
  "user": {
    "id": "e3b0c442-98fc-1c14-9afb-4c7c66424400",
    "email": "customer@company.com",
    "name": "Acme Corp",
    "api_key_prefix": "hr_live_9f8b7c6d...",
    "created_at": "2026-10-21T17:00:00Z",
    "updated_at": "2026-10-21T17:00:00Z"
  }
}
```

#### 3. Regenerate API Key
Immediately revokes the previous API key and returns a new active key.
```http
POST /api/v1/users/me/regenerate-key
Authorization: Bearer <api_key>
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

### Subscriptions API

#### 1. Create a Subscription
Registers a webhook endpoint URL with SSRF pre-validation.
```http
POST /api/v1/subscriptions
Authorization: Bearer <api_key>
Content-Type: application/json
```
**Request Body:**
```json
{
  "url": "https://api.company.com/webhook",
  "event_types": ["payment.succeeded", "invoice.created"]
}
```
**Response (201 Created):**
```json
{
  "subscription": {
    "id": "7ca64731-0306-4447-909d-cbbe6a048705",
    "url": "https://api.company.com/webhook",
    "event_types": ["payment.succeeded", "invoice.created"],
    "active": true,
    "created_at": "2026-10-21T17:10:00Z",
    "updated_at": "2026-10-21T17:10:00Z"
  },
  "secret": "whsec_G3l4Yy9wXv7K1bB2cN3dE4fG5hJ6kL7mN8pQ9rS0tU"
}
```

#### 2. List Subscriptions
```http
GET /api/v1/subscriptions
Authorization: Bearer <api_key>
```
**Response (200 OK):**
```json
{
  "data": [
    {
      "id": "7ca64731-0306-4447-909d-cbbe6a048705",
      "url": "https://api.company.com/webhook",
      "event_types": ["payment.succeeded", "invoice.created"],
      "active": true,
      "created_at": "2026-10-21T17:10:00Z",
      "updated_at": "2026-10-21T17:10:00Z"
    }
  ]
}
```

#### 3. Update Subscription
```http
PATCH /api/v1/subscriptions/{id}
Authorization: Bearer <api_key>
Content-Type: application/json
```
**Request Body (optional fields):**
```json
{
  "url": "https://api.company.com/v2/webhook",
  "event_types": ["payment.*"],
  "active": true
}
```

#### 4. Delete Subscription
```http
DELETE /api/v1/subscriptions/{id}
Authorization: Bearer <api_key>
```
**Response:** `204 No Content`

---

### Events API

#### 1. Publish an Event
```http
POST /api/v1/events
Authorization: Bearer <api_key>
Content-Type: application/json
Idempotency-Key: evt_req_992144 (optional)
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
    "currency": "USD"
  },
  "idempotency_key": "evt_req_992144",
  "created_at": "2026-10-21T17:15:00Z"
}
```

#### 2. List Events
```http
GET /api/v1/events?limit=50&offset=0
Authorization: Bearer <api_key>
```

#### 3. Get Event by ID
```http
GET /api/v1/events/{id}
Authorization: Bearer <api_key>
```

---

### Deliveries & DLQ Management API

#### 1. List Deliveries
```http
GET /api/v1/deliveries?limit=50&offset=0
GET /api/v1/deliveries?event_id=b3e09887-b1ef-4fe1-ba55-fc4294b05537
GET /api/v1/deliveries?subscription_id=7ca64731-0306-4447-909d-cbbe6a048705
Authorization: Bearer <api_key>
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
      "created_at": "2026-10-21T17:15:01Z",
      "updated_at": "2026-10-21T17:15:02Z"
    }
  ]
}
```

#### 2. Get Delivery Details & Attempt Audit History
```http
GET /api/v1/deliveries/{id}
Authorization: Bearer <api_key>
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
  "created_at": "2026-10-21T17:15:01Z",
  "updated_at": "2026-10-21T17:15:08Z",
  "attempts": [
    {
      "id": "e91244e8-8a8b-4c55-b040-57f9ea11e134",
      "delivery_id": "c71a3964-b054-46f9-b883-ec7d2cebb918",
      "attempt_number": 1,
      "http_status": 503,
      "duration_ms": 320,
      "error": "HTTP status 503",
      "created_at": "2026-10-21T17:15:02Z"
    },
    {
      "id": "f03451c2-9b9c-4d66-c151-68a0fb22f245",
      "delivery_id": "c71a3964-b054-46f9-b883-ec7d2cebb918",
      "attempt_number": 2,
      "http_status": 200,
      "duration_ms": 115,
      "error": "",
      "created_at": "2026-10-21T17:15:08Z"
    }
  ]
}
```

#### 3. Retry a Specific Delivery (DLQ Redelivery)
Resets a failed or dead-letter delivery back to `pending` and immediately pushes it into the active delivery queue.
```http
POST /api/v1/deliveries/{id}/retry
Authorization: Bearer <api_key>
```
**Response (200 OK):**
```json
{
  "message": "delivery reset and queued for redelivery",
  "delivery_id": "c71a3964-b054-46f9-b883-ec7d2cebb918",
  "status": "pending"
}
```

#### 4. Batch Replay Dead-Letter Deliveries
Bulk replays dead-letter webhooks matching optional subscription or event filters.
```http
POST /api/v1/deliveries/replay
Authorization: Bearer <api_key>
Content-Type: application/json
```
**Request Body:**
```json
{
  "subscription_id": "7ca64731-0306-4447-909d-cbbe6a048705",
  "event_id": "",
  "limit": 50
}
```
**Response (200 OK):**
```json
{
  "message": "dead-letter deliveries replayed",
  "count": 14,
  "delivery_ids": [
    "c71a3964-b054-46f9-b883-ec7d2cebb918",
    "d82b4075-c165-57fa-c994-fd8e3dfcc029"
  ]
}
```

---

## Webhook Verification Guide

Outgoing requests carry the following headers:
- `X-HookRelay-Event-ID`: Unique event identifier
- `X-HookRelay-Delivery`: Unique delivery identifier
- `X-HookRelay-Timestamp`: Unix timestamp (seconds)
- `X-HookRelay-Signature`: Format `t=<timestamp>,v1=<hex_hmac_sha256>`

### Using Go (`pkg/verifier`)

```go
package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/purangoswami2001/hookrelay/pkg/verifier"
)

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	secret := "whsec_G3l4Yy9wXv7K1bB2cN3dE4fG5hJ6kL7mN8pQ9rS0tU"

	// Reads body, verifies HMAC-SHA256 and ensures timestamp drift <= 5 minutes
	payload, err := verifier.VerifyRequest(r, secret, 5*time.Minute)
	if err != nil {
		http.Error(w, fmt.Sprintf("Forbidden: %v", err), http.StatusForbidden)
		return
	}

	fmt.Printf("Received authenticated event: %s\n", string(payload))
	w.WriteHeader(http.StatusOK)
}
```

### Using Node.js / Express

```javascript
const crypto = require('crypto');

function verifyHookRelaySignature(secret, rawBody, signatureHeader) {
  const parts = signatureHeader.split(',');
  const timestamp = parts.find(p => p.startsWith('t='))?.split('=')[1];
  const receivedSig = parts.find(p => p.startsWith('v1='))?.split('=')[1];

  if (!timestamp || !receivedSig) {
    throw new Error('Malformed signature header');
  }

  // Prevent replay attacks (5 minute tolerance)
  const now = Math.floor(Date.now() / 1000);
  if (Math.abs(now - parseInt(timestamp, 10)) > 300) {
    throw new Error('Timestamp drift too large (replay attack)');
  }

  const message = `${timestamp}.${rawBody}`;
  const computedSig = crypto.createHmac('sha256', secret).update(message).digest('hex');

  const trusted = Buffer.from(computedSig, 'hex');
  const untrusted = Buffer.from(receivedSig, 'hex');

  if (trusted.length !== untrusted.length || !crypto.timingSafeEqual(trusted, untrusted)) {
    throw new Error('Invalid HMAC signature');
  }

  return true;
}
```

### Using Python

```python
import hmac
import hashlib
import time

def verify_hookrelay_webhook(secret: str, raw_body: bytes, sig_header: str, tolerance_seconds: int = 300) -> bool:
    pairs = dict(item.split("=", 1) for item in sig_header.split(","))
    timestamp = pairs.get("t")
    received_sig = pairs.get("v1")

    if not timestamp or not received_sig:
        return False

    # Check timestamp tolerance
    if abs(time.time() - int(timestamp)) > tolerance_seconds:
        return False

    message = f"{timestamp}.".encode("utf-8") + raw_body
    computed_sig = hmac.new(secret.encode("utf-8"), message, hashlib.sha256).hexdigest()

    return hmac.compare_digest(computed_sig, received_sig)
```

---

## Retry Policy & Failure Modes

### Exponential Backoff Schedule
HookRelay computes retry delays using full randomized jitter:
$$\text{delay} = \min(\text{BaseDelay} \times 2^{\text{attempt}-1}, \text{MaxDelay}) \pm \text{jitter}$$

- **Base Delay**: `2 seconds`
- **Max Delay**: `30 minutes`
- **Max Retry Attempts**: `8`

| Outcome | HTTP Codes / Conditions | Resulting Status | Action |
| :--- | :--- | :--- | :--- |
| **Success** | HTTP `200`–`299` | `delivered` | Completed successfully |
| **Transient Error** | HTTP `408`, `429`, `500`, `502`, `503`, `504`, TCP reset, timeout | `retry_waiting` | Enqueued into Redis ZSET for delayed retry |
| **Fatal Client Error** | HTTP `400`, `401`, `403`, `404`, `422` (except 408/429) | `dead_letter` | Moved to DLQ; requires manual inspection |
| **Max Retries Reached** | Exhausted 8 attempts | `dead_letter` | Moved to DLQ; eligible for self-service replay |

---

## Database Migrations

Managed via standard SQL files in `migrations/`:

| Version | Migration File | Description |
| :--- | :--- | :--- |
| **001** | `001_create_subscriptions` | Webhook URLs, subscribed event types, secret keys |
| **002** | `002_create_events` | Ingested events, JSONB data, idempotency keys |
| **003** | `003_create_deliveries` | Webhook delivery states, attempt counts, retry timestamps |
| **004** | `004_create_delivery_attempts` | Complete audit log for each HTTP delivery attempt |
| **005** | `005_create_outbox` | Transactional outbox table for at-least-once Redis publishing |
| **006** | `006_create_users` | Multi-tenant customer accounts & SHA-256 hashed API keys |

---

## Postman Collection

A complete, pre-configured Postman collection and environment are provided in [`postman/`](file:///Users/apple/go/src/projects/hookrelay/postman):
- `postman/hookrelay.postman_collection.json`: Covers all endpoints (Users, Subscriptions, Events, Deliveries, DLQ Replay, Health, and Metrics). Includes test scripts for automatic token and variable chaining.
- `postman/hookrelay.postman_environment.json`: Pre-configured environment targeting `http://localhost:8080`.
