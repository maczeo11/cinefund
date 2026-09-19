import { useState, useEffect } from 'react'
import { createCampaign, publishCampaign, addTier, uploadVideoFileToS3, POSTER_PRESETS, getPosterForCampaign, FALLBACK_POSTER_SVG } from '../api'
import { toPaise } from '../format'
import AuthModal, { getActiveUser, setActiveUser, DEMO_USERS, type UserProfile } from './AuthModal.tsx'

const CATEGORIES = ['DRAMA', 'COMEDY', 'DOCUMENTARY', 'ANIMATION', 'HORROR', 'SCIFI', 'EXPERIMENTAL'] as const

type Props = { onDone: () => void }

export default function CampaignForm({ onDone }: Props) {
  const [film, setFilm] = useState({ title: '', tagline: '', synopsis: '', category: 'DRAMA', goal: '' })
  const [reward, setReward] = useState({ title: '', min: '', description: '' })
  const [posterUrl, setPosterUrl] = useState<string>(POSTER_PRESETS[0].url)
  const [selectedPresetId, setSelectedPresetId] = useState<string | null>(POSTER_PRESETS[0].id)
  const [videoFile, setVideoFile] = useState<File | null>(null)
  const [uploadProgress, setUploadProgress] = useState<number | null>(null)
  const [uploadStage, setUploadStage] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [authModalOpen, setAuthModalOpen] = useState(false)

  const [activeUser, setActiveUserLocal] = useState<UserProfile | null>(getActiveUser())

  useEffect(() => {
    const handleAuthChange = () => setActiveUserLocal(getActiveUser())
    window.addEventListener('cinefund_auth_change', handleAuthChange)
    return () => window.removeEventListener('cinefund_auth_change', handleAuthChange)
  }, [])

  const set = <T extends object>(obj: T, setter: React.Dispatch<React.SetStateAction<T>>) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) =>
    setter({ ...obj, [e.target.name]: e.target.value })

  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    if (e.target.files && e.target.files[0]) {
      const file = e.target.files[0]
      if (file.size > 500 * 1024 * 1024) {
        setError(`File is ${(file.size / (1024 * 1024)).toFixed(1)}MB. Maximum allowed upload size is 500MB.`)
      } else {
        setError(null)
      }
      setVideoFile(file)
    }
  }

  function handleSelectPreset(preset: typeof POSTER_PRESETS[0]) {
    setPosterUrl(preset.url)
    setSelectedPresetId(preset.id)
    if (!film.category || film.category === 'DRAMA') {
      setFilm(prev => ({ ...prev, category: preset.genre }))
    }
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    if (!film.title || !film.tagline || !film.goal) {
      setError('Title, tagline, and funding goal are required.')
      return
    }
    setSubmitting(true)

    const creatorId = activeUser?.id || DEMO_USERS[0].id
    const creatorName = activeUser?.name || 'Ava Chen'
    const finalPoster = posterUrl.trim() || getPosterForCampaign({ category: film.category })

    try {
      setUploadStage('Setting up campaign details...')
      const campaign = await createCampaign({
        creator_id: creatorId,
        creator_name: creatorName,
        title: film.title,
        tagline: film.tagline,
        synopsis: film.synopsis || film.tagline,
        category: film.category,
        goal: toPaise(film.goal),
        poster_url: finalPoster,
      })

      if (reward.title && reward.min) {
        await addTier(campaign.id, {
          title: reward.title,
          description: reward.description,
          min_amount: toPaise(reward.min),
          quantity_limit: null,
        })
      }

      // If a video file was selected, upload directly to S3 via Presigned URL
      if (videoFile) {
        setUploadStage('Requesting S3 Presigned URL & uploading video...')
        setUploadProgress(0)
        await uploadVideoFileToS3(videoFile, creatorId, campaign.id, (pct) => {
          setUploadProgress(pct)
          setUploadStage(`Uploading ${videoFile.name} to S3: ${pct}%`)
        })
        setUploadStage('Upload verified. Preparing release...')
      }

      await publishCampaign(campaign.id)
      onDone()
    } catch (err) {
      setError((err as Error).message)
      setSubmitting(false)
      setUploadProgress(null)
      setUploadStage(null)
    }
  }

  // Guest State Guard: Prompt visitor to sign in or use demo
  if (!activeUser) {
    return (
      <div className="max-w-md mx-auto py-16 text-center space-y-6 animate-fadeIn">
        <div className="h-16 w-16 mx-auto rounded-2xl bg-amber/15 border border-amber/30 text-amber font-cinema text-3xl flex items-center justify-center shadow-lg">
          🎬
        </div>
        <div className="space-y-2">
          <h2 className="font-cinema text-2xl font-bold text-silver">
            Launch Your Film Campaign
          </h2>
          <p className="text-xs text-silver-dim leading-relaxed">
            Sign in or register to publish your project, upload master workprints, and receive backer funding.
          </p>
        </div>

        <div className="space-y-2.5 pt-2">
          <button
            type="button"
            onClick={() => setAuthModalOpen(true)}
            className="w-full py-3 px-4 rounded-xl bg-amber hover:bg-amber-bright text-ink font-semibold text-sm transition-all shadow-md active:scale-95 cursor-pointer"
          >
            Sign In / Create Account
          </button>

          <button
            type="button"
            onClick={() => {
              setActiveUser(DEMO_USERS[0])
              setActiveUserLocal(DEMO_USERS[0])
            }}
            className="w-full py-2.5 px-4 rounded-xl border border-white/15 bg-white/[0.04] hover:bg-white/[0.08] text-silver text-xs font-mono transition-all cursor-pointer"
          >
            ⚡ Test with Director Demo (Ava Chen)
          </button>
        </div>

        <AuthModal
          isOpen={authModalOpen}
          onClose={() => setAuthModalOpen(false)}
          initialTab="signin"
        />
      </div>
    )
  }

  return (
    <div className="max-w-2xl mx-auto py-4 animate-fadeIn">
      {/* Session Identity Ribbon */}
      <div className="p-3 rounded-xl bg-[#12151E] border border-white/10 mb-6 flex items-center justify-between shadow-sm">
        <div className="flex items-center gap-2.5 min-w-0">
          <div className="h-8 w-8 rounded-lg bg-amber text-ink font-cinema font-bold text-xs flex items-center justify-center shrink-0 overflow-hidden">
            {activeUser.photoURL || (activeUser.avatar && activeUser.avatar.startsWith('http')) ? (
              <img src={activeUser.photoURL || activeUser.avatar} alt={activeUser.name} className="w-full h-full object-cover" />
            ) : (
              activeUser.avatar || (activeUser.name ? activeUser.name[0].toUpperCase() : 'A')
            )}
          </div>
          <div className="min-w-0">
            <span className="text-xs font-bold text-silver block truncate">
              {activeUser.name}
            </span>
            <span className="text-[10px] font-mono text-silver-dim truncate block">
              {activeUser.email}
            </span>
          </div>
        </div>

        <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-amber/20 text-amber font-semibold uppercase">
          {activeUser.role}
        </span>
      </div>

      <div className="mb-6 space-y-1">
        <h1 className="font-cinema text-3xl font-extrabold text-silver">Launch a Film Campaign</h1>
        <p className="text-xs text-silver-dim">
          Set up your story, funding goals, and workprint reel. Backer funds are safeguarded in double-entry escrow.
        </p>
      </div>

      <form onSubmit={submit} className="space-y-6">
        <div className="p-6 rounded-2xl bg-[#12151E] border border-white/10 space-y-5 shadow-lg">
          <div>
            <label className="block text-xs font-medium text-silver mb-1.5" htmlFor="title">
              Film Title
            </label>
            <input
              id="title"
              name="title"
              value={film.title}
              onChange={set(film, setFilm)}
              placeholder="e.g. The Last Projectionist"
              className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-silver mb-1.5" htmlFor="tagline">
              Logline / Tagline
            </label>
            <input
              id="tagline"
              name="tagline"
              value={film.tagline}
              onChange={set(film, setFilm)}
              placeholder="One line that captures the soul of your film"
              className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-silver mb-1.5" htmlFor="synopsis">
              Full Synopsis & Director's Vision
            </label>
            <textarea
              id="synopsis"
              name="synopsis"
              rows={4}
              value={film.synopsis}
              onChange={set(film, setFilm)}
              placeholder="Describe your film's narrative, visual approach, and where production funds will be directed."
              className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
            />
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-medium text-silver mb-1.5" htmlFor="category">
                Genre
              </label>
              <select
                id="category"
                name="category"
                value={film.category}
                onChange={set(film, setFilm)}
                className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver focus:outline-none focus:border-amber transition-all"
              >
                {CATEGORIES.map(c => (
                  <option key={c} value={c} className="bg-[#12151E] text-silver">
                    {c}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-xs font-medium text-silver mb-1.5" htmlFor="goal">
                Funding Goal (₹)
              </label>
              <input
                id="goal"
                name="goal"
                type="number"
                min={1000}
                value={film.goal}
                onChange={set(film, setFilm)}
                placeholder="500000"
                className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
              />
            </div>
          </div>

          {/* Cinematic Poster Artwork & Presets Section */}
          <div className="border-t border-white/[0.08] pt-5 space-y-3">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-silver" htmlFor="posterUrl">
                Poster Artwork (16:9 Landscape)
              </label>
              <span className="text-[10px] font-mono text-amber">
                High-Resolution Preview
              </span>
            </div>

            <input
              id="posterUrl"
              type="url"
              value={posterUrl}
              onChange={e => {
                setPosterUrl(e.target.value)
                setSelectedPresetId(null)
              }}
              placeholder="https://images.unsplash.com/... or choose preset below"
              className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-xs font-mono text-silver placeholder-silver-faint focus:outline-none focus:border-amber transition-all"
            />

            {/* Curated Presets Grid */}
            <div>
              <span className="text-[11px] text-silver-dim block mb-2 font-mono">
                Or select a curated visual style:
              </span>
              <div className="grid grid-cols-2 sm:grid-cols-3 gap-2.5">
                {POSTER_PRESETS.map(preset => {
                  const isSelected = selectedPresetId === preset.id || posterUrl === preset.url
                  return (
                    <button
                      key={preset.id}
                      type="button"
                      onClick={() => handleSelectPreset(preset)}
                      className={`p-2 rounded-xl border text-left transition-all flex flex-col gap-1.5 group cursor-pointer ${
                        isSelected
                          ? 'border-amber bg-amber/15 shadow-[0_0_12px_rgba(229,169,60,0.25)] ring-1 ring-amber/50'
                          : 'border-white/10 bg-black/40 hover:border-white/25 hover:bg-white/[0.03]'
                      }`}
                    >
                      <div className="aspect-video w-full rounded-lg overflow-hidden relative bg-black">
                        <img
                          src={preset.url}
                          alt={preset.title}
                          onError={(e) => {
                            e.currentTarget.onerror = null
                            e.currentTarget.src = FALLBACK_POSTER_SVG
                          }}
                          className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300"
                        />
                        <span className="absolute bottom-1 left-1 px-1.5 py-0.2 rounded bg-black/80 backdrop-blur-md text-[8px] font-mono text-amber font-semibold uppercase">
                          {preset.genre}
                        </span>
                      </div>
                      <span className="text-[10px] font-bold text-silver truncate group-hover:text-amber transition-colors">
                        {preset.title}
                      </span>
                    </button>
                  )
                })}
              </div>
            </div>

            {/* Live 16:9 Thumbnail Preview */}
            <div className="p-3.5 rounded-xl bg-black/50 border border-white/10 space-y-2 mt-3">
              <span className="text-[10px] font-mono uppercase tracking-wider text-silver-dim block">
                Catalog Thumbnail Preview (16:9)
              </span>
              <div className="relative aspect-video w-full max-w-sm mx-auto rounded-xl overflow-hidden border border-white/15 shadow-xl bg-black">
                <img
                  src={posterUrl || getPosterForCampaign({ category: film.category })}
                  alt="Poster preview"
                  onError={(e) => {
                    e.currentTarget.onerror = null
                    e.currentTarget.src = FALLBACK_POSTER_SVG
                  }}
                  className="w-full h-full object-cover"
                />
                <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-transparent to-transparent pointer-events-none" />
                <div className="absolute top-2 left-2 px-2 py-0.5 rounded bg-black/75 backdrop-blur-md border border-white/20 text-[9px] font-mono text-silver">
                  35MM
                </div>
                <div className="absolute bottom-2 left-2 right-2">
                  <span className="font-cinema font-bold text-sm text-silver block truncate">
                    {film.title || 'Untitled Film'}
                  </span>
                  <span className="text-[10px] font-mono text-amber uppercase">
                    {film.category}
                  </span>
                </div>
              </div>
            </div>
          </div>

          {/* Master Video Reel Upload */}
          <div className="border-t border-white/[0.08] pt-4">
            <label className="block text-xs font-medium text-silver mb-1" htmlFor="video">
              Workprint Reel or Trailer (.mp4) — Optional
            </label>
            <input
              id="video"
              type="file"
              accept="video/mp4,video/*"
              onChange={handleFileChange}
              className="file:mr-4 file:py-2 file:px-4 file:rounded-lg file:border-0 file:text-xs file:font-semibold file:bg-white/10 file:text-white hover:file:bg-white/20 cursor-pointer text-sm text-silver"
            />
            {videoFile && (
              <p className="text-xs text-silver mt-1.5 font-mono">
                Selected: <span className="text-white">{videoFile.name}</span> ({(videoFile.size / (1024 * 1024)).toFixed(2)} MB)
              </p>
            )}
            <p className="text-[11px] text-silver-dim mt-1">
              Uploaded securely to private storage and encoded into multi-bitrate HLS streams.
            </p>
          </div>
        </div>

        {/* First Reward Tier */}
        <div className="p-6 rounded-2xl bg-[#12151E] border border-white/10 space-y-4 shadow-lg">
          <h3 className="font-semibold text-silver text-sm">First Backer Reward Tier — Optional</h3>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-medium text-silver mb-1.5" htmlFor="rtitle">
                Tier Title
              </label>
              <input
                id="rtitle"
                name="title"
                value={reward.title}
                onChange={set(reward, setReward)}
                placeholder="e.g. Workprint Access & Digital Credit"
                className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-silver mb-1.5" htmlFor="rmin">
                Minimum Pledge (₹)
              </label>
              <input
                id="rmin"
                name="min"
                type="number"
                min={1}
                value={reward.min}
                onChange={set(reward, setReward)}
                placeholder="1000"
                className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
              />
            </div>
          </div>
          <div>
            <label className="block text-xs font-medium text-silver mb-1.5" htmlFor="rdesc">
              Reward Deliverables
            </label>
            <input
              id="rdesc"
              name="description"
              value={reward.description}
              onChange={set(reward, setReward)}
              placeholder="Online screening access, digital master credit, invite to premiere"
              className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
            />
          </div>
        </div>

        {uploadStage && (
          <div className="p-4 rounded-xl bg-black/40 border border-white/10 space-y-2">
            <div className="flex justify-between text-xs font-mono text-amber">
              <span>{uploadStage}</span>
              {uploadProgress !== null && <span>{uploadProgress}%</span>}
            </div>
            {uploadProgress !== null && (
              <div className="w-full h-1.5 bg-white/10 rounded-full overflow-hidden">
                <div
                  className="h-full bg-amber transition-all duration-300"
                  style={{ width: `${uploadProgress}%` }}
                />
              </div>
            )}
          </div>
        )}

        {error && (
          <p className="text-xs font-mono text-crimson bg-crimson/10 p-3 rounded-xl border border-crimson/30">
            {error}
          </p>
        )}

        <div className="flex gap-3 pt-2">
          <button
            type="submit"
            className="flex-1 py-3.5 rounded-xl bg-amber hover:bg-amber-bright text-ink font-cinema font-bold text-sm tracking-wider shadow-lg transition-all active:scale-[0.99] disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
            disabled={submitting}
          >
            {submitting ? (uploadStage ? 'Uploading Video…' : 'Publishing Campaign…') : 'Publish Campaign'}
          </button>
          <button
            type="button"
            className="px-5 py-3.5 rounded-xl bg-white/[0.04] hover:bg-white/[0.08] text-silver text-xs font-medium transition-colors cursor-pointer"
            onClick={onDone}
            disabled={submitting}
          >
            Cancel
          </button>
        </div>
      </form>
    </div>
  )
}
