# Devlog

Running notes: what broke, what I tried, what I chose. Two or three sentences per
entry. Newest first.

**Why this file exists.** It's the record of the things that don't survive into
the code — the dead ends, the wrong first guesses, the hour lost to a hostname.
It's also the most convincing evidence that the work is yours, because nobody
fabricates a wrong turn.

Write an entry when:

- something took more than an hour to figure out
- you tried an approach and abandoned it
- you hit a footgun that isn't obvious from the code
- you made a call you might second-guess later

Don't write an entry for routine progress. `git log` already covers that.

---

## Format

```markdown
## YYYY-MM-DD — Short title

**Problem.** What was happening.
**Tried.** What didn't work, and why.
**Chose.** What ended up working, and the trade-off.
```

---

## Example entries

Delete these once you have real ones — they're here to show the register and the
level of detail.

## 2026-08-05 — MinIO presigned URLs 403 in the browser

**Problem.** Presigned PUT worked from a Go integration test and failed from the
browser with `SignatureDoesNotMatch`.

**Tried.** String-replacing `minio:9000` with `localhost:9000` after signing.
Doesn't work — the host is part of the SigV4 signature, so rewriting it
invalidates it.

**Chose.** Two MinIO clients: one on the internal endpoint for server-side
operations (`HEAD`, `Put`), one on the public endpoint used only for signing
browser-bound URLs. `S3_ENDPOINT` and `S3_PUBLIC_ENDPOINT` are the same value in
production and differ locally. Made `Audience` an explicit parameter on
`PresignedGet` rather than a global, because the transcoder needs the internal
one and the browser needs the public one.

## 2026-08-04 — Change stream never fired

**Problem.** `mediawatcher` connected, logged no errors, and emitted nothing when
an asset moved to `UPLOADED`.

**Tried.** Assumed the `$match` pipeline was wrong. Rewrote it three times.

**Chose.** The pipeline was fine — the problem was the missing
`SetFullDocument(options.UpdateLookup)`. Without it an `update` event contains
only the changed fields, so `fullDocument.status` in the `$match` is empty and
nothing ever matches. Also learned that `UpdateLookup` re-reads the document at
lookup time, so the event can reflect a *later* state than the change that
triggered it — the consumer now re-reads status defensively instead of trusting
the event as a point-in-time snapshot.
