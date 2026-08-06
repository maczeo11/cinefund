# ADR-0002: Modular monolith with separate worker binaries

**Status:** accepted
**Date:** 2026-08-02

## Context

CineFund has workloads with genuinely different resource profiles and failure
characteristics: an HTTP API (latency-sensitive, low CPU), FFmpeg transcoding
(CPU-saturating, long-running), an outbox dispatcher (I/O-bound, must not stall),
and scheduled jobs (must run once).

Running all of that in one process means a runaway FFmpeg starves HTTP handlers,
and scaling for transcode capacity means scaling the API too.

The obvious counter-move is microservices. It is also the obvious over-correction
for a solo project.

## Options considered

**Single process, goroutines for everything.** Simplest to build and deploy. But
FFmpeg subprocesses would compete with request handling for CPU, an OOM in a
worker kills the API, and there's no way to scale transcoding independently.

**Microservices — separate services with HTTP/gRPC contracts between them.**
Real isolation. Costs: versioned inter-service contracts, service discovery,
distributed tracing as a requirement rather than a nicety, no cross-service
transactions, and N repositories or a monorepo with N deployment pipelines. For
one developer this is most of the work and none of the product.

**One module, several binaries.** All binaries share `internal/`, the same domain
types, and the same repositories. They differ only in what they run.

## Decision

**One Go module producing seven binaries:** `api`, `dispatcher`, `mediawatcher`,
`transcoder`, `notifier`, `scheduler`, `migrate`.

Enforce internal boundaries with package structure and a `depguard` lint rule
(`internal/platform` may not import any domain package), not with network calls.

## Consequences

**Good**

- Process isolation where it matters: FFmpeg cannot take down the API; transcoders
  scale independently on queue depth.
- No inter-service contracts to version. A domain type change is one compile.
- One test suite, one CI pipeline, one deployment unit.
- Refactoring across boundaries is a rename, not a migration.
- If a component genuinely needs to become a service later, the package boundary
  is already the seam.

**Bad**

- Every binary links the whole module, so images are larger than necessary and a
  change anywhere rebuilds everything.
- Boundaries are enforced by lint and discipline, not by the network. It is
  *possible* to import `pledge` from `media`; nothing physically stops you.
- Seven processes to run locally. Mitigated by `make dev`.
- Shared library versions: all binaries move together. You cannot upgrade the
  Kafka client for the transcoder alone.

**Commits us to**

- `mediawatcher` and `scheduler` are singletons (one resume token; one cron
  owner). They need `replicas: 1` plus, for the scheduler, a Redis lease as a
  safety net.
- The gRPC worker registry is per-API-replica in-memory state, so scaling the API
  past one replica requires a Redis pub/sub fan-out for cancel commands. Noted in
  [13 §5](../13-GRPC-CONTROL-PLANE.md#5-server-side).
