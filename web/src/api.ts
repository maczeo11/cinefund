// CineFund API — TypeScript typed, REST docs align with docs/API.md:6 + README.md:121
// Base: http://localhost:8080/api/v1 — all amounts in paise (int)
// LIVE BACKEND ONLY — no fake/mock fallback. If backend down, throw → UI shows "offline".

const BASE = (import.meta.env.VITE_API_BASE as string) || 'http://localhost:8080/api/v1'

// Small helper to show where we forward to (for HealthBar / debugging)
export const getApiBase = () => BASE

export type Campaign = {
  id: string
  creator_id: string
  title: string
  tagline: string
  synopsis: string
  category: string
  status: 'DRAFT' | 'LIVE' | 'CLOSED' | string
  goal_amount: number // paise
  raised_amount: number
  backer_count: number
  deadline?: string
  created_at: string
}

export type Tier = {
  id: string
  campaign_id: string
  title: string
  description: string
  min_amount: number // paise
  quantity_limit: number | null
  claimed_count: number
}

export type Pledge = {
  id: string
  campaign_id: string
  tier_id: string | null
  amount: number // paise
  currency: string
  status: string
  order_id: string
}

export type Config = {
  razorpay_key_id: string
}

async function request<T>(path: string, opts: { method?: string; body?: unknown } = {}): Promise<T> {
  const { method = 'GET', body } = opts
  const res = await fetch(BASE + path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const detail = await res.json().catch(() => null as unknown)
    const msg = (detail as { error?: { message?: string } } | null)?.error?.message
    throw new Error(msg || `${method} ${path} failed (${res.status})`)
  }
  return res.json() as Promise<T>
}

export const getConfig = () => request<Config>('/config')
export const getCampaigns = () => request<Campaign[]>('/campaigns')
export const getCampaign = (id: string) => request<Campaign>(`/campaigns/${id}`)
export const createCampaign = (data: {
  creator_id: string
  title: string
  tagline: string
  synopsis: string
  category: string
  goal: number // paise
}) => request<Campaign>('/campaigns', { method: 'POST', body: data })

export const publishCampaign = (id: string) => request<Campaign>(`/campaigns/${id}/publish`, { method: 'POST' })

export const getTiers = (id: string) => request<Tier[]>(`/campaigns/${id}/tiers`)

export const addTier = (
  id: string,
  data: { title: string; description: string; min_amount: number; quantity_limit: number | null },
) => request<Tier>(`/campaigns/${id}/tiers`, { method: 'POST', body: data })

export const createPledge = (
  id: string,
  data: { backer_id: string; tier_id: string | null; amount: number; message: string; anonymous: boolean },
) => request<Pledge>(`/campaigns/${id}/pledges`, { method: 'POST', body: data })

// Settles pledge from Checkout callback — server re-reads amount from Razorpay
export const confirmPledge = (pledgeId: string, checkout: unknown) =>
  request<Pledge & { status: string }>(`/pledges/${pledgeId}/confirm`, { method: 'POST', body: checkout })
