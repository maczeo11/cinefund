const BASE = import.meta.env.VITE_API_BASE || 'http://localhost:8080/api/v1'

async function request(path, { method = 'GET', body } = {}) {
  const res = await fetch(BASE + path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    // error responses are {error:{code,message}}, but a proxy or a crash can
    // hand us HTML instead, so fall back to the status line
    const detail = await res.json().catch(() => null)
    throw new Error(detail?.error?.message || `${method} ${path} failed (${res.status})`)
  }
  return res.json()
}

export const getConfig = () => request('/config')

export const getCampaigns = () => request('/campaigns')

export const getCampaign = id => request(`/campaigns/${id}`)

export const createCampaign = data => request('/campaigns', { method: 'POST', body: data })

export const publishCampaign = id => request(`/campaigns/${id}/publish`, { method: 'POST' })

export const getTiers = id => request(`/campaigns/${id}/tiers`).catch(() => [])

export const addTier = (id, data) => request(`/campaigns/${id}/tiers`, { method: 'POST', body: data })

export const createPledge = (id, data) => request(`/campaigns/${id}/pledges`, { method: 'POST', body: data })

// Settles the pledge from Checkout's callback. Razorpay's webhook does the same
// thing, but it can't reach a dev machine and lags in production, so the
// browser reports back too. The server re-reads the amount from Razorpay.
export const confirmPledge = (pledgeId, checkout) =>
  request(`/pledges/${pledgeId}/confirm`, { method: 'POST', body: checkout })
