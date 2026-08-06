# 10 — Object Storage

MinIO locally, any S3-compatible service in production. One client interface,
`minio-go/v7` behind it.

---

## 1. Buckets

| Bucket | Access | Contents | Who can write | Who can read |
| --- | --- | --- | --- | --- |
| `cinefund-originals` | **private** | uploaded source files, nothing else | client (presigned PUT) | **transcoder only** |
| `cinefund-media` | **private** | renditions, thumbnails, captions | transcoder | api (presigned GET), transcoder |
| `cinefund-public` | public read | avatars, cover images, public posters | api | anyone |
| `cinefund-backup` | private, versioned | database dumps, ledger exports | scheduler | ops |

### Why originals get their own bucket

Not for any triggering reason — see §1.1 — but as a **permission boundary**.

[05 §6](05-AUTH-SECURITY.md#6-presigned-url-safety) says "never presign a GET for
the original." In a single bucket that is a *convention*: the API holds
credentials that can read `originals/`, and the only thing stopping it is that
nobody wrote the code. One careless handler and you're serving a 4 GB ProRes
master to a browser.

With a separate bucket, **the API has no read credentials for originals at all.**
The rule becomes structurally impossible to break rather than merely
discouraged — the same reasoning as enforcing tier limits with a `CHECK`
constraint instead of an `if` statement.

Three things fall out of it for free:

- **Lifecycle** — originals transition to Glacier at 90 days; renditions never do.
  A whole-bucket rule instead of a prefix filter.
- **Blast radius** — a bug in `DeletePrefix` cannot reach originals from a
  process that can't authenticate to them.
- **Cost visibility** — storage spend splits cleanly into "masters we keep" and
  "derivatives we serve", which is the split that matters when the bill grows.

**Both private buckets must have public access completely disabled**, verified by
a test doing an unsigned GET on a known key and asserting 403.

### 1.1 There are no storage event triggers

Worth stating explicitly, because it's the first thing anyone with AWS
experience checks.

**Nothing in CineFund is triggered by an object write.** No S3 event
notifications, no Lambda, no bucket-to-queue wiring. The transcoder writes
hundreds of segments and no event fires anywhere.

The pipeline is triggered by a **database status flip**
([08 §6.1](08-EVENTING-OUTBOX-KAFKA.md#61-the-feedback-loop-rule)), so the
classic loop —

```
object write → bucket event → function → object write to the same bucket → …
```

— cannot occur, regardless of how the buckets are arranged.

If you ever *do* add storage event notifications, the bucket split above becomes
the primary defence rather than a nicety: a notification on `cinefund-originals`
can never be re-triggered by the transcoder, because the transcoder writes only
to `cinefund-media`. Prefix filters would also work, but a filter is a
configuration you can get wrong and a separate bucket is not.

---

## 2. Key layout

Deterministic, hierarchical, and never derived from user input.

```
cinefund-originals/
└── {asset_id}/{sanitised_filename}          # written once, read only by the transcoder

cinefund-media/
├── renditions/{asset_id}/v{pipeline_version}/
│   ├── master.m3u8
│   ├── 1080p/index.m3u8
│   ├── 1080p/seg_00001.ts …
│   ├── 720p/…
│   └── 360p/…
└── thumbs/{asset_id}/
    ├── poster_1280.webp
    └── poster_640.webp

cinefund-public/
├── avatars/{user_id}/{size}.webp
└── covers/{campaign_id}/{size}.webp
```

Three properties this buys you:

1. **Delete an asset with one prefix delete per bucket.** `{id}/` in originals,
   `renditions/{id}/` and `thumbs/{id}/` in media.
2. **`v{pipeline_version}` makes re-transcoding safe.** New version writes a
   fresh tree; the old one keeps serving until the swap; a lifecycle rule
   removes it later. No in-place mutation of files a player is mid-stream on.
3. **No client string ever appears in a path position.** `sanitised_filename` is
   `[a-zA-Z0-9._-]` only, length-capped, and it's the *last* segment — even a
   pathological value can't escape the asset's prefix.

```go
func sanitiseFilename(s string) string {
    s = filepath.Base(s)                        // strip any path
    s = regexp.MustCompile(`[^a-zA-Z0-9._-]`).ReplaceAllString(s, "_")
    if len(s) > 100 { s = s[:100] }
    if s == "" || s == "." || s == ".." { s = "upload" }
    return s
}
```

---

## 3. The Store interface

```go
// internal/platform/objectstore
type Store interface {
    PresignedPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (*PresignedUpload, error)
    PresignedGet(ctx context.Context, key string, ttl time.Duration) (string, error)
    Head(ctx context.Context, key string) (*ObjectInfo, error)
    Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
    DeletePrefix(ctx context.Context, prefix string) error
    PublicURL(key string) string
}
```

Small on purpose. Every method maps to one S3 operation, and nothing in the
interface implies buffering a whole object in memory except `Put`, which is only
used by the transcoder for playlists and thumbnails — small files.

Two implementations: `s3Store` (real) and `memStore` (tests). The in-memory one
is ~80 lines and removes MinIO from every unit test.

---

## 4. Multipart uploads

Single-part PUT caps at 5 GiB and, more practically, a 4 GB upload over a flaky
connection that fails at 95% has to restart from zero. Above 100 MB, use
multipart.

```jsonc
// POST /api/v1/media/uploads → 201 for a large file
{ "asset_id": "0192c...",
  "upload": {
    "mode": "multipart",
    "upload_id": "2~abc123…",
    "part_size": 16777216,                   // 16 MiB
    "parts": [
      { "part_number": 1, "url": "https://…?partNumber=1&uploadId=…" },
      { "part_number": 2, "url": "https://…?partNumber=2&uploadId=…" }
    ],
    "expires_at": "2026-08-02T14:00:00Z"
  } }
```

Client uploads parts (in parallel, 3–4 at a time), collects each response's
`ETag`, then:

```jsonc
// POST /api/v1/media/uploads/{id}/complete
{ "upload_id": "2~abc123…",
  "parts": [ { "part_number": 1, "etag": "\"9b2c…\"" },
             { "part_number": 2, "etag": "\"1f4a…\"" } ] }
```

Server calls `CompleteMultipartUpload`, then `HEAD` to verify, then flips status.

Constraints to know before you hit them:

- Minimum part size is **5 MiB** for all parts except the last. 16 MiB is a good
  default: 4 GB / 16 MiB = 250 parts, well under the 10,000 limit.
- Presigning 250 URLs up front is fine (they're just signatures, generated
  offline, no API calls). For very large files, presign in pages of 100 and let
  the client request more.
- **Abandoned multipart uploads still consume storage and are invisible in
  listings.** Set a lifecycle rule to abort incomplete uploads after 1 day, or
  they accumulate as a slow storage leak that nobody notices until the bill.

---

## 5. Lifecycle rules

```jsonc
{
  "Rules": [
    { "ID": "abort-incomplete-multipart",
      "Status": "Enabled",
      "AbortIncompleteMultipartUpload": { "DaysAfterInitiation": 1 } },

    { "ID": "expire-orphaned-originals",
      "Status": "Enabled",
      "Filter": { "Tag": { "Key": "status", "Value": "orphaned" } },
      "Expiration": { "Days": 7 } },

    // on cinefund-originals: whole-bucket rule, no prefix filter needed
    { "ID": "archive-originals",
      "Status": "Enabled",
      "Transition": { "Days": 90, "StorageClass": "GLACIER" } },

    { "ID": "expire-old-pipeline-versions",
      "Status": "Enabled",
      "Filter": { "Tag": { "Key": "superseded", "Value": "true" } },
      "Expiration": { "Days": 30 } }
  ]
}
```

Originals to cold storage after 90 days: once renditions exist, the original is a
master copy you need only for re-transcoding, which is rare. It's also the
largest object in the system. On S3 this is real money; on MinIO it's a no-op but
the rule documents intent.

**Deletion is never synchronous.** `DELETE /media/assets/{id}` tags objects and
lets the lifecycle rule remove them. A synchronous prefix delete of 400 segments
is a slow request that can half-fail and leave the asset in an unknown state.

---

## 6. MinIO locally

```yaml
minio:
  image: minio/minio
  command: server /data --console-address ":9001"
  environment:
    MINIO_ROOT_USER: minioadmin
    MINIO_ROOT_PASSWORD: minioadmin
  ports: ["9000:9000", "9001:9001"]
  volumes: [minio_data:/data]
  healthcheck:
    test: ["CMD", "mc", "ready", "local"]
    interval: 5s
    retries: 20

minio-init:
  image: minio/mc
  depends_on: { minio: { condition: service_healthy } }
  entrypoint: >
    /bin/sh -c "
    mc alias set local http://minio:9000 minioadmin minioadmin;
    mc mb -p local/cinefund-originals local/cinefund-media
          local/cinefund-public local/cinefund-backup;
    mc anonymous set download local/cinefund-public;
    mc anonymous set none     local/cinefund-media;
    mc anonymous set none     local/cinefund-originals;
    "
```

### The presigned-URL hostname trap

This will cost you an hour if you don't know it. The API signs URLs using its own
view of MinIO (`minio:9000`, the Docker network name). The **browser** cannot
resolve `minio`. It needs `localhost:9000`.

You cannot fix this by string-replacing the host after signing — the hostname is
part of the SigV4 signature and rewriting it invalidates it.

The correct fix is to sign with the **public** endpoint from the start:

```go
// Two clients, deliberately.
internalClient, _ := minio.New("minio:9000", opts)        // server-side ops: HEAD, Put, Get
presignClient,  _ := minio.New(cfg.PublicEndpoint, opts)  // signing only: "localhost:9000"
```

In production both are the same value and this collapses to one client. Locally
they differ. Set `S3_ENDPOINT=minio:9000` and `S3_PUBLIC_ENDPOINT=localhost:9000`.

Same class of problem applies to the transcoder, which is *inside* the network
and therefore wants the internal endpoint for its presigned GET of the original.
So: **presign for the browser with the public endpoint; presign for a worker with
the internal one.** Make it an explicit parameter, not a global:

```go
func (s *s3Store) PresignedGet(ctx context.Context, key string, ttl time.Duration, aud Audience) (string, error)
// Audience: AudienceBrowser | AudienceInternal
```

---

## 7. Production notes

- **CORS on `cinefund-originals`** must allow `PUT` from your web origin, with
  `Content-Type` in `AllowedHeaders` and `ETag` in `ExposeHeaders`. Without the
  `ETag` exposure, browser multipart uploads cannot read part ETags and
  completion fails — a confusing, silent failure.
- **Credentials scoped per role**, which is the whole point of the bucket split:

  | Principal | `cinefund-originals` | `cinefund-media` | `cinefund-public` |
  | --- | --- | --- | --- |
  | `api` | `PutObject` only (to presign uploads) | `GetObject`, `HeadObject` | `PutObject`, `GetObject` |
  | `transcoder` | `GetObject`, `HeadObject` | `PutObject`, `GetObject`, `DeleteObject` | — |
  | `scheduler` | `DeleteObject` | `DeleteObject` | — |

  Note the API cannot `GetObject` from originals **at all**. That is what turns
  "never presign a GET for the original" from a convention into an impossibility.
  No `s3:*`, ever.
- **Server-side encryption** (`SSE-S3`) on both private buckets. One header, no
  key management, satisfies the encryption-at-rest question by default.
- **CDN in front of `cinefund-public` and the renditions prefix.** Segment
  requests are the highest-volume traffic in the system by orders of magnitude,
  and they are perfectly cacheable — immutable content at immutable keys.
- **Cache headers:** segments and renditions `Cache-Control: public, max-age=31536000, immutable`
  (the `v{pipeline_version}` in the key is the cache buster). Playlists served by
  the API: `Cache-Control: private, no-store`, because they contain signed URLs.

---

## 8. Tests

| # | Scenario | Assertion |
| --- | --- | --- |
| S0 | **API credentials attempt `GetObject` on `cinefund-originals`** | **AccessDenied — the permission boundary is real, not a convention** |
| S1 | Unsigned GET on a rendition key | 403 |
| S2 | Presigned PUT with wrong Content-Type | rejected by storage |
| S3 | Presigned PUT exceeding the declared size | rejected |
| S4 | Presigned URL after TTL | 403 |
| S5 | `sanitiseFilename("../../etc/passwd")` | no separators survive; key stays under the asset prefix |
| S6 | Multipart upload of a 200 MB file | completes; HEAD size matches |
| S7 | Abandoned multipart | not visible in list; aborted by lifecycle |
| S8 | Browser-audience presign | host is the public endpoint and the signature validates against it |
| S9 | `DeletePrefix` on an asset | originals, renditions and thumbs all gone |
