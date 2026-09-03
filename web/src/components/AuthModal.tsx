import { useState, useEffect } from 'react'

export type UserProfile = {
  id: string
  name: string
  email: string
  role: 'CREATOR' | 'BACKER' | 'ADMIN'
  avatar: string
  tag: string
}

export const DEMO_USERS: UserProfile[] = [
  {
    id: '00000000-0000-0000-0000-000000000002',
    name: 'Ravi',
    email: 'backer@cinefund.dev',
    role: 'BACKER',
    avatar: 'R',
    tag: 'Film Patron & Tier Collector',
  },
  {
    id: '00000000-0000-0000-0000-000000000001',
    name: 'Ava',
    email: 'creator@cinefund.dev',
    role: 'CREATOR',
    avatar: 'A',
    tag: 'Independent 35mm Director',
  },
]

const STORAGE_KEY = 'cinefund_current_user'

export function getActiveUser(): UserProfile | null {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'null') return null
    if (saved) return JSON.parse(saved)
  } catch {
    // fallback
  }
  return DEMO_USERS[0]
}

export function setActiveUser(user: UserProfile | null) {
  if (user === null) {
    localStorage.setItem(STORAGE_KEY, 'null')
  } else {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(user))
  }
  window.dispatchEvent(new Event('cinefund_auth_change'))
}

export function logout() {
  setActiveUser(null)
}

type Props = {
  isOpen: boolean
  onClose: () => void
}

export default function AuthModal({ isOpen, onClose }: Props) {
  const [current, setCurrent] = useState<UserProfile>(getActiveUser())
  const [tab, setTab] = useState<'switch' | 'session'>('switch')

  useEffect(() => {
    setCurrent(getActiveUser())
  }, [isOpen])

  if (!isOpen) return null

  function handleSelect(user: UserProfile) {
    setCurrent(user)
    setActiveUser(user)
    onClose()
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-md animate-fadeIn">
      <div className="relative w-full max-w-md bg-celluloid border border-white/10 rounded-2xl p-6 shadow-2xl overflow-hidden">
        {/* Cinema film frame markers */}
        <div className="flex items-center justify-between pb-4 border-b border-white/10 mb-5">
          <div className="flex items-center gap-2">
            <span className="h-2 w-2 rounded-full bg-amber animate-pulse" />
            <span className="font-display font-semibold tracking-wider text-xs uppercase text-silver">
              Cinema Identity & Session
            </span>
          </div>
          <button
            onClick={onClose}
            className="text-silver-dim hover:text-white transition-colors text-sm px-2 py-1 rounded bg-white/[0.05]"
          >
            ✕
          </button>
        </div>

        {/* Tab switch */}
        <div className="flex gap-2 p-1 bg-black/40 rounded-lg mb-5 text-xs font-mono">
          <button
            onClick={() => setTab('switch')}
            className={`flex-1 py-1.5 rounded-md transition-colors ${
              tab === 'switch' ? 'bg-amber text-ink font-semibold' : 'text-silver-dim hover:text-white'
            }`}
          >
            Switch Role
          </button>
          <button
            onClick={() => setTab('session')}
            className={`flex-1 py-1.5 rounded-md transition-colors ${
              tab === 'session' ? 'bg-amber text-ink font-semibold' : 'text-silver-dim hover:text-white'
            }`}
          >
            Security Architecture
          </button>
        </div>

        {tab === 'switch' ? (
          <div>
            <p className="text-xs text-silver-dim mb-4 leading-relaxed">
              Select a seeded profile to test CineFund as either an indie filmmaker launching campaigns or a backer pledging escrow funds.
            </p>
            <div className="space-y-3">
              {DEMO_USERS.map(user => {
                const isActive = current.id === user.id
                return (
                  <button
                    key={user.id}
                    onClick={() => handleSelect(user)}
                    className={`w-full text-left p-3.5 rounded-xl border transition-all flex items-center gap-4 ${
                      isActive
                        ? 'border-amber/60 bg-amber/10 shadow-[0_0_15px_rgba(229,169,60,0.15)]'
                        : 'border-white/10 bg-white/[0.02] hover:border-white/20 hover:bg-white/[0.04]'
                    }`}
                  >
                    <div
                      className={`h-11 w-11 rounded-full flex items-center justify-center font-cinema text-lg font-bold ${
                        isActive ? 'bg-amber text-ink' : 'bg-white/10 text-silver'
                      }`}
                    >
                      {user.avatar}
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-semibold text-silver text-sm">{user.name}</span>
                        <span
                          className={`text-[10px] px-1.5 py-0.5 rounded font-mono font-medium ${
                            user.role === 'CREATOR'
                              ? 'bg-amber/20 text-amber border border-amber/30'
                              : 'bg-white/10 text-silver-dim border border-white/10'
                          }`}
                        >
                          {user.role}
                        </span>
                      </div>
                      <p className="text-xs text-silver-dim truncate mt-0.5">{user.tag}</p>
                      <p className="text-[11px] font-mono text-silver-faint truncate">{user.email}</p>
                    </div>
                    {isActive && (
                      <span className="text-amber text-xs font-mono font-bold">Active</span>
                    )}
                  </button>
                )
              })}
            </div>

            {current && (
              <button
                onClick={() => {
                  logout()
                  onClose()
                }}
                className="w-full mt-4 py-2.5 rounded-xl border border-crimson/30 bg-crimson/10 hover:bg-crimson/20 text-crimson text-xs font-mono font-medium transition-all"
              >
                Log Out of Current Session
              </button>
            )}
          </div>
        ) : (
          <div className="space-y-3 text-xs">
            <div className="p-3 bg-black/40 border border-white/10 rounded-xl">
              <span className="cinema-tag text-[10px] text-amber">PostgreSQL Migration 0003</span>
              <h4 className="font-semibold text-silver mt-1 mb-1">Refresh Token Family Rotation</h4>
              <p className="text-silver-dim leading-relaxed">
                Tokens are stored in PostgreSQL <code className="text-amber">refresh_token_families</code>. Rotation uses atomic CAS on <code className="text-amber">current_jti</code>. If an old token is reused, the entire family is burned.
              </p>
            </div>
            <div className="p-3 bg-black/40 border border-white/10 rounded-xl">
              <span className="cinema-tag text-[10px] text-amber">Go Backend Config</span>
              <h4 className="font-semibold text-silver mt-1 mb-1">Dual 32-Byte Cryptographic Secrets</h4>
              <p className="text-silver-dim leading-relaxed">
                Enforced by <code className="text-amber">config.go:Validate()</code>: <code className="text-white">JWT_ACCESS_SECRET</code> (15m expiry) and <code className="text-white">JWT_REFRESH_SECRET</code> (720h HTTP-only cookie).
              </p>
            </div>
          </div>
        )}

        <div className="mt-5 pt-4 border-t border-white/10 flex justify-end">
          <button
            onClick={onClose}
            className="px-4 py-2 rounded-lg bg-white/10 text-silver hover:bg-white/20 text-xs font-medium transition-colors"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}
