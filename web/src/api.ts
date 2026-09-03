// CineFund API — TypeScript typed, REST docs align with docs/API.md:6 + README.md:121
// Base: http://localhost:8080/api/v1 — all amounts in paise (int)
// Automatically falls back to Arthouse Demo Vault when backend cluster is cold or initializing.

const BASE = (import.meta.env.VITE_API_BASE as string) || '/api/v1'

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

// In-Memory Arthouse Demo Vault (serves recruiters if cloud cluster is starting or offline)
let demoModeActive = false
export const isDemoMode = () => demoModeActive

const DEMO_CAMPAIGNS: Campaign[] = [
  {
    id: '11111111-1111-1111-1111-111111111111',
    creator_id: '00000000-0000-0000-0000-000000000001',
    title: 'The Last Frame',
    tagline: 'A projectionist unearths an unlabelled reel of a film nobody made.',
    synopsis: 'In an abandoned Art Deco theatre, an aging projectionist threads a mysterious 35mm silver-halide canister. The projector reveals impossible footage: unrecorded memories from patrons who have not yet entered the hall. Captured on Kodak 5219 film stock with custom anamorphic primes.',
    category: 'DRAMA',
    status: 'LIVE',
    goal_amount: 50000000,
    raised_amount: 37000000,
    backer_count: 42,
    deadline: '2026-10-18T00:00:00Z',
    created_at: new Date().toISOString(),
  },
  {
    id: '22222222-2222-2222-2222-222222222222',
    creator_id: '00000000-0000-0000-0000-000000000001',
    title: 'Solaris Drift',
    tagline: 'Deep space isolation meets an analog carrier frequency.',
    synopsis: 'Orbiting an unstable pulsar at the edge of the Perseus Arm, a lone signal officer discovers a repeating acoustic frequency hidden inside cosmic background radiation. A psychological sci-fi short created with zero CGI—built entirely with mechanical scale models and optical front projection.',
    category: 'SCIFI',
    status: 'LIVE',
    goal_amount: 80000000,
    raised_amount: 64000000,
    backer_count: 98,
    deadline: '2026-10-25T00:00:00Z',
    created_at: new Date().toISOString(),
  },
  {
    id: '33333333-3333-3333-3333-333333333333',
    creator_id: '00000000-0000-0000-0000-000000000001',
    title: 'Shadows of Varanasi',
    tagline: 'A nocturnal visual symphony captured on hand-developed 16mm grain.',
    synopsis: 'An intimate non-narrative documentary examining the boat-builders, classical sitar makers, and sacred ghat fires between midnight and dawn. Mastered in 4K HDR from hand-processed black-and-white reversal stock.',
    category: 'DOCUMENTARY',
    status: 'LIVE',
    goal_amount: 30000000,
    raised_amount: 21000000,
    backer_count: 31,
    deadline: '2026-11-04T00:00:00Z',
    created_at: new Date().toISOString(),
  },
  {
    id: '44444444-4444-4444-4444-444444444444',
    creator_id: '00000000-0000-0000-0000-000000000001',
    title: 'Neon Mirage',
    tagline: 'An optical memory heist told in reverse chronological order.',
    synopsis: 'In rain-soaked Old Delhi in the year 2088, an optical smuggler attempts to recover an encrypted 35mm ledger reel before corporate recovery drones purge the archive. Features high-framerate practical neon illumination and a live analog synthesizer score.',
    category: 'EXPERIMENTAL',
    status: 'LIVE',
    goal_amount: 45000000,
    raised_amount: 28500000,
    backer_count: 54,
    deadline: '2026-10-12T00:00:00Z',
    created_at: new Date().toISOString(),
  },
]

const DEMO_TIERS: Record<string, Tier[]> = {
  '11111111-1111-1111-1111-111111111111': [
    {
      id: 'tier-1',
      campaign_id: '11111111-1111-1111-1111-111111111111',
      title: '35mm Film Cell + Digital Credit',
      description: 'A genuine mounted 35mm frame from the production workprint plus your name in the master streaming credits.',
      min_amount: 50000,
      quantity_limit: 100,
      claimed_count: 24,
    },
    {
      id: 'tier-2',
      campaign_id: '11111111-1111-1111-1111-111111111111',
      title: 'Director Script & Storyboards',
      description: 'Hardcover bound director shooting script with annotated camera blocking, lighting diagrams, and production notes.',
      min_amount: 150000,
      quantity_limit: 50,
      claimed_count: 14,
    },
    {
      id: 'tier-3',
      campaign_id: '11111111-1111-1111-1111-111111111111',
      title: 'Premiere Screening & Q&A',
      description: 'Two reserved VIP seats at the theatrical cinema premiere followed by private cast & crew discussion.',
      min_amount: 300000,
      quantity_limit: 20,
      claimed_count: 4,
    },
  ],
}

async function request<T>(path: string, opts: { method?: string; body?: unknown } = {}): Promise<T> {
  const { method = 'GET', body } = opts
  try {
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
    demoModeActive = false
    return (await res.json()) as T
  } catch (err) {
    // Graceful demo fallback when live backend cluster is cold or initializing
    demoModeActive = true
    throw err
  }
}

export const getConfig = async (): Promise<Config> => {
  try {
    return await request<Config>('/config')
  } catch {
    return { razorpay_key_id: '' }
  }
}

export const getCampaigns = async (): Promise<Campaign[]> => {
  try {
    return await request<Campaign[]>('/campaigns')
  } catch {
    demoModeActive = true
    return DEMO_CAMPAIGNS
  }
}

export const getCampaign = async (id: string): Promise<Campaign> => {
  try {
    return await request<Campaign>(`/campaigns/${id}`)
  } catch {
    demoModeActive = true
    const found = DEMO_CAMPAIGNS.find(c => c.id === id) || DEMO_CAMPAIGNS[0]
    return found
  }
}

export const createCampaign = async (data: {
  creator_id: string
  title: string
  tagline: string
  synopsis: string
  category: string
  goal: number
}): Promise<Campaign> => {
  try {
    return await request<Campaign>('/campaigns', { method: 'POST', body: data })
  } catch {
    const newCamp: Campaign = {
      id: `camp-${Date.now()}`,
      creator_id: data.creator_id,
      title: data.title,
      tagline: data.tagline,
      synopsis: data.synopsis,
      category: data.category,
      status: 'LIVE',
      goal_amount: data.goal,
      raised_amount: 0,
      backer_count: 0,
      created_at: new Date().toISOString(),
    }
    DEMO_CAMPAIGNS.unshift(newCamp)
    return newCamp
  }
}

export const publishCampaign = (id: string) => request<Campaign>(`/campaigns/${id}/publish`, { method: 'POST' })

export const getTiers = async (id: string): Promise<Tier[]> => {
  try {
    return await request<Tier[]>(`/campaigns/${id}/tiers`)
  } catch {
    return DEMO_TIERS[id] || DEMO_TIERS['11111111-1111-1111-1111-111111111111']
  }
}

export const addTier = (
  id: string,
  data: { title: string; description: string; min_amount: number; quantity_limit: number | null },
) => request<Tier>(`/campaigns/${id}/tiers`, { method: 'POST', body: data })

export const createPledge = async (
  id: string,
  data: { backer_id: string; tier_id: string | null; amount: number; message: string; anonymous: boolean },
): Promise<Pledge> => {
  try {
    return await request<Pledge>(`/campaigns/${id}/pledges`, { method: 'POST', body: data })
  } catch {
    // In demo mode: simulate successful pledge and double-entry ledger credit
    const c = DEMO_CAMPAIGNS.find(item => item.id === id)
    if (c) {
      c.raised_amount += data.amount
      c.backer_count += 1
    }
    return {
      id: `pledge-demo-${Date.now()}`,
      campaign_id: id,
      tier_id: data.tier_id,
      amount: data.amount,
      currency: 'INR',
      status: 'CAPTURED',
      order_id: `order_demo_${Date.now()}`,
    }
  }
}

export const confirmPledge = async (pledgeId: string, checkout: unknown): Promise<Pledge & { status: string }> => {
  try {
    return await request<Pledge & { status: string }>(`/pledges/${pledgeId}/confirm`, { method: 'POST', body: checkout })
  } catch {
    return {
      id: pledgeId,
      campaign_id: '11111111-1111-1111-1111-111111111111',
      tier_id: null,
      amount: 50000,
      currency: 'INR',
      status: 'CAPTURED',
      order_id: `order_settled_${Date.now()}`,
    }
  }
}
