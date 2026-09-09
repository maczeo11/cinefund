import { useState, useEffect, useRef } from 'react'
import { getAuthToken } from '../api'

const HLS_CDN = 'https://cdn.jsdelivr.net/npm/hls.js@1'

declare global {
  interface Window {
    Hls?: {
      isSupported: () => boolean
      Events: { ERROR: string }
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      new (opts: unknown): any
    }
    Razorpay?: unknown
  }
}

function isSameOrigin(url: string): boolean {
  try {
    return new URL(url, window.location.href).origin === window.location.origin
  } catch {
    return false
  }
}

function attachHls(video: HTMLVideoElement, src: string, onFatal: () => void, withAuth: boolean): unknown {
  if (!window.Hls?.isSupported()) {
    video.src = src
    return null
  }
  // Auth-gated playlists (private bucket) need the Bearer token on playlist
  // fetches. Segment URLs are presigned S3 links on another origin, so the
  // header is only attached same-origin: S3 rejects requests that mix a
  // presigned query string with an Authorization header.
  const hls = new window.Hls({
    capLevelToPlayerSize: true,
    xhrSetup: withAuth
      ? (xhr: XMLHttpRequest, url: string) => {
          if (isSameOrigin(url)) {
            const token = getAuthToken()
            if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`)
          }
        }
      : undefined,
  })
  hls.loadSource(src)
  hls.attachMedia(video)
  hls.on(window.Hls.Events.ERROR, (_: unknown, data: { fatal: boolean }) => {
    if (data.fatal) onFatal()
  })
  return hls
}

type Props = { src: string; poster?: string; title?: string; withAuth?: boolean }

export default function VideoPlayer({ src, poster, title, withAuth = false }: Props) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [playing, setPlaying] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const video = videoRef.current
    if (!src || !video) return
    const fail = () => setError('This stream is unavailable.')

    if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = src
      return
    }
    if (window.Hls) {
      const hls = attachHls(video, src, fail, withAuth) as { destroy?: () => void } | null
      return () => hls?.destroy?.()
    }

    let hls: { destroy?: () => void } | undefined
    const script = document.createElement('script')
    script.src = HLS_CDN
    script.async = true
    script.onload = () => {
      hls = attachHls(video, src, fail, withAuth) as { destroy?: () => void }
    }
    script.onerror = () => {
      video.src = src
    }
    document.body.appendChild(script)

    return () => {
      hls?.destroy?.()
      script.remove()
    }
  }, [src, withAuth])

  function play() {
    videoRef.current?.play().catch(() => setError('Playback could not start.'))
  }

  return (
    <div className="frame rounded-lg overflow-hidden border border-white/10">
      <video ref={videoRef} controls poster={poster} onPlay={() => setPlaying(true)} onPause={() => setPlaying(false)} />
      {!playing && !error && (
        <div className="frame-cover" onClick={play}>
          <span className="label bg-black/40 px-2 py-1 rounded">Play</span>
          <span className="frame-cue">{title}</span>
        </div>
      )}
      {error && <p className="frame-note">{error}</p>}
    </div>
  )
}
