import { useState, useEffect, useCallback } from 'react'
import { getCampaign, getTiers, getConfig, createPledge, confirmPledge } from '../api'
import { rupees, toPaise, percentOf, daysLeft } from '../format'
import VideoPlayer from './VideoPlayer'

const BACKER_ID = '00000000-0000-0000-0000-000000000002'
const SAMPLE_HLS_STREAM = 'https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8'

// idle -> ordering -> paying -> confirming -> done
function CampaignDetail({ id, onBack }) {
  const [campaign, setCampaign] = useState(null)
  const [tiers, setTiers] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [checkoutKey, setCheckoutKey] = useState('')

  const [tier, setTier] = useState(null)
  const [amount, setAmount] = useState('')
  const [message, setMessage] = useState('')
  const [phase, setPhase] = useState('idle')
  const [pledgeError, setPledgeError] = useState(null)

  const refresh = useCallback(async () => {
    const [c, t] = await Promise.all([getCampaign(id), getTiers(id)])
    setCampaign(c)
    setTiers(t || [])
  }, [id])

  useEffect(() => {
    setLoading(true)
    Promise.all([refresh(), getConfig()])
      .then(([, cfg]) => setCheckoutKey(cfg.razorpay_key_id || ''))
      .catch(err => setError(err.message))
      .finally(() => setLoading(false))
  }, [id, refresh])

  function selectTier(t) {
    if (tier?.id === t.id) {
      setTier(null)
      return
    }
    setTier(t)
    setAmount(String(t.min_amount / 100))
  }

  // The server owns the outcome: it re-reads the payment from Razorpay and
  // only then moves the pledge. We just report that checkout finished.
  async function confirm(pledgeId, checkout) {
    setPhase('confirming')
    try {
      const result = await confirmPledge(pledgeId, checkout)
      if (result.status !== 'CAPTURED') {
        // authorized but not captured yet; the webhook will finish it
        setPledgeError('Payment received. It will show up here once it settles.')
        setPhase('idle')
        return
      }
      await refresh()
      setPhase('done')
      setAmount('')
      setMessage('')
      setTier(null)
    } catch (err) {
      setPledgeError(`Payment went through but we could not record it: ${err.message}`)
      setPhase('idle')
    }
  }

  function openCheckout(pledge) {
    const rzp = new window.Razorpay({
      key: checkoutKey,
      order_id: pledge.order_id,
      amount: pledge.amount,
      currency: pledge.currency || 'INR',
      name: 'CineFund',
      description: campaign.title,
      theme: { color: '#c8553d', backdrop_color: '#0b0b0c' },
      handler: response => confirm(pledge.id, response),
      modal: {
        ondismiss: () => {
          setPledgeError('Checkout closed before payment.')
          setPhase('idle')
        },
      },
    })
    rzp.on('payment.failed', res => {
      setPledgeError(res.error?.description || 'The payment was declined.')
      setPhase('idle')
    })
    setPhase('paying')
    rzp.open()
  }

  async function submit(e) {
    e.preventDefault()
    setPledgeError(null)

    const paise = toPaise(amount)
    if (!amount || !Number.isFinite(paise) || paise < 100) {
      setPledgeError('Enter an amount of ₹1 or more.')
      return
    }
    if (tier && paise < tier.min_amount) {
      setPledgeError(`${tier.title} starts at ${rupees(tier.min_amount)}.`)
      return
    }

    setPhase('ordering')
    try {
      const pledge = await createPledge(id, {
        backer_id: BACKER_ID,
        tier_id: tier ? tier.id : null,
        amount: paise,
        message,
        anonymous: false,
      })

      if (checkoutKey && window.Razorpay) {
        openCheckout(pledge)
      } else {
        // no Razorpay key configured, so the API is on the fake gateway and
        // has already marked the order paid
        await confirm(pledge.id, { razorpay_order_id: pledge.order_id })
      }
    } catch (err) {
      setPledgeError(err.message)
      setPhase('idle')
    }
  }

  if (loading) return <p className="status">Loading…</p>
  if (error) return <p className="status notice">{error}</p>
  if (!campaign) return <p className="status">Not found.</p>

  const days = daysLeft(campaign.deadline)
  const busy = phase !== 'idle' && phase !== 'done'
  const busyLabel = { ordering: 'Opening checkout…', paying: 'Waiting for payment…', confirming: 'Recording…' }

  return (
    <div>
      <div style={{ paddingTop: 32 }}>
        <button className="link" onClick={onBack}>← Index</button>
      </div>

      <div className="detail">
        <article>
          <div className="detail-meta">
            <span className="label">{campaign.category}</span>
            <span className="dot">·</span>
            <span className="label">{campaign.status}</span>
          </div>
          <h1 className="detail-title">{campaign.title}</h1>
          <p className="detail-tagline">{campaign.tagline}</p>

          <VideoPlayer src={SAMPLE_HLS_STREAM} title="Pitch reel" />

          <section className="section">
            <h2>Synopsis</h2>
            <p className="prose">{campaign.synopsis || campaign.tagline}</p>
          </section>

          {tiers.length > 0 && (
            <section className="section">
              <h2>Rewards</h2>
              <ul className="tiers">
                {tiers.map(t => {
                  const soldOut = t.quantity_limit && t.claimed_count >= t.quantity_limit
                  return (
                    <li key={t.id} className="tier">
                      <button
                        className="tier-btn"
                        aria-pressed={tier?.id === t.id}
                        disabled={soldOut}
                        onClick={() => selectTier(t)}
                      >
                        <span className="tier-row">
                          <span className="tier-name">{t.title}</span>
                          <span className="tier-min num">{rupees(t.min_amount)}+</span>
                        </span>
                        {t.description && <span className="tier-desc">{t.description}</span>}
                        <span className="tier-stock label">
                          {soldOut
                            ? 'Sold out'
                            : t.quantity_limit
                              ? `${t.claimed_count} of ${t.quantity_limit} claimed`
                              : `${t.claimed_count} claimed`}
                        </span>
                      </button>
                    </li>
                  )
                })}
              </ul>
            </section>
          )}
        </article>

        <aside className="rail">
          <div className="rail-sum num">{rupees(campaign.raised_amount)}</div>
          <div className="rail-of num">
            pledged of {rupees(campaign.goal_amount)}
          </div>

          <div className="meter meter-tall rail-meter">
            <span style={{ width: `${percentOf(campaign.raised_amount, campaign.goal_amount)}%` }} />
          </div>

          <div className="rail-figures">
            <div className="rail-figure">
              <b className="num">{campaign.backer_count}</b>
              <span className="label">Backers</span>
            </div>
            <div className="rail-figure">
              <b className="num">{days !== null && days > 0 ? days : 0}</b>
              <span className="label">Days left</span>
            </div>
          </div>

          {campaign.status === 'LIVE' && (
            <form onSubmit={submit} className="form" style={{ marginTop: 28 }}>
              <div className="field">
                <label className="label" htmlFor="amount">
                  {tier ? `Backing ${tier.title}` : 'Pledge amount'}
                </label>
                <input
                  id="amount"
                  type="number"
                  min="1"
                  value={amount}
                  onChange={e => setAmount(e.target.value)}
                  placeholder="₹500"
                />
              </div>

              <div className="field">
                <label className="label" htmlFor="note">A note to the crew</label>
                <input
                  id="note"
                  type="text"
                  value={message}
                  onChange={e => setMessage(e.target.value)}
                  placeholder="Optional"
                />
              </div>

              {pledgeError && <p className="notice">{pledgeError}</p>}
              {phase === 'done' && (
                <p className="notice notice-done">
                  Recorded. Thank you for backing this one.
                </p>
              )}

              <button type="submit" className="btn btn-wide" disabled={busy}>
                {busy ? busyLabel[phase] : 'Back this film'}
              </button>
            </form>
          )}
        </aside>
      </div>
    </div>
  )
}

export default CampaignDetail
