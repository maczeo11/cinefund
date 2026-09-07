import { useState, useEffect, useMemo } from 'react'
import { getCampaigns, getPosterForCampaign, FALLBACK_POSTER_SVG, type Campaign } from '../api'
import { rupees, percentOf, daysLeft } from '../format'

const CATEGORIES = ['ALL', 'DRAMA', 'COMEDY', 'DOCUMENTARY', 'ANIMATION', 'HORROR', 'SCIFI', 'EXPERIMENTAL'] as const

// Deterministic film gauge tag by campaign index
const GAUGES = ['35MM CELLULOID', '16MM REVERSAL', '70MM ANAMORPHIC', 'SUPER 16']

type Props = { onSelect: (id: string) => void }

export default function CampaignList({ onSelect }: Props) {
  const [campaigns, setCampaigns] = useState<Campaign[]>([])
  const [loading, setLoading] = useState(true)
  const [category, setCategory] = useState<(typeof CATEGORIES)[number]>('ALL')
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(1)
  const PAGE_SIZE = 6

  useEffect(() => {
    getCampaigns()
      .then(data => setCampaigns((data as Campaign[]) || []))
      .catch(() => {})
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

  useEffect(() => setPage(1), [category, query])

  const totalPages = Math.max(1, Math.ceil(shown.length / PAGE_SIZE))
  const paged = shown.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)

  return (
    <div className="py-4">
      {/* Cinematic Hero Section */}
      <div className="relative mb-10 pb-8 border-b border-white/[0.08]">
        <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-amber/10 border border-amber/25 text-amber text-xs font-mono mb-4">
          <span className="h-1.5 w-1.5 rounded-full bg-amber animate-pulse" />
          <span>INDEPENDENT FILMMAKERS • DIRECT-TO-BACKER ESCROW</span>
        </div>

        <h1 className="font-cinema text-3xl sm:text-4xl lg:text-5xl font-extrabold tracking-tight text-silver max-w-3xl leading-tight">
          Films Forged in the <span className="text-amber italic">Darkroom</span>
        </h1>

        <p className="mt-4 text-silver-dim text-sm sm:text-base max-w-2xl leading-relaxed font-sans">
          Short films, auteur features, and experimental reels funded in the open. 
          Every rupee is locked in dual-entry ledger escrow until production wraps.
        </p>

        {/* Search & Filter Bar */}
        <div className="mt-7 flex flex-col sm:flex-row gap-3 items-stretch sm:items-center justify-between">
          <div className="relative flex-1 max-w-md">
            <input
              value={query}
              onChange={e => setQuery(e.target.value)}
              placeholder="Search by title, director, or reel tagline…"
              className="w-full bg-celluloid border border-white/10 rounded-xl px-4 py-2.5 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber/50 focus:ring-1 focus:ring-amber/50 transition-all"
            />
            {query && (
              <button
                onClick={() => setQuery('')}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-silver-dim hover:text-white"
              >
                ✕
              </button>
            )}
          </div>
          <div className="flex items-center gap-3 text-xs font-mono text-silver-dim self-end sm:self-center">
            <span className="h-2 w-2 rounded-full bg-emerald-500" />
            <span>{shown.length} Films in Catalog</span>
          </div>
        </div>
      </div>

      {/* Genre Filter Chips */}
      <div className="flex gap-2 overflow-x-auto pb-4 mb-8 scrollbar-none font-mono text-xs">
        {CATEGORIES.map(cat => {
          const active = category === cat
          return (
            <button
              key={cat}
              onClick={() => setCategory(cat)}
              className={`px-3.5 py-1.5 rounded-lg whitespace-nowrap transition-all uppercase tracking-wider ${
                active
                  ? 'bg-amber text-ink font-bold shadow-[0_0_12px_rgba(229,169,60,0.3)]'
                  : 'bg-white/[0.03] border border-white/10 text-silver-dim hover:text-silver hover:bg-white/[0.06]'
              }`}
            >
              {cat === 'ALL' ? 'All Formats' : cat}
            </button>
          )
        })}
      </div>

      {/* Loading State */}
      {loading && (
        <div className="p-12 text-center text-sm font-mono text-silver-dim">
          <span className="inline-block animate-spin mr-2">✦</span> Threading 35mm film reels…
        </div>
      )}

      {/* Empty State */}
      {!loading && shown.length === 0 && (
        <div className="p-12 text-center bg-celluloid border border-white/10 rounded-2xl">
          <p className="font-cinema text-lg text-silver mb-2">No reels matching filter</p>
          <p className="text-xs text-silver-dim font-sans mb-4">Try clearing your search or launch a new campaign.</p>
          <button onClick={() => { setCategory('ALL'); setQuery('') }} className="tw-badge">
            Reset Filters
          </button>
        </div>
      )}

      {/* Cinematic Film Cards Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {paged.map((c, i) => {
          const days = daysLeft(c.deadline)
          const gauge = GAUGES[i % GAUGES.length]
          const percent = percentOf(c.raised_amount, c.goal_amount)
          const poster = getPosterForCampaign(c)

          return (
            <div
              key={c.id}
              onClick={() => onSelect(c.id)}
              className="group bg-celluloid border border-white/[0.08] hover:border-amber/40 rounded-2xl p-4 sm:p-5 transition-all duration-300 cursor-pointer flex flex-col justify-between hover:shadow-[0_4px_35px_rgba(229,169,60,0.15)] relative overflow-hidden"
            >
              {/* Subtle film grain gradient overlay */}
              <div className="absolute inset-0 bg-gradient-to-br from-amber/[0.02] to-transparent pointer-events-none" />

              <div>
                {/* Widescreen 16:9 Cinematic Poster Thumbnail */}
                <div className="relative aspect-video w-full overflow-hidden rounded-xl bg-black/80 border border-white/[0.1] mb-4 group-hover:border-amber/40 transition-colors shadow-lg">
                  <img
                    src={poster}
                    alt={c.title}
                    loading="lazy"
                    onError={(e) => {
                      e.currentTarget.onerror = null
                      e.currentTarget.src = FALLBACK_POSTER_SVG
                    }}
                    className="w-full h-full object-cover object-center group-hover:scale-105 transition-transform duration-700 ease-out"
                  />

                  {/* Gradient overlays for cinematic depth & legibility */}
                  <div className="absolute inset-0 bg-gradient-to-t from-celluloid via-celluloid/20 to-transparent pointer-events-none" />
                  <div className="absolute inset-0 bg-gradient-to-b from-black/40 via-transparent to-transparent pointer-events-none" />

                  {/* Top Badge Overlay */}
                  <div className="absolute top-2.5 left-2.5 right-2.5 flex items-center justify-between gap-2 pointer-events-none">
                    <span className="px-2 py-0.5 rounded bg-black/75 backdrop-blur-md border border-white/20 text-[10px] font-mono text-silver font-semibold shadow">
                      {gauge}
                    </span>
                    <span className="px-2 py-0.5 rounded-full bg-black/75 backdrop-blur-md border border-white/20 text-[10px] font-mono font-semibold flex items-center gap-1.5 shadow">
                      <span className="h-1.5 w-1.5 rounded-full bg-crimson animate-ping" />
                      <span className="text-crimson">{c.status}</span>
                    </span>
                  </div>

                  {/* Bottom Category Badge */}
                  <div className="absolute bottom-2.5 left-2.5 pointer-events-none">
                    <span className="px-2.5 py-0.5 rounded-full bg-amber/20 backdrop-blur-md border border-amber/40 text-[10px] font-mono text-amber font-bold uppercase tracking-wider shadow">
                      {c.category}
                    </span>
                  </div>
                </div>

                {/* Film Title & Tagline */}
                <h2 className="font-cinema text-xl sm:text-2xl font-bold text-silver group-hover:text-amber transition-colors tracking-wide line-clamp-1">
                  {c.title}
                </h2>
                <p className="mt-1.5 text-xs sm:text-sm text-silver-dim font-sans line-clamp-2 leading-relaxed">
                  {c.tagline}
                </p>
              </div>

              {/* Funding Progress Section */}
              <div className="mt-6 pt-4 border-t border-white/[0.06]">
                <div className="flex justify-between items-baseline mb-2">
                  <div className="font-mono">
                    <span className="text-base sm:text-lg font-bold text-silver">{rupees(c.raised_amount)}</span>
                    <span className="text-xs text-silver-faint ml-1.5">of {rupees(c.goal_amount)}</span>
                  </div>
                  <span className="text-xs font-mono font-bold text-amber bg-amber/10 px-2 py-0.5 rounded border border-amber/20">
                    {percent}% FUNDED
                  </span>
                </div>

                {/* Precision Film Exposure Meter */}
                <div className="w-full h-2 rounded-full bg-black/60 border border-white/10 overflow-hidden relative">
                  <div
                    className="h-full bg-gradient-to-r from-amber to-amber-bright rounded-full transition-all duration-500 shadow-[0_0_8px_rgba(229,169,60,0.4)]"
                    style={{ width: `${Math.min(100, percent)}%` }}
                  />
                </div>

                {/* Footer Credits */}
                <div className="flex items-center justify-between text-xs font-mono text-silver-dim mt-3.5">
                  <div className="flex items-center gap-1.5">
                    <span className="text-silver font-semibold">{c.backer_count}</span>
                    <span className="text-silver-faint">Backers</span>
                  </div>

                  <div className="flex items-center gap-1.5">
                    <span className="text-silver font-semibold">{days !== null && days > 0 ? `${days}d` : 'Closing'}</span>
                    <span className="text-silver-faint">remaining</span>
                  </div>

                  <span className="text-amber text-[11px] group-hover:translate-x-1 transition-transform flex items-center gap-1">
                    View Reel →
                  </span>
                </div>
              </div>
            </div>
          )
        })}
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between mt-10 pt-6 border-t border-white/[0.08] font-mono text-xs">
          <button
            disabled={page === 1}
            onClick={() => setPage(p => p - 1)}
            className="px-4 py-2 rounded-lg border border-white/10 bg-white/[0.02] text-silver hover:bg-white/[0.06] disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            ← Previous Reels
          </button>
          <span className="text-silver-dim">
            Frame {page} of {totalPages}
          </span>
          <button
            disabled={page === totalPages}
            onClick={() => setPage(p => p + 1)}
            className="px-4 py-2 rounded-lg border border-white/10 bg-white/[0.02] text-silver hover:bg-white/[0.06] disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            Next Reels →
          </button>
        </div>
      )}
    </div>
  )
}
