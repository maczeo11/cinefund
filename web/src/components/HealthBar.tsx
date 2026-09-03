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
    fetch(healthUrl)
      .then(r => r.json().then(j => ({ ok: r.ok, j })))
      .then(({ ok, j }) => {
        setReady(j as Ready)
        setMode(ok && (j as Ready)?.postgres === 'ok' ? 'live' : 'archive')
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
          <span className="font-semibold tracking-wider uppercase">Live AWS EC2 Cluster</span>
        </span>
      )}

      {mode === 'archive' && (
        <span className="inline-flex items-center gap-1.5 text-amber">
          <span className="h-2 w-2 rounded-full bg-amber animate-pulse" />
          <span className="font-semibold tracking-wider uppercase">Cluster Connecting...</span>
        </span>
      )}

      {ready && (
        <div className="flex items-center gap-3">
          <span className={`text-[11px] ${ready.postgres === 'ok' ? 'text-emerald-400' : 'text-crimson'}`}>
            Neon PG: {ready.postgres}
          </span>
          <span className={`text-[11px] ${ready.redis === 'ok' ? 'text-emerald-400' : 'text-amber'}`}>
            Redis: {ready.redis}
          </span>
        </div>
      )}

      {mode === 'live' && (
        <span className="text-emerald-300/90 text-[11px] ml-auto hidden sm:inline">
          Live Go Backend • Dual-Entry Escrow Ledger & Outbox Active
        </span>
      )}

      {mode === 'archive' && (
        <span className="text-silver-dim text-[11px] ml-auto">
          Sample films ready • Connecting to AWS EC2 cluster
        </span>
      )}
    </div>
  )
}
