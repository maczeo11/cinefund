# 04 — API Spec

Every HTTP endpoint. Base path `/api/v1` for everything except `/health`,
`/metrics` and `/webhooks/*`.

---

## 1. Conventions

### Envelope

Success responses return the resource at the top level — no `{"data": ...}`
wrapper for single resources. Collections are wrapped because they need
pagination metadata.

```jsonc
// single
{ "id": "...", "title": "..." }

// collection
{
  "items": [ ... ],
  "page": { "limit": 20, "next_cursor": "eyJpZCI6...", "has_more": true }
}
```

**Cursor pagination, not offset.** `OFFSET 40000` makes Postgres scan and discard
40,000 rows, and it produces duplicate/missing items when the underlying list
changes between pages. The cursor is base64 of `{"id": "...", "sort_value": ...}`
and translates to `WHERE (sort_value, id) < ($1, $2) ORDER BY sort_value DESC, id DESC`.

### Errors

One shape, everywhere:

```jsonc
{
  "error": {
    "code": "CAMPAIGN_NOT_EDITABLE",     // stable, machine-readable, SCREAMING_SNAKE
    "message": "Campaign cannot be edited while it is in review.",
    "details": { "status": "IN_REVIEW" },     // optional, structured
    "request_id": "01J8X..."
  }
}
```

`code` is the contract. `message` is for humans and may be reworded freely;
never make a client branch on it.

| Status | When | Example codes |
| --- | --- | --- |
| 400 | malformed body, bad param | `INVALID_BODY`, `INVALID_CURSOR` |
| 401 | missing/invalid/expired token | `UNAUTHENTICATED`, `TOKEN_EXPIRED`, `TOKEN_REUSED` |
| 403 | authenticated but not permitted | `FORBIDDEN`, `NOT_CAMPAIGN_OWNER`, `NO_ENTITLEMENT` |
| 404 | doesn't exist, or exists but you can't see it | `NOT_FOUND` |
| 409 | state machine / uniqueness conflict | `CAMPAIGN_NOT_EDITABLE`, `TIER_SOLD_OUT`, `DEADLINE_PASSED` |
| 422 | semantically invalid | `VALIDATION_FAILED`, `IDEMPOTENCY_KEY_REUSED` |
| 429 | rate limited | `RATE_LIMITED` |
| 500 | unhandled | `INTERNAL` |
| 503 | downstream unavailable | `PAYMENT_PROVIDER_UNAVAILABLE`, `STORAGE_UNAVAILABLE` |

**404-not-403 for resources you can't see.** A `DRAFT` campaign belonging to
someone else returns 404, not 403 — otherwise the API confirms the resource
exists, which is an enumeration oracle.

### Auth transport

Access token in an `httpOnly; Secure; SameSite=Lax` cookie named `cf_at`, and
accepted as `Authorization: Bearer <token>` as a fallback for non-browser
clients. Refresh token in `cf_rt`, `httpOnly`, `Path=/api/v1/auth`, so it is
never sent to any other endpoint.

Because cookies are the primary transport, **every state-changing request must
also carry `X-CSRF-Token`** matching a non-httpOnly `cf_csrf` cookie
(double-submit). `SameSite=Lax` alone does not cover top-level POST navigations.

### Headers

| Header | Direction | Purpose |
| --- | --- | --- |
| `X-Request-ID` | both | echoed; generated if absent |
| `traceparent` | in | W3C trace context, propagated to Kafka and workers |
| `Idempotency-Key` | in | required on `POST /pledges`, `POST /media/uploads`, `POST /payouts` |
| `X-CSRF-Token` | in | required on all mutating requests when using cookie auth |
| `X-RateLimit-Limit` / `-Remaining` / `-Reset` | out | on every response |
| `Retry-After` | out | on 429 and 503 |

---

## 2. Health & ops

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| GET | `/health/live` | none | process is up. Never touches a dependency. |
| GET | `/health/ready` | none | pings Postgres and Redis with a 2s budget. 503 if either fails. |
| GET | `/metrics` | internal only | Prometheus exposition |

Separating live from ready matters: a liveness probe that checks the database
will restart your API during a database blip, turning a partial outage into a
total one.

---

## 3. Auth

### `POST /api/v1/auth/register`

```jsonc
// request
{ "email": "b@example.com", "password": "…", "display_name": "Bhanu" }
// 201
{ "id": "...", "email": "...", "display_name": "...", "role": "USER" }
```

- `password`: ≥ 12 chars, checked against a small common-password denylist.
- `role` is **never** read from the body.
- Always returns 201 even if the email exists — the duplicate is signalled by
  email, not by the API, to avoid account enumeration. (Rate-limited hard.)

### `POST /api/v1/auth/login`

```jsonc
{ "email": "...", "password": "..." }
// 200 + Set-Cookie: cf_at, cf_rt, cf_csrf
{ "user": { "id": "...", "display_name": "...", "role": "USER",
            "is_creator": true }, "expires_in": 900 }
```

Constant-time comparison, and run the bcrypt/argon2 verify **even when the user
doesn't exist** (against a dummy hash), so response timing doesn't leak account
existence.

### `POST /api/v1/auth/refresh`

Reads `cf_rt`. Rotates. On reuse detection: burns the family, clears cookies,
`401 TOKEN_REUSED`. See [05](05-AUTH-SECURITY.md#refresh-rotation).

### `POST /api/v1/auth/logout`

Revokes the family, clears all three cookies, adds the access token's `jti` to
the Redis denylist for its remaining lifetime. 204.

### `GET /api/v1/auth/me`

Current user + creator status + counts. 401 if unauthenticated.

---

## 4. Creator profiles

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| POST | `/creator-profile` | user | apply to become a creator (`PENDING`) |
| GET | `/creator-profile` | user | own profile + status |
| PATCH | `/creator-profile` | user | edit while `PENDING` or `REJECTED` |
| GET | `/admin/creator-profiles?status=PENDING` | admin | review queue |
| POST | `/admin/creator-profiles/{id}/approve` | admin | approve, audit-logged |
| POST | `/admin/creator-profiles/{id}/reject` | admin | `{reason}`, audit-logged |

---

## 5. Campaigns

### `GET /api/v1/campaigns`

Public listing, served from the **Postgres catalog projection**.

| Param | Type | Default | Notes |
| --- | --- | --- | --- |
| `q` | string | — | text search, weighted title > tagline > synopsis |
| `category` | enum | — | |
| `status` | enum | `LIVE` | only `LIVE`, `FUNDED`, `RELEASED` are publicly queryable |
| `sort` | enum | `trending` | `trending` \| `newest` \| `ending_soon` \| `most_funded` |
| `cursor` | string | — | opaque |
| `limit` | int | 20 | max 50 |

`ending_soon` filters to `deadline > now()` implicitly — a campaign that ended
four hours ago is not "ending soon", and forgetting this is a classic.

```jsonc
// 200
{ "items": [ { "id": "...", "slug": "...", "title": "...", "tagline": "...",
               "cover_url": "...", "category": "DRAMA",
               "funding": { "goal_amount": 50000000, "raised_amount": 31200000,
                            "percent_funded": 62, "backer_count": 118 },
               "deadline": "2026-09-30T18:29:59Z", "days_left": 12,
               "creator": { "display_name": "...", "avatar_url": "..." } } ],
  "page": { "limit": 20, "next_cursor": "...", "has_more": true } }
```

### `GET /api/v1/campaigns/{slug}`

The campaign page: static content and **funding numbers** from the same
Postgres store — no cross-store read to reconcile.

```jsonc
// 200
{
  "id": "...", "slug": "...", "title": "...", "tagline": "...",
  "synopsis": "...", "risks": "...", "category": "DRAMA",
  "status": "LIVE",
  "creator": { "user_id": "...", "display_name": "...", "avatar_url": "...",
               "campaigns_created": 3, "campaigns_funded": 2 },
  "funding": { "goal_amount": 50000000, "raised_amount": 31200000,
               "backer_count": 118, "percent_funded": 62, "currency": "INR" },
  "tiers": [ { "id": "...", "title": "Digital Access", "min_amount": 50000,
               "description": "...", "remaining": null, "sold_out": false,
               "estimated_delivery": "2026-12-01",
               "grants": { "download": false, "credit": false, "bts": false } } ],
  "pitch": { "playback_url": "https://…/master.m3u8?X-Amz-…",
             "poster_url": "...", "duration_seconds": 124,
             "expires_at": "2026-08-02T12:05:00Z" },
  "published_at": "...", "deadline": "...",
  "viewer": { "has_pledged": true, "pledge_ids": ["..."], "is_owner": false }
}
```

The `viewer` block is only present when authenticated. Putting per-viewer state
in a sub-object (rather than sprinkling `is_*` fields at the top level) keeps
the response cacheable: everything outside `viewer` and `pitch` is identical for
all users and can be cached in Redis; those two are stitched in per request.

### Campaign writes

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| POST | `/campaigns` | creator | creates `DRAFT`; slug auto-derived, collision-suffixed |
| PATCH | `/campaigns/{id}` | owner | 409 `CAMPAIGN_NOT_EDITABLE` per the field/state matrix in [00 §5.2](00-PRODUCT-SPEC.md#52-fields-editable-per-campaign-state) |
| POST | `/campaigns/{id}/tiers` | owner | only in `DRAFT` |
| PATCH | `/campaigns/{id}/tiers/{tid}` | owner | only in `DRAFT` |
| DELETE | `/campaigns/{id}/tiers/{tid}` | owner | only in `DRAFT`, 409 if any pledge references it |
| POST | `/campaigns/{id}/submit` | owner | `DRAFT → IN_REVIEW`; runs the full validation gate |
| POST | `/campaigns/{id}/cancel` | owner | only if `raised_amount == 0`; else 409 |
| GET | `/campaigns/{id}/backers` | owner | paginated, respects `anonymous` |
| POST | `/campaigns/{id}/updates` | owner | posts an update, emits a notification event |
| GET | `/campaigns/{id}/updates` | public/backer | `BACKERS_ONLY` filtered by entitlement |

**Submit validation gate** — all must pass or 422 with a `details.failures[]` array:

```
✓ title, tagline, synopsis non-empty
✓ goal_amount >= 100000 (₹1,000)
✓ duration_days between 7 and 90
✓ at least one reward tier
✓ tier min_amounts strictly increasing by sort_order
✓ cover image uploaded
✓ pitch media asset exists and status == READY
✓ creator profile status == APPROVED
```

Returning *all* failures at once, not the first, is the difference between one
round-trip and eight.

### Admin campaign review

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/admin/campaigns?status=IN_REVIEW` | queue |
| POST | `/admin/campaigns/{id}/approve` | → `LIVE`, sets `published_at`, resolves `deadline = published_at + duration_days`, emits `campaign.published` |
| POST | `/admin/campaigns/{id}/reject` | `{reason}` → `DRAFT` |
| POST | `/admin/campaigns/{id}/force-fail` | `{reason}` → `FAILED` + refunds everything |

---

## 6. Pledges

### `POST /api/v1/campaigns/{id}/pledges`

**Requires `Idempotency-Key`.**

```jsonc
// request
{ "tier_id": "0192d...", "amount": 100000, "anonymous": false,
  "message": "Can't wait to see this." }

// 201
{ "pledge_id": "0192k...",
  "status": "CREATED",
  "amount": 100000,
  "payment": {
    "provider": "razorpay",
    "order_id": "order_QxYz123",
    "key_id": "rzp_test_abc",          // publishable key, safe to expose
    "amount": 100000,
    "currency": "INR",
    "prefill": { "email": "b@example.com", "name": "Bhanu" }
  } }
```

Server-side checks, in this order (fail fast, cheapest first):

```
1. campaign.status == LIVE                  → else 409 CAMPAIGN_NOT_LIVE
2. now() < deadline - 60s                   → else 409 DEADLINE_PASSED
3. backer_id != campaign.creator_id         → else 403 CREATOR_CANNOT_PLEDGE
4. tier belongs to this campaign            → else 422
5. amount >= tier.min_amount                → else 422 AMOUNT_BELOW_TIER
6. SELECT tier FOR UPDATE; claimed < limit  → else 409 TIER_SOLD_OUT
7. INSERT pledge (CREATED)
8. Razorpay Orders.Create(receipt = pledge_id)   ← the only network call
9. UPDATE pledge SET provider_order_id
```

Step 8 is outside the row-lock's critical section as much as possible — holding
a `FOR UPDATE` lock on a hot tier row across a 300 ms external HTTP call is how
you serialise your entire checkout flow under load. Commit the pledge insert
first, then create the order, then update. A pledge with no order id is cleaned
up by the reconciliation sweep.

### `POST /api/v1/pledges/{id}/confirm`

The browser reports back here when Razorpay Checkout's `handler` fires, passing
the three fields Checkout hands it:

```json
{
  "razorpay_order_id": "order_NxYz...",
  "razorpay_payment_id": "pay_NxYz...",
  "razorpay_signature": "9f86d081..."
}
```

Returns `{"id": "...", "status": "CAPTURED"}`.

The signature is HMAC-SHA256 over `order_id|payment_id` keyed with the **API key
secret**, not the webhook secret. Getting those two confused fails in a way that
looks exactly like a forged request.

Nothing here is trusted for money. The signature only proves the caller talked
to Checkout; the amount, fee and payment id are read back from
`FetchPayments(order_id)` and the pledge is settled by the same code the webhook
runs. Confirming a pledge twice, or confirming one the webhook already handled,
is a no-op — the status check in `applyCapture` is the guard.

If the provider reports the payment as authorized but not yet captured, the
endpoint returns the unchanged status and leaves the webhook to finish.

This exists because webhooks cannot reach a developer machine at all, and
because in production they can lag long enough that a backer watches the total
sit still after paying. The webhook remains the source of truth and the only
path that runs unattended.

### Other pledge endpoints

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| GET | `/pledges/{id}` | owner | poll target when confirm returns a non-final status |
| GET | `/me/pledges` | user | paginated history with campaign summaries |
| POST | `/pledges/{id}/cancel` | owner | rule F9: `LIVE` and >24h to deadline; → `REFUND_PENDING` |

If `confirm` comes back with anything other than `CAPTURED`, poll
`GET /pledges/{id}` with backoff (1s, 2s, 4s, cap 5s, give up at 60s) and show
"we'll email you when it clears" rather than spinning forever.

---

## 7. Webhooks

### `POST /webhooks/razorpay`

**Not under `/api/v1`. No auth middleware. No CSRF. No rate limit.**

- Requires the **raw request body** for HMAC verification — register this route
  before any body-consuming middleware, and read with `io.ReadAll(c.Request.Body)`
  then restore it. Gin's `ShouldBindJSON` consumes the body; verifying the
  signature against a re-marshalled struct will fail intermittently on key
  ordering and whitespace, and you will lose an evening to it.
- Verify `X-Razorpay-Signature` = `HMAC_SHA256(body, webhook_secret)`, compared
  with `hmac.Equal`.
- Returns **200 on success and on duplicate**; **5xx on any failure to record**,
  so Razorpay retries.

Handled events: `payment.authorized`, `payment.captured`, `payment.failed`,
`refund.processed`, `refund.failed`, `order.paid`. Anything else: record in
`payment_events` and return 200 (acknowledged, unhandled).

Full flow in [06](06-PAYMENTS-RAZORPAY.md).

---

## 8. Media

### `POST /api/v1/media/uploads`

**Requires `Idempotency-Key`.** Creator-only.

```jsonc
// request
{ "purpose": "FILM", "campaign_id": "0192f...",
  "filename": "the-last-bus-home.mov",
  "content_type": "video/quicktime",
  "size_bytes": 4812339201 }

// 201
{ "asset_id": "0192c...",
  "upload": {
    "method": "PUT",
    "url": "https://localhost:9000/cinefund-originals/0192c.../…?X-Amz-…",
    "headers": { "Content-Type": "video/quicktime" },
    "expires_at": "2026-08-02T12:15:00Z"
  } }
```

Validation:

- `content_type` ∈ allow-list (`video/mp4`, `video/quicktime`, `video/x-matroska`,
  `image/jpeg`, `image/png`, `image/webp`).
- `size_bytes` ≤ 8 GiB for video, ≤ 10 MiB for images. **Pinned into the
  presigned policy**, not merely validated — otherwise a client can presign for
  a 1 MB image and upload 50 GB.
- The object key is server-generated from the asset UUID. Never from the
  client's filename; the filename is stored as metadata only. Client-controlled
  keys are a path-traversal and overwrite vector.

For files > 100 MB, respond with a multipart plan instead — see
[10 §4](10-OBJECT-STORAGE.md#4-multipart-uploads).

### `POST /api/v1/media/uploads/{asset_id}/complete`

Server `HEAD`s the object, verifies it exists and its size matches within
tolerance, then flips status to `UPLOADED` — the transition that emits the
`media.uploaded` outbox event which starts the transcode job. 409 if the
object is missing.

### `GET /api/v1/media/assets/{id}`

Owner or admin. Returns status, progress (joined from `transcode_jobs`),
renditions, and error detail on failure.

```jsonc
{ "asset_id": "...", "status": "TRANSCODING", "progress": 0.53,
  "eta_seconds": 380,
  "tasks": [ { "name": "1080p", "status": "SUCCEEDED", "progress": 1.0 },
             { "name": "720p", "status": "RUNNING", "progress": 0.41 } ] }
```

### `DELETE /api/v1/media/assets/{id}`

Soft-delete. Owner only, and only if the asset isn't referenced by a `LIVE` or
later campaign. Object cleanup happens via a lifecycle rule, not synchronously.

---

## 9. Films & playback

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| GET | `/films` | public | catalog projection, same pagination/sort conventions |
| GET | `/films/{slug}` | public | metadata + `viewer.can_watch` + `viewer.reason` |
| GET | `/films/{id}/playback` | conditional | **the authorisation endpoint** |
| GET | `/films/{id}/download` | entitled | presigned GET of the top rendition, TTL 1h, rate-limited to 3/day |
| POST | `/films/{id}/progress` | user | `{position_seconds}`; fire-and-forget, batched to Postgres |

### `GET /api/v1/films/{id}/playback`

```jsonc
// 200
{ "master_url": "https://cdn/…/master.m3u8?token=…",
  "expires_at": "2026-08-02T12:05:00Z",
  "duration_seconds": 847,
  "poster_url": "...",
  "captions": [ { "lang": "en", "label": "English", "url": "..." } ],
  "resume_position_seconds": 412 }

// 403
{ "error": { "code": "NO_ENTITLEMENT",
             "message": "This film is available to backers until 15 Nov 2026.",
             "details": { "reason": "EARLY_ACCESS_WINDOW",
                          "public_from": "2026-11-15T00:00:00Z" } } }
```

Never cached. Never served from a projection. The entitlement check hits
Postgres every time — see [01 §5.4](01-ARCHITECTURE.md#54-playback-authorisation).

---

## 10. Payouts

| Method | Path | Auth | Notes |
| --- | --- | --- | --- |
| POST | `/campaigns/{id}/payout` | owner | requires `Idempotency-Key`; only when campaign is `RELEASED`; computes fee, creates `REQUESTED` payout |
| GET | `/campaigns/{id}/payout` | owner | status |
| GET | `/admin/payouts?status=REQUESTED` | admin | queue |
| POST | `/admin/payouts/{id}/approve` | admin | |
| POST | `/admin/payouts/{id}/mark-paid` | admin | `{reference}` → writes ledger entries, moves pledges to `SETTLED` |

---

## 11. Admin & ops

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/admin/ledger/accounts/{id}/entries` | ledger inspection |
| GET | `/admin/reconciliation/latest` | last run's invariant results |
| POST | `/admin/dlq/{topic}/replay` | replay N messages from a DLQ topic |
| GET | `/admin/outbox/stats` | unpublished count, oldest age, failures |
| POST | `/admin/media/assets/{id}/retranscode` | force a new `pipeline_version` |
| GET | `/admin/audit?target_type=&target_id=` | audit trail |

Every one of these is audit-logged with the acting admin's id.

---

## 12. Rate limit tiers

Applied by route group. Full mechanism in [12](12-RATE-LIMITING.md).

| Group | Limit (per IP) | Per user | Notes |
| --- | --- | --- | --- |
| `auth/login`, `auth/register` | 5 / 15 min | — | plus a per-email counter to stop distributed credential stuffing |
| `auth/refresh` | 30 / hour | 30 / hour | |
| public reads | 120 / min | — | |
| authenticated reads | 300 / min | 300 / min | |
| `POST /pledges` | 10 / min | 10 / min | |
| `POST /media/uploads` | 5 / hour | 20 / day | presigning is cheap; the storage it authorises is not |
| `/films/*/download` | — | 3 / day | |
| `/webhooks/*` | **exempt** | — | signature verification is the gate; rate-limiting a payment provider loses money |

---

## 13. OpenAPI

Hand-write `api/openapi.yaml` alongside the handlers and serve it at
`/api/v1/openapi.yaml`. Generate the TypeScript client from it, not by hand.

Do **not** generate the Go server from the spec — hand-written handlers with a
hand-written spec, and a contract test asserting they agree, is a better trade
here than fighting a code generator over Gin idioms.
