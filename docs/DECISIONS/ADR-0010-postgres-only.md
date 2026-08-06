# ADR-0010: Postgres only — reversing the polyglot split

**Status:** accepted
**Date:** 2026-08-06
**Supersedes:** [ADR-0001](ADR-0001-polyglot-persistence.md), and the Mongo half of [ADR-0004](ADR-0004-outbox-vs-change-streams.md)

## Context

ADR-0001 chose Postgres for money and Mongo for the catalog, and it argued the
case well. Four days later, at the point of actually writing `docker-compose.yml`
and the first migration, the decision was re-examined against a constraint the
original ADR did not weigh: **this is a solo build on a calendar that competes
with placement interviews.**

Three things changed the arithmetic.

**The second store costs more than it looked.** Not the Mongo container — the
consequences. An eventually-consistent read model needs a projector consumer, a
rebuild path for when the projection drifts, a documented staleness bound at
every read site, and a replica set in local development so change streams work
at all. That is B6, D0 and D5 in the build order, plus the replica-set setup in
A0: roughly 25–30 hours, and a second failure mode in every integration test.

**The outbox already solves reliable publishing.** ADR-0004 gave Mongo change
streams to the media pipeline because "Postgres has no CDC without Debezium."
True — which is precisely *why the outbox exists*. Once the outbox is built, the
change stream is a second mechanism doing a job the first one already does. Two
event paths is more to reason about, more to explain, and more to get subtly
wrong under redelivery.

**The catalog was never the hard part.** The genuinely document-shaped data is
probe output and rendition lists — nested, per-codec, schema-varying. `JSONB`
holds those without complaint, and none of them are queried in ways that need
Mongo's operators.

There is also a portfolio consideration, and it is worth writing down honestly
rather than pretending the decision was purely technical: DynamoDB already
covers NoSQL on the strength of prior work. A second NoSQL store demonstrates
nothing new, whereas Postgres transactions, partial indexes,
`FOR UPDATE SKIP LOCKED` and deferred constraint triggers are a distinct and
currently unrepresented skill.

## Options considered

**Keep the split as designed.** The strongest argument is that change streams
are genuinely elegant for the media pipeline, and ADR-0001's reasoning about
document-shaped data is not wrong. Rejected because the cost lands entirely on
the scarce resource — calendar — and buys capability that is redundant with the
outbox.

**Postgres only, catalog in `JSONB`.** One store, one event mechanism, every
financial invariant declarative. Costs: no change streams, so media status
transitions publish through the outbox like everything else; text search is
weaker without an extension; and `pitch_asset_id` stops being a cross-store
identifier and becomes a real foreign key, which is strictly better.

**Postgres now, Mongo later if needed.** Effectively the same as the above,
with the option left open. This is the honest framing: nothing here forecloses
adding Mongo when a read pattern actually demands it.

## Decision

**Postgres is the only datastore.** The catalog, media assets and transcode jobs
move into Postgres, with `JSONB` columns for the genuinely variable parts
(ffprobe output, rendition lists).

The transactional outbox becomes the single event mechanism. `cmd/mediawatcher`
is deleted from the build plan; media status transitions write to the outbox
inside the same transaction as the status change, exactly like pledge captures.

`gRPC` also goes (previously C1). Worker progress writes to Postgres and jobs
arrive over Kafka; a separate control-plane protocol for a status field was not
earning its 4 hours.

## Consequences

**Good.**

- Group B6, D0 and D5 disappear from the plan. Roughly 25–30 hours recovered.
- One event path instead of two. `docs/08` gets shorter and easier to defend.
- `campaigns.pitch_asset_id` becomes a real foreign key with referential
  integrity, rather than a documented convention the application must uphold.
- Local development is five containers, not six, and no replica-set init.
- "Why two databases?" — a question that has to be defended — becomes "why one?",
  which answers itself.

**Bad.**

- The media pipeline loses a genuinely nice mechanism. Change streams are the
  right tool when you have Mongo; this is giving up something real.
- Text search over film titles will need `pg_trgm` or a `tsvector` column when
  it matters. Deferred, not solved.
- ADR-0001 and ADR-0004 are now historical. They stay in the tree because the
  reasoning is still worth reading — a superseded ADR is a record of how the
  thinking moved, which is more useful than a deleted one.

**What this commits us to.**

- Every event flows through the outbox. No component may be triggered by a store
  it writes to (ADR-0009 still holds, and is now easier to satisfy).
- The catalog is a read concern inside the same database, so any future read
  model is a materialised view or a projected table — not a second store —
  unless a measured read pattern justifies revisiting this.
