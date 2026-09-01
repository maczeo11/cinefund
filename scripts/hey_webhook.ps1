# hey / vegeta alternative using Go's built-in bench — no install needed.
# This replays the same signed webhook body N times with C concurrency.
# First, start the API: docker compose up -d ; go run ./cmd/api
# Then capture one valid webhook body:
#   go test -run TestHandleWebhook_CaptureHappyPath -v ./internal/pledge 2>&1
#   # copy the body + sig from logs into payload.json / sig.txt
#
# With hey (install: go install github.com/rakyll/hey@latest):
#   hey -n 1000 -c 50 -m POST -H "X-Razorpay-Signature: $(cat sig.txt)" -D payload.json http://localhost:8080/api/v1/webhooks/razorpay
#
# With k6 (recommended for p95 thresholds):
#   k6 run scripts/k6_webhook.js
#
# Quick fake/no-DB bench (what you just ran):
#   go test -run TestLatencyReport -v ./internal/pledge -count=1
# Output: min/p50/p95/p99/avg for 50x and 1000x — paste p95 into resume as "fake, no network".
Write-Host "See comments above. No binary needed — use: go test -run TestLatencyReport -v ./internal/pledge -count=1"
