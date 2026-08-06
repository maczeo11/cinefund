# 14 — Observability

Three signals, one correlation id. The goal is that a single question —
"what happened to pledge X?" — can be answered from the outside, without
attaching a debugger.

> **Status note (2026-08-06).** Mongo is no longer in the stack
> ([ADR-0010](DECISIONS/ADR-0010-postgres-only.md)); references to it in
> dependency health/readiness tables below are historical.

---

## 1. Correlation

Every request gets a `request_id`. Every request also starts (or joins) a trace
with a `trace_id`. **The trace id is the one that crosses process boundaries**,
which makes it the important one.

```
HTTP request  → trace_id generated / parsed from traceparent
              → stored in ctx
              → logged on every line
              → written to outbox.trace_id
              → dispatcher copies it into a Kafka header
              → consumer parses the header, restores it into ctx
              → transcoder logs it, writes it to transcode_jobs.trace_id
```

That chain is the whole design. It means you can take a `trace_id` from an HTTP
access log and find the FFmpeg invocation it eventually caused, 40 minutes later,
in a different container.

```go
// Kafka producer
Headers: []kgo.RecordHeader{{Key: "traceparent", Value: []byte(traceparentFrom(ctx))}}

// Kafka consumer
func ContextFromKafkaHeaders(ctx context.Context, hs []kgo.RecordHeader) context.Context {
    carrier := propagation.MapCarrier{}
    for _, h := range hs { carrier[h.Key] = string(h.Value) }
    return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
```

Use W3C `traceparent` format rather than a bare UUID. It's what OpenTelemetry
propagates natively, it carries sampling decisions, and it costs nothing extra.

---

## 2. Logging

`log/slog` with a JSON handler. One structured line per request, plus explicit
lines for domain events worth a record.

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: cfg.LogLevel,
    ReplaceAttr: redactSensitive,       // see below
}))
```

### The standard fields

Every line, without exception:

```jsonc
{ "time": "2026-08-02T11:04:22.481Z", "level": "INFO", "msg": "request completed",
  "service": "api", "version": "v0.4.1",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "request_id": "01J8X…",
  "user_id": "0192a…",            // omitted when anonymous
  "method": "POST", "path": "/api/v1/campaigns/:id/pledges",
  "status": 201, "duration_ms": 47 }
```

Note `path` is the **route pattern**, not the concrete URL. Logging
`/api/v1/campaigns/0192f.../pledges` makes the field unaggregatable — you can
never ask "how slow is the pledge endpoint" because every path is unique. Gin
gives you `c.FullPath()` for this.

### Redaction

```go
var sensitiveKeys = map[string]bool{
    "password": true, "password_hash": true, "token": true, "access_token": true,
    "refresh_token": true, "authorization": true, "cookie": true, "signature": true,
    "razorpay_signature": true, "secret": true, "api_key": true, "card": true,
}

func redactSensitive(_ []string, a slog.Attr) slog.Attr {
    if sensitiveKeys[strings.ToLower(a.Key)] { return slog.String(a.Key, "[REDACTED]") }
    return a
}
```

An allow-list would be safer still, but a deny-list plus a code-review habit is
the realistic trade. What matters most: **never log a whole webhook payload or a
whole request body.** Log `event_id`, `payment_id`, `amount`, `status`. The full
payload lives in `payment_events.payload` where it's access-controlled, not in a
log aggregator where it's grep-able by anyone with read access.

### What to log at each level

| Level | Use for | Example |
| --- | --- | --- |
| `DEBUG` | off in production; local development detail | ffmpeg argv |
| `INFO` | request completion, domain state transitions | `campaign transitioned`, `pledge captured` |
| `WARN` | degraded but handled | `redis unavailable, cache bypassed`, `rate limiter fell back` |
| `ERROR` | needs a human eventually | `webhook processing failed`, `transcode job exhausted retries` |

There is no `FATAL`. A process that must die calls `os.Exit` after an `ERROR`
line — and only in `main`, never in a library.

**Every `ERROR` line should be actionable.** If a line fires 400 times a day and
nobody does anything, it's a `WARN` or it's noise, and noise is what makes people
stop reading logs.

---

## 3. Metrics

Prometheus, exposed on `/metrics`, bound to an internal listener.

### RED for every service

```
http_requests_total{method, route, status}
http_request_duration_seconds{method, route}       histogram
http_requests_in_flight{route}                     gauge
```

Buckets matter. The default Prometheus buckets top out at 10 s, which is useless
for anything slow and over-detailed for anything fast:

```go
Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
```

### The business metrics that actually matter

These are the ones that tell you the *system* is broken when every technical
metric looks fine:

```
pledges_created_total{campaign_category}
pledges_captured_total{campaign_category}
pledge_amount_paise_total                          counter
webhook_events_total{type, result="applied|duplicate|invalid_sig|failed"}
refunds_total{reason, status}
campaigns_transitioned_total{from, to}
ledger_imbalance_detected_total                    ← should always be 0
reconciliation_failures_total{check}               ← should always be 0
```

`ledger_imbalance_detected_total` and `reconciliation_failures_total` are
**alert-on-any-increase** metrics. They are the ones that catch a money bug
before a user does.

### Pipeline health

```
outbox_pending_rows                                gauge
outbox_lag_seconds                                 gauge   ← the single best pipeline metric
outbox_publish_duration_seconds                    histogram
kafka_consumer_lag{group, topic, partition}        gauge
kafka_messages_processed_total{group, topic, result}
dlq_messages_total{topic}                          ← alert on any increase
```

`outbox_lag_seconds` = `now() − min(created_at) WHERE published_at IS NULL`. One
gauge that catches: dispatcher crashed, Kafka down, broker full, poison event
looping. If you only had one metric in this whole system, this would be a strong
candidate.

### Transcoding

```
transcode_jobs_total{status}
transcode_duration_seconds{rung}                   histogram, buckets to 3600
transcode_realtime_factor{rung}                    histogram — encode speed vs content duration
transcode_queue_depth                              gauge
transcode_active_jobs{worker_id}                   gauge
transcode_worker_free_disk_bytes{worker_id}        gauge
ffmpeg_failures_total{stage, reason}
```

`transcode_realtime_factor` (FFmpeg's `speed=2.18x`) is the leading indicator for
capacity. When it drifts below ~1.0, you cannot keep up with real-time ingestion
and the queue will grow without bound. Watch it before you watch queue depth,
because queue depth tells you the problem has already happened.

### Cardinality discipline

**Never** put `user_id`, `campaign_id`, `asset_id`, or a raw URL path in a label.
Each distinct value is a separate time series; a few thousand users becomes a few
million series and kills Prometheus. `worker_id` is acceptable because workers
number in the tens. When in doubt, ask: "can this take more than ~100 values?" If
yes, it belongs in a log line, not a label.

---

## 4. Tracing

OpenTelemetry, OTLP exporter, Jaeger locally.

```go
tp := sdktrace.NewTracerProvider(
    sdktrace.WithBatcher(otlpExporter),
    sdktrace.WithResource(resource.NewWithAttributes(
        semconv.SchemaURL,
        semconv.ServiceName("cinefund-api"),
        semconv.ServiceVersion(build.Version),
    )),
    sdktrace.WithSampler(sdktrace.ParentBased(
        sdktrace.TraceIDRatioBased(cfg.TraceSampleRatio))),  // 0.1 in prod, 1.0 locally
)
```

### Spans worth creating

Auto-instrument HTTP (`otelgin`), Postgres (`otelpgx`), Redis, and gRPC. Then add
manual spans only where they answer a question:

| Span | Attributes |
| --- | --- |
| `pledge.create` | `campaign_id`, `tier_id`, `amount` |
| `razorpay.orders.create` | `duration`, `status_code` |
| `webhook.process` | `event_type`, `result` |
| `outbox.dispatch` | `batch_size` |
| `transcode.job` | `asset_id`, `rung_count`, `source_duration` |
| `ffmpeg.encode` | `rung`, `duration`, `realtime_factor` |
| `playlist.rewrite` | `segment_count` |

**Sampling and money don't mix.** At 10% sampling you will lose the trace for the
exact payment you need to investigate. Force-sample anything on the money path:

```go
if isMoneyPath(route) { ctx = trace.ContextWithSpanContext(ctx, forceSampled(sc)) }
```

Payment volume is low enough that 100% sampling on webhooks and pledge creation
costs almost nothing, and the one time you need it, it's there.

---

## 5. SLOs

Define them, or "is it slow?" has no answer.

| SLO | Target | Window | Why this number |
| --- | --- | --- | --- |
| API availability | 99.5% | 30 d | ~3.6 h/month; realistic for a single-VM deployment |
| `GET /campaigns/{slug}` p95 | < 200 ms | 7 d | the page that converts |
| `POST /pledges` p95 | < 800 ms | 7 d | includes a Razorpay round trip |
| Webhook processing p99 | < 2 s | 7 d | must beat the provider's retry window |
| Outbox lag p99 | < 5 s | 7 d | catalog freshness |
| Transcode completion | 95% within 3× content duration | 7 d | user-visible "when will my film be ready" |
| **Ledger correctness** | **100%** | always | not an SLO, an invariant. Any failure is an incident. |

The last row is the point of the table. Everything above it has an error budget.
The ledger does not.

---

## 6. Alerts

Alert on **symptoms users feel** and on **invariants that must hold**. Not on CPU.

| Alert | Condition | Severity |
| --- | --- | --- |
| API down | `up{job="api"} == 0` for 2 m | **page** |
| Error rate | 5xx ratio > 2% for 5 m | **page** |
| Ledger imbalance | `increase(ledger_imbalance_detected_total[1h]) > 0` | **page** |
| Reconciliation failure | `increase(reconciliation_failures_total[1h]) > 0` | **page** |
| Webhook failures | `rate(webhook_events_total{result="failed"}[10m]) > 0.1` | **page** |
| Outbox stalled | `outbox_lag_seconds > 30` for 5 m | **page** |
| Postgres unreachable | readiness failing 2 m | **page** |
| DLQ received a message | `increase(dlq_messages_total[15m]) > 0` | ticket |
| Consumer lag | `kafka_consumer_lag > 1000` for 10 m | ticket |
| Transcode failure rate | > 10% over 1 h | ticket |
| Encode speed | `transcode_realtime_factor < 1.0` p50 for 30 m | ticket |
| Rate limiter fallback | `increase(rate_limit_fallback_total[5m]) > 0` | ticket |
| Invalid webhook signatures | > 10 in 5 m | ticket (misconfig or probing) |
| Campaign past deadline | invariant I7 fires | ticket |
| Disk on transcoders | free < 20 GB | ticket |

Everything that pages must be **actionable at 3 a.m.** If the runbook entry is
"look at it tomorrow", it's a ticket. An alert nobody can act on trains people to
ignore alerts, and then the real one gets ignored too.

---

## 7. Dashboards

Four, no more. A dashboard nobody opens is worse than no dashboard because it
implies coverage that doesn't exist.

**1. Service health** — request rate, error rate, p50/p95/p99 latency by route,
in-flight requests, dependency health.

**2. Money** — pledges created vs captured (the gap is your checkout drop-off),
capture rate, amount over time, webhook results by type, refunds by reason,
ledger imbalance counter, reconciliation status.

**3. Pipeline** — outbox lag and pending count, consumer lag by group, DLQ depth,
messages processed, dispatcher batch size.

**4. Media** — queue depth, active jobs by worker, duration by rung, realtime
factor, failure reasons, worker free disk.

---

## 8. Local stack

```yaml
prometheus:  { image: prom/prometheus,          ports: ["9091:9090"] }
grafana:     { image: grafana/grafana,          ports: ["3001:3000"] }
jaeger:      { image: jaegertracing/all-in-one, ports: ["16686:16686", "4318:4318"] }
```

Provision Grafana dashboards as JSON in `deploy/grafana/dashboards/` and commit
them. A dashboard that exists only in someone's browser is lost on the next
`docker compose down -v`, and rebuilding it is an hour you'll resent.

---

## 9. Health endpoints

```go
// /health/live — is the process alive? NEVER touches a dependency.
func Live(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) }

// /health/ready — can it serve? Checks dependencies with a hard budget.
func Ready(deps Deps) gin.HandlerFunc {
    return func(c *gin.Context) {
        ctx, cancel := context.WithTimeout(c, 2*time.Second)
        defer cancel()

        results := map[string]string{}
        code := 200
        for name, check := range deps.Checks() {
            if err := check(ctx); err != nil {
                results[name] = "error: " + err.Error()
                if deps.IsCritical(name) { code = 503 }
            } else {
                results[name] = "ok"
            }
        }
        c.JSON(code, gin.H{"status": statusFor(code), "checks": results})
    }
}
```

**Critical:** Postgres. **Non-critical:** Redis, Mongo, Kafka, S3 — degraded, not
down. A readiness probe that 503s because Redis is slow will pull every replica
out of the load balancer and take down a site that was still perfectly capable of
serving. Getting the critical/non-critical split right is the difference between
graceful degradation and a self-inflicted outage.

And, once more, because it's the most common mistake in this file:
**liveness never checks a dependency.** A liveness probe that pings the database
restarts your API during a database blip.
