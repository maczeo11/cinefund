const BASE = 'http://localhost:8080/api/v1'

export async function getCampaigns() {
  const res = await fetch(`${BASE}/campaigns`)
  if (!res.ok) throw new Error('failed to fetch campaigns')
  return res.json()
}

export async function getCampaign(id) {
  const res = await fetch(`${BASE}/campaigns/${id}`)
  if (!res.ok) throw new Error('campaign not found')
  return res.json()
}

export async function createCampaign(data) {
  const res = await fetch(`${BASE}/campaigns`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
  if (!res.ok) {
    const err = await res.json()
    throw new Error(err.error?.message || 'failed to create campaign')
  }
  return res.json()
}

export async function publishCampaign(id) {
  const res = await fetch(`${BASE}/campaigns/${id}/publish`, { method: 'POST' })
  if (!res.ok) throw new Error('failed to publish')
  return res.json()
}

export async function createPledge(campaignId, data) {
  const res = await fetch(`${BASE}/campaigns/${campaignId}/pledges`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
  if (!res.ok) {
    const err = await res.json()
    throw new Error(err.error?.message || 'pledge failed')
  }
  return res.json()
}
