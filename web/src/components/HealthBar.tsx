import { useEffect, useState } from 'react'
import { getApiBase, getConfig } from '../api'

type Ready = { postgres: string; redis: string }
type Mode = 'live' | 'fake' | 'archive'

export default function HealthBar() {
  const [ready, setReady] = useState<Ready | null>(null)
  const [mode, setMode] = useState<Mode>('archive')
  const base = getApiBase()

  useEffect(() => {
    const healthUrl = `${base.replace('/api/v1', '')}/health/ready`
    Promise.all([
      fetch(healthUrl).then(r => r.json().then(j => ({ ok: r.ok, j }))),
      getConfig().catch(() => ({ razorpay_key_id: '' } as { razorpay_key_id: string })),
    ])
      .then(([{ ok, j }, cfg]) => {
        setReady(j as Ready)
        setMode(!ok ? 'archive' : (cfg as { razorpay_key_id?: string }).razorpay_key_id ? 'live' : 'fake')
      })
      .catch(() => {
        setMode('archive')
      })
  }, [base])

  return (
    <div className="w-full border-b border-white/[0.06] bg-celluloid/80 px-[var(--gutter)] py-2 flex flex-wrap gap-3 sm:gap-5 text-xs items-center font-mono">
      {mode === 'live' && (
        <span className="inline-flex items-center gap-1.5 text-emerald-400">
          <span className="h-2 w-2 rounded-full bg-emerald-500 animate-pulse" />
          <span className="font-semibold tracking-wider uppercase">Live Go Cluster</span>
        </span>
      )}

      {mode === 'fake' && (
        <span className="inline-flex items-center gap-1.5 text-amber">
          <span className="h-2 w-2 rounded-full bg-amber animate-pulse" />
          <span className="font-semibold tracking-wider uppercase">Portfolio Dev Gateway</span>
        </span>
      )}

      {mode === 'archive' && (
        <span className="inline-flex items-center gap-1.5 text-amber">
          <span className="h-2 w-2 rounded-full bg-amber animate-pulse" />
          <span className="font-semibold tracking-wider uppercase">35mm Preview Archive</span>
        </span>
      )}

      <span className="text-silver-faint hidden md:inline">Endpoint: {base}</span>

      {ready && (
        <div className="flex items-center gap-3">
          <span className={`text-[11px] ${ready.postgres === 'ok' ? 'text-emerald-400' : 'text-crimson'}`}>
            PG: {ready.postgres}
          </span>
          <span className={`text-[11px] ${ready.redis === 'ok' ? 'text-emerald-400' : 'text-amber'}`}>
            Redis: {ready.redis}
          </span>
        </div>
      )}

      {mode === 'archive' && (
        <span className="text-silver-dim text-[11px] ml-auto">
          Sample films ready for evaluation • Connects seamlessly to live cluster via <code className="text-amber">VITE_API_BASE</code>
        </span>
      )}

      {mode === 'fake' && (
        <span className="text-amber/80 text-[11px] ml-auto hidden lg:inline">
          HMAC + Redis SETNX + double-entry escrow active (paise integers)
        </span>
      )}

      {mode === 'live' && (
        <span className="text-emerald-300/80 text-[11px] ml-auto hidden lg:inline">
          PostgreSQL Outbox + SKIP LOCKED transaction pooling live
        </span>
      )}
    </div>
  )
}
