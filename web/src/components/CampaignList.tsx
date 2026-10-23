import { useState, useEffect, useMemo } from 'react'
import { getCampaigns, getPosterForCampaign, FALLBACK_POSTER_SVG, type Campaign } from '../api'
import { rupees, percentOf, daysLeft, campaignPhase, PHASE_LABEL, type Phase } from '../format'
import HowItWorks from './HowItWorks.tsx'
import { usePageTitle } from '../usePageTitle'

// Red is reserved for "live"; funded reads teal; everything else is muted.
const PHASE_DOT: Record<Phase, string> = {
  live: 'bg-crimson',
  funded: 'bg-emerald-400',
  ended: 'bg-silver-faint',
  draft: 'bg-silver-faint',
}
const PHASE_TEXT: Record<Phase, string> = {
  live: 'text-crimson',
  funded: 'text-emerald-400',
  ended: 'text-silver-dim',
  draft: 'text-silver-dim',
}

const CATEGORIES = ['ALL', 'DRAMA', 'COMEDY', 'DOCUMENTARY', 'ANIMATION', 'HORROR', 'SCIFI', 'EXPERIMENTAL'] as const

const PHASE_RANK: Record<Phase, number> = { live: 0, funded: 1, ended: 2, draft: 3 }

type Props = { onSelect: (id: string) => void; onStartCampaign: () => void }

export default function CampaignList({ onSelect, onStartCampaign }: Props) {
  usePageTitle()
  const [campaigns, setCampaigns] = useState<Campaign[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [category, setCategory] = useState<(typeof CATEGORIES)[number]>('ALL')
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(1)
  const PAGE_SIZE = 6

  useEffect(() => {
    let cancelled = false
    getCampaigns()
      .then(data => {
        if (!cancelled) setCampaigns(data || [])
      })
      .catch(err => {
        if (!cancelled) setLoadError((err as Error).message || 'Could not load films.')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const stats = useMemo(
    () => ({
      live: campaigns.filter(c => campaignPhase(c) === 'live').length,
      raised: campaigns.reduce((sum, c) => sum + (c.raised_amount || 0), 0),
      backers: campaigns.reduce((sum, c) => sum + (c.backer_count || 0), 0),
    }),
    [campaigns],
  )

  const shown = useMemo(() => {
    let list = campaigns
    if (category !== 'ALL') list = list.filter(c => c.category?.toUpperCase() === category)
    if (query.trim()) {
      const q = query.toLowerCase()
      list = list.filter(c => c.title.toLowerCase().includes(q) || c.tagline.toLowerCase().includes(q))
    }
    // Films you can still back come first; finished ones sink (stable sort keeps server order within a group).
    return [...list].sort((a, b) => PHASE_RANK[campaignPhase(a)] - PHASE_RANK[campaignPhase(b)])
  }, [campaigns, category, query])

  useEffect(() => setPage(1), [category, query])

  const totalPages = Math.max(1, Math.ceil(shown.length / PAGE_SIZE))
  const paged = shown.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)

  return (
    <div className="py-4">
      {/* Hero: what this is, what to do next */}
      <section aria-labelledby="hero-heading" className="relative pt-6 sm:pt-12 pb-10 sm:pb-14">
        <h1 id="hero-heading" className="font-cinema text-4xl sm:text-5xl lg:text-6xl font-extrabold tracking-tight text-silver max-w-4xl leading-[1.08]">
          Fund independent films.{' '}
          <span className="text-amber italic">Watch them come to life.</span>
        </h1>

        <p className="mt-5 text-silver-dim text-base sm:text-lg max-w-2xl leading-relaxed font-sans">
          Back a filmmaker&rsquo;s project with any amount, pick a reward if you like, and stream their reel
          right on the film&rsquo;s page. Your money is only kept if the film reaches its goal.
        </p>

        <div className="mt-8 flex flex-wrap items-center gap-3">
          <a
            href="#films"
            className="px-6 py-3 rounded-xl bg-amber hover:bg-amber-bright text-ink text-sm font-semibold shadow-md transition-colors"
          >
            Browse films
          </a>
          <a
            href="#how-heading"
            className="px-6 py-3 rounded-xl border border-white/15 bg-white/[0.04] hover:bg-white/[0.08] text-silver text-sm font-medium transition-colors"
          >
            How it works
          </a>
        </div>

        {/* Real numbers from the catalogue, once it has loaded */}
        {!loading && !loadError && campaigns.length > 0 && (
          <dl className="mt-10 grid grid-cols-3 max-w-xl gap-4 border-t border-white/[0.08] pt-6">
            <div>
              <dt className="label">Films raising</dt>
              <dd className="font-cinema text-2xl sm:text-3xl text-silver num">{stats.live}</dd>
            </div>
            <div>
              <dt className="label">Pledged</dt>
              <dd className="font-cinema text-2xl sm:text-3xl text-silver num">{rupees(stats.raised)}</dd>
            </div>
            <div>
              <dt className="label">Backers</dt>
              <dd className="font-cinema text-2xl sm:text-3xl text-silver num">{stats.backers.toLocaleString('en-IN')}</dd>
            </div>
          </dl>
        )}
      </section>

      {/* Catalogue */}
      <div id="films" className="scroll-mt-24 flex flex-col sm:flex-row gap-3 items-stretch sm:items-center justify-between mb-4">
        <h2 className="font-cinema text-2xl font-bold text-silver tracking-wide">Films</h2>
        <div className="flex items-center gap-3 sm:w-auto">
          <div className="relative flex-1 sm:w-72">
            <input
              type="search"
              aria-label="Search films"
              value={query}
              onChange={e => setQuery(e.target.value)}
              placeholder="Search films by title…"
              className="w-full bg-celluloid border border-white/10 rounded-xl px-4 py-2.5 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber/50 focus:ring-1 focus:ring-amber/50 transition-all"
            />
            {query && (
              <button
                type="button"
                aria-label="Clear search"
                onClick={() => setQuery('')}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-silver-dim hover:text-white"
              >
                ✕
              </button>
            )}
          </div>
          {!loading && (
            <span className="text-xs font-mono text-silver-dim whitespace-nowrap" aria-live="polite">
              {shown.length} {shown.length === 1 ? 'film' : 'films'}
            </span>
          )}
        </div>
      </div>

      {/* Genre Filter Chips */}
      <div className="flex gap-2 overflow-x-auto pb-4 mb-8 scrollbar-none font-mono text-xs">
        {CATEGORIES.map(cat => {
          const active = category === cat
          return (
            <button
              key={cat}
              type="button"
              aria-pressed={active}
              onClick={() => setCategory(cat)}
              className={`px-3.5 py-1.5 rounded-lg whitespace-nowrap transition-all uppercase tracking-wider ${
                active
                  ? 'bg-amber text-ink font-bold shadow-[0_0_12px_rgba(229,169,60,0.3)]'
                  : 'bg-white/[0.03] border border-white/10 text-silver-dim hover:text-silver hover:bg-white/[0.06]'
              }`}
            >
              {cat === 'ALL' ? 'All genres' : cat}
            </button>
          )
        })}
      </div>

      {/* Loading State */}
      {loading && (
        <div role="status" aria-busy="true" aria-label="Loading films" className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {[0, 1, 2, 3].map(i => (
            <div key={i} className="bg-celluloid border border-white/[0.08] rounded-2xl p-4 sm:p-5">
              <div className="skeleton aspect-video w-full rounded-xl mb-4" />
              <div className="skeleton h-6 w-2/3 mb-2.5" />
              <div className="skeleton h-3.5 w-full mb-1.5" />
              <div className="skeleton h-3.5 w-4/5" />
              <div className="mt-6 pt-4 border-t border-white/[0.06]">
                <div className="skeleton h-5 w-1/2 mb-3" />
                <div className="skeleton h-2 w-full rounded-full" />
              </div>
            </div>
          ))}
          <span className="sr-only">Loading films…</span>
        </div>
      )}

      {/* Error State */}
      {!loading && loadError && (
        <div className="p-12 text-center bg-celluloid border border-crimson/30 rounded-2xl" role="alert">
          <p className="font-cinema text-lg text-silver mb-2">We couldn&rsquo;t load the films</p>
          <p className="text-xs text-silver-dim font-sans">{loadError}</p>
        </div>
      )}

      {/* Empty State */}
      {!loading && !loadError && shown.length === 0 && (
        <div className="p-12 text-center bg-celluloid border border-white/10 rounded-2xl">
          <p className="font-cinema text-lg text-silver mb-2">No films match your search</p>
          <p className="text-xs text-silver-dim font-sans mb-4">Try a different genre or clear your search.</p>
          <button onClick={() => { setCategory('ALL'); setQuery('') }} className="tw-badge">
            Reset Filters
          </button>
        </div>
      )}

      {/* Cinematic Film Cards Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {paged.map(c => {
          const days = daysLeft(c.deadline)
          const percent = percentOf(c.raised_amount, c.goal_amount)
          const poster = getPosterForCampaign(c)
          const phase = campaignPhase(c)
          const reached = c.goal_amount > 0 && c.raised_amount >= c.goal_amount

          return (
            <div
              key={c.id}
              role="link"
              tabIndex={0}
              aria-label={`${c.title} — view film`}
              onClick={() => onSelect(c.id)}
              onKeyDown={e => {
                if (e.key === 'Enter' || (e.key === ' ' && e.target === e.currentTarget)) {
                  e.preventDefault()
                  onSelect(c.id)
                }
              }}
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
                    <span className="px-2 py-0.5 rounded-full bg-black/75 backdrop-blur-md border border-white/20 text-[10px] font-mono font-semibold flex items-center gap-1.5 shadow">
                      {/* Only a live campaign earns the red dot, and it's still: a pulse on every card is noise. */}
                      <span className={`h-1.5 w-1.5 rounded-full ${PHASE_DOT[phase]}`} aria-hidden="true" />
                      <span className={PHASE_TEXT[phase]}>{PHASE_LABEL[phase]}</span>
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
                  <span
                    className={`text-xs font-mono font-bold px-2 py-0.5 rounded border ${
                      reached
                        ? 'text-emerald-400 bg-emerald-400/10 border-emerald-400/25'
                        : 'text-amber bg-amber/10 border-amber/20'
                    }`}
                  >
                    {reached ? 'GOAL REACHED' : `${percent}% FUNDED`}
                  </span>
                </div>

                {/* Precision Film Exposure Meter: amber while raising, teal once the goal is met */}
                <div
                  className="w-full h-2 rounded-full bg-black/60 border border-white/10 overflow-hidden relative"
                  role="progressbar"
                  aria-label={`${c.title} funding`}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-valuenow={percent}
                >
                  <div
                    className={`h-full rounded-full transition-all duration-500 ${
                      reached
                        ? 'bg-gradient-to-r from-emerald-500 to-emerald-400 shadow-[0_0_8px_rgb(var(--c-teal)/0.4)]'
                        : 'bg-gradient-to-r from-amber to-amber-bright shadow-[0_0_8px_rgb(var(--c-amber)/0.4)]'
                    }`}
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
                    {phase === 'live' ? (
                      <>
                        <span className="text-silver font-semibold">{days !== null && days > 0 ? `${days}d` : '—'}</span>
                        <span className="text-silver-faint">remaining</span>
                      </>
                    ) : (
                      <span className="text-silver-faint">
                        {phase === 'funded' ? 'Campaign funded' : phase === 'ended' ? 'Campaign ended' : 'Not yet live'}
                      </span>
                    )}
                  </div>

                  <span className="text-amber text-[11px] group-hover:translate-x-1 transition-transform flex items-center gap-1">
                    View film →
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
            ← Previous
          </button>
          <span className="text-silver-dim">
            Page {page} of {totalPages}
          </span>
          <button
            disabled={page === totalPages}
            onClick={() => setPage(p => p + 1)}
            className="px-4 py-2 rounded-lg border border-white/10 bg-white/[0.02] text-silver hover:bg-white/[0.06] disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            Next →
          </button>
        </div>
      )}

      <HowItWorks />

      {/* Filmmaker call to action */}
      <section className="mb-4 rounded-2xl border border-amber/25 bg-amber/[0.06] p-6 sm:p-8 flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h2 className="font-cinema text-xl sm:text-2xl font-bold text-silver">Making a film?</h2>
          <p className="mt-1 text-sm text-silver-dim max-w-xl">
            Set a goal and a deadline, add rewards, upload a reel, and let backers fund it.
          </p>
        </div>
        <button
          type="button"
          onClick={onStartCampaign}
          className="shrink-0 px-6 py-3 rounded-xl bg-amber hover:bg-amber-bright text-ink text-sm font-semibold transition-colors cursor-pointer"
        >
          Start a campaign
        </button>
      </section>
    </div>
  )
}
