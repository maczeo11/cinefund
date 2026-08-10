# API Reference

Base URL: `http://localhost:8080`

All monetary amounts are in **paise** (1 INR = 100 paise), represented as integers.
Resource IDs are UUIDs.

## Errors

Errors return a JSON body with a `kind` and `message`:

```json
{
  "error": {
    "kind": "NOT_FOUND",
    "message": "campaign not found"
  }
}
```

| Kind            | HTTP Status |
|-----------------|-------------|
| INVALID         | 400         |
| UNAUTHORIZED    | 401         |
| FORBIDDEN       | 403         |
| NOT_FOUND       | 404         |
| CONFLICT        | 409         |
| UNPROCESSABLE   | 422         |
| RATE_LIMITED    | 429         |
| INTERNAL        | 500         |
| UNAVAILABLE     | 503         |

---

## Health

### GET /health/live

Returns 200 if the process is up.

```json
{"status": "ok", "version": "dev"}
```

### GET /health/ready

Pings Postgres and Redis. Returns 503 if Postgres is down. Degrades
gracefully if only Redis is unavailable.

```json
{"postgres": "ok", "redis": "ok"}
```

---

## Campaigns

### GET /api/v1/campaigns

Returns the 50 most recent campaigns.

### POST /api/v1/campaigns

Creates a campaign in DRAFT status.

```json
{
  "creator_id": "uuid",
  "title": "The Last Frame",
  "tagline": "A short film about memory",
  "synopsis": "...",
  "category": "drama",
  "goal": 500000
}
```

Returns 201 with the created campaign.

### GET /api/v1/campaigns/:id

Returns a single campaign. 404 if it doesn't exist.

### POST /api/v1/campaigns/:id/publish

Moves a DRAFT campaign to LIVE. Sets a 30-day deadline.
Returns 409 if the campaign isn't in DRAFT.

### POST /api/v1/campaigns/:id/tiers

Adds a reward tier.

```json
{
  "title": "Premiere Ticket",
  "description": "Early screening access",
  "min_amount": 100000,
  "quantity_limit": 50
}
```

`quantity_limit` is optional — omit or pass null for unlimited.

Returns 201 with the tier.

---

## Pledges

### POST /api/v1/campaigns/:id/pledges

Creates a pledge and initiates a Razorpay payment order.

**Validations:**
- Campaign must be LIVE with >60 seconds until deadline
- Backer can't be the campaign creator
- Amount must meet tier minimum (if tier specified)
- Tier must not be sold out

```json
{
  "backer_id": "uuid",
  "tier_id": "uuid or null",
  "amount": 100000,
  "anonymous": false,
  "message": "Good luck with the film"
}
```

Response (201):

```json
{
  "id": "uuid",
  "status": "CREATED",
  "order_id": "order_ABC123",
  "amount": 100000,
  "currency": "INR"
}
```

The client takes the `order_id` and completes payment through the Razorpay
frontend SDK. Razorpay then sends a webhook to confirm.

---

## Webhooks

### POST /webhooks/razorpay

Receives payment webhooks from Razorpay. Not called by clients directly.

**Signature:** Verified via HMAC-SHA256 using the `X-Razorpay-Signature` header
and constant-time comparison.

**Handled events:**

- `payment.captured` — marks pledge as CAPTURED, increments campaign totals and
  tier count, writes double-entry ledger entries, emits outbox event. All in one
  Postgres transaction.
- `payment.failed` — marks pledge as FAILED.

**Idempotency:** Duplicate webhooks are rejected at two layers — Redis SETNX
(fast path) and a Postgres unique constraint on `payment_events` (durable
fallback). If the DB write fails, the Redis key is released so Razorpay
retries aren't blocked.

Returns 200 `{"status": "ok"}` on success, 401 for bad signatures, 409 for
duplicates.

---

## Uploads

### POST /api/v1/uploads

Creates a media asset record and returns a presigned S3 PUT URL. The client
uploads the file directly to object storage — bytes never pass through the
API server.

```json
{
  "owner_id": "uuid",
  "campaign_id": "uuid or null",
  "purpose": "pitch_video",
  "content_type": "video/mp4"
}
```

Response (201):

```json
{
  "asset_id": "uuid",
  "upload_url": "https://..."
}
```

### POST /api/v1/uploads/:id/complete

Confirms that a file was uploaded. The API does a HEAD check on S3 to verify,
then marks the asset as UPLOADED and enqueues a transcode job through the
outbox.

Returns 200 `{"status": "queued"}`.

Returns 404 if the asset doesn't exist, 409 if it's not in PENDING_UPLOAD
status.
