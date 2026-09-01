import { useEffect, useState } from 'react'
import { getApiBase, getConfig } from '../api'

type Ready = { postgres: string; redis: string }
type Mode = 'live' | 'fake' | 'offline'

export default function HealthBar() {
  const [ready, setReady] = useState<Ready | null>(null)
  const [ok, setOk] = useState<boolean | null>(null)
  const [mode, setMode] = useState<Mode>('offline')
  const base = getApiBase()

  useEffect(() => {
    // Live backend only — hit /health/ready + /config to detect fake gateway (empty razorpay_key_id)
    const healthUrl = `${base.replace('/api/v1', '')}/health/ready`
    Promise.all([
      fetch(healthUrl).then(r => r.json().then(j => ({ ok: r.ok, j }))),
      getConfig().catch(() => ({ razorpay_key_id: '' } as { razorpay_key_id: string })),
    ])
      .then(([{ ok, j }, cfg]) => {
        setOk(ok)
        setReady(j as Ready)
        // fake gateway when no Razorpay creds (dev mode) — see config.go:UseFakeGateway()
        setMode(!ok ? 'offline' : (cfg as { razorpay_key_id?: string }).razorpay_key_id ? 'live' : 'fake')
      })
      .catch(() => {
        setOk(false)
        setMode('offline')
      })
  }, [base])

  if (ok === null) return null

  const pill =
    mode === 'live'
      ? 'text-green-400 bg-green-500'
      : mode === 'fake'
        ? 'text-amber-300 bg-amber-500'
        : 'text-red-400 bg-red-500'

  const label = mode === 'live' ? 'LIVE backend' : mode === 'fake' ? 'FAKE gateway (dev)' : 'OFFLINE — backend not reachable'

  return (
    <div className="w-full border-b border-white/10 bg-white/[0.02] px-[var(--gutter)] py-2 flex flex-wrap gap-4 text-xs items-center">
      <span className={`inline-flex items-center gap-1.5 ${pill.split(' ')[0]}`}>
        <span className={`h-2 w-2 rounded-full ${pill.split(' ')[1]} animate-pulse`} /> {label}
      </span>
      <span className="text-white/30 hidden md:inline">→ {base}</span>
      {ready && (
        <>
          <span className={`text-white/40 ${ready.postgres === 'ok' ? '' : 'text-red-400'}`}>postgres: {ready.postgres}</span>
          <span className={`text-white/40 ${ready.redis === 'ok' ? '' : 'text-amber-400'}`}>redis: {ready.redis}</span>
        </>
      )}
      {mode === 'offline' && <span className="text-red-300">Check: make up → make migrate → make run-api (:8080) or set VITE_API_BASE to live URL</span>}
      {mode === 'fake' && <span className="text-amber-200/70 hidden md:inline">POST /webhooks/razorpay → HMAC + SETNX + ledger — fake gateway auto-active when RAZORPAY_KEY_ID=""</span>}
      {mode === 'live' && <span className="text-green-200/60 hidden md:inline">POST /webhooks/razorpay → HMAC + SETNX + ledger SKIPPED LOCKED</span>}
    </div>
  )
}
