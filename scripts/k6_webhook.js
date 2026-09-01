// k6 load test for CineFund running API (needs Postgres/Redis running)
// Usage:
//   k6 run scripts/k6_webhook.js
//   k6 run --vus 50 --duration 30s scripts/k6_webhook.js
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 50,
  duration: '30s',
  thresholds: {
    http_req_duration: ['p(95)<300', 'p(99)<600'], // ms
    http_req_failed: ['rate<0.01'],
  },
};

const BASE = __ENV.BASE_URL || 'http://localhost:8080';

export function setup() {
  // create a live campaign once
  const res = http.post(`${BASE}/api/v1/campaigns`, JSON.stringify({
    title: `bench-${Date.now()}`, tagline: 'k6', synopsis: 'bench', category: 'DRAMA', goal: 500000
  }), { headers: { 'Content-Type': 'application/json' } });
  const id = res.json('id') || res.json('campaign.id');
  // publish it
  http.post(`${BASE}/api/v1/campaigns/${id}/publish`);
  return { campaignId: id };
}

export default function (data) {
  // Each VU creates a pledge then hits webhook via fake gateway — 
  // for a real external load test, replace with your Razorpay webhook replayer.
  const pledgeRes = http.post(`${BASE}/api/v1/campaigns/${data.campaignId}/pledges`, JSON.stringify({
    backer_id: '00000000-0000-0000-0000-000000000002',
    amount: 10000,
  }), { headers: { 'Content-Type': 'application/json' } });
  check(pledgeRes, { 'pledge 201': (r) => r.status === 201 });

  sleep(0.2);

  // Optional: hit the webhook endpoint with a replayed signed body
  // You can record one valid body from `go test -run TestHandleWebhook -v` and replay it:
  // const webhookRes = http.post(`${BASE}/api/v1/webhooks/razorpay`, body, {
  //   headers: { 'Content-Type': 'application/json', 'X-Razorpay-Signature': sig }
  // });
  // check(webhookRes, { 'webhook 200': (r) => r.status === 200 });
}
