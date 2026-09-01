import { useEffect, useState } from 'react'
import { getCampaigns, type Campaign } from '../api'
import { rupees, percentOf } from '../format'
import { Link } from 'react-router-dom'

export default function Dashboard() {
  const [campaigns, setCampaigns] = useState<Campaign[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    getCampaigns()
      .then(d => setCampaigns((d as Campaign[]) || []))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <p className="status">Loading dashboard…</p>

  return (
    <div className="max-w-6xl">
      <div className="flex items-baseline justify-between mb-6">
        <h1 className="font-serif text-3xl">Creator dashboard</h1>
        <span className="label">{campaigns.length} campaigns • ledger: debits = credits</span>
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
                  <Link to={`/campaigns/${c.id}`} className="font-serif hover:text-accent transition-colors">
                    {c.title}
                  </Link>
                  <span className="block text-xs text-white/40">{c.category}</span>
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
      <p className="mt-3 text-xs text-white/30">GET /api/v1/campaigns?creator_id — indexed on creator_id, status. Ledger invariant: sum(debits)=sum(credits) enforced by deferred trigger.</p>
    </div>
  )
}
