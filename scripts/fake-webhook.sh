#!/usr/bin/env bash
# Sends a signed payment.captured webhook to the local API.
#
# Usage:
#   ./scripts/fake-webhook.sh <order_id> <amount_paise> [event_id]
#
# The signature is computed exactly the way Razorpay does: HMAC-SHA256 over the
# raw body with the webhook secret. --data-raw is essential - --data strips
# whitespace and the HMAC would not match.

set -euo pipefail

ORDER_ID="${1:?usage: fake-webhook.sh <order_id> <amount_paise> [event_id]}"
AMOUNT="${2:?missing amount in paise}"
EVENT_ID="${3:-evt_$(date +%s)}"
SECRET="${RAZORPAY_WEBHOOK_SECRET:-dev-webhook-secret}"
URL="${API_URL:-http://localhost:8080/webhooks/razorpay}"

BODY=$(printf '{"id":"%s","event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_%s","order_id":"%s","amount":%s,"fee":0,"tax":0,"status":"captured","notes":{"pledge_id":""}}}}}' \
  "$EVENT_ID" "$ORDER_ID" "$ORDER_ID" "$AMOUNT")

SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | awk '{print $NF}')

echo "POST $URL"
echo "event_id=$EVENT_ID order_id=$ORDER_ID amount=$AMOUNT"
curl -s -X POST "$URL" \
  -H "Content-Type: application/json" \
  -H "X-Razorpay-Signature: $SIG" \
  --data-raw "$BODY"
echo
