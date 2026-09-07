import { useState } from 'react'
import VideoPlayer from './VideoPlayer.tsx'
import { FALLBACK_POSTER_SVG } from '../api'

const REELS = [
  {
    id: 'reel-1',
    title: 'The Last Frame',
    subtitle: '35mm Master (Sci-Fi Cut)',
    src: 'https://demo.unified-streaming.com/k8s/features/stable/video/tears-of-steel/tears-of-steel.ism/.m3u8',
    aspect: '2.39:1 Anamorphic',
    fps: '24.000 fps',
    ladder: '1080p / 720p / 480p ABR',
    poster: 'https://images.unsplash.com/photo-1489599849927-2ee91cede3ba?auto=format&fit=crop&w=1200&q=80',
    category: '35MM NOIR',
  },
  {
    id: 'reel-2',
    title: 'Solaris Drift',
    subtitle: 'Outer Orbit Carrier Test',
    src: 'https://bitdash-a.akamaihd.net/content/sintel/hls/playlist.m3u8',
    aspect: '1.85:1 Flat',
    fps: '24.000 fps',
    ladder: '1080p / 720p ABR',
    poster: 'https://images.unsplash.com/photo-1451187580459-43490279c0fa?auto=format&fit=crop&w=1200&q=80',
    category: '70MM SCIFI',
  },
]

export default function Watch() {
  const [activeReel, setActiveReel] = useState(REELS[0])

  return (
    <div className="max-w-5xl py-4 animate-fadeIn">
      {/* Screening Room Header */}
      <div className="mb-6 pb-6 border-b border-white/[0.08] flex flex-col md:flex-row md:items-end justify-between gap-4">
        <div>
          <div className="inline-flex items-center gap-2 px-2.5 py-0.5 rounded-full bg-amber/10 border border-amber/25 text-amber text-[11px] font-mono mb-2">
            <span className="h-1.5 w-1.5 rounded-full bg-amber animate-pulse" />
            <span>DIRECTOR WORKPRINT SCREENING ROOM</span>
          </div>
          <h1 className="font-cinema text-3xl sm:text-4xl font-extrabold text-silver">
            The Cinema Hall
          </h1>
          <p className="text-xs sm:text-sm text-silver-dim mt-1.5 max-w-2xl font-sans">
            Stream master cuts transcoded via CineFund's Kafka-driven FFmpeg worker pool with atomic HLS master playlists.
          </p>
        </div>
      </div>

      {/* Reel Selection Cards with 16:9 Posters */}
      <div className="mb-6">
        <span className="text-[11px] font-mono uppercase tracking-wider text-silver-dim block mb-3">
          Select Master Reel to Project:
        </span>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          {REELS.map(r => {
            const isActive = activeReel.id === r.id
            return (
              <div
                key={r.id}
                onClick={() => setActiveReel(r)}
                className={`p-3 rounded-2xl border transition-all cursor-pointer flex gap-3.5 items-center ${
                  isActive
                    ? 'border-amber bg-amber/15 shadow-[0_0_20px_rgba(229,169,60,0.25)] ring-1 ring-amber/50'
                    : 'border-white/10 bg-celluloid hover:border-white/20 hover:bg-white/[0.03]'
                }`}
              >
                <div className="relative aspect-video w-32 rounded-xl overflow-hidden shrink-0 border border-white/10 bg-black">
                  <img
                    src={r.poster}
                    alt={r.title}
                    onError={(e) => {
                      e.currentTarget.onerror = null
                      e.currentTarget.src = FALLBACK_POSTER_SVG
                    }}
                    className="w-full h-full object-cover"
                  />
                  <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-transparent to-transparent pointer-events-none" />
                  <span className="absolute bottom-1 left-1 px-1.5 py-0.2 rounded bg-black/80 backdrop-blur-md text-[8px] font-mono text-amber font-bold uppercase">
                    {r.category}
                  </span>
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <h3 className="font-cinema font-bold text-sm text-silver truncate">
                      {r.title}
                    </h3>
                    {isActive && (
                      <span className="px-1.5 py-0.2 rounded bg-amber text-ink text-[9px] font-mono font-bold shrink-0">
                        NOW PLAYING
                      </span>
                    )}
                  </div>
                  <p className="text-[11px] text-silver-dim font-sans truncate mt-0.5">{r.subtitle}</p>
                  <p className="text-[10px] font-mono text-silver-faint mt-1">{r.aspect} • {r.fps}</p>
                </div>
              </div>
            )
          })}
        </div>
      </div>

      {/* Main Cinema Theater Frame */}
      <div className="bg-black/90 p-2 sm:p-4 rounded-2xl border border-white/10 shadow-[0_0_40px_rgba(0,0,0,0.8)]">
        <VideoPlayer src={activeReel.src} title={activeReel.title} />

        {/* Active Reel Timecode & Metadata */}
        <div className="mt-3 px-2 flex flex-wrap items-center justify-between gap-2 text-xs font-mono text-silver-dim">
          <div className="flex items-center gap-3">
            <span className="text-silver font-semibold">{activeReel.title}</span>
            <span className="text-silver-faint">·</span>
            <span className="text-amber">{activeReel.aspect}</span>
          </div>
          <div className="flex items-center gap-3 text-[11px]">
            <span className="cinema-tag">{activeReel.fps}</span>
            <span className="cinema-tag">{activeReel.ladder}</span>
          </div>
        </div>
      </div>

      {/* Technical Architecture Cards */}
      <div className="mt-8 grid grid-cols-1 md:grid-cols-3 gap-4 font-mono text-xs">
        <div className="p-4 bg-celluloid border border-white/10 rounded-xl">
          <span className="text-amber font-semibold">1. FFprobe Analysis</span>
          <p className="text-silver-dim mt-1.5 leading-relaxed font-sans text-xs">
            Probes duration, codec, and resolution. Strict rotation handling and validation before queueing.
          </p>
        </div>

        <div className="p-4 bg-celluloid border border-white/10 rounded-xl">
          <span className="text-amber font-semibold">2. ABR Strict GOP 48</span>
          <p className="text-silver-dim mt-1.5 leading-relaxed font-sans text-xs">
            24fps with 48-frame keyframe intervals, scene detection off. Seamless quality switching without glitching.
          </p>
        </div>

        <div className="p-4 bg-celluloid border border-white/10 rounded-xl">
          <span className="text-amber font-semibold">3. Crash-Safe Fencing</span>
          <p className="text-silver-dim mt-1.5 leading-relaxed font-sans text-xs">
            Workers claim jobs with <code className="text-amber">SKIP LOCKED</code> leases and heartbeat. Fencing tokens prevent stale worker overwrites.
          </p>
        </div>
      </div>
    </div>
  )
}
