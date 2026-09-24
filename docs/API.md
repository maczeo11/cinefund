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

## Auth

### POST /api/v1/auth/demo

Issues a session token for one of the two public demo accounts. Mounted only
when `DEMO_LOGIN_ENABLED=true`; it cannot mint a token for any other user.

```json
{ "account": "creator" }
```

`account` is `creator` (Ava) or `backer` (Ravi). Response (200) has the same
shape as `/auth/firebase`: `{ "token": "...", "user": { "id", "email", "name", "role" } }`.

Other endpoints authenticate with `Authorization: Bearer <token>`. A bare
`X-User-ID` header is accepted only when `ALLOW_DEV_IDENTITY_HEADER=true`,
which config validation refuses outside `APP_ENV=development`.

---

## Pledges

### POST /api/v1/campaigns/:id/pledges (auth)

Creates a pledge and initiates a Razorpay payment order. The backer is the
authenticated caller; any `backer_id` in the body is ignored.

**Validations:**
- Campaign must be LIVE with >60 seconds until deadline
- Backer can't be the campaign creator
- Amount must meet tier minimum (if tier specified)
- Tier must not be sold out

```json
{
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

### POST /api/v1/pledges/:id/confirm (auth)

Settles a pledge from Checkout's browser callback. Only the pledge's backer
can call it. Body: `razorpay_order_id`, `razorpay_payment_id`,
`razorpay_signature`. Response: `{ "id": "uuid", "status": "CAPTURED" }`.

A capture that cannot count toward the campaign (the tier sold out while the
backer was paying, or the paid amount differs from the pledge) is still
recorded: the pledge becomes `REFUND_PENDING` with a `failure_reason`, the
money is booked to `BACKER_REFUND_PAYABLE`, and a `pledge.refund_required`
event is emitted. Webhooks for such captures return 200, not a retryable error.

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

---

## Playback (private-bucket HLS)

The object-storage bucket is private: browsers never get raw S3 URLs. The API
serves only playlists (a few KB of text, `Cache-Control: no-store`) while
video bytes stream straight from storage over short-lived presigned URLs
(10-minute TTL), so a link copied out of DevTools dies within minutes.

### GET /api/v1/campaigns/:id/video (public)

Returns the newest READY asset for a campaign, if any:

```json
{
  "asset_id": "uuid",
  "status": "READY",
  "master_url": "/api/v1/videos/<asset_id>/master.m3u8"
}
```

Returns `{"asset_id": null, "status": "NONE"}` when the campaign has no
watchable reel yet.

### GET /api/v1/videos/:id/master.m3u8 (auth)

Serves the master playlist with variant references rewritten to the
API-relative endpoints below, so every subsequent playlist fetch also passes
auth. Players must send auth on playlist requests: hls.js via an
`xhrSetup` Bearer header (same-origin only — S3 rejects requests that mix a
presigned query string with an `Authorization` header), Safari native playback
via the `cf_at` cookie the API already accepts.

Returns 404 for unknown assets, 409 while the video is not READY.

### GET /api/v1/videos/:id/variants/:rung/index.m3u8 (auth)

Serves one variant playlist with every `seg_*.ts` line replaced by a
short-lived presigned URL. `:rung` must be a rendition recorded on the asset
(e.g. `720p`); anything else returns 400/404.
