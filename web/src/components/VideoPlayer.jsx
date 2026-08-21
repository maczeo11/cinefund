import { useState, useEffect, useRef } from 'react'

const HLS_CDN = 'https://cdn.jsdelivr.net/npm/hls.js@1'

function attachHls(video, src, onFatal) {
  if (!window.Hls?.isSupported()) {
    video.src = src
    return null
  }
  const hls = new window.Hls({ capLevelToPlayerSize: true })
  hls.loadSource(src)
  hls.attachMedia(video)
  hls.on(window.Hls.Events.ERROR, (_, data) => {
    if (data.fatal) onFatal()
  })
  return hls
}

function VideoPlayer({ src, poster, title }) {
  const videoRef = useRef(null)
  const [playing, setPlaying] = useState(false)
  const [error, setError] = useState(null)

  useEffect(() => {
    const video = videoRef.current
    if (!src || !video) return

    const fail = () => setError('This stream is unavailable.')

    // Safari plays HLS natively; everyone else needs hls.js, loaded on demand
    // so the bundle doesn't carry it for a page that may never show a video.
    if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = src
      return
    }
    if (window.Hls) {
      const hls = attachHls(video, src, fail)
      return () => hls?.destroy()
    }

    let hls
    const script = document.createElement('script')
    script.src = HLS_CDN
    script.async = true
    script.onload = () => { hls = attachHls(video, src, fail) }
    script.onerror = () => { video.src = src }
    document.body.appendChild(script)

    return () => {
      hls?.destroy()
      script.remove()
    }
  }, [src])

  function play() {
    videoRef.current?.play().catch(() => setError('Playback could not start.'))
  }

  return (
    <div className="frame">
      <video
        ref={videoRef}
        controls
        poster={poster}
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
      />
      {!playing && !error && (
        <div className="frame-cover" onClick={play}>
          <span className="label">Play</span>
          <span className="frame-cue">{title}</span>
        </div>
      )}
      {error && <p className="frame-note">{error}</p>}
    </div>
  )
}

export default VideoPlayer
