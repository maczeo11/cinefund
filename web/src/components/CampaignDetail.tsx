import { useState, useEffect, useCallback, useRef } from 'react'
import { useLocation } from 'react-router-dom'
import { ApiError, getCampaign, getTiers, getConfig, createPledge, confirmPledge, uploadVideoFileToS3, getPosterForCampaign, getCampaignVideo, playbackMasterUrl, isBackendUnavailable, FALLBACK_POSTER_SVG, type Campaign, type Tier } from '../api'
import { rupees, toPaise, percentOf, daysLeft, formatDate, campaignPhase, remainingToGoal, PHASE_LABEL } from '../format'
import ReelPanel, { type Reel } from './ReelPanel.tsx'
import HowItWorks from './HowItWorks.tsx'
import AuthModal from './AuthModal.tsx'
import Avatar from './Avatar.tsx'
import { useActiveUser } from '../auth'
import { loadRazorpay, type RazorpayCheckout } from '../razorpay'
import { usePageTitle } from '../usePageTitle'

// Only ever shown in demo mode, when the backend can't be reached, so the
// player can still be tried. It is labelled as a sample wherever it appears.
const SAMPLE_REEL = 'https://demo.unified-streaming.com/k8s/features/stable/video/tears-of-steel/tears-of-steel.ism/.m3u8'
const PRESET_AMOUNTS = [500, 1000, 2500, 5000] as const

type Props = { id: string; onBack: () => void }
type Phase = 'idle' | 'ordering' | 'paying' | 'confirming' | 'done'

export default function CampaignDetail({ id, onBack }: Props) {
  const [campaign, setCampaign] = useState<Campaign | null>(null)
  const [tiers, setTiers] = useState<Tier[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [checkoutKey, setCheckoutKey] = useState('')

  const [tier, setTier] = useState<Tier | null>(null)
  const [amount, setAmount] = useState('')
  const [message, setMessage] = useState('')
  const [phase, setPhase] = useState<Phase>('idle')
  const [pledgeError, setPledgeError] = useState<string | null>(null)
  const [authModalOpen, setAuthModalOpen] = useState(false)
  const [copied, setCopied] = useState(false)

  const [reel, setReel] = useState<Reel>({ kind: 'loading' })
  // blob: URL of a just-uploaded local file, previewed until the transcode is
  // ready. Blob URLs die with the page, so they are never persisted.
  const previewUrl = useRef<string | null>(null)
  const location = useLocation()

  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const [uploading, setUploading] = useState(false)
  const [uploadProgress, setUploadProgress] = useState<number | null>(null)
  const [uploadSuccess, setUploadSuccess] = useState<string | null>(null)
  const [uploadError, setUploadError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    const [c, t] = await Promise.all([getCampaign(id), getTiers(id)])
    setCampaign(c)
    setTiers(t || [])
  }, [id])

  useEffect(() => {
    setLoading(true)
    Promise.all([refresh(), getConfig()])
      .then(([, cfg]) => setCheckoutKey((cfg as { razorpay_key_id?: string }).razorpay_key_id || ''))
      .catch(err => {
        const notFound = err instanceof ApiError && (err.status === 404 || err.code === 'INVALID_ID')
        setError(notFound ? 'This film could not be found.' : (err as Error).message)
      })
      .finally(() => setLoading(false))
  }, [id, refresh])

  useEffect(() => () => {
    if (previewUrl.current) URL.revokeObjectURL(previewUrl.current)
  }, [])

  // Ask the server whether this film has a processed reel. Only when the server
  // can't be reached do we fall back to the labelled sample clip; a film that
  // genuinely has no reel says so instead of playing someone else's footage.
  useEffect(() => {
    let cancelled = false
    setReel({ kind: 'loading' })
    getCampaignVideo(id)
      .then(v => {
        if (cancelled) return
        setReel(v.status === 'READY' && v.asset_id ? { kind: 'server', src: playbackMasterUrl(v.asset_id) } : { kind: 'none' })
      })
      .catch(err => {
        if (cancelled) return
        setReel(isBackendUnavailable(err) ? { kind: 'sample', src: SAMPLE_REEL } : { kind: 'none' })
      })
    return () => {
      cancelled = true
    }
  }, [id])

  const activeUser = useActiveUser()
  usePageTitle(campaign?.title)

  // Arriving from "Watch" (…#watch) lands on the player, not the top of the page.
  useEffect(() => {
    if (!loading && location.hash === '#watch') {
      document.getElementById('watch')?.scrollIntoView({ block: 'start' })
    }
  }, [loading, location.hash])

  function selectTier(t: Tier) {
    if (tier?.id === t.id) {
      setTier(null)
      return
    }
    setTier(t)
    setAmount(String(t.min_amount / 100))
    setPledgeError(null)
  }

  async function handleShare() {
    try {
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(window.location.href)
      } else {
        const input = document.createElement('input')
        input.value = window.location.href
        document.body.appendChild(input)
        input.select()
        document.execCommand('copy')
        document.body.removeChild(input)
      }
      setCopied(true)
      setTimeout(() => setCopied(false), 2200)
    } catch {
      // ignore
    }
  }

  async function confirm(pledgeId: string, checkout: unknown) {
    setPhase('confirming')
    try {
      const result = await confirmPledge(pledgeId, checkout)
      if (result.status === 'REFUND_PENDING') {
        // The payment landed after the reward sold out; it is marked for refund.
        await refresh()
        setPledgeError('That reward sold out while you were paying. Your payment is recorded and marked for a full refund.')
        setPhase('idle')
        return
      }
      if (result.status !== 'CAPTURED') {
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
      // We can't tell whether money moved, only that the server didn't confirm
      // it. Payment webhooks settle the pledge independently of this call.
      setPledgeError(`We couldn't confirm your payment yet (${(err as Error).message}). If you were charged, it will appear in My Pledges once it settles.`)
      setPhase('idle')
    }
  }

  function openCheckout(Rzp: RazorpayCheckout, pledge: { id: string; order_id: string; amount: number; currency?: string }) {
    const rzp = new Rzp({
      key: checkoutKey,
      order_id: pledge.order_id,
      amount: pledge.amount,
      currency: pledge.currency || 'INR',
      name: 'CineFund',
      description: campaign?.title,
      theme: { color: '#E5A93C', backdrop_color: '#0C0E14' },
      handler: (response: unknown) => confirm(pledge.id, response),
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

  async function submit(e: React.FormEvent) {
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
    if (!activeUser) {
      setAuthModalOpen(true)
      setPledgeError('Please sign in or select a demo account to back this film.')
      return
    }
    setPhase('ordering')
    const backerId = activeUser.id
    const backerName = activeUser.name
    try {
      const pledge = await createPledge(id, {
        backer_id: backerId,
        backer_name: backerName,
        tier_id: tier ? tier.id : null,
        amount: paise,
        message,
        anonymous: false,
      })
      if (checkoutKey && !pledge.id.startsWith('pledge-demo-')) {
        // Real gateway: the user must complete Razorpay Checkout. If the script
        // can't load we stop here rather than pretending the payment happened.
        openCheckout(await loadRazorpay(), pledge)
      } else {
        // Fake gateway (dev) or offline demo: nothing to pay, settle directly.
        await confirm(pledge.id, { razorpay_order_id: pledge.order_id })
      }
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setAuthModalOpen(true)
        setPledgeError('Your session is not signed in to the server. Sign in with Google or a demo profile to back this film.')
      } else {
        setPledgeError((err as Error).message)
      }
      setPhase('idle')
    }
  }

  async function handleVideoUpload() {
    if (!uploadFile || !activeUser) return
    setUploading(true)
    setUploadError(null)
    setUploadSuccess(null)
    setUploadProgress(0)
    try {
      const assetId = await uploadVideoFileToS3(uploadFile, activeUser.id, id, (pct) => {
        setUploadProgress(pct)
      })
      if (previewUrl.current) URL.revokeObjectURL(previewUrl.current)
      previewUrl.current = URL.createObjectURL(uploadFile)
      setReel({ kind: 'preview', src: previewUrl.current })
      setUploadSuccess(
        assetId.startsWith('asset-demo-')
          ? `"${uploadFile.name}" is previewing locally (demo mode — nothing was uploaded).`
          : `"${uploadFile.name}" uploaded. It is being prepared for streaming and will replace this preview once it is ready.`
      )
      setUploadFile(null)
      setUploadProgress(null)
    } catch (err) {
      setUploadError((err as Error).message)
      setUploadProgress(null)
    } finally {
      setUploading(false)
    }
  }

  if (loading) {
    return (
      <div className="py-2" role="status" aria-busy="true" aria-label="Loading film">
        <div className="skeleton h-56 sm:h-72 md:h-80 w-full rounded-2xl mb-6" />
        <div className="grid gap-8 lg:grid-cols-[1fr_22rem]">
          <div className="space-y-3">
            <div className="skeleton aspect-video w-full rounded-2xl" />
            <div className="skeleton h-5 w-1/3" />
            <div className="skeleton h-3.5 w-full" />
            <div className="skeleton h-3.5 w-5/6" />
          </div>
          <div className="skeleton h-72 w-full rounded-xl" />
        </div>
        <span className="sr-only">Loading film…</span>
      </div>
    )
  }
  if (error) {
    return (
      <div className="py-16 max-w-md mx-auto text-center space-y-4" role="alert">
        <p className="notice notice-error text-left">{error}</p>
        <button
          onClick={onBack}
          className="px-4 py-2 rounded-lg border border-white/15 bg-white/[0.04] hover:bg-white/[0.08] text-xs font-mono text-silver cursor-pointer"
        >
          ← All films
        </button>
      </div>
    )
  }
  if (!campaign) return <p className="status">Not found.</p>

  const days = daysLeft(campaign.deadline)
  const stage = campaignPhase(campaign)
  const goalReached = campaign.goal_amount > 0 && campaign.raised_amount >= campaign.goal_amount
  const toGo = remainingToGoal(campaign.raised_amount, campaign.goal_amount)

  // Pledge-form hints, derived from what's typed so they update as you type.
  const paise = toPaise(amount)
  const validAmount = amount !== '' && Number.isFinite(paise) && paise >= 100
  const belowTier = !!tier && validAmount && paise < tier.min_amount
  const unlocked = validAmount
    ? [...tiers]
        .filter(t => t.min_amount <= paise && !(t.quantity_limit && t.claimed_count >= t.quantity_limit))
        .sort((a, b) => b.min_amount - a.min_amount)[0]
    : undefined
  const busy = phase !== 'idle' && phase !== 'done'
  const busyLabel: Record<Exclude<Phase, 'idle' | 'done'>, string> = {
    ordering: 'Opening checkout…',
    paying: 'Waiting for payment…',
    confirming: 'Recording…',
  }

  const poster = getPosterForCampaign(campaign)

  // Only this campaign's own creator sees the uploader. (The server enforces
  // the same rule; this just avoids offering an action that would be refused.)
  const isCreator = activeUser?.id === campaign.creator_id

  const directorName = campaign.creator_name ||
    (activeUser && activeUser.id === campaign.creator_id ? activeUser.name :
     campaign.creator_id === '00000000-0000-0000-0000-000000000001' ? 'Ava Chen Studio' : 'Independent Director')

  return (
    <div className="py-2 animate-fadeIn max-[940px]:pb-20">
      <div className="mb-4 flex items-center justify-between gap-3">
        <button
          onClick={onBack}
          className="inline-flex items-center gap-2 px-3 py-1.5 rounded-lg border border-white/10 bg-white/[0.02] hover:bg-white/[0.06] text-xs font-medium text-silver hover:text-amber transition-colors cursor-pointer"
        >
          ← All films
        </button>

        <button
          type="button"
          onClick={handleShare}
          className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-white/15 hover:border-amber/40 bg-white/[0.04] hover:bg-white/[0.08] text-xs font-medium text-silver hover:text-amber transition-all cursor-pointer shadow-sm active:scale-95"
          title="Share film with friends or backers"
        >
          {copied ? (
            <>
              <span className="text-emerald-400">✓</span>
              <span className="text-emerald-400 font-semibold font-mono text-[11px]">Link Copied!</span>
            </>
          ) : (
            <>
              <span aria-hidden="true">🔗</span>
              <span>Share</span>
            </>
          )}
        </button>
      </div>

      {/* Cinematic Hero Backdrop Banner */}
      <div className="relative w-full h-56 sm:h-72 md:h-80 rounded-2xl overflow-hidden mb-6 border border-white/[0.1] shadow-2xl bg-obsidian group">
        <img
          src={poster}
          alt={campaign.title}
          onError={(e) => {
            e.currentTarget.onerror = null
            e.currentTarget.src = FALLBACK_POSTER_SVG
          }}
          className="w-full h-full object-cover object-center filter brightness-[0.7] contrast-[1.05] group-hover:scale-[1.02] transition-transform duration-700"
        />
        {/* Multi-layered cinematic gradients */}
        <div className="absolute inset-0 bg-gradient-to-t from-obsidian via-obsidian/40 to-transparent pointer-events-none" />
        <div className="absolute inset-0 bg-gradient-to-r from-obsidian/85 via-obsidian/30 to-transparent pointer-events-none" />

        {/* Banner Overlays */}
        <div className="absolute bottom-4 left-4 right-4 sm:bottom-6 sm:left-6 sm:right-6 flex flex-col sm:flex-row sm:items-end justify-between gap-3">
          <div>
            <div className="flex items-center gap-2 mb-2 text-xs font-mono">
              <span className="px-2.5 py-0.5 rounded bg-amber/20 backdrop-blur-md border border-amber/35 text-amber font-semibold text-[11px] uppercase">
                {campaign.category}
              </span>
            </div>
            <h1 className="font-cinema text-2xl sm:text-4xl lg:text-5xl font-extrabold text-silver tracking-wide leading-tight drop-shadow-md">
              {campaign.title}
            </h1>
            <p className="text-sm sm:text-base text-silver-dim font-sans max-w-xl line-clamp-2 mt-1">
              {campaign.tagline}
            </p>
          </div>

          <div className="flex items-center gap-2">
            {stage === 'live' ? (
              <span className="cinema-live px-3 py-1 text-xs shadow-lg bg-crimson/25 backdrop-blur-md">
                <span className="h-2 w-2 rounded-full bg-crimson animate-ping" aria-hidden="true" />
                LIVE · ESCROW
              </span>
            ) : (
              <span
                className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full border text-xs font-semibold tracking-wider backdrop-blur-md shadow-lg ${
                  stage === 'funded'
                    ? 'bg-emerald-400/15 border-emerald-400/40 text-emerald-400'
                    : 'bg-black/60 border-white/20 text-silver-dim'
                }`}
              >
                {PHASE_LABEL[stage]}
              </span>
            )}
          </div>
        </div>
      </div>

      <div className="detail">
        <article>
          <section id="watch" aria-labelledby="watch-heading" className="scroll-mt-24 mb-8">
            <div className="flex items-baseline justify-between gap-3 mb-3">
              <h2 id="watch-heading" className="font-cinema text-xl font-bold text-silver tracking-wide">
                Watch the reel
              </h2>
              <span className="text-xs text-silver-faint uppercase tracking-wider">{campaign.category}</span>
            </div>
            <div className="bg-black/85 p-2 sm:p-3 rounded-2xl border border-white/10 shadow-[0_0_30px_rgba(0,0,0,0.6)]">
              <ReelPanel
                reel={reel}
                title={campaign.title}
                poster={poster}
                signedIn={!!activeUser}
                isCreator={isCreator}
                onSignIn={() => setAuthModalOpen(true)}
              />
            </div>
          </section>

          {/* S3 Direct Video Uploader - Restricted to Creator */}
          {isCreator && (
            <div className="bg-amber/5 border border-amber/20 rounded-2xl p-5 mb-8 space-y-4 shadow-sm">
              <div className="flex items-center justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="h-2 w-2 rounded-full bg-amber animate-pulse" />
                    <h3 className="text-sm font-bold text-silver font-cinema tracking-wide">
                      Upload your reel
                    </h3>
                  </div>
                  <p className="text-xs text-silver-dim mt-0.5">
                    MP4, MOV, WebM or MKV, up to 500 MB. We encode it for streaming; backers can watch it here once it is ready.
                  </p>
                </div>
                <span className="px-2.5 py-0.5 rounded text-[10px] font-mono border border-amber/30 text-amber bg-amber/10 font-semibold">
                  ONLY YOU SEE THIS
                </span>
              </div>

              <div className="flex flex-col sm:flex-row items-stretch sm:items-center gap-3">
                <input
                  type="file"
                  accept="video/mp4,video/*"
                  onChange={(e) => {
                    if (e.target.files && e.target.files[0]) {
                      setUploadFile(e.target.files[0])
                      setUploadSuccess(null)
                      setUploadError(null)
                    }
                  }}
                  className="file:mr-3 file:py-1.5 file:px-3 file:rounded-md file:border-0 file:text-xs file:font-mono file:bg-white/10 file:text-white hover:file:bg-white/20 cursor-pointer text-xs text-silver"
                />
                <button
                  type="button"
                  onClick={handleVideoUpload}
                  disabled={!uploadFile || uploading}
                  className="px-4 py-2 rounded-lg bg-amber hover:bg-amber-bright text-ink font-bold text-xs transition-all disabled:opacity-30 disabled:cursor-not-allowed whitespace-nowrap cursor-pointer"
                >
                  {uploading ? (uploadProgress !== null ? `Uploading ${uploadProgress}%` : 'Preparing…') : 'Upload reel'}
                </button>
              </div>

              {uploadFile && (
                <p className="text-[11px] font-mono text-silver-faint">
                  Selected: <span className="text-white">{uploadFile.name}</span> ({(uploadFile.size / (1024 * 1024)).toFixed(2)} MB)
                </p>
              )}

              {uploadProgress !== null && (
                <div className="space-y-1.5">
                  <div className="flex justify-between text-[11px] font-mono text-amber">
                    <span>Uploading…</span>
                    <span>{uploadProgress}%</span>
                  </div>
                  <div className="w-full h-1.5 bg-white/10 rounded-full overflow-hidden">
                    <div
                      className="h-full bg-amber transition-all duration-200"
                      style={{ width: `${uploadProgress}%` }}
                    />
                  </div>
                </div>
              )}

              {uploadSuccess && <p className="text-xs font-mono text-emerald-400 bg-emerald-950/30 p-2.5 rounded border border-emerald-800/40">{uploadSuccess}</p>}
              {uploadError && <p className="text-xs font-mono text-rose-400 bg-rose-950/30 p-2.5 rounded border border-rose-800/40">{uploadError}</p>}
            </div>
          )}

          {/* Film Synopsis & Concept */}
          <section className="section bg-celluloid border border-white/[0.08] p-5 sm:p-7 rounded-2xl mb-8 shadow-lg">
            <h2 className="font-cinema text-xl sm:text-2xl font-bold text-silver tracking-wide mb-3.5">
              About this film
            </h2>
            <p className="text-sm sm:text-base text-silver-dim font-sans leading-relaxed mb-6 whitespace-pre-line">
              {campaign.synopsis || campaign.tagline || 'The filmmaker has not added a description yet.'}
            </p>

            <dl className="border-t border-white/[0.08] pt-4 grid grid-cols-2 sm:grid-cols-3 gap-x-6 gap-y-3 text-sm">
              <div>
                <dt className="label">Filmmaker</dt>
                <dd className="text-silver font-medium">{directorName}</dd>
              </div>
              <div>
                <dt className="label">Genre</dt>
                <dd className="text-silver font-medium capitalize">{(campaign.category || 'Cinema').toLowerCase()}</dd>
              </div>
              {campaign.deadline && (
                <div>
                  <dt className="label">{stage === 'live' ? 'Funding ends' : 'Funding ended'}</dt>
                  <dd className="text-silver font-medium">{formatDate(campaign.deadline)}</dd>
                </div>
              )}
            </dl>
          </section>

          {tiers.length > 0 && (
            <section className="section" aria-labelledby="rewards-heading">
              <h2 id="rewards-heading">Choose a reward</h2>
              <p className="-mt-3 mb-5 text-sm text-silver-dim">
                Rewards are optional. Pick one and the amount fills in for you, or enter any amount in the box beside it.
              </p>
              <ul className="tiers">
                {tiers.map(t => {
                  const soldOut = !!t.quantity_limit && t.claimed_count >= (t.quantity_limit as number)
                  return (
                    <li key={t.id} className="tier">
                      <button
                        className={`tier-btn transition-all ${
                          tier?.id === t.id ? '!border-amber bg-amber/10 shadow-[0_0_15px_rgba(229,169,60,0.15)] ring-1 ring-amber/50' : ''
                        }`}
                        aria-pressed={tier?.id === t.id}
                        disabled={!!soldOut}
                        onClick={() => selectTier(t)}
                      >
                        <span className="tier-row">
                          <span className="tier-name flex items-center gap-2">
                            <span>{t.title}</span>
                            {tier?.id === t.id && (
                              <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-amber/20 text-amber font-bold border border-amber/40">
                                ✓ SELECTED
                              </span>
                            )}
                          </span>
                          <span className="tier-min num">{rupees(t.min_amount)}+</span>
                        </span>
                        {t.description && <span className="tier-desc">{t.description}</span>}
                        <span className="tier-stock label">
                          {soldOut ? 'Sold out' : t.quantity_limit ? `${t.claimed_count} of ${t.quantity_limit} claimed` : `${t.claimed_count} claimed`}
                        </span>
                      </button>
                    </li>
                  )
                })}
              </ul>
            </section>
          )}
        </article>

        <aside id="back" aria-label="Back this film" className="rail tw-card !p-6 scroll-mt-24">
          <div className="rail-sum num">{rupees(campaign.raised_amount)}</div>
          <div className="rail-of num">pledged of {rupees(campaign.goal_amount)}</div>
          <div
            className="meter meter-tall rail-meter"
            role="progressbar"
            aria-label="Funding progress"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={percentOf(campaign.raised_amount, campaign.goal_amount)}
          >
            <span
              style={{
                width: `${percentOf(campaign.raised_amount, campaign.goal_amount)}%`,
                background: goalReached ? 'var(--teal)' : undefined,
              }}
            />
          </div>
          <p className={`text-xs font-mono -mt-3 mb-5 ${goalReached ? 'text-emerald-400' : 'text-silver-dim'}`}>
            {goalReached
              ? 'Goal reached'
              : `${percentOf(campaign.raised_amount, campaign.goal_amount)}% funded · ${rupees(toGo)} to go`}
          </p>
          <div className="rail-figures">
            <div className="rail-figure">
              <b className="num">{campaign.backer_count}</b>
              <span className="label">Backers</span>
            </div>
            <div className="rail-figure">
              <b className="num">{stage === 'live' ? (days !== null && days > 0 ? days : 0) : '—'}</b>
              <span className="label">Days left</span>
            </div>
          </div>

          {stage !== 'live' && (
            <div className="mt-6 p-4 rounded-xl bg-black/40 border border-white/10 space-y-1" role="status">
              <p className="font-cinema text-silver">
                {stage === 'funded' ? 'This film is funded' : stage === 'draft' ? 'Not yet published' : 'This campaign has ended'}
              </p>
              <p className="text-xs text-silver-dim leading-relaxed">
                {stage === 'funded'
                  ? 'Thank you to everyone who backed it. Pledges are closed.'
                  : stage === 'draft'
                    ? 'The creator is still preparing this campaign. Pledges open when it goes live.'
                    : 'It is no longer accepting pledges. Browse other films.'}
              </p>
            </div>
          )}

          {stage === 'live' && (
            <form onSubmit={submit} className="form" style={{ marginTop: 24 }}>
              <details className="group rounded-xl border border-white/10 bg-black/30 px-3.5 py-2.5">
                <summary className="cursor-pointer list-none text-xs font-semibold text-silver flex items-center justify-between">
                  How does backing work?
                  <span className="text-silver-faint transition-transform group-open:rotate-180" aria-hidden="true">▾</span>
                </summary>
                <div className="mt-3">
                  <HowItWorks compact />
                </div>
              </details>

              {/* Active Session Info Card */}
              <div className="p-2.5 rounded-xl bg-black/40 border border-white/10 mb-4 flex items-center justify-between text-xs">
                <div className="flex items-center gap-2 min-w-0">
                  <Avatar user={activeUser} className="h-7 w-7 rounded-full text-xs border border-white/10" />
                  <div className="truncate">
                    <span className="text-silver font-medium block truncate">
                      {activeUser ? activeUser.name : 'Guest Visitor'}
                    </span>
                    <span className="text-[10px] font-mono text-silver-faint">
                      {activeUser ? `${activeUser.role}` : 'Sign in to record pledge'}
                    </span>
                  </div>
                </div>
                {!activeUser ? (
                  <button
                    type="button"
                    onClick={() => setAuthModalOpen(true)}
                    className="text-[10px] font-mono text-amber border border-amber/30 px-2 py-0.5 rounded bg-amber/10 hover:bg-amber/20 transition-colors cursor-pointer"
                  >
                    Sign In
                  </button>
                ) : activeUser.offline ? (
                  <span
                    className="text-[10px] font-mono text-silver-dim border border-white/15 px-1.5 py-0.5 rounded bg-white/[0.04]"
                    title="The server could not be reached; this session only exists in this browser"
                  >
                    Offline demo
                  </span>
                ) : (
                  <span className="text-[10px] font-mono text-amber border border-amber/30 px-1.5 py-0.5 rounded bg-amber/10">
                    Signed in
                  </span>
                )}
              </div>

              {/* Quick Pledge Amount Presets */}
              <div className="space-y-1.5 mb-3">
                <div className="flex items-center justify-between text-xs">
                  <span className="text-[11px] font-mono text-silver-dim">
                    Quick Preset:
                  </span>
                  {tier && (
                    <span className="text-[10px] font-mono text-amber">
                      Tier min: {rupees(tier.min_amount)}
                    </span>
                  )}
                </div>
                <div className="grid grid-cols-4 gap-1.5">
                  {PRESET_AMOUNTS.map(preset => {
                    const isSelected = amount === String(preset)
                    return (
                      <button
                        key={preset}
                        type="button"
                        onClick={() => {
                          setAmount(String(preset))
                          setPledgeError(null)
                          if (tier && preset * 100 < tier.min_amount) {
                            setTier(null)
                          }
                        }}
                        className={`py-1.5 px-1 rounded-lg text-xs font-mono font-medium transition-all border cursor-pointer text-center ${
                          isSelected
                            ? '!border-amber bg-amber/20 text-amber font-bold shadow-[0_0_10px_rgba(229,169,60,0.25)]'
                            : 'border-white/10 bg-black/40 text-silver-dim hover:border-amber/40 hover:text-silver'
                        }`}
                      >
                        ₹{preset.toLocaleString('en-IN')}
                      </button>
                    )
                  })}
                </div>
              </div>

              <div className="field">
                <label className="label" htmlFor="amount">
                  {tier ? `Backing ${tier.title}` : 'Custom pledge amount'}
                </label>
                <input
                  id="amount"
                  type="number"
                  inputMode="decimal"
                  min={1}
                  step="any"
                  value={amount}
                  onChange={e => setAmount(e.target.value)}
                  placeholder="₹500"
                  aria-invalid={belowTier || undefined}
                  aria-describedby="amount-hint"
                />
                <p
                  id="amount-hint"
                  className={`text-[11px] font-mono min-h-[1rem] ${belowTier ? 'text-crimson' : 'text-silver-dim'}`}
                  aria-live="polite"
                >
                  {belowTier && tier
                    ? `${tier.title} starts at ${rupees(tier.min_amount)}.`
                    : !tier && unlocked
                      ? (
                        <>
                          Unlocks <span className="text-silver">{unlocked.title}</span>{' '}
                          <button type="button" onClick={() => selectTier(unlocked)} className="text-amber underline underline-offset-2 cursor-pointer">
                            claim it
                          </button>
                        </>
                      )
                      : ''}
                </p>
              </div>
              <div className="field">
                <label className="label" htmlFor="note">A note to the crew</label>
                <input id="note" type="text" maxLength={500} value={message} onChange={e => setMessage(e.target.value)} placeholder="Optional" />
              </div>
              {pledgeError && <p className="notice notice-error" role="alert">{pledgeError}</p>}
              {phase === 'done' && <p className="notice notice-done" role="status">Recorded. Thank you for backing this one.</p>}
              <button type="submit" className="btn btn-wide cursor-pointer" disabled={busy}>
                {busy
                  ? busyLabel[phase as Exclude<Phase, 'idle' | 'done'>]
                  : !activeUser
                    ? 'Sign in & back this film'
                    : validAmount
                      ? `Back this film · ${rupees(paise)}`
                      : 'Back this film'}
              </button>
              <p className="text-xs text-silver-dim text-center leading-relaxed">
                <span aria-hidden="true">🔒 </span>
                Secure payment with Razorpay. All-or-nothing: if this film doesn&rsquo;t reach {rupees(campaign.goal_amount)}
                {campaign.deadline ? ` by ${formatDate(campaign.deadline)}` : ' by its deadline'}, you&rsquo;re refunded in full.
              </p>
            </form>
          )}
        </aside>
      </div>

      {/* On phones the pledge form is far down the page; keep the main action in reach. */}
      {stage === 'live' && (
        <div className="hidden max-[940px]:flex fixed inset-x-0 bottom-0 z-30 items-center justify-between gap-3 border-t border-white/10 bg-obsidian/95 backdrop-blur-md px-4 py-3">
          <div className="min-w-0 text-xs">
            <p className="font-semibold text-silver truncate">{rupees(campaign.raised_amount)} pledged</p>
            <p className="text-silver-dim truncate">
              {goalReached ? 'Goal reached' : `${percentOf(campaign.raised_amount, campaign.goal_amount)}% of ${rupees(campaign.goal_amount)}`}
              {days !== null && days > 0 ? ` · ${days}d left` : ''}
            </p>
          </div>
          <button
            type="button"
            onClick={() => document.getElementById('back')?.scrollIntoView({ behavior: 'smooth', block: 'start' })}
            className="shrink-0 px-5 py-2.5 rounded-xl bg-amber hover:bg-amber-bright text-ink text-sm font-semibold cursor-pointer"
          >
            Back this film
          </button>
        </div>
      )}

      <AuthModal
        isOpen={authModalOpen}
        onClose={() => setAuthModalOpen(false)}
      />
    </div>
  )
}
