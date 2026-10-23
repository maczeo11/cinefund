import { useState, useEffect, useRef } from 'react'
import type Hls from 'hls.js'
import { getAuthToken } from '../api'

function isSameOrigin(url: string): boolean {
  try {
    return new URL(url, window.location.href).origin === window.location.origin
  } catch {
    return false
  }
}

type Props = { src: string; poster?: string; title?: string; withAuth?: boolean; label?: string }

export default function VideoPlayer({ src, poster, title, withAuth = false, label = 'Reel' }: Props) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [playing, setPlaying] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const video = videoRef.current
    if (!src || !video) return
    setError(null)
    setPlaying(false)
    const fail = () => setError('This stream is unavailable.')

    // Safari plays HLS natively (auth rides on the cf_at cookie instead).
    if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = src
      return () => {
        video.removeAttribute('src')
        video.load()
      }
    }

    // hls.js is bundled as its own chunk and only fetched when a player mounts,
    // so it is served from our origin rather than a third-party CDN.
    let cancelled = false
    let hls: Hls | undefined
    import('hls.js')
      .then(({ default: HlsCtor }) => {
        if (cancelled) return
        if (!HlsCtor.isSupported()) {
          fail()
          return
        }
        // Auth-gated playlists need the Bearer token. Segment URLs are
        // presigned links on another origin, so the header is attached
        // same-origin only: S3 rejects a presigned query string combined with
        // an Authorization header, and we don't want to leak the token anyway.
        hls = new HlsCtor({
          capLevelToPlayerSize: true,
          xhrSetup: withAuth
            ? (xhr, url) => {
                if (isSameOrigin(url)) {
                  const token = getAuthToken()
                  if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`)
                }
              }
            : undefined,
        })
        hls.on(HlsCtor.Events.ERROR, (_event, data) => {
          if (data.fatal) fail()
        })
        hls.loadSource(src)
        hls.attachMedia(video)
      })
      .catch(fail)

    return () => {
      cancelled = true
      hls?.destroy()
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
        playsInline
        preload="metadata"
        poster={poster}
        aria-label={title}
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
              {label}
            </span>
          </div>

          {/* Centered Glowing Play Icon */}
          <div className="self-center my-auto pointer-events-none">
            <div className="w-14 h-14 sm:w-16 sm:h-16 rounded-full bg-amber text-ink flex items-center justify-center shadow-[0_0_30px_rgba(229,169,60,0.5)] group-hover:scale-110 group-hover:bg-amber-bright transition-transform duration-300">
              <svg className="w-6 h-6 sm:w-7 sm:h-7 translate-x-0.5 fill-current" viewBox="0 0 24 24" aria-hidden="true">
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
      {error && <p className="frame-note" role="alert">{error}</p>}
    </div>
  )
}
