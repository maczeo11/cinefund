#!/usr/bin/env bash
# Quick demo: create a campaign, pledge, fire a webhook, check everything worked.
# Needs: docker compose up, make migrate, make seed, make run-api (in another terminal)
set -euo pipefail

API="${API_URL:-http://localhost:8080}"

echo "=== CineFund Demo ==="
echo ""

# 1. list campaigns (seeded data)
echo "1) GET /campaigns"
CAMPAIGNS=$(curl -sf "$API/campaigns" | head -c 500)
echo "$CAMPAIGNS" | python3 -m json.tool 2>/dev/null || echo "$CAMPAIGNS"
echo ""

CAMPAIGN_ID=$(echo "$CAMPAIGNS" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'])" 2>/dev/null || echo "")
if [ -z "$CAMPAIGN_ID" ]; then
  echo "no campaigns found, creating one..."
  CAMPAIGN_ID=$(curl -sf -X POST "$API/campaigns" \
    -H "Content-Type: application/json" \
    -d '{"title":"Demo Film","creator_id":"00000000-0000-0000-0000-000000000001","goal_amount":1000000,"deadline":"2027-01-01T00:00:00Z"}' \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
  echo "created campaign: $CAMPAIGN_ID"
fi

# 2. create a pledge
echo "2) POST /campaigns/$CAMPAIGN_ID/pledges"
PLEDGE=$(curl -sf -X POST "$API/campaigns/$CAMPAIGN_ID/pledges" \
  -H "Content-Type: application/json" \
  -d "{\"backer_id\":\"$(uuidgen 2>/dev/null || python3 -c 'import uuid; print(uuid.uuid4())')\",\"amount\":50000}")
echo "$PLEDGE" | python3 -m json.tool 2>/dev/null || echo "$PLEDGE"
echo ""

PLEDGE_ID=$(echo "$PLEDGE" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
ORDER_ID=$(echo "$PLEDGE" | python3 -c "import sys,json; print(json.load(sys.stdin)['provider_order_id'])")

echo "pledge_id=$PLEDGE_ID order_id=$ORDER_ID"
echo ""

# 3. fire a fake webhook (payment captured)
echo "3) Webhook: payment.captured"
./scripts/fake-webhook.sh "$ORDER_ID" 50000
echo ""

# 4. check the pledge status changed to CAPTURED
echo "4) Verify pledge is CAPTURED"
sleep 1
STATUS=$(curl -sf "$API/campaigns/$CAMPAIGN_ID/pledges/$PLEDGE_ID" \
  | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null || echo "check manually")
echo "   status = $STATUS"
echo ""

# 5. fire the same webhook again — should be deduplicated
echo "5) Re-deliver same webhook (idempotency test)"
./scripts/fake-webhook.sh "$ORDER_ID" 50000
echo "   (should return duplicate/200, campaign raised_amount unchanged)"
echo ""

echo "=== Done ==="
