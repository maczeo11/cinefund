import VideoPlayer from './VideoPlayer.tsx'

const SAMPLE_HLS = 'https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8'

export default function Watch() {
  return (
    <div className="max-w-4xl">
      <h1 className="font-serif text-3xl mb-2">Watch</h1>
      <p className="text-sm text-white/50 mb-6">HLS ABR — transcoder probes with ffprobe, builds ladder down from source (no upscale), GOP 48 @24fps, scene detection off, master playlist atomic.</p>
      <VideoPlayer src={SAMPLE_HLS} title="Demo stream — gated by funding" />
      <div className="mt-4 tw-card !py-3 flex gap-4 text-xs">
        <span className="label">SKIP LOCKED leasing</span>
        <span className="text-white/40">workers heartbeat lease → fencing token prevents stale write</span>
        <span className="ml-auto label">MinIO presigned PUT</span>
      </div>
    </div>
  )
}
