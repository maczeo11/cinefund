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
  poster_url?: string
  cover_key?: string
  backdrop_url?: string
  creator_name?: string
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

export const FALLBACK_POSTER_SVG = `data:image/svg+xml;utf8,<svg xmlns="http://www.w3.org/2000/svg" width="800" height="450" viewBox="0 0 800 450"><defs><linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="%230B0D13"/><stop offset="50%" stop-color="%23181D2C"/><stop offset="100%" stop-color="%230B0D13"/></linearGradient></defs><rect width="800" height="450" fill="url(%23g)"/><circle cx="400" cy="200" r="64" fill="none" stroke="%23E5A93C" stroke-width="3" opacity="0.6"/><circle cx="400" cy="200" r="22" fill="%23E5A93C" opacity="0.4"/><circle cx="365" cy="200" r="12" fill="%230B0D13" stroke="%23E5A93C" stroke-width="2" opacity="0.7"/><circle cx="435" cy="200" r="12" fill="%230B0D13" stroke="%23E5A93C" stroke-width="2" opacity="0.7"/><circle cx="400" cy="165" r="12" fill="%230B0D13" stroke="%23E5A93C" stroke-width="2" opacity="0.7"/><circle cx="400" cy="235" r="12" fill="%230B0D13" stroke="%23E5A93C" stroke-width="2" opacity="0.7"/><text x="400" y="310" fill="%23E5A93C" font-family="Cinzel,serif" font-size="18" font-weight="bold" letter-spacing="4" text-anchor="middle">CINEFUND 35MM REEL</text><text x="400" y="335" fill="%238B95A5" font-family="monospace" font-size="11" letter-spacing="2" text-anchor="middle">CELLULOID VAULT</text></svg>`

export const GENRE_POSTERS: Record<string, string> = {
  DRAMA: 'https://images.unsplash.com/photo-1489599849927-2ee91cede3ba?auto=format&fit=crop&w=1200&q=80',
  SCIFI: 'https://images.unsplash.com/photo-1451187580459-43490279c0fa?auto=format&fit=crop&w=1200&q=80',
  DOCUMENTARY: 'https://images.unsplash.com/photo-1561361513-2d000a50f0dc?auto=format&fit=crop&w=1200&q=80',
  EXPERIMENTAL: 'https://images.unsplash.com/photo-1509198397868-475647b2a1e5?auto=format&fit=crop&w=1200&q=80',
  HORROR: 'https://images.unsplash.com/photo-1509248961158-e54f6934749c?auto=format&fit=crop&w=1200&q=80',
  COMEDY: 'https://images.unsplash.com/photo-1514306191717-452ec28c7814?auto=format&fit=crop&w=1200&q=80',
  ANIMATION: 'https://images.unsplash.com/photo-1534447677768-be436bb09401?auto=format&fit=crop&w=1200&q=80',
}

export const POSTER_PRESETS = [
  {
    id: 'preset-noir',
    title: '35mm Noir Thriller',
    genre: 'DRAMA',
    url: 'https://images.unsplash.com/photo-1489599849927-2ee91cede3ba?auto=format&fit=crop&w=1200&q=80',
    description: '35mm Art Deco cinema projection room, silver-halide canister mood.',
  },
  {
    id: 'preset-scifi',
    title: 'Solaris Cosmic Drift',
    genre: 'SCIFI',
    url: 'https://images.unsplash.com/photo-1451187580459-43490279c0fa?auto=format&fit=crop&w=1200&q=80',
    description: 'Deep space cosmic nebula, orbital carrier frequency, analog pulsar.',
  },
  {
    id: 'preset-varanasi',
    title: '16mm Ghats Nocturne',
    genre: 'DOCUMENTARY',
    url: 'https://images.unsplash.com/photo-1561361513-2d000a50f0dc?auto=format&fit=crop&w=1200&q=80',
    description: 'Nocturnal visual symphony, 16mm hand-developed grain, sacred river dawn.',
  },
  {
    id: 'preset-neon',
    title: 'Cyberpunk Neon Heist',
    genre: 'EXPERIMENTAL',
    url: 'https://images.unsplash.com/photo-1509198397868-475647b2a1e5?auto=format&fit=crop&w=1200&q=80',
    description: 'Rain-soaked neon alleys, analog optical synthesizer heist.',
  },
  {
    id: 'preset-alpine',
    title: '70mm Alpine Vista',
    genre: 'DOCUMENTARY',
    url: 'https://images.unsplash.com/photo-1464822759023-fed622ff2c3b?auto=format&fit=crop&w=1200&q=80',
    description: 'Sweeping 70mm anamorphic mountain vista, auteur scale cinematography.',
  },
  {
    id: 'preset-anamorphic',
    title: 'Moody Anamorphic Drama',
    genre: 'DRAMA',
    url: 'https://images.unsplash.com/photo-1536440136628-849c177e76a1?auto=format&fit=crop&w=1200&q=80',
    description: 'Warm cinema halogen lighting, reflective character drama.',
  },
]

export function getPosterForCampaign(campaign?: { poster_url?: string; cover_key?: string; category?: string } | null): string {
  if (campaign?.poster_url) return campaign.poster_url
  if (campaign?.cover_key && (campaign.cover_key.startsWith('http') || campaign.cover_key.startsWith('/'))) return campaign.cover_key
  const cat = (campaign?.category || 'DRAMA').toUpperCase()
  return GENRE_POSTERS[cat] || GENRE_POSTERS.DRAMA
}

// In-Memory Arthouse Demo Vault (serves recruiters if cloud cluster is starting or offline)
let demoModeActive = false
export const isDemoMode = () => demoModeActive

const DEMO_CAMPAIGNS: Campaign[] = [
  {
    id: '11111111-1111-1111-1111-111111111111',
    creator_id: '00000000-0000-0000-0000-000000000001',
    creator_name: 'Ava Chen',
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
    poster_url: 'https://images.unsplash.com/photo-1489599849927-2ee91cede3ba?auto=format&fit=crop&w=1200&q=80',
  },
  {
    id: '22222222-2222-2222-2222-222222222222',
    creator_id: '00000000-0000-0000-0000-000000000001',
    creator_name: 'Marcus Vance',
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
    poster_url: 'https://images.unsplash.com/photo-1451187580459-43490279c0fa?auto=format&fit=crop&w=1200&q=80',
  },
  {
    id: '33333333-3333-3333-3333-333333333333',
    creator_id: '00000000-0000-0000-0000-000000000001',
    creator_name: 'Priya Sharma',
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
    poster_url: 'https://images.unsplash.com/photo-1561361513-2d000a50f0dc?auto=format&fit=crop&w=1200&q=80',
  },
  {
    id: '44444444-4444-4444-4444-444444444444',
    creator_id: '00000000-0000-0000-0000-000000000001',
    creator_name: 'Kaelen Voss',
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
    poster_url: 'https://images.unsplash.com/photo-1509198397868-475647b2a1e5?auto=format&fit=crop&w=1200&q=80',
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
  const headers: Record<string, string> = {}
  if (body) {
    headers['Content-Type'] = 'application/json'
  }
  try {
    const userStr = localStorage.getItem('cinefund_current_user')
    if (userStr && userStr !== 'null') {
      const u = JSON.parse(userStr)
      if (u?.token) {
        headers['Authorization'] = `Bearer ${u.token}`
      }
      if (u?.id) {
        headers['X-User-ID'] = u.id
      }
    }
  } catch {
    // ignore
  }
  try {
    const res = await fetch(BASE + path, {
      method,
      headers: Object.keys(headers).length > 0 ? headers : undefined,
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

function getMergedDemoCampaigns(): Campaign[] {
  let campaigns = DEMO_CAMPAIGNS
  try {
    const stored = JSON.parse(localStorage.getItem('cinefund_custom_campaigns') || '[]')
    if (Array.isArray(stored) && stored.length > 0) {
      const existingIds = new Set(DEMO_CAMPAIGNS.map(c => c.id))
      const custom = stored.filter((c: Campaign) => !existingIds.has(c.id))
      campaigns = [...custom, ...DEMO_CAMPAIGNS]
    }
  } catch {
    // fallback
  }
  return campaigns.map(c => ({
    ...c,
    poster_url: c.poster_url || getPosterForCampaign(c),
  }))
}

export type UserPledgeRecord = {
  id: string
  campaign_id: string
  campaign_title: string
  tier_id: string | null
  tier_title?: string
  amount: number // paise
  currency: string
  status: string
  order_id: string
  backer_id: string
  backer_name?: string
  poster_url?: string
  created_at: string
}

const SEEDED_PLEDGES: UserPledgeRecord[] = [
  {
    id: 'pledge-seed-1',
    campaign_id: '11111111-1111-1111-1111-111111111111',
    campaign_title: 'The Last Frame',
    tier_id: 'tier-1',
    tier_title: '35mm Film Cell + Digital Credit',
    amount: 50000,
    currency: 'INR',
    status: 'CAPTURED',
    order_id: 'order_seed_ravi_1',
    backer_id: '00000000-0000-0000-0000-000000000002',
    backer_name: 'Ravi',
    poster_url: 'https://images.unsplash.com/photo-1489599849927-2ee91cede3ba?auto=format&fit=crop&w=1200&q=80',
    created_at: new Date(Date.now() - 86400000 * 3).toISOString(),
  },
  {
    id: 'pledge-seed-2',
    campaign_id: '22222222-2222-2222-2222-222222222222',
    campaign_title: 'Solaris Drift',
    tier_id: null,
    tier_title: 'Direct Escrow Patronage',
    amount: 150000,
    currency: 'INR',
    status: 'CAPTURED',
    order_id: 'order_seed_ravi_2',
    backer_id: '00000000-0000-0000-0000-000000000002',
    backer_name: 'Ravi',
    poster_url: 'https://images.unsplash.com/photo-1451187580459-43490279c0fa?auto=format&fit=crop&w=1200&q=80',
    created_at: new Date(Date.now() - 86400000).toISOString(),
  },
]

export function getUserPledges(backerId?: string): UserPledgeRecord[] {
  try {
    const saved = localStorage.getItem('cinefund_user_pledges')
    const allPledges: UserPledgeRecord[] = saved ? JSON.parse(saved) : SEEDED_PLEDGES
    if (!saved) {
      localStorage.setItem('cinefund_user_pledges', JSON.stringify(SEEDED_PLEDGES))
    }
    if (backerId) {
      return allPledges.filter(p => p.backer_id === backerId)
    }
    return allPledges
  } catch {
    return backerId ? SEEDED_PLEDGES.filter(p => p.backer_id === backerId) : SEEDED_PLEDGES
  }
}

export function recordUserPledge(pledge: UserPledgeRecord) {
  try {
    const existing = getUserPledges()
    existing.unshift(pledge)
    localStorage.setItem('cinefund_user_pledges', JSON.stringify(existing))
    window.dispatchEvent(new Event('cinefund_pledge_added'))
  } catch {
    // ignore
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
    const res = await request<Campaign[]>('/campaigns')
    // enrich any response missing poster_url with genre defaults
    return res.map(c => ({
      ...c,
      poster_url: c.poster_url || getPosterForCampaign(c),
    }))
  } catch {
    demoModeActive = true
    return getMergedDemoCampaigns()
  }
}

export const getCampaign = async (id: string): Promise<Campaign> => {
  try {
    const camp = await request<Campaign>(`/campaigns/${id}`)
    return {
      ...camp,
      poster_url: camp.poster_url || getPosterForCampaign(camp),
    }
  } catch {
    demoModeActive = true
    const all = getMergedDemoCampaigns()
    const found = all.find(c => c.id === id) || all[0]
    return {
      ...found,
      poster_url: found.poster_url || getPosterForCampaign(found),
    }
  }
}

export const createCampaign = async (data: {
  creator_id: string
  title: string
  tagline: string
  synopsis: string
  category: string
  goal: number
  poster_url?: string
  creator_name?: string
}): Promise<Campaign> => {
  try {
    const payload = {
      ...data,
      cover_key: data.poster_url,
    }
    return await request<Campaign>('/campaigns', { method: 'POST', body: payload })
  } catch {
    const poster = data.poster_url?.trim() || getPosterForCampaign({ category: data.category })
    const newCamp: Campaign = {
      id: `camp-${Date.now()}`,
      creator_id: data.creator_id,
      creator_name: data.creator_name || 'Ava Chen',
      title: data.title,
      tagline: data.tagline,
      synopsis: data.synopsis,
      category: data.category,
      status: 'LIVE',
      goal_amount: data.goal,
      raised_amount: 0,
      backer_count: 0,
      created_at: new Date().toISOString(),
      poster_url: poster,
    }
    DEMO_CAMPAIGNS.unshift(newCamp)
    try {
      const stored = JSON.parse(localStorage.getItem('cinefund_custom_campaigns') || '[]')
      stored.unshift(newCamp)
      localStorage.setItem('cinefund_custom_campaigns', JSON.stringify(stored))
    } catch {
      // quota fallback
    }
    return newCamp
  }
}

export const publishCampaign = async (id: string): Promise<Campaign> => {
  try {
    return await request<Campaign>(`/campaigns/${id}/publish`, { method: 'POST' })
  } catch {
    demoModeActive = true
    const all = getMergedDemoCampaigns()
    const found = all.find(c => c.id === id)
    if (found) {
      found.status = 'LIVE'
    }
    try {
      const stored = JSON.parse(localStorage.getItem('cinefund_custom_campaigns') || '[]')
      const target = stored.find((c: Campaign) => c.id === id)
      if (target) {
        target.status = 'LIVE'
        localStorage.setItem('cinefund_custom_campaigns', JSON.stringify(stored))
      }
    } catch {
      // ignore
    }
    return found || DEMO_CAMPAIGNS[0]
  }
}

export const getTiers = async (id: string): Promise<Tier[]> => {
  try {
    return await request<Tier[]>(`/campaigns/${id}/tiers`)
  } catch {
    try {
      const customTiers = JSON.parse(localStorage.getItem('cinefund_custom_tiers') || '{}')
      if (customTiers[id] && customTiers[id].length > 0) {
        return customTiers[id]
      }
    } catch {
      // ignore
    }
    return DEMO_TIERS[id] || DEMO_TIERS['11111111-1111-1111-1111-111111111111'] || []
  }
}

export const addTier = async (
  id: string,
  data: { title: string; description: string; min_amount: number; quantity_limit: number | null },
): Promise<Tier> => {
  try {
    return await request<Tier>(`/campaigns/${id}/tiers`, { method: 'POST', body: data })
  } catch {
    const newTier: Tier = {
      id: `tier-${Date.now()}`,
      campaign_id: id,
      title: data.title,
      description: data.description,
      min_amount: data.min_amount,
      quantity_limit: data.quantity_limit,
      claimed_count: 0,
    }
    if (!DEMO_TIERS[id]) {
      DEMO_TIERS[id] = []
    }
    DEMO_TIERS[id].push(newTier)
    try {
      const stored = JSON.parse(localStorage.getItem('cinefund_custom_tiers') || '{}')
      if (!stored[id]) stored[id] = []
      stored[id].push(newTier)
      localStorage.setItem('cinefund_custom_tiers', JSON.stringify(stored))
    } catch {
      // quota fallback
    }
    return newTier
  }
}

export const createPledge = async (
  id: string,
  data: { backer_id: string; tier_id: string | null; amount: number; message: string; anonymous: boolean; backer_name?: string },
): Promise<Pledge> => {
  const all = getMergedDemoCampaigns()
  const camp = all.find(c => c.id === id)
  const tierList = DEMO_TIERS[id] || []
  const matchedTier = tierList.find(t => t.id === data.tier_id)

  try {
    const pledge = await request<Pledge>(`/campaigns/${id}/pledges`, { method: 'POST', body: data })
    recordUserPledge({
      id: pledge.id,
      campaign_id: id,
      campaign_title: camp?.title || '35mm Film Project',
      tier_id: data.tier_id,
      tier_title: matchedTier?.title || 'Backer Pledge',
      amount: data.amount,
      currency: 'INR',
      status: 'CAPTURED',
      order_id: pledge.order_id,
      backer_id: data.backer_id,
      backer_name: data.backer_name || 'Anonymous Patron',
      poster_url: getPosterForCampaign(camp),
      created_at: new Date().toISOString(),
    })
    return pledge
  } catch {
    // In demo mode: simulate successful pledge and double-entry ledger credit
    const c = DEMO_CAMPAIGNS.find(item => item.id === id)
    if (c) {
      c.raised_amount += data.amount
      c.backer_count += 1
    }
    const pledgeId = `pledge-demo-${Date.now()}`
    const orderId = `order_demo_${Date.now()}`

    recordUserPledge({
      id: pledgeId,
      campaign_id: id,
      campaign_title: camp?.title || '35mm Film Project',
      tier_id: data.tier_id,
      tier_title: matchedTier?.title || 'Backer Pledge',
      amount: data.amount,
      currency: 'INR',
      status: 'CAPTURED',
      order_id: orderId,
      backer_id: data.backer_id,
      backer_name: data.backer_name || 'Film Patron',
      poster_url: getPosterForCampaign(camp),
      created_at: new Date().toISOString(),
    })

    return {
      id: pledgeId,
      campaign_id: id,
      tier_id: data.tier_id,
      amount: data.amount,
      currency: 'INR',
      status: 'CAPTURED',
      order_id: orderId,
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

export type PresignResponse = {
  asset_id: string
  upload_url: string
}

export const getPresignedUploadUrl = async (data: {
  owner_id: string
  campaign_id?: string
  purpose?: string
  content_type: string
}): Promise<PresignResponse> => {
  return await request<PresignResponse>('/uploads', { method: 'POST', body: data })
}

export const completeUpload = async (assetId: string): Promise<void> => {
  await request<void>(`/uploads/${assetId}/complete`, { method: 'POST' })
}

export const uploadVideoFileToS3 = async (
  file: File,
  ownerId: string,
  campaignId?: string,
  onProgress?: (pct: number) => void
): Promise<string> => {
  try {
    // 1. Fetch presigned S3 PUT URL from CineFund API
    const presign = await getPresignedUploadUrl({
      owner_id: ownerId,
      campaign_id: campaignId,
      purpose: 'FILM',
      content_type: file.type || 'video/mp4',
    })

    // 2. Direct browser-to-S3 upload with live progress tracking
    await new Promise<void>((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('PUT', presign.upload_url, true)
      xhr.setRequestHeader('Content-Type', file.type || 'video/mp4')
      if (xhr.upload && onProgress) {
        xhr.upload.onprogress = (e) => {
          if (e.lengthComputable) {
            onProgress(Math.round((e.loaded / e.total) * 100))
          }
        }
      }
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) resolve()
        else reject(new Error(`S3 direct upload failed with HTTP status ${xhr.status}`))
      }
      xhr.onerror = () => reject(new Error('Network error during S3 direct upload'))
      xhr.send(file)
    })

    // 3. Notify CineFund backend to verify S3 object and enqueue transcode job
    await completeUpload(presign.asset_id)

    return presign.asset_id
  } catch {
    // Resilient fallback for cold backend / offline demo mode
    demoModeActive = true
    const localBlobUrl = URL.createObjectURL(file)
    if (campaignId) {
      try {
        localStorage.setItem(`cinefund_video_${campaignId}`, localBlobUrl)
      } catch {
        // ignore
      }
    }
    // Simulate progressive streaming upload
    if (onProgress) {
      for (let pct = 25; pct <= 100; pct += 25) {
        onProgress(pct)
        await new Promise(r => setTimeout(r, 60))
      }
    }
    return `asset-demo-${Date.now()}`
  }
}
