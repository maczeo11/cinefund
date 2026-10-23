import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { rupees } from '../format'
import { getUserPledges, FALLBACK_POSTER_SVG, type UserPledgeRecord } from '../api'
import { useActiveUser } from '../auth'
import { usePageTitle } from '../usePageTitle'
import AuthModal from './AuthModal.tsx'

// What each payment state means to the person who paid.
const STATUS: Record<string, { label: string; hint: string; className: string }> = {
  CAPTURED: { label: 'Confirmed', hint: 'Your payment is confirmed and counts toward the goal.', className: 'text-emerald-400 border-emerald-400/30 bg-emerald-400/10' },
  CREATED: { label: 'Awaiting payment', hint: 'You started this pledge but the payment has not completed.', className: 'text-amber border-amber/30 bg-amber/10' },
  AUTHORIZED: { label: 'Processing', hint: 'Your bank has approved the payment; confirmation is on its way.', className: 'text-amber border-amber/30 bg-amber/10' },
  FAILED: { label: 'Failed', hint: 'The payment did not go through. You were not charged.', className: 'text-crimson border-crimson/30 bg-crimson/10' },
  REFUND_PENDING: { label: 'Refund on the way', hint: 'Your money is being returned.', className: 'text-amber border-amber/30 bg-amber/10' },
  REFUNDED: { label: 'Refunded', hint: 'Your money has been returned in full.', className: 'text-silver-dim border-white/15 bg-white/[0.04]' },
  REFUND_FAILED: { label: 'Refund problem', hint: 'We could not complete your refund automatically.', className: 'text-crimson border-crimson/30 bg-crimson/10' },
}
const FALLBACK_STATUS = { label: 'Unknown', hint: '', className: 'text-silver-dim border-white/15 bg-white/[0.04]' }

const dateFmt = (iso: string) =>
  new Date(iso).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' })

// Pledge history is kept in this browser (localStorage); the server is the
// record of truth for payments, and settles them independently.
export default function Pledges() {
  usePageTitle('My pledges')
  const activeUser = useActiveUser()
  const [pledges, setPledges] = useState<UserPledgeRecord[]>([])
  const [authOpen, setAuthOpen] = useState(false)

  useEffect(() => {
    const reload = () => setPledges(activeUser ? getUserPledges(activeUser.id) : [])
    reload()
    window.addEventListener('cinefund_pledge_added', reload)
    return () => window.removeEventListener('cinefund_pledge_added', reload)
  }, [activeUser])

  const confirmedTotal = pledges.filter(p => p.status === 'CAPTURED').reduce((sum, p) => sum + p.amount, 0)

  return (
    <div className="max-w-4xl mx-auto py-2 animate-fadeIn space-y-8">
      <header className="border-b border-white/[0.08] pb-6 flex flex-col sm:flex-row sm:items-end justify-between gap-4">
        <div>
          <h1 className="font-cinema text-3xl sm:text-4xl font-extrabold text-silver">My pledges</h1>
          <p className="mt-2 text-sm text-silver-dim max-w-xl leading-relaxed">
            Films you&rsquo;ve backed. A pledge counts toward a film&rsquo;s goal once its payment is confirmed. If a film
            misses its goal by the deadline, you&rsquo;re refunded in full.
          </p>
        </div>
        {pledges.length > 0 && (
          <dl className="rounded-xl border border-white/10 bg-celluloid px-5 py-3 text-right shrink-0">
            <dt className="label">Confirmed total</dt>
            <dd className="font-cinema text-2xl text-silver num">{rupees(confirmedTotal)}</dd>
          </dl>
        )}
      </header>

      {!activeUser ? (
        <div className="tw-card text-center py-12 space-y-4">
          <p className="font-cinema text-lg text-silver">Sign in to see your pledges</p>
          <p className="text-sm text-silver-dim max-w-md mx-auto">
            Your pledges are tied to your account. Sign in, or browse films and back one to get started.
          </p>
          <div className="flex flex-wrap items-center justify-center gap-3 pt-1">
            <button
              type="button"
              onClick={() => setAuthOpen(true)}
              className="px-5 py-2.5 rounded-xl bg-amber hover:bg-amber-bright text-ink text-sm font-semibold transition-colors cursor-pointer"
            >
              Sign in
            </button>
            <Link to="/" className="px-5 py-2.5 rounded-xl border border-white/15 bg-white/[0.04] text-silver text-sm hover:bg-white/[0.08]">
              Browse films
            </Link>
          </div>
        </div>
      ) : pledges.length === 0 ? (
        <div className="tw-card text-center py-12 space-y-4">
          <p className="font-cinema text-lg text-silver">You haven&rsquo;t backed a film yet</p>
          <p className="text-sm text-silver-dim max-w-md mx-auto">
            Find a film you like, choose a reward and an amount, and it will show up here.
          </p>
          <Link to="/" className="inline-block px-5 py-2.5 rounded-xl bg-amber hover:bg-amber-bright text-ink text-sm font-semibold transition-colors">
            Browse films
          </Link>
        </div>
      ) : (
        <ul className="space-y-4">
          {pledges.map(pledge => {
            const status = STATUS[pledge.status] ?? FALLBACK_STATUS
            return (
              <li
                key={pledge.id}
                className="tw-card !p-4 sm:!p-5 hover:border-amber/40 flex flex-col sm:flex-row items-stretch sm:items-center gap-4 sm:gap-6"
              >
                {pledge.poster_url && (
                  <div className="relative w-full sm:w-44 aspect-video rounded-xl overflow-hidden shrink-0 border border-white/15 bg-black">
                    <img
                      src={pledge.poster_url}
                      alt=""
                      onError={e => {
                        e.currentTarget.onerror = null
                        e.currentTarget.src = FALLBACK_POSTER_SVG
                      }}
                      className="w-full h-full object-cover"
                    />
                  </div>
                )}

                <div className="flex-1 min-w-0 space-y-1.5">
                  <div className="flex flex-wrap items-center gap-2">
                    <Link
                      to={`/campaigns/${pledge.campaign_id}`}
                      className="font-cinema font-bold text-base sm:text-lg text-silver hover:text-amber transition-colors truncate"
                    >
                      {pledge.campaign_title}
                    </Link>
                    <span
                      className={`px-2 py-0.5 rounded-full border text-[11px] font-semibold ${status.className}`}
                      title={status.hint}
                    >
                      {status.label}
                    </span>
                  </div>
                  <p className="text-sm text-silver-dim">
                    Reward: <span className="text-silver">{pledge.tier_title || 'No reward — general support'}</span>
                  </p>
                  <p className="text-xs text-silver-faint">
                    {dateFmt(pledge.created_at)} · Ref {pledge.order_id.slice(-10)}
                  </p>
                  {status.hint && <p className="text-xs text-silver-dim">{status.hint}</p>}
                </div>

                <div className="sm:text-right shrink-0 border-t sm:border-t-0 border-white/[0.06] pt-3 sm:pt-0">
                  <p className="font-cinema text-xl sm:text-2xl font-bold text-silver num">{rupees(pledge.amount)}</p>
                </div>
              </li>
            )
          })}
        </ul>
      )}

      <AuthModal isOpen={authOpen} onClose={() => setAuthOpen(false)} />
    </div>
  )
}
