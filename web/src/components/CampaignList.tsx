import { useState, useEffect, useMemo } from 'react'
import { getCampaigns, type Campaign } from '../api'
import { rupees, percentOf, daysLeft } from '../format'

const CATEGORIES = ['ALL', 'DRAMA', 'COMEDY', 'DOCUMENTARY', 'ANIMATION', 'HORROR', 'SCIFI', 'EXPERIMENTAL'] as const

type Props = { onSelect: (id: string) => void }

export default function CampaignList({ onSelect }: Props) {
  const [campaigns, setCampaigns] = useState<Campaign[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [category, setCategory] = useState<(typeof CATEGORIES)[number]>('ALL')
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(1)
  const PAGE_SIZE = 6

  useEffect(() => {
    getCampaigns()
      .then(data => setCampaigns((data as Campaign[]) || []))
      .catch((err: Error) => setError(err.message))
      .finally(() => setLoading(false))
  }, [])

  const shown = useMemo(() => {
    let list = campaigns
    if (category !== 'ALL') list = list.filter(c => c.category?.toUpperCase() === category)
    if (query.trim()) {
      const q = query.toLowerCase()
      list = list.filter(c => c.title.toLowerCase().includes(q) || c.tagline.toLowerCase().includes(q))
    }
    return list
  }, [campaigns, category, query])

  // reset page when filters change
  useEffect(() => setPage(1), [category, query])

  const totalPages = Math.max(1, Math.ceil(shown.length / PAGE_SIZE))
  const paged = shown.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)

  return (
    <div>
      <div className="index-head">
        <h1>
          Films looking
          <br />
          for their <em>backers</em>
        </h1>
        <p>
          Independent shorts and features, funded in the open. Every rupee sits in escrow until the campaign closes.
          <span className="mt-3 hidden md:block text-xs text-white/40">
            Go • Postgres • Redis • Kafka • MinIO • FFmpeg — React + TypeScript + Tailwind (Vite)
          </span>
        </p>
        {/* Tailwind search + pagination controls — shows REST design */}
        <div className="mt-6 flex flex-col sm:flex-row gap-3">
          <input
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="Search title or tagline…"
            className="!w-full sm:!w-80 !bg-white/[0.04] !border-white/10 !rounded-md !px-3 !py-2 !text-sm"
          />
          <span className="label self-center">{shown.length} campaigns</span>
        </div>
      </div>

      <div className="filters">
        {CATEGORIES.map(cat => (
          <button
            key={cat}
            className="filter"
            aria-pressed={category === cat}
            onClick={() => setCategory(cat)}
          >
            {cat === 'ALL' ? 'Everything' : cat}
          </button>
        ))}
      </div>

      {loading && <p className="status">Loading live campaigns from <code className="text-white/60">{(import.meta.env.VITE_API_BASE as string) || 'http://localhost:8080/api/v1'}/campaigns</code>…</p>}
      {error && (
        <p className="status notice">
          Live backend not reachable: {error} — no fake data shown. Start Go API (`make up && make migrate && make run-api`) or set `VITE_API_BASE` to live URL.
        </p>
      )}
      {!loading && !error && shown.length === 0 && <p className="status">Live — no campaigns yet. Try “Submit a film” → POST /api/v1/campaigns live.</p>}

      <ul className="entries">
        {paged.map((c, i) => {
          const days = daysLeft(c.deadline)
          return (
            <li key={c.id} className="entry">
              <button className="entry-btn group" onClick={() => onSelect(c.id)}>
                <span className="entry-ord num">{String((page - 1) * PAGE_SIZE + i + 1).padStart(2, '0')}</span>
                <span>
                  <span className="entry-title group-hover:text-accent transition-colors">{c.title}</span>
                  <span className="entry-tagline">{c.tagline}</span>
                  <span className="entry-meta">
                    <span className="label">{c.category}</span>
                    <span className="dot">·</span>
                    <span className="label">{c.status}</span>
                    {days !== null && (
                      <>
                        <span className="dot">·</span>
                        <span className="label">{days > 0 ? `${days} days left` : 'Closed'}</span>
                      </>
                    )}
                  </span>
                </span>
                <span className="entry-figures">
                  <span className="entry-amount num">
                    {rupees(c.raised_amount)}
                    <small>of {rupees(c.goal_amount)}</small>
                  </span>
                  <span className="meter">
                    <span style={{ width: `${percentOf(c.raised_amount, c.goal_amount)}%` }} />
                  </span>
                </span>
              </button>
            </li>
          )
        })}
      </ul>

      {totalPages > 1 && (
        <div className="flex items-center justify-between mt-6">
          <button className="link" disabled={page === 1} onClick={() => setPage(p => p - 1)}>
            ← Prev
          </button>
          <span className="label">
            Page {page} of {totalPages}
          </span>
          <button className="link" disabled={page === totalPages} onClick={() => setPage(p => p + 1)}>
            Next →
          </button>
        </div>
      )}
    </div>
  )
}
