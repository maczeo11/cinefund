# ADR-0008: HLS served via a playlist rewriter, not a public bucket

**Status:** accepted
**Date:** 2026-08-02

## Context

Backer-only films must not be watchable by non-backers. The media lives in a
private S3/MinIO bucket as an HLS tree:

```
master.m3u8  →  720p/index.m3u8  →  720p/seg_00001.ts …
```

HLS uses **relative references**. A player given a signed URL for `master.m3u8`
resolves `720p/index.m3u8` against it and requests that path — **without a
signature**. Private bucket, so: 403. The video never starts, and the browser
console shows a manifest error that doesn't obviously point at signing.

This is the part of the media pipeline that surprises people, because everything
works fine right up until the bucket stops being public.

## Options considered

**A. Public bucket.** Trivially simple, zero access control. Fine for trailers
and public films; unacceptable for backer-only content, which is the entire
reward mechanic.

**B. Sign every URL inside the playlists at transcode time.** Then the playlists
are static and self-contained. But signatures expire, so the playlist has to be
regenerated before expiry, and a signature TTL long enough to be safe (days) is
long enough that a leaked URL is a real problem. It also bakes access into an
artefact that is supposed to be viewer-independent.

**C. CDN signed cookies / path tokens.** CloudFront signed cookies, or a CDN
token covering a path prefix, authorise an entire directory in one grant. This is
the production answer. It requires a CDN with that capability, which local MinIO
does not have.

**D. Playlist rewriter.** The API serves the playlists as text, rewriting every
referenced URL into a presigned URL at request time. Segments still go directly
from storage to the player.

## Decision

**Option D for v1, with a documented path to C.**

```
GET /api/v1/films/{id}/hls/master.m3u8       → authorise → fetch → rewrite → serve
GET /api/v1/films/{id}/hls/{rung}/index.m3u8 → authorise → fetch → rewrite → serve
GET  <presigned segment URL>                 → straight to storage
```

The API serves **only playlists** — a few kilobytes of text per request. Video
bytes never pass through Go, which preserves the core constraint of the whole
media design.

Two implementation rules:

- **Validate every line before signing it.** A playlist is a file you generated,
  but treat it as untrusted input: reject any media line containing `/` or `..`.
  Two lines of code, and it closes the path where a crafted filename becomes a
  signed URL to an arbitrary object.
- **Segment URL TTL is `max(30 min, 2 × content duration)`, capped at 6 hours.**
  A player fetching a `VOD` playlist once at the start must still be able to
  request the final segment near the end of a long film. Signing segments for 10
  minutes produces a video that mysteriously stops partway through — a bug that
  is very hard to reproduce on a short test clip.

Public films skip the rewriter entirely and serve from `cinefund-public` with
long-lived immutable cache headers, because there is nothing to protect.

## Consequences

**Good**

- Real per-viewer authorisation on backer-only content.
- Works with plain MinIO/S3. No CDN required to develop or demo.
- Entitlements are checked against Postgres on every playlist request, so a
  revoked entitlement stops playback on the next fetch.
- Bandwidth still bypasses the API entirely.

**Bad**

- The API is on the playback path for playlists. It must be up for a film to
  start, and it must be fast — this is why playlist responses are `no-store` but
  cheap.
- Signing ~250 segment URLs per variant playlist costs a few milliseconds of CPU
  per request. Measurable at scale; negligible here.
- Playlists cannot be CDN-cached, because they contain per-viewer signatures.
- A viewer who extracts a segment URL can share it for the remainder of its TTL.
  Accepted: this is not DRM, and the README says so.

**Commits us to**

- Path validation in the rewriter as a security control, with a test asserting a
  crafted `../` line is rejected.
- Documenting honestly that CineFund has **no DRM**. Signed URLs deter casual
  sharing; they do not prevent a determined viewer from downloading the
  renditions. Claiming otherwise would be false.

## Migration path to C

When a CDN is added: serve `master.m3u8` with a signed cookie or path token
covering `renditions/{asset_id}/v{n}/`, and drop the rewriter for authorised
playback. The authorisation logic is unchanged — only the mechanism by which the
grant reaches the player. The rewriter stays as the fallback for environments
without a CDN, which is also what makes local development keep working.
