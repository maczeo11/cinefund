import { useState } from 'react'
import { createCampaign, publishCampaign, addTier, uploadVideoFileToS3 } from '../api'
import { toPaise } from '../format'

const CATEGORIES = ['DRAMA', 'COMEDY', 'DOCUMENTARY', 'ANIMATION', 'HORROR', 'SCIFI', 'EXPERIMENTAL'] as const
const CREATOR_ID = '00000000-0000-0000-0000-000000000001'

type Props = { onDone: () => void }

export default function CampaignForm({ onDone }: Props) {
  const [film, setFilm] = useState({ title: '', tagline: '', synopsis: '', category: 'DRAMA', goal: '' })
  const [reward, setReward] = useState({ title: '', min: '', description: '' })
  const [videoFile, setVideoFile] = useState<File | null>(null)
  const [uploadProgress, setUploadProgress] = useState<number | null>(null)
  const [uploadStage, setUploadStage] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const set = <T extends object>(obj: T, setter: React.Dispatch<React.SetStateAction<T>>) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) =>
    setter({ ...obj, [e.target.name]: e.target.value })

  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    if (e.target.files && e.target.files[0]) {
      const file = e.target.files[0]
      if (file.size > 50 * 1024 * 1024) {
        setError(`File is ${(file.size / (1024 * 1024)).toFixed(1)}MB. Free Tier S3 Demo recommends files under 50MB.`)
      } else {
        setError(null)
      }
      setVideoFile(file)
    }
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    if (!film.title || !film.tagline || !film.goal) {
      setError('Title, tagline and goal are required.')
      return
    }
    setSubmitting(true)
    try {
      setUploadStage('Creating campaign record in Postgres...')
      const campaign = await createCampaign({
        creator_id: CREATOR_ID,
        title: film.title,
        tagline: film.tagline,
        synopsis: film.synopsis || film.tagline,
        category: film.category,
        goal: toPaise(film.goal),
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
        setUploadStage('Requesting S3 Presigned URL & uploading directly...')
        setUploadProgress(0)
        await uploadVideoFileToS3(videoFile, CREATOR_ID, campaign.id, (pct) => {
          setUploadProgress(pct)
          setUploadStage(`Uploading ${videoFile.name} to AWS S3: ${pct}%`)
        })
        setUploadStage('Direct S3 upload verified. Queued FFmpeg HLS transcode worker.')
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

  return (
    <div className="form-page max-w-2xl">
      <h1 className="font-serif text-3xl mb-2">Submit a film</h1>
      <p className="text-sm text-white/50 mb-8">TypeScript + REST `POST /api/v1/campaigns` → Postgres + outbox → Kafka. Goal in ₹, stored as paise (int).</p>

      <form onSubmit={submit} className="form">
        <div className="tw-card !p-6 space-y-6">
          <div className="field">
            <label className="label" htmlFor="title">Title</label>
            <input id="title" name="title" value={film.title} onChange={set(film, setFilm)} placeholder="The Last Frame" />
          </div>
          <div className="field">
            <label className="label" htmlFor="tagline">Tagline</label>
            <input id="tagline" name="tagline" value={film.tagline} onChange={set(film, setFilm)} placeholder="One line that sells the whole thing" />
          </div>
          <div className="field">
            <label className="label" htmlFor="synopsis">Synopsis</label>
            <textarea id="synopsis" name="synopsis" rows={5} value={film.synopsis} onChange={set(film, setFilm)} placeholder="The story, the crew, where the money goes." />
          </div>
          <div className="pair">
            <div className="field">
              <label className="label" htmlFor="category">Genre</label>
              <select id="category" name="category" value={film.category} onChange={set(film, setFilm)}>
                {CATEGORIES.map(c => (
                  <option key={c} value={c}>
                    {c}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label className="label" htmlFor="goal">Goal (₹)</label>
              <input id="goal" name="goal" type="number" min={1000} value={film.goal} onChange={set(film, setFilm)} placeholder="50000" />
            </div>
          </div>

          <div className="field border-t border-white/10 pt-4">
            <label className="label flex items-center justify-between" htmlFor="video">
              <span>Master Film Reel or Teaser (.mp4) — Optional</span>
              <span className="text-[11px] text-amber">Direct S3 Presigned Upload</span>
            </label>
            <input
              id="video"
              type="file"
              accept="video/mp4,video/*"
              onChange={handleFileChange}
              className="file:mr-4 file:py-2 file:px-4 file:rounded-md file:border-0 file:text-xs file:font-mono file:bg-white/10 file:text-white hover:file:bg-white/20 cursor-pointer text-sm text-silver"
            />
            {videoFile && (
              <p className="text-xs text-silver mt-1.5 font-mono">
                Selected: <span className="text-white">{videoFile.name}</span> ({(videoFile.size / (1024 * 1024)).toFixed(2)} MB)
              </p>
            )}
            <p className="text-[11px] text-silver-dim mt-1">
              Bypasses API servers: bytes stream straight from browser to AWS S3 bucket, then triggers FFmpeg HLS transcode.
            </p>
          </div>
        </div>

        <fieldset className="fieldset tw-card !p-6">
          <legend className="label">First reward tier — optional</legend>
          <div className="pair">
            <div className="field">
              <label className="label" htmlFor="rtitle">Tier name</label>
              <input id="rtitle" name="title" value={reward.title} onChange={set(reward, setReward)} placeholder="Screen credit" />
            </div>
            <div className="field">
              <label className="label" htmlFor="rmin">From (₹)</label>
              <input id="rmin" name="min" type="number" min={1} value={reward.min} onChange={set(reward, setReward)} placeholder="1000" />
            </div>
          </div>
          <div className="field">
            <label className="label" htmlFor="rdesc">What backers get</label>
            <input id="rdesc" name="description" value={reward.description} onChange={set(reward, setReward)} placeholder="Name in end credits, digital release" />
          </div>
        </fieldset>

        {uploadStage && (
          <div className="p-4 rounded-lg bg-black/40 border border-white/10 space-y-2">
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

        {error && <p className="notice">{error}</p>}

        <div className="actions">
          <button type="submit" className="btn flex-1" disabled={submitting}>
            {submitting ? (uploadStage ? 'Uploading to S3…' : 'Publishing…') : 'Publish Film & Upload Reel'}
          </button>
          <button type="button" className="link" onClick={onDone} disabled={submitting}>
            Cancel
          </button>
        </div>
      </form>
    </div>
  )
}
