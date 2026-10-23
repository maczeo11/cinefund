import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { FALLBACK_POSTER_SVG, getCampaignVideo, getCampaigns, getPosterForCampaign, isBackendUnavailable, type Campaign } from '../api'
import { campaignPhase, PHASE_LABEL } from '../format'
import { useActiveUser } from '../auth'
import { usePageTitle } from '../usePageTitle'

type ReelState = 'ready' | 'sample' | 'none'
type Entry = { campaign: Campaign; reel: ReelState }

// Each reel check is one small request; cap how many films we probe at once.
const MAX_FILMS = 12

const REEL_BADGE: Record<ReelState, { text: string; className: string }> = {
  ready: { text: 'Reel available', className: 'text-emerald-400 border-emerald-400/30 bg-emerald-400/10' },
  sample: { text: 'Sample reel', className: 'text-amber border-amber/30 bg-amber/10' },
  none: { text: 'No reel yet', className: 'text-silver-dim border-white/15 bg-white/[0.04]' },
}

export default function Watch() {
  usePageTitle('Watch')
  const user = useActiveUser()
  const [entries, setEntries] = useState<Entry[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    getCampaigns()
      .then(async campaigns => {
        const films = campaigns.filter(c => campaignPhase(c) !== 'draft').slice(0, MAX_FILMS)
        const results = await Promise.allSettled(films.map(c => getCampaignVideo(c.id)))
        const list: Entry[] = films.map((campaign, i) => {
          const r = results[i]
          if (r.status === 'fulfilled') return { campaign, reel: r.value.status === 'READY' ? 'ready' : 'none' }
          // Server unreachable: the film page offers a labelled sample clip.
          return { campaign, reel: isBackendUnavailable(r.reason) ? 'sample' : 'none' }
        })
        // Films you can actually watch first.
        const rank: Record<ReelState, number> = { ready: 0, sample: 1, none: 2 }
        list.sort((a, b) => rank[a.reel] - rank[b.reel])
        if (!cancelled) setEntries(list)
      })
      .catch(err => {
        if (!cancelled) setError((err as Error).message || 'Could not load films.')
      })
    return () => {
      cancelled = true
    }
  }, [])

  const watchable = entries?.filter(e => e.reel !== 'none').length ?? 0

  return (
    <div className="max-w-6xl py-4 animate-fadeIn">
      <header className="mb-8 pb-6 border-b border-white/[0.08]">
        <h1 className="font-cinema text-3xl sm:text-4xl font-extrabold text-silver">Watch</h1>
        <p className="mt-2 max-w-2xl text-sm sm:text-base text-silver-dim leading-relaxed">
          Stream reels shared by filmmakers. Pick a film below and press play on its page.
          {!user && ' You’ll be asked to sign in (it’s free) before the reel starts.'}
        </p>
      </header>

      {error && (
        <p role="alert" className="notice notice-error">
          {error}
        </p>
      )}

      {!entries && !error && (
        <div role="status" aria-busy="true" aria-label="Loading films" className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2, 3, 4, 5].map(i => (
            <div key={i} className="rounded-2xl border border-white/[0.08] bg-celluloid p-3">
              <div className="skeleton aspect-video w-full rounded-xl" />
              <div className="skeleton mt-3 h-5 w-2/3" />
              <div className="skeleton mt-2 h-3.5 w-1/3" />
            </div>
          ))}
          <span className="sr-only">Loading films…</span>
        </div>
      )}

      {entries && entries.length === 0 && (
        <div className="rounded-2xl border border-white/10 bg-celluloid p-12 text-center">
          <p className="font-cinema text-lg text-silver">Nothing to watch yet</p>
          <p className="mt-1 text-sm text-silver-dim">Films appear here as soon as filmmakers publish campaigns.</p>
          <Link to="/" className="inline-block mt-4 px-4 py-2 rounded-lg bg-amber text-ink text-xs font-semibold">
            Browse films
          </Link>
        </div>
      )}

      {entries && entries.length > 0 && (
        <>
          {watchable === 0 && (
            <p className="mb-5 text-sm text-silver-dim">
              No filmmaker has shared a reel yet. You can still back these films and check back for reels later.
            </p>
          )}
          <ul className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
            {entries.map(({ campaign: c, reel }) => {
              const badge = REEL_BADGE[reel]
              return (
                <li key={c.id}>
                  <Link
                    to={reel === 'none' ? `/campaigns/${c.id}` : `/campaigns/${c.id}#watch`}
                    className="group block rounded-2xl border border-white/[0.08] bg-celluloid p-3 transition-all hover:border-amber/40 hover:shadow-[0_4px_30px_rgb(var(--c-amber)/0.12)]"
                  >
                    <div className="relative aspect-video overflow-hidden rounded-xl bg-black">
                      <img
                        src={getPosterForCampaign(c)}
                        alt=""
                        loading="lazy"
                        onError={e => {
                          e.currentTarget.onerror = null
                          e.currentTarget.src = FALLBACK_POSTER_SVG
                        }}
                        className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105"
                      />
                      <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-transparent to-transparent" />
                      {reel !== 'none' && (
                        <span
                          className="absolute inset-0 m-auto h-12 w-12 rounded-full bg-amber text-ink flex items-center justify-center shadow-[0_0_24px_rgb(var(--c-amber)/0.5)]"
                          aria-hidden="true"
                        >
                          <svg className="h-5 w-5 translate-x-0.5 fill-current" viewBox="0 0 24 24">
                            <path d="M8 5v14l11-7z" />
                          </svg>
                        </span>
                      )}
                    </div>
                    <div className="mt-3 flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <h2 className="font-cinema text-base font-bold text-silver truncate group-hover:text-amber transition-colors">
                          {c.title}
                        </h2>
                        <p className="mt-0.5 text-xs text-silver-dim capitalize">
                          {(c.category || 'Film').toLowerCase()} · {PHASE_LABEL[campaignPhase(c)].toLowerCase()}
                        </p>
                      </div>
                      <span className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-semibold ${badge.className}`}>
                        {badge.text}
                      </span>
                    </div>
                  </Link>
                </li>
              )
            })}
          </ul>
        </>
      )}
    </div>
  )
}
