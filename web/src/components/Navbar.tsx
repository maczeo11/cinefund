import { useState, useEffect } from 'react'
import AuthModal, { getActiveUser, logout, type UserProfile } from './AuthModal.tsx'

type Props = {
  onHome: () => void
  onCreate: () => void
}

export default function Navbar({ onHome, onCreate }: Props) {
  const [authOpen, setAuthOpen] = useState(false)
  const [user, setUser] = useState<UserProfile | null>(getActiveUser())

  useEffect(() => {
    const handleAuthChange = () => setUser(getActiveUser())
    window.addEventListener('cinefund_auth_change', handleAuthChange)
    return () => window.removeEventListener('cinefund_auth_change', handleAuthChange)
  }, [])

  return (
    <>
      <header className="sticky top-0 z-40 bg-obsidian/95 backdrop-blur-md border-b border-white/[0.08] px-[var(--gutter)] py-3.5 flex items-center justify-between">
        {/* Brand */}
        <button
          className="flex items-center gap-2.5 text-left group transition-transform active:scale-95"
          onClick={onHome}
          aria-label="CineFund Home"
        >
          <div className="h-8 w-8 rounded-lg bg-gradient-to-br from-amber to-amber-bright/80 flex items-center justify-center text-ink font-cinema font-bold text-sm shadow-[0_0_12px_rgba(229,169,60,0.3)]">
            35
          </div>
          <div>
            <span className="font-cinema font-black tracking-widest text-lg text-silver group-hover:text-amber transition-colors">
              CINE<span className="text-amber">FUND</span>
            </span>
            <span className="hidden sm:block text-[9px] font-mono tracking-widest text-silver-dim uppercase">
              35mm Indie Crowdfunding & HLS Streaming
            </span>
          </div>
        </button>

        {/* Navigation & User Menu */}
        <nav className="flex items-center gap-3 sm:gap-5">
          <button
            className="text-xs font-mono tracking-wider uppercase text-silver-dim hover:text-white transition-colors px-2 py-1"
            onClick={onHome}
          >
            Vault
          </button>

          <button
            className="text-xs font-medium px-3.5 py-1.5 rounded-lg bg-amber hover:bg-amber-bright text-ink font-semibold shadow-[0_0_15px_rgba(229,169,60,0.2)] hover:shadow-[0_0_20px_rgba(229,169,60,0.4)] transition-all active:scale-95"
            onClick={onCreate}
          >
            + Submit Film
          </button>

          {/* User Auth Switcher & Log Out Buttons */}
          {user ? (
            <div className="flex items-center gap-2">
              <button
                onClick={() => setAuthOpen(true)}
                className="flex items-center gap-2 px-2.5 py-1.5 rounded-lg border border-white/10 bg-white/[0.04] hover:bg-white/[0.08] hover:border-amber/40 transition-all text-xs"
                title="Switch User Role"
              >
                <span className="h-5 w-5 rounded-full bg-amber/20 text-amber font-mono font-bold text-[11px] flex items-center justify-center">
                  {user.avatar}
                </span>
                <span className="hidden sm:inline font-medium text-silver">{user.name}</span>
                <span className="text-[9px] font-mono px-1 py-0.5 rounded bg-white/10 text-silver-dim hidden md:inline">
                  {user.role}
                </span>
              </button>

              <button
                onClick={() => {
                  logout()
                }}
                className="text-xs font-mono text-silver-dim hover:text-crimson px-2.5 py-1.5 rounded-lg border border-white/10 hover:border-crimson/30 hover:bg-crimson/10 transition-all"
                title="Log Out"
              >
                Log Out
              </button>
            </div>
          ) : (
            <button
              onClick={() => setAuthOpen(true)}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-amber/40 bg-amber/10 hover:bg-amber/20 text-amber text-xs font-medium font-mono transition-all active:scale-95 shadow-[0_0_12px_rgba(229,169,60,0.15)]"
            >
              <span>Log In</span>
            </button>
          )}
        </nav>
      </header>

      <AuthModal isOpen={authOpen} onClose={() => setAuthOpen(false)} />
    </>
  )
}
