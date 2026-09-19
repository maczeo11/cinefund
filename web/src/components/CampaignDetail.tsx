import { useState, useEffect, useCallback } from 'react'
import { getCampaign, getTiers, getConfig, createPledge, confirmPledge, uploadVideoFileToS3, getPosterForCampaign, getCampaignVideo, playbackMasterUrl, FALLBACK_POSTER_SVG, type Campaign, type Tier } from '../api'
import { rupees, toPaise, percentOf, daysLeft } from '../format'
import VideoPlayer from './VideoPlayer.tsx'
import AuthModal, { getActiveUser, type UserProfile } from './AuthModal.tsx'

const CINEMATIC_HLS_STREAM = 'https://demo.unified-streaming.com/k8s/features/stable/video/tears-of-steel/tears-of-steel.ism/.m3u8'
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

  const [videoSrc, setVideoSrc] = useState<string>(() => {
    return localStorage.getItem(`cinefund_video_${id}`) || CINEMATIC_HLS_STREAM
  })
  // True when the reel streams from the auth-gated playback API (private
  // bucket + short-lived segment URLs) instead of the public fallback.
  const [videoAuth, setVideoAuth] = useState(false)

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
      .catch(err => setError(`Live backend error: ${(err as Error).message} — check VITE_API_BASE`))
      .finally(() => setLoading(false))
  }, [id, refresh])

  // Prefer the protected transcode output when the backend has a READY reel
  // for this campaign; otherwise keep the local/demo fallback.
  useEffect(() => {
    let cancelled = false
    setVideoAuth(false)
    setVideoSrc(localStorage.getItem(`cinefund_video_${id}`) || CINEMATIC_HLS_STREAM)
    getCampaignVideo(id)
      .then(v => {
        if (!cancelled && v.status === 'READY' && v.asset_id) {
          setVideoSrc(playbackMasterUrl(v.asset_id))
          setVideoAuth(true)
        }
      })
      .catch(() => {
        // Backend unreachable or no reel yet — keep the fallback source.
      })
    return () => {
      cancelled = true
    }
  }, [id])

  const [activeUser, setActiveUser] = useState<UserProfile | null>(getActiveUser())

  useEffect(() => {
    const handleAuthChange = () => setActiveUser(getActiveUser())
    window.addEventListener('cinefund_auth_change', handleAuthChange)
    return () => window.removeEventListener('cinefund_auth_change', handleAuthChange)
  }, [])

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
      if ((result as { status: string }).status !== 'CAPTURED') {
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
      setPledgeError(`Payment went through but we could not record it: ${(err as Error).message}`)
      setPhase('idle')
    }
  }

  function openCheckout(pledge: { id: string; order_id: string; amount: number; currency?: string }) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const Rzp = (window as any).Razorpay
    if (!Rzp) return
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
    rzp.on('payment.failed', (res: { error?: { description?: string } }) => {
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
    const currentUser = getActiveUser()
    if (!currentUser) {
      setAuthModalOpen(true)
      setPledgeError('Please sign in or select a demo account to back this film.')
      return
    }
    setPhase('ordering')
    const backerId = currentUser.id
    const backerName = currentUser.name
    try {
      const pledge = await createPledge(id, {
        backer_id: backerId,
        backer_name: backerName,
        tier_id: tier ? tier.id : null,
        amount: paise,
        message,
        anonymous: false,
      })
      if (checkoutKey && (window as unknown as { Razorpay?: unknown }).Razorpay) {
        openCheckout(pledge as unknown as { id: string; order_id: string; amount: number })
      } else {
        await confirm((pledge as { id: string }).id, { razorpay_order_id: (pledge as { order_id: string }).order_id })
      }
    } catch (err) {
      setPledgeError((err as Error).message)
      setPhase('idle')
    }
  }

  async function handleVideoUpload() {
    if (!uploadFile) return
    setUploading(true)
    setUploadError(null)
    setUploadSuccess(null)
    setUploadProgress(0)
    try {
      const activeUser = getActiveUser()
      const ownerId = activeUser?.id || '00000000-0000-0000-0000-000000000001'
      await uploadVideoFileToS3(uploadFile, ownerId, id, (pct) => {
        setUploadProgress(pct)
      })
      const localUrl = URL.createObjectURL(uploadFile)
      setVideoSrc(localUrl)
      try {
        localStorage.setItem(`cinefund_video_${id}`, localUrl)
      } catch {
        // quota fallback
      }
      setUploadSuccess(`Success! "${uploadFile.name}" streamed to AWS S3. Master player switched to your reel!`)
      setUploadFile(null)
      setUploadProgress(null)
    } catch (err) {
      setUploadError((err as Error).message)
      setUploadProgress(null)
    } finally {
      setUploading(false)
    }
  }

  if (loading) return <p className="status">Loading…</p>
  if (error) return <p className="status notice">{error}</p>
  if (!campaign) return <p className="status">Not found.</p>

  const days = daysLeft(campaign.deadline)
  const busy = phase !== 'idle' && phase !== 'done'
  const busyLabel: Record<Exclude<Phase, 'idle' | 'done'>, string> = {
    ordering: 'Opening checkout…',
    paying: 'Waiting for payment…',
    confirming: 'Recording…',
  }

  const poster = getPosterForCampaign(campaign)

  const isCreator = Boolean(
    activeUser && (
      activeUser.id === campaign.creator_id ||
      activeUser.role === 'CREATOR'
    )
  )

  const directorName = campaign.creator_name ||
    (activeUser && activeUser.id === campaign.creator_id ? activeUser.name :
     campaign.creator_id === '00000000-0000-0000-0000-000000000001' ? 'Ava Chen Studio' : 'Independent Director')

  return (
    <div className="py-2 animate-fadeIn">
      <div className="mb-4 flex items-center justify-between gap-3">
        <button
          onClick={onBack}
          className="inline-flex items-center gap-2 px-3 py-1.5 rounded-lg border border-white/10 bg-white/[0.02] hover:bg-white/[0.06] text-xs font-mono text-silver hover:text-amber transition-colors cursor-pointer"
        >
          ← Return to Film Vault
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
              <span>🔗</span>
              <span>Share Film</span>
            </>
          )}
        </button>
      </div>

      {/* Cinematic Hero Backdrop Banner */}
      <div className="relative w-full h-56 sm:h-72 md:h-80 rounded-2xl overflow-hidden mb-6 border border-white/[0.1] shadow-2xl bg-[#0B0D13] group">
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
              <span className="px-2.5 py-0.5 rounded bg-black/80 backdrop-blur-md border border-white/20 text-silver font-semibold text-[11px]">
                35MM MASTER WORKPRINT
              </span>
              <span className="text-silver-faint">·</span>
              <span className="px-2.5 py-0.5 rounded bg-amber/20 backdrop-blur-md border border-amber/35 text-amber font-semibold text-[11px] uppercase">
                {campaign.category}
              </span>
            </div>
            <h1 className="font-cinema text-2xl sm:text-4xl lg:text-5xl font-extrabold text-silver tracking-wide leading-tight drop-shadow-md">
              {campaign.title}
            </h1>
            <p className="text-xs sm:text-sm text-silver-dim font-sans max-w-xl line-clamp-1 mt-1">
              {campaign.tagline}
            </p>
          </div>

          <div className="flex items-center gap-2">
            <span className="cinema-live px-3 py-1 text-xs shadow-lg bg-crimson/25 backdrop-blur-md">
              <span className="h-2 w-2 rounded-full bg-crimson animate-ping" />
              {campaign.status} ESCROW
            </span>
          </div>
        </div>
      </div>

      <div className="detail">
        <article>
          <div className="flex items-center gap-3 mb-3 text-xs font-mono">
            <span className="cinema-tag">DIRECTOR WORKPRINT REEL</span>
            <span className="text-silver-faint">·</span>
            <span className="text-silver-dim uppercase">{campaign.category}</span>
          </div>

          <div className="bg-black/85 p-2 sm:p-3 rounded-2xl border border-white/10 mb-4 shadow-[0_0_30px_rgba(0,0,0,0.6)]">
            <VideoPlayer src={videoSrc} withAuth={videoAuth} title={`${campaign.title} — Workprint Reel`} />
          </div>

          {/* S3 Direct Video Uploader - Restricted to Creator */}
          {isCreator && (
            <div className="bg-amber/5 border border-amber/20 rounded-2xl p-5 mb-8 space-y-4 shadow-sm">
              <div className="flex items-center justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="h-2 w-2 rounded-full bg-amber animate-pulse" />
                    <h3 className="text-sm font-bold text-silver font-cinema tracking-wide">
                      Director Studio — Video Master Upload
                    </h3>
                  </div>
                  <p className="text-xs text-silver-dim font-mono mt-0.5">
                    Direct Browser-to-S3 Presigned Upload • Never touches API memory
                  </p>
                </div>
                <span className="px-2.5 py-0.5 rounded text-[10px] font-mono border border-amber/30 text-amber bg-amber/10 font-semibold">
                  CREATOR WORKBENCH
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
                  className="px-4 py-2 rounded-lg bg-amber hover:bg-amber-light text-night font-bold text-xs transition-all disabled:opacity-30 disabled:cursor-not-allowed whitespace-nowrap cursor-pointer"
                >
                  {uploading ? (uploadProgress !== null ? `Uploading: ${uploadProgress}%` : 'Presigning…') : 'Upload to AWS S3'}
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
                    <span>Direct streaming to S3...</span>
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
            <div className="flex items-center gap-2.5 mb-3.5">
              <span className="h-2 w-2 rounded-full bg-amber shadow-[0_0_8px_rgba(229,169,60,0.6)]" />
              <h2 className="font-cinema text-xl sm:text-2xl font-bold text-silver tracking-wide">
                Film Synopsis & Concept
              </h2>
            </div>
            <p className="text-sm sm:text-base text-silver-dim font-sans leading-relaxed mb-6 whitespace-pre-line">
              {campaign.synopsis || campaign.tagline || 'No synopsis provided for this production yet.'}
            </p>

            {/* Metadata Badges Ribbon */}
            <div className="border-t border-white/[0.08] pt-4 flex flex-wrap items-center gap-2 sm:gap-3 text-xs font-mono">
              <div className="px-3 py-1.5 rounded-lg bg-white/[0.03] border border-white/10 text-silver-dim flex items-center gap-1.5">
                <span className="text-silver-faint">Reel ID:</span>
                <span className="text-silver font-semibold">{campaign.id.slice(0, 8)}…</span>
              </div>
              <div className="px-3 py-1.5 rounded-lg bg-white/[0.03] border border-white/10 text-silver-dim flex items-center gap-1.5">
                <span className="text-silver-faint">Director:</span>
                <span className="text-silver font-semibold">{directorName}</span>
              </div>
              <div className="px-3 py-1.5 rounded-lg bg-white/[0.03] border border-white/10 text-silver-dim flex items-center gap-1.5">
                <span className="text-silver-faint">Category:</span>
                <span className="text-silver font-semibold uppercase">{campaign.category || 'Cinema'}</span>
              </div>
              <div className="px-3 py-1.5 rounded-lg bg-amber/5 border border-amber/25 text-amber flex items-center gap-1.5">
                <span className="text-amber/70">Escrow:</span>
                <span className="font-semibold">PostgreSQL Ledger</span>
              </div>
            </div>
          </section>

          {tiers.length > 0 && (
            <section className="section">
              <h2>Rewards</h2>
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

        <aside className="rail tw-card !p-6">
          <div className="rail-sum num">{rupees(campaign.raised_amount)}</div>
          <div className="rail-of num">pledged of {rupees(campaign.goal_amount)}</div>
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
            <form onSubmit={submit} className="form" style={{ marginTop: 24 }}>
              {/* Active Session Info Card */}
              <div className="p-2.5 rounded-xl bg-black/40 border border-white/10 mb-4 flex items-center justify-between text-xs">
                <div className="flex items-center gap-2 min-w-0">
                  <div className="h-7 w-7 rounded-full bg-amber/20 text-amber font-cinema font-bold text-xs flex items-center justify-center shrink-0 overflow-hidden border border-white/10">
                    {activeUser?.photoURL || (activeUser?.avatar && activeUser.avatar.startsWith('http')) ? (
                      <img
                        src={activeUser.photoURL || activeUser.avatar}
                        alt={activeUser.name}
                        className="w-full h-full object-cover"
                      />
                    ) : (
                      <span>{activeUser?.avatar || (activeUser?.name ? activeUser.name[0].toUpperCase() : '👤')}</span>
                    )}
                  </div>
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
                ) : (
                  <span className="text-[10px] font-mono text-amber border border-amber/30 px-1.5 py-0.5 rounded bg-amber/10">
                    Verified
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
                <input id="amount" type="number" min={1} value={amount} onChange={e => setAmount(e.target.value)} placeholder="₹500" />
              </div>
              <div className="field">
                <label className="label" htmlFor="note">A note to the crew</label>
                <input id="note" type="text" value={message} onChange={e => setMessage(e.target.value)} placeholder="Optional" />
              </div>
              {pledgeError && <p className="notice">{pledgeError}</p>}
              {phase === 'done' && <p className="notice notice-done">Recorded. Thank you for backing this one.</p>}
              <button type="submit" className="btn btn-wide cursor-pointer" disabled={busy}>
                {busy ? busyLabel[phase as Exclude<Phase, 'idle' | 'done'>] : activeUser ? 'Back this film' : 'Sign in & Back this film'}
              </button>
              <p className="text-[11px] text-white/40 text-center font-mono">🔒 Escrow Protected • Direct-to-Production</p>
            </form>
          )}
        </aside>
      </div>

      <AuthModal
        isOpen={authModalOpen}
        onClose={() => setAuthModalOpen(false)}
        initialTab="signin"
      />
    </div>
  )
}
