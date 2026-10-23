import { useEffect, useState } from 'react'
import { getCampaigns, getPosterForCampaign, FALLBACK_POSTER_SVG, type Campaign } from '../api'
import { rupees, percentOf } from '../format'
import { Link } from 'react-router-dom'
import HealthBar from './HealthBar.tsx'
import { usePageTitle } from '../usePageTitle'

export default function Dashboard() {
  usePageTitle('System status')
  const [campaigns, setCampaigns] = useState<Campaign[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    getCampaigns()
      .then(d => {
        if (!cancelled) setCampaigns(d || [])
      })
      .catch(err => {
        if (!cancelled) setLoadError((err as Error).message)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (loading) return <div className="max-w-6xl space-y-4" role="status" aria-busy="true" aria-label="Loading status"><div className="skeleton h-10 w-full" /><div className="skeleton h-8 w-1/3" /><div className="skeleton h-64 w-full rounded-xl" /><span className="sr-only">Loading…</span></div>
  if (loadError) return <p className="status notice" role="alert">{loadError}</p>

  return (
    <div className="max-w-6xl space-y-6">
      <div className="rounded-xl overflow-hidden border border-white/10">
        <HealthBar />
      </div>

      <div className="flex items-baseline justify-between mb-6">
        <h1 className="font-serif text-3xl">System status</h1>
        <span className="label">{campaigns.length} campaigns</span>
      </div>

      <div className="tw-card !p-0 overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-white/[0.04] border-b border-white/10">
            <tr className="label !text-[0.65rem]">
              <th className="text-left px-4 py-3 font-medium">Film</th>
              <th className="text-left px-4 py-3 font-medium">Status</th>
              <th className="text-right px-4 py-3 font-medium">Raised</th>
              <th className="text-right px-4 py-3 font-medium">Goal</th>
              <th className="text-left px-4 py-3 font-medium">Progress</th>
              <th className="text-right px-4 py-3 font-medium">Backers</th>
            </tr>
          </thead>
          <tbody>
            {campaigns.slice(0, 20).map(c => (
              <tr key={c.id} className="border-b border-white/[0.06] hover:bg-white/[0.03] transition-colors">
                <td className="px-4 py-3">
                  <div className="flex items-center gap-3">
                    <div className="w-12 h-7 rounded overflow-hidden shrink-0 bg-black border border-white/10">
                      <img
                        src={getPosterForCampaign(c)}
                        alt={c.title}
                        onError={(e) => {
                          e.currentTarget.onerror = null
                          e.currentTarget.src = FALLBACK_POSTER_SVG
                        }}
                        className="w-full h-full object-cover"
                      />
                    </div>
                    <div>
                      <Link to={`/campaigns/${c.id}`} className="font-serif hover:text-accent transition-colors font-medium">
                        {c.title}
                      </Link>
                      <span className="block text-xs text-silver-faint">{c.category}</span>
                    </div>
                  </div>
                </td>
                <td className="px-4 py-3">
                  <span className={`tw-badge ${c.status === 'LIVE' ? '!bg-green-500/20 !text-green-400' : c.status === 'CLOSED' ? '!bg-white/10 !text-white/50' : ''}`}>
                    {c.status}
                  </span>
                </td>
                <td className="px-4 py-3 text-right num">{rupees(c.raised_amount)}</td>
                <td className="px-4 py-3 text-right num text-white/50">{rupees(c.goal_amount)}</td>
                <td className="px-4 py-3 w-32">
                  <div className="meter">
                    <span style={{ width: `${percentOf(c.raised_amount, c.goal_amount)}%` }} />
                  </div>
                </td>
                <td className="px-4 py-3 text-right num">{c.backer_count}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="mt-3 text-xs text-silver-faint">Operator view: service health and every campaign on the platform.</p>
    </div>
  )
}
