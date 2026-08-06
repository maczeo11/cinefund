# Decision Records

One file per contested decision. Format: context → decision → consequences,
including the consequences you don't like.

**Write the ADR before the code it justifies, and commit it separately.** An ADR
written afterwards is a rationalisation; one written before is a design. It also
happens to be the clearest evidence that the design is yours.

| # | Decision | Status |
| --- | --- | --- |
| [0001](ADR-0001-polyglot-persistence.md) | Postgres for money, Mongo for catalog | superseded by 0010 |
| [0002](ADR-0002-modular-monolith.md) | Modular monolith with separate worker binaries | accepted |
| [0003](ADR-0003-webhook-idempotency.md) | Two-layer webhook idempotency (Redis + Postgres) | accepted |
| [0004](ADR-0004-outbox-vs-change-streams.md) | Outbox for Postgres, change streams for Mongo | superseded by 0010 |
| [0005](ADR-0005-kafka-and-franz-go.md) | Kafka as the event bus, franz-go as the client | accepted |
| [0006](ADR-0006-rate-limiter-fail-open.md) | The rate limiter fails open | accepted |
| [0007](ADR-0007-tier-reservation.md) | Reward tiers claimed at capture, with soft holds | accepted |
| [0008](ADR-0008-hls-access-control.md) | HLS served via a playlist rewriter, not a public bucket | accepted |
| [0009](ADR-0009-no-trigger-feedback-loops.md) | No component is triggered by a store it writes to | accepted |
| [0010](ADR-0010-postgres-only.md) | Postgres only — reversing the polyglot split | accepted |

## Template

```markdown
# ADR-000N: Title

**Status:** proposed | accepted | superseded by ADR-000M
**Date:** YYYY-MM-DD

## Context
What forces are at play. What makes this a real decision rather than an obvious one.

## Options considered
Each with its actual trade-off, including the one you rejected and why.

## Decision
What was chosen, stated plainly.

## Consequences
Good, bad, and what this now commits you to.
```
