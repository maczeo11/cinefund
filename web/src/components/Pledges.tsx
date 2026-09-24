import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { rupees } from '../format'
import { getUserPledges, FALLBACK_POSTER_SVG, type UserPledgeRecord } from '../api'
import { getActiveUser, signInAsDemo, DEMO_USERS, type UserProfile } from './AuthModal.tsx'

type LedgerEntry = {
  id: string
  pledge_id: string
  account: string
  type: 'DEBIT' | 'CREDIT'
  amount: number
  created_at: string
}

export default function Pledges() {
  const [activeUser, setActiveUser] = useState<UserProfile | null>(getActiveUser())
  const [userPledges, setUserPledges] = useState<UserPledgeRecord[]>([])
  const [filterMode, setFilterMode] = useState<'my' | 'all'>('my')
  const [ledgerEntries, setLedgerEntries] = useState<LedgerEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [switchError, setSwitchError] = useState<string | null>(null)

  const reloadPledges = () => {
    const user = getActiveUser()
    setActiveUser(user)
    if (filterMode === 'my' && user) {
      setUserPledges(getUserPledges(user.id))
    } else {
      setUserPledges(getUserPledges())
    }
  }

  useEffect(() => {
    reloadPledges()

    const handleUpdate = () => reloadPledges()
    window.addEventListener('cinefund_auth_change', handleUpdate)
    window.addEventListener('cinefund_pledge_added', handleUpdate)

    // Try to fetch ledger if live backend is running
    const base = (import.meta.env.VITE_API_BASE as string) || '/api/v1'
    fetch(`${base}/ledger`)
      .then(r => (r.ok ? r.json() : []))
      .then(d => setLedgerEntries(Array.isArray(d) ? d : []))
      .catch(() => setLedgerEntries([]))
      .finally(() => setLoading(false))

    return () => {
      window.removeEventListener('cinefund_auth_change', handleUpdate)
      window.removeEventListener('cinefund_pledge_added', handleUpdate)
    }
  }, [filterMode])

  if (loading) {
    return (
      <div className="py-12 text-center text-sm font-mono text-silver-dim">
        <span className="inline-block animate-spin mr-2">✦</span> Reading PostgreSQL Escrow Ledger…
      </div>
    )
  }

  const myCount = activeUser ? getUserPledges(activeUser.id).length : 0
  const allCount = getUserPledges().length

  return (
    <div className="max-w-4xl mx-auto py-2 animate-fadeIn space-y-8">
      {/* Page Header */}
      <div className="border-b border-white/[0.08] pb-6 flex flex-col md:flex-row md:items-end justify-between gap-4">
        <div>
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-amber/10 border border-amber/25 text-amber text-xs font-mono mb-2.5">
            <span className="h-1.5 w-1.5 rounded-full bg-amber animate-pulse" />
            <span>DOUBLE-ENTRY ESCROW SYSTEM</span>
          </div>
          <h1 className="font-cinema text-3xl sm:text-4xl font-extrabold text-silver">
            Film Escrow & Backer Pledges
          </h1>
          <p className="text-xs sm:text-sm text-silver-dim font-sans mt-1 max-w-2xl">
            Every CAPTURED pledge is locked in double-entry escrow. DEBIT & CREDIT entries are written within the same atomic PostgreSQL transaction.
          </p>
        </div>

        {activeUser && (
          <div className="p-3 rounded-xl bg-celluloid border border-white/10 flex items-center gap-3 self-start md:self-auto shadow-md">
            <div className="h-9 w-9 rounded-xl bg-gradient-to-tr from-amber to-amber-bright text-ink font-cinema font-bold text-xs flex items-center justify-center overflow-hidden shrink-0">
              {activeUser.photoURL || (activeUser.avatar && activeUser.avatar.startsWith('http')) ? (
                <img src={activeUser.photoURL || activeUser.avatar} alt={activeUser.name} className="w-full h-full object-cover" />
              ) : (
                activeUser.avatar || (activeUser.name ? activeUser.name[0].toUpperCase() : '👤')
              )}
            </div>
            <div>
              <span className="text-xs font-bold text-silver block">{activeUser.name}</span>
              <span className="text-[10px] font-mono text-amber">{activeUser.role} Profile</span>
            </div>
          </div>
        )}
      </div>

      {/* Filter Tabs */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-white/[0.06] pb-3">
        <div className="flex gap-2 font-mono text-xs">
          <button
            type="button"
            onClick={() => setFilterMode('my')}
            className={`px-3 py-1.5 rounded-lg border transition-all ${
              filterMode === 'my'
                ? 'bg-amber text-ink font-bold border-amber shadow-[0_0_10px_rgba(229,169,60,0.25)]'
                : 'border-white/10 text-silver-dim hover:text-silver hover:bg-white/[0.04]'
            }`}
          >
            My Session Pledges ({myCount})
          </button>
          <button
            type="button"
            onClick={() => setFilterMode('all')}
            className={`px-3 py-1.5 rounded-lg border transition-all ${
              filterMode === 'all'
                ? 'bg-amber text-ink font-bold border-amber shadow-[0_0_10px_rgba(229,169,60,0.25)]'
                : 'border-white/10 text-silver-dim hover:text-silver hover:bg-white/[0.04]'
            }`}
          >
            All Platform Pledges ({allCount})
          </button>
        </div>
        <span className="text-xs font-mono text-silver-dim hidden sm:inline">
          {userPledges.length} Pledges Listed
        </span>
      </div>

      {/* Backed Films Catalog with Posters */}
      <div>
        <div className="flex items-center justify-between mb-4">
          <h2 className="font-cinema text-xl font-bold text-silver flex items-center gap-2">
            <span>{filterMode === 'my' ? 'My Active Allocations' : 'All Vault Pledges'}</span>
            <span className="px-2 py-0.2 rounded-full bg-amber/20 text-amber text-xs font-mono">
              {userPledges.length} Active
            </span>
          </h2>
          <span className="text-xs font-mono text-silver-dim">
            Guaranteed Reward Allocation
          </span>
        </div>

        {userPledges.length === 0 ? (
          <div className="tw-card text-center py-12 space-y-4">
            <div className="h-12 w-12 rounded-full bg-white/5 border border-white/10 flex items-center justify-center text-xl mx-auto">
              🎟️
            </div>
            <div>
              <p className="font-cinema text-base text-silver">
                {filterMode === 'my' && activeUser
                  ? `No pledges in this session for ${activeUser.name} (${activeUser.role})`
                  : 'No pledges recorded yet'}
              </p>
              <p className="text-xs text-silver-dim max-w-md mx-auto font-sans mt-1">
                {activeUser?.role === 'CREATOR'
                  ? 'As a filmmaker you launch and manage campaigns. To test backer pledges, switch to Ravi or view all ledger records.'
                  : 'Browse the 35mm vault and back an auteur reel. Your double-entry ledger allocation will appear here instantly.'}
              </p>
            </div>
            <div className="flex flex-wrap items-center justify-center gap-3 pt-2">
              <Link
                to="/"
                className="px-4 py-2 rounded-lg bg-amber text-ink font-semibold text-xs font-mono shadow-[0_0_15px_rgba(229,169,60,0.2)] hover:bg-amber-bright transition-colors"
              >
                Explore Film Vault →
              </Link>
              {activeUser?.role === 'CREATOR' && (
                <button
                  type="button"
                  onClick={() => {
                    setSwitchError(null)
                    signInAsDemo(DEMO_USERS[1])
                      .then(() => setFilterMode('my'))
                      .catch(err => setSwitchError((err as Error).message))
                  }}
                  className="px-4 py-2 rounded-lg border border-amber/40 bg-amber/10 text-amber text-xs font-mono hover:bg-amber/20 transition-colors"
                >
                  ⚡ Switch to Ravi (Demo Backer)
                </button>
              )}
              {switchError && <p className="w-full text-xs text-crimson">{switchError}</p>}
              <button
                type="button"
                onClick={() => setFilterMode('all')}
                className="px-4 py-2 rounded-lg border border-white/10 bg-white/[0.04] text-silver text-xs font-mono hover:bg-white/[0.08] transition-colors"
              >
                View All Ledger Allocations ({allCount})
              </button>
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            {userPledges.map(pledge => (
              <div
                key={pledge.id}
                className="tw-card !p-4 sm:!p-5 border border-white/[0.08] hover:border-amber/40 transition-all flex flex-col sm:flex-row items-stretch sm:items-center gap-4 sm:gap-6 bg-celluloid shadow-lg"
              >
                {/* 16:9 Poster Thumbnail */}
                {pledge.poster_url && (
                  <div className="relative w-full sm:w-44 aspect-video rounded-xl overflow-hidden shrink-0 border border-white/15 bg-black">
                    <img
                      src={pledge.poster_url}
                      alt={pledge.campaign_title}
                      onError={(e) => {
                        e.currentTarget.onerror = null
                        e.currentTarget.src = FALLBACK_POSTER_SVG
                      }}
                      className="w-full h-full object-cover"
                    />
                    <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-transparent to-transparent pointer-events-none" />
                    <span className="absolute bottom-1.5 left-1.5 px-1.5 py-0.2 rounded bg-black/80 backdrop-blur-md text-[9px] font-mono text-amber font-semibold border border-amber/30">
                      35MM ESCROW
                    </span>
                  </div>
                )}

                {/* Info & Tier */}
                <div className="flex-1 min-w-0 space-y-1.5">
                  <div className="flex flex-wrap items-center gap-2">
                    <Link
                      to={`/campaigns/${pledge.campaign_id}`}
                      className="font-cinema font-bold text-base sm:text-lg text-silver hover:text-amber transition-colors truncate"
                    >
                      {pledge.campaign_title}
                    </Link>
                    <span className="px-2 py-0.2 rounded text-[10px] font-mono bg-emerald-500/20 text-emerald-400 border border-emerald-500/30 font-semibold">
                      {pledge.status}
                    </span>
                  </div>

                  <p className="text-xs text-silver-dim font-sans">
                    Reward Tier: <strong className="text-silver">{pledge.tier_title || 'Direct Patronage'}</strong>
                  </p>

                  <div className="flex flex-wrap items-center gap-3 text-[11px] font-mono text-silver-faint pt-1">
                    <span>Backer: <strong className="text-silver">{pledge.backer_name || 'Ravi Patel'}</strong></span>
                    <span>•</span>
                    <span>Order: <code className="text-amber">{pledge.order_id.slice(0, 16)}</code></span>
                  </div>
                </div>

                {/* Amount & Double Entry Status */}
                <div className="text-left sm:text-right shrink-0 border-t sm:border-t-0 border-white/[0.06] pt-3 sm:pt-0">
                  <div className="font-mono text-lg sm:text-xl font-bold text-amber">
                    {rupees(pledge.amount)}
                  </div>
                  <span className="text-[10px] font-mono text-silver-faint block mt-0.5">
                    DEBIT: Escrow Reserve
                  </span>
                  <span className="text-[10px] font-mono text-emerald-400 block">
                    CREDIT: Production Acct
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* PostgreSQL Double-Entry Ledger Invariant Audit */}
      {ledgerEntries.length > 0 && (
        <div className="pt-6 border-t border-white/[0.08]">
          <h3 className="font-cinema text-lg font-bold text-silver mb-3">
            PostgreSQL Double-Entry Accounting Log
          </h3>
          <div className="tw-card !p-0 overflow-hidden">
            <table className="w-full text-xs font-mono">
              <thead className="bg-white/[0.04] border-b border-white/10 text-silver-dim text-[11px]">
                <tr>
                  <th className="text-left px-4 py-3 font-semibold">Transaction Type</th>
                  <th className="text-left px-4 py-3 font-semibold">Account Destination</th>
                  <th className="text-right px-4 py-3 font-semibold">Paise Amount</th>
                </tr>
              </thead>
              <tbody>
                {ledgerEntries.slice(0, 10).map(e => (
                  <tr key={e.id} className="border-b border-white/[0.04] hover:bg-white/[0.02]">
                    <td className="px-4 py-2.5 font-bold">
                      <span className={e.type === 'DEBIT' ? 'text-amber' : 'text-emerald-400'}>
                        {e.type}
                      </span>
                    </td>
                    <td className="px-4 py-2.5 text-silver-dim">{e.account}</td>
                    <td className="px-4 py-2.5 text-right text-silver font-bold">{rupees(e.amount)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Engineering Note */}
      <div className="p-4 rounded-xl bg-black/40 border border-white/[0.08] text-xs font-mono text-silver-dim flex items-center justify-between gap-4">
        <span>🔒 Ledger Guarantee: Sum(Debits) == Sum(Credits) enforced by PostgreSQL deferred triggers</span>
        <span className="text-amber shrink-0">ACID Isolation</span>
      </div>
    </div>
  )
}
