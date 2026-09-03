import { useState } from 'react'
import VideoPlayer from './VideoPlayer.tsx'

const REELS = [
  {
    id: 'reel-1',
    title: 'The Last Frame — 35mm Master (Sci-Fi Cut)',
    src: 'https://demo.unified-streaming.com/k8s/features/stable/video/tears-of-steel/tears-of-steel.ism/.m3u8',
    aspect: '2.39:1 Anamorphic',
    fps: '24.000 fps',
    ladder: '1080p / 720p / 480p ABR',
  },
  {
    id: 'reel-2',
    title: 'Solaris Drift — Outer Orbit Carrier Test',
    src: 'https://bitdash-a.akamaihd.net/content/sintel/hls/playlist.m3u8',
    aspect: '1.85:1 Flat',
    fps: '24.000 fps',
    ladder: '1080p / 720p ABR',
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

        {/* Reel Selector Buttons */}
        <div className="flex gap-2 font-mono text-xs">
          {REELS.map(r => (
            <button
              key={r.id}
              onClick={() => setActiveReel(r)}
              className={`px-3 py-1.5 rounded-lg border transition-all ${
                activeReel.id === r.id
                  ? 'bg-amber text-ink font-bold border-amber shadow-[0_0_12px_rgba(229,169,60,0.3)]'
                  : 'bg-white/[0.03] border-white/10 text-silver-dim hover:text-white'
              }`}
            >
              {r.id === 'reel-1' ? 'Reel 01' : 'Reel 02'}
            </button>
          ))}
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
