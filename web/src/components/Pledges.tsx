import { useEffect, useState } from 'react'
import { rupees } from '../format'

type LedgerEntry = { id: string; pledge_id: string; account: string; type: 'DEBIT' | 'CREDIT'; amount: number; created_at: string }

export default function Pledges() {
  const [entries, setEntries] = useState<LedgerEntry[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    // Demo: try to fetch ledger if endpoint exists, else show editorial placeholder
    const base = (import.meta.env.VITE_API_BASE as string) || 'http://localhost:8080/api/v1'
    fetch(`${base}/ledger`)
      .then(r => (r.ok ? r.json() : []))
      .then(d => setEntries(Array.isArray(d) ? d : []))
      .catch(() => setEntries([]))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <p className="status">Loading ledger…</p>

  return (
    <div className="max-w-3xl">
      <h1 className="font-serif text-3xl mb-2">Your pledges</h1>
      <p className="text-sm text-white/50 mb-6">Double-entry: every CAPTURED pledge writes DEBIT/CREDIT pair in same Postgres transaction — deferred trigger ensures balance.</p>

      {entries.length === 0 ? (
        <div className="tw-card text-center py-12">
          <p className="label mb-2">No ledger entries yet</p>
          <p className="text-sm text-white/50">Back a film from the index — `POST /campaigns/:id/pledges` → `confirm` → ledger pair appears here.</p>
          <p className="mt-4 text-xs text-white/30">POST /webhooks/razorpay HMAC → Redis SETNX → Postgres unique → ledger</p>
        </div>
      ) : (
        <ul className="space-y-2">
          {entries.slice(0, 20).map(e => (
            <li key={e.id} className="flex justify-between items-baseline border-b border-white/10 py-3 font-mono text-sm">
              <span className={e.type === 'DEBIT' ? 'text-accent' : 'text-green-400'}>{e.type}</span>
              <span className="num">{rupees(e.amount)}</span>
              <span className="text-white/30 text-xs">{e.account}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
