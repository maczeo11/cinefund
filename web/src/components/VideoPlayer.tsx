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
    <div className="frame rounded-lg overflow-hidden border border-white/10 relative group">
      <video
        ref={videoRef}
        controls
        poster={poster}
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        className="w-full h-full object-cover"
      />
      {!playing && !error && (
        <div
          className="absolute inset-0 bg-gradient-to-t from-black/85 via-black/30 to-black/40 flex flex-col justify-between p-4 sm:p-6 cursor-pointer group-hover:from-black/75 transition-all"
          onClick={play}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              play()
            }
          }}
          aria-label={title ? `Play ${title}` : 'Play video reel'}
        >
          {/* Top Reel Badge */}
          <div className="flex items-center justify-between pointer-events-none">
            <span className="px-2.5 py-1 rounded bg-black/75 backdrop-blur-md border border-amber/30 text-[10px] font-mono uppercase tracking-widest text-amber font-bold shadow">
              35mm Workprint Reel
            </span>
          </div>

          {/* Centered Glowing Play Icon */}
          <div className="self-center my-auto pointer-events-none">
            <div className="w-14 h-14 sm:w-16 sm:h-16 rounded-full bg-amber text-night flex items-center justify-center shadow-[0_0_30px_rgba(229,169,60,0.5)] group-hover:scale-110 group-hover:bg-amber-bright transition-transform duration-300">
              <svg className="w-6 h-6 sm:w-7 sm:h-7 translate-x-0.5 fill-current" viewBox="0 0 24 24">
                <path d="M8 5v14l11-7z" />
              </svg>
            </div>
          </div>

          {/* Bottom Title Bar */}
          {title ? (
            <div className="flex items-center gap-2 pointer-events-none">
              <span className="text-white font-cinema text-sm sm:text-base font-medium tracking-wide truncate drop-shadow-md">
                {title}
              </span>
            </div>
          ) : (
            <div />
          )}
        </div>
      )}
      {error && <p className="frame-note">{error}</p>}
    </div>
  )
}
