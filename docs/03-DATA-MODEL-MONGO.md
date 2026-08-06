# 03 — Mongo Data Model

> **Obsolete as of 2026-08-06.** MongoDB was removed from the architecture before
> any code was written — see [ADR-0010](DECISIONS/ADR-0010-postgres-only.md) and
> [ADR-0001](DECISIONS/ADR-0001-polyglot-persistence.md). Everything in this
> document's remit — the catalog, media assets, transcode jobs — lives in
> Postgres `JSONB` columns, and the outbox replaces change streams as the event
> mechanism. This file is kept as a historical record only.
>
> Re-numbering note: the document tree was written when 03 was Mongo. The gap
> (00, 01, 02, 04…) is deliberate; IDs stay stable so links don't rot.

Mongo owns read-heavy, shape-shifting data: the public catalog, media assets and
their renditions, transcode jobs, and user-generated content. None of it is
authoritative for money.

**Conventions:**

- `_id` is a **string UUID** (v7), not an `ObjectID`. Reason: these IDs cross into
  Postgres columns (`campaigns.pitch_asset_id`, `entitlements.film_id`) and into
  Kafka payloads, and a consistent UUID everywhere beats translating formats at
  three boundaries.
- Timestamps are BSON `date`, UTC.
- Money mirrored from Postgres is `long` paise, and every such field is suffixed
  `_amount` and documented as **a projection, not a source of truth**.
- Every collection has `schema_version` (int). When a document shape changes,
  bump it and handle both shapes in the reader for one release. This is the
  Mongo equivalent of a migration and you will be glad you started it early.

---

## 1. `films` — the public catalog read model

Projected from Kafka events. **Never written by a request handler** except for
creator-owned editorial fields. If you find yourself writing pledge amounts here
from an HTTP handler, the projection has been bypassed.

```jsonc
{
  "_id": "0192f3a1-...",              // = campaign_id; a film is 1:1 with its campaign
  "schema_version": 1,

  "slug": "the-last-bus-home",
  "title": "The Last Bus Home",
  "tagline": "Everyone has one ride they never took.",
  "synopsis": "...",
  "category": "DRAMA",
  "language": "en",
  "runtime_seconds": 847,             // filled after transcode probe

  "creator": {                        // denormalised snapshot, refreshed on user.updated
    "user_id": "0192a...",
    "display_name": "Bhanu Teja",
    "avatar_url": "https://cdn/.../avatar.webp"
  },

  "campaign": {                       // projection of the Postgres row
    "status": "RELEASED",
    "goal_amount": 50000000,
    "raised_amount": 61250000,        // PROJECTION — authoritative value is in Postgres
    "backer_count": 213,
    "deadline": ISODate("2026-09-30T18:29:59Z"),
    "funded_at": ISODate("2026-09-30T18:30:02Z")
  },

  "media": {
    "pitch_asset_id": "0192b...",
    "film_asset_id":  "0192c...",
    "trailer_asset_id": null,
    "playback": {                     // copied from the asset when it reaches READY
      "master_key": "renditions/0192c.../master.m3u8",
      "duration_seconds": 847,
      "poster_key": "thumbs/0192c.../poster_1280.webp",
      "variants": [
        { "height": 1080, "bandwidth": 5200000, "codec": "avc1.640028" },
        { "height": 720,  "bandwidth": 2800000, "codec": "avc1.4d401f" },
        { "height": 480,  "bandwidth": 1400000, "codec": "avc1.4d401e" },
        { "height": 360,  "bandwidth":  800000, "codec": "avc1.42c01e" }
      ],
      "captions": [
        { "lang": "en", "label": "English", "key": "captions/0192c.../en.vtt" }
      ]
    }
  },

  "visibility": "PUBLIC",             // PUBLIC | BACKERS_ONLY | UNLISTED
  "early_access_until": ISODate("2026-11-15T00:00:00Z"),
  "released_at": ISODate("2026-10-16T00:00:00Z"),

  "credits": [                        // built from CREDIT entitlements at release
    { "role": "Executive Producer", "name": "A. Backer" }
  ],
  "tags": ["short", "mumbai", "night"],

  "stats": {                          // updated by the analytics rollup, not per-request
    "view_count": 4021,
    "like_count": 388,
    "trending_score": 72.4
  },

  "created_at": ISODate(...),
  "updated_at": ISODate(...)
}
```

### Indexes

```js
db.films.createIndex({ slug: 1 }, { unique: true })
db.films.createIndex({ visibility: 1, released_at: -1 })
db.films.createIndex({ category: 1, "stats.trending_score": -1 })
db.films.createIndex({ "creator.user_id": 1, released_at: -1 })
db.films.createIndex({ tags: 1 })
db.films.createIndex(
  { title: "text", synopsis: "text", tags: "text" },
  { weights: { title: 10, tags: 5, synopsis: 1 }, name: "films_search" }
)
```

The weighted text index is what makes a title match outrank a synopsis match.
One text index per collection is a hard Mongo limit — spend it here.

---

## 2. `campaign_pages` — the campaign read model

Separate from `films` because a campaign exists (and is browsable) long before
a film does, and the two have different lifecycles and different query patterns.

```jsonc
{
  "_id": "0192f3a1-...",              // = campaign_id
  "schema_version": 1,
  "slug": "the-last-bus-home",
  "title": "...",
  "tagline": "...",
  "synopsis": "...",
  "risks": "...",
  "category": "DRAMA",
  "cover_url": "https://cdn/.../cover.webp",

  "creator": { "user_id": "...", "display_name": "...", "avatar_url": "...",
               "campaigns_created": 3, "campaigns_funded": 2 },

  "funding": {                        // PROJECTION of Postgres; see staleness note below
    "goal_amount": 50000000,
    "raised_amount": 31200000,
    "backer_count": 118,
    "percent_funded": 62,
    "currency": "INR"
  },

  "tiers": [                          // denormalised; tiers are frozen once LIVE
    { "tier_id": "0192d...", "title": "Digital Access", "min_amount": 50000,
      "description": "...", "quantity_limit": null, "claimed_count": 90,
      "estimated_delivery": ISODate("2026-12-01T00:00:00Z"),
      "grants": { "download": false, "credit": false, "bts": false } }
  ],

  "status": "LIVE",
  "published_at": ISODate(...),
  "deadline": ISODate(...),
  "pitch": { "asset_id": "...", "master_key": "...", "poster_key": "...",
             "duration_seconds": 124 },

  "created_at": ISODate(...),
  "updated_at": ISODate(...)
}
```

### Indexes

```js
db.campaign_pages.createIndex({ slug: 1 }, { unique: true })
db.campaign_pages.createIndex({ status: 1, deadline: 1 })
db.campaign_pages.createIndex({ status: 1, "funding.percent_funded": -1 })
db.campaign_pages.createIndex({ category: 1, status: 1, published_at: -1 })
db.campaign_pages.createIndex({ "creator.user_id": 1 })
db.campaign_pages.createIndex(
  { title: "text", tagline: "text", synopsis: "text" },
  { weights: { title: 10, tagline: 5, synopsis: 1 }, name: "campaigns_search" }
)
```

### The staleness rule, stated once

`funding.raised_amount` here can lag Postgres by up to a few seconds. That is
**acceptable for the list/browse view** and **not acceptable on the campaign
detail page**, because a backer who just pledged must see their money reflected
or they will pledge again.

So the detail endpoint does a two-source read:

```go
page, err := s.mongo.GetCampaignPage(ctx, slug)          // Mongo: everything static
funding, err := s.pg.GetCampaignFunding(ctx, page.ID)    // Postgres: raised, backers, tier claims
page.Funding = funding                                    // authoritative overwrite
```

One extra sub-millisecond indexed Postgres read on the single most important
page in the product. Worth it, and worth being able to explain why.

---

## 3. `media_assets` — the pipeline's source of truth

This is the collection the **change stream watches**. Every status transition
here is a pipeline event.

```jsonc
{
  "_id": "0192c...",
  "schema_version": 1,

  "owner_id": "0192a...",             // user who uploaded
  "campaign_id": "0192f...",          // nullable — avatars have none
  "purpose": "FILM",                  // PITCH | FILM | TRAILER | BTS | AVATAR | COVER

  "status": "READY",                  // see 00-PRODUCT-SPEC §5.4
  "status_reason": null,              // populated on REJECTED / FAILED

  "original": {
    "key": "0192c.../the-last-bus-home.mov",
    "bucket": "cinefund-originals",
    "size_bytes": 4_812_339_201,
    "content_type": "video/quicktime",
    "etag": "\"9b2c...\"",
    "checksum_sha256": null,          // optional; only if the client sends it
    "uploaded_at": ISODate(...)
  },

  "probe": {                          // raw-ish ffprobe output, trimmed to what we use
    "duration_seconds": 847.32,
    "container": "mov,mp4,m4a,3gp,3g2,mj2",
    "bitrate": 42_000_000,
    "video": { "codec": "prores", "width": 3840, "height": 2160,
               "fps": 24.0, "pix_fmt": "yuv422p10le", "rotation": 0 },
    "audio": { "codec": "pcm_s24le", "channels": 2, "sample_rate": 48000 },
    "probed_at": ISODate(...)
  },

  "ladder": [                          // computed from probe; never transcode UP
    { "name": "1080p", "height": 1080, "video_bitrate": 5000000, "audio_bitrate": 128000 },
    { "name": "720p",  "height":  720, "video_bitrate": 2800000, "audio_bitrate": 128000 },
    { "name": "480p",  "height":  480, "video_bitrate": 1400000, "audio_bitrate":  96000 },
    { "name": "360p",  "height":  360, "video_bitrate":  800000, "audio_bitrate":  64000 }
  ],

  "renditions": [
    { "name": "1080p", "key": "renditions/0192c.../1080p/index.m3u8",
      "height": 1080, "width": 1920, "bandwidth": 5200000,
      "codec": "avc1.640028", "segment_count": 85, "size_bytes": 530_112_000,
      "completed_at": ISODate(...) }
  ],
  "master_key": "renditions/0192c.../master.m3u8",
  "poster_key": "thumbs/0192c.../poster_1280.webp",
  "sprite_key": "thumbs/0192c.../sprite.jpg",
  "sprite_vtt_key": "thumbs/0192c.../sprite.vtt",

  "pipeline_version": 3,               // bump to force a re-transcode of everything
  "attempts": 1,

  "created_at": ISODate(...),
  "updated_at": ISODate(...)
}
```

### Indexes

```js
db.media_assets.createIndex({ status: 1, updated_at: 1 })
db.media_assets.createIndex({ owner_id: 1, created_at: -1 })
db.media_assets.createIndex({ campaign_id: 1, purpose: 1 })
db.media_assets.createIndex({ "original.key": 1 }, { unique: true })
// TTL sweep for uploads that were presigned but never completed
db.media_assets.createIndex(
  { created_at: 1 },
  { expireAfterSeconds: 86400, partialFilterExpression: { status: "PENDING_UPLOAD" } }
)
```

That TTL index is a small piece of hygiene with a real payoff: presigned uploads
that are abandoned (user closed the tab) would otherwise accumulate forever as
`PENDING_UPLOAD` rows pointing at objects that don't exist.

`{"original.key": 1}` unique prevents two assets claiming the same object, which
is what would otherwise happen if a client retried `POST /media/uploads` with the
same filename and the key scheme weren't UUID-prefixed.

---

## 4. `transcode_jobs`

High write churn — progress heartbeats every few seconds. Kept separate from
`media_assets` so the change stream on assets isn't flooded with progress
updates. **This separation is load-bearing:** if progress lived on the asset, the
watcher would see thousands of irrelevant change events per job.

```jsonc
{
  "_id": "0192e...",
  "schema_version": 1,

  "asset_id": "0192c...",
  "pipeline_version": 3,
  "status": "RUNNING",                // QUEUED | RUNNING | SUCCEEDED | FAILED | CANCELLED
  "attempt": 1,
  "max_attempts": 3,

  "worker_id": "transcoder-7f3a@10.0.0.14",
  "lease_expires_at": ISODate(...),   // heartbeat extends this; expiry = reclaimable

  "tasks": [
    { "name": "1080p", "status": "SUCCEEDED", "progress": 1.0,
      "started_at": ISODate(...), "finished_at": ISODate(...),
      "duration_seconds": 412, "output_key": "renditions/.../1080p/index.m3u8" },
    { "name": "720p",  "status": "RUNNING", "progress": 0.41,
      "started_at": ISODate(...) }
  ],

  "progress": 0.53,                   // weighted across tasks, for the UI
  "eta_seconds": 380,

  "error": null,                      // { stage, message, ffmpeg_tail } on failure
  "trace_id": "00-4bf92f...-01",

  "created_at": ISODate(...),
  "updated_at": ISODate(...)
}
```

### Indexes

```js
db.transcode_jobs.createIndex({ asset_id: 1, pipeline_version: 1 }, { unique: true })
db.transcode_jobs.createIndex({ status: 1, lease_expires_at: 1 })
db.transcode_jobs.createIndex({ created_at: -1 })
```

`{asset_id, pipeline_version}` unique **is the job-level idempotency guarantee**.
A duplicate Kafka delivery tries to insert the same pair, gets a duplicate-key
error, and the consumer treats that as "already claimed" and acks. No second
worker starts, no duplicate renditions, no wasted CPU.

The reclaim query, run by every worker on a timer:

```js
db.transcode_jobs.findOneAndUpdate(
  { status: "RUNNING", lease_expires_at: { $lt: new Date() } },
  { $set: { worker_id: myID, lease_expires_at: in60s, status: "RUNNING" },
    $inc: { attempt: 1 } },
  { sort: { lease_expires_at: 1 } }
)
```

Atomic find-and-modify — two workers cannot reclaim the same job.

---

## 5. `campaign_updates`

Append-only posts from a creator to their backers.

```jsonc
{
  "_id": "0192g...",
  "schema_version": 1,
  "campaign_id": "0192f...",
  "author_id": "0192a...",
  "title": "We finished principal photography",
  "body_markdown": "...",
  "visibility": "PUBLIC",             // PUBLIC | BACKERS_ONLY
  "media_asset_ids": ["0192h..."],
  "notified_at": ISODate(...),        // set by the notifier consumer
  "created_at": ISODate(...)
}
```

```js
db.campaign_updates.createIndex({ campaign_id: 1, created_at: -1 })
```

## 6. `comments`

```jsonc
{
  "_id": "0192i...",
  "schema_version": 1,
  "target_type": "CAMPAIGN",          // CAMPAIGN | FILM
  "target_id": "0192f...",
  "author": { "user_id": "...", "display_name": "...", "avatar_url": "..." },
  "body": "...",
  "parent_id": null,                  // one level of threading only
  "status": "VISIBLE",                // VISIBLE | HIDDEN | DELETED
  "created_at": ISODate(...)
}
```

```js
db.comments.createIndex({ target_type: 1, target_id: 1, created_at: -1 })
db.comments.createIndex({ "author.user_id": 1, created_at: -1 })
```

One level of threading, enforced in the service (`parent_id`'s parent must be
null). Unbounded threading is a rabbit hole with no product value here.

## 7. `processed_events` — consumer-side dedupe

Kafka is at-least-once. Every consumer that isn't naturally idempotent records
what it has handled.

```jsonc
{
  "_id": "notifier:0192j-event-uuid",   // "{consumer_group}:{event_id}"
  "consumer": "notifier",
  "event_id": "0192j...",
  "event_type": "pledge.captured",
  "processed_at": ISODate(...)
}
```

```js
db.processed_events.createIndex({ processed_at: 1 }, { expireAfterSeconds: 604800 })
```

The composite `_id` gives free atomic dedupe: `insertOne` either succeeds (first
time) or raises duplicate-key (already handled → ack and move on). The 7-day TTL
bounds growth; Kafka retention is shorter than that, so a message can never
outlive its dedupe record.

---

## 8. `analytics_daily`

Pre-aggregated rollups written by the scheduler. Never computed per request.

```jsonc
{
  "_id": "film:0192f...:2026-08-02",
  "schema_version": 1,
  "scope": "film",
  "scope_id": "0192f...",
  "date": "2026-08-02",
  "views": 341, "unique_viewers": 288,
  "watch_seconds": 214_882,
  "completion_rate": 0.62,
  "pledges": 0, "pledged_amount": 0
}
```

```js
db.analytics_daily.createIndex({ scope: 1, scope_id: 1, date: -1 })
db.analytics_daily.createIndex({ date: 1 }, { expireAfterSeconds: 63072000 })  // 2 years
```

---

## 9. Replica set is mandatory

Change streams require an oplog, which requires a replica set. Even a
single-node deployment must be initialised as one:

```yaml
# deploy/docker-compose.yml
mongo:
  image: mongo:7
  command: ["--replSet", "rs0", "--bind_ip_all"]
  healthcheck:
    test: mongosh --quiet --eval "try { rs.status().ok } catch (e) { rs.initiate({_id:'rs0',members:[{_id:0,host:'mongo:27017'}]}).ok }"
    interval: 5s
    retries: 20
```

Connection string must include `replicaSet=rs0&directConnection=true` for local
single-node. Forgetting this produces `The $changeStream stage is only supported
on replica sets`, which is one of the more confusing first-run errors in this
project — hence writing it down before you hit it.

---

## 10. What is *not* in Mongo, and why

| Tempting to put here | Actually goes in | Why |
| --- | --- | --- |
| Pledge records | Postgres | needs multi-row transactions with the ledger |
| Entitlements | Postgres | authorisation must be transactional and constraint-checked |
| `raised_amount` as truth | Postgres | it's a projection here, and the doc says so in three places on purpose |
| Session/refresh tokens | Postgres | reuse detection is a compare-and-swap |
| Rate limit counters | Redis | wrong durability profile entirely |
| Video bytes | S3/MinIO | GridFS is not an object store; do not be tempted |
