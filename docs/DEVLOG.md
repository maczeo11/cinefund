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

## 2026-08-06 — Rotation sign conventions disagree between ffprobe's two sources

**Problem.** `TestValidate_RotatedPortraitAccepted` asserted rotation 90 for a
portrait clip and got 270. Both numbers swap the dimensions, so the ladder came
out right either way and the bug would have sat there indefinitely — but they
both write to one `media_assets.rotation` column, so a query like
`WHERE rotation = 90` would have silently missed half the portrait uploads.

**Tried.** Widening the test to accept either value. Wrong instinct: it hides a
real inconsistency rather than fixing it, and the column would still hold two
values for one physical orientation.

**Chose.** Normalise both sources to "degrees clockwise the stored frame must be
turned to display upright". The legacy `rotate` tag already uses that
convention; the display matrix reports the transform it applies, which is the
inverse, so side data gets negated. `-90` in the matrix and `90` in the tag now
both parse to 90.

## 2026-08-06 — docs/09 contradicts itself on the CODECS string

**Problem.** Writing `BuildMasterPlaylist` against §7 of the media pipeline doc,
which shows four different codec strings — `avc1.640028` (High@4.0) for 1080p
down to `avc1.42c01e` (Baseline@3.0) for 360p. But §4 of the same document pins
`-profile:v main -level 4.0` on every rung, which produces `avc1.4d0028` for all
of them.

**Chose.** The encoder settings win, and the codec string is now derived from
them rather than kept in a parallel table that can drift. Worth recording
because §7 explicitly warns that an inaccurate CODECS string makes Safari refuse
a variant, usually silently — so following the example would have produced
exactly the "works in Chrome, black screen on iPhone" bug the section warns
about. A test asserts the string matches the encoder settings.

## 2026-08-06 — Auto-capture collapses AUTHORIZED → CAPTURED into one event

**Problem.** First webhook test failed with `illegal transition CREATED ->
CAPTURED`. I had copied the five-state diagram literally, where every pledge
visits AUTHORIZED first.

**Tried.** Asserting the transition as illegal and making the test wait for a
hypothetical `payment.authorized` event that Razorpay never sends in
auto-capture mode.

**Chose.** Read docs/00 §5.3: "Razorpay in auto-capture mode collapses
AUTHORIZED → CAPTURED into a single payment.captured event." Made
`CREATED -> CAPTURED` legal in the state machine while keeping AUTHORIZED as a
modelled state for the manual-capture future. The test now reflects what the
provider actually does.

## 2026-08-06 — Docker Desktop fails: HCS service not available

**Problem.** `docker info` returns "Docker Desktop is unable to start"; the
engine logs `HCS_E_SERVICE_NOT_AVAILABLE` from WSL. `vmcompute` service is
STOPPED and cannot be started (Access denied — needs elevation).

**Tried.** `Start-Service vmcompute`, `sc.exe start vmcompute`. Both blocked
without admin rights.

**Chose.** Left the migrations unverified against live Postgres; the pledge
suite runs fully offline against the fake gateway (that was the point of the
three-method interface). To unblock: start Docker Desktop from an elevated
terminal, or enable the Windows Hypervisor Platform feature and reboot.


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

## 2026-08-06 — Doc sync: Mongo/change-stream material is historical

**Problem.** Several documents still described a `mediawatcher` + Mongo change
stream for the media pipeline. That component was never built — MongoDB was
dropped before any code was written ([ADR-0010](DECISIONS/ADR-0010-postgres-only.md))
and the outbox drives media jobs like every other event.

**Chose.** Kept the design-era documents in the tree (superseded material is
history, per repo convention) but marked them with status banners pointing at
ADR-0010, and corrected the README, local-dev, build-order and API docs to match
the actual repository. Also removed an earlier draft devlog entry about a
"change stream never fired" that described debugging a component that never
existed — a fabricated wrong turn is worse than no entry at all.
