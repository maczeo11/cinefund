import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import AuthModal, { getActiveUser, logout, type UserProfile } from './AuthModal.tsx'
import { getUserPledges } from '../api'

type Props = {
  onHome: () => void
  onCreate: () => void
}

export default function Navbar({ onHome, onCreate }: Props) {
  const navigate = useNavigate()
  const [authOpen, setAuthOpen] = useState(false)
  const [authTab, setAuthTab] = useState<'signin' | 'signup' | 'architecture'>('signin')
  const [dropdownOpen, setDropdownOpen] = useState(false)
  const [user, setUser] = useState<UserProfile | null>(getActiveUser())
  const [pledgeCount, setPledgeCount] = useState<number>(0)
  const dropdownRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const handleAuthChange = () => {
      const active = getActiveUser()
      setUser(active)
      if (active) {
        setPledgeCount(getUserPledges(active.id).length)
      } else {
        setPledgeCount(0)
      }
    }

    const handlePledgeChange = () => {
      const active = getActiveUser()
      if (active) {
        setPledgeCount(getUserPledges(active.id).length)
      }
    }

    handleAuthChange()
    window.addEventListener('cinefund_auth_change', handleAuthChange)
    window.addEventListener('cinefund_pledge_added', handlePledgeChange)
    return () => {
      window.removeEventListener('cinefund_auth_change', handleAuthChange)
      window.removeEventListener('cinefund_pledge_added', handlePledgeChange)
    }
  }, [])

  // Close dropdown on click outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setDropdownOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  function handleSignOut() {
    logout()
    setDropdownOpen(false)
  }

  function openAuth(tab: 'signin' | 'signup' | 'architecture' = 'signin') {
    setAuthTab(tab)
    setAuthOpen(true)
    setDropdownOpen(false)
  }

  return (
    <>
      <header className="sticky top-0 z-40 bg-obsidian/95 backdrop-blur-md border-b border-white/[0.08] px-[var(--gutter)] py-3 flex items-center justify-between">
        {/* Brand Logo */}
        <button
          className="flex items-center gap-2.5 text-left group transition-transform active:scale-95"
          onClick={onHome}
          aria-label="CineFund Home"
        >
          <div className="h-8 w-8 rounded-lg bg-gradient-to-br from-amber via-amber-bright to-amber/80 flex items-center justify-center text-ink font-cinema font-bold text-sm shadow-[0_0_15px_rgba(229,169,60,0.35)] group-hover:scale-105 transition-transform">
            35
          </div>
          <div>
            <div className="flex items-center gap-1.5">
              <span className="font-cinema font-black tracking-widest text-lg text-silver group-hover:text-amber transition-colors">
                CINE<span className="text-amber">FUND</span>
              </span>
              <span className="text-[9px] font-mono px-1.5 py-0.2 rounded bg-amber/15 text-amber border border-amber/30 hidden sm:inline-block">
                35MM
              </span>
            </div>
            <span className="hidden sm:block text-[9px] font-mono tracking-widest text-silver-dim uppercase">
              Celluloid Crowdfunding & HLS Streaming
            </span>
          </div>
        </button>

        {/* Navigation & User Menu */}
        <nav className="flex items-center gap-3 sm:gap-4">
          <button
            className="text-xs font-mono tracking-wider uppercase text-silver-dim hover:text-white transition-colors px-2 py-1 hidden md:block"
            onClick={onHome}
          >
            Vault
          </button>

          <button
            className="text-xs font-mono tracking-wider uppercase text-silver-dim hover:text-white transition-colors px-2 py-1 hidden lg:block"
            onClick={() => navigate('/watch')}
          >
            Screening Room
          </button>

          <button
            className="text-xs font-semibold px-3 sm:px-3.5 py-1.5 rounded-lg bg-amber hover:bg-amber-bright text-ink shadow-[0_0_15px_rgba(229,169,60,0.2)] hover:shadow-[0_0_20px_rgba(229,169,60,0.4)] transition-all active:scale-95 flex items-center gap-1.5"
            onClick={onCreate}
          >
            <span>+</span>
            <span className="hidden sm:inline">Submit Film</span>
            <span className="sm:hidden">Submit</span>
          </button>

          {/* User Auth Dropdown */}
          {user ? (
            <div className="relative" ref={dropdownRef}>
              <button
                onClick={() => setDropdownOpen(o => !o)}
                className={`flex items-center gap-2.5 px-2.5 py-1.5 rounded-xl border transition-all text-xs select-none ${
                  dropdownOpen
                    ? 'border-amber bg-amber/10 shadow-[0_0_15px_rgba(229,169,60,0.25)]'
                    : 'border-white/10 bg-white/[0.04] hover:bg-white/[0.08] hover:border-amber/40'
                }`}
                title="Account Menu"
                aria-expanded={dropdownOpen}
              >
                {/* User Avatar */}
                <div className="h-6 w-6 rounded-full bg-gradient-to-tr from-amber to-amber-bright text-ink font-cinema font-bold text-xs flex items-center justify-center shadow-inner overflow-hidden flex-shrink-0">
                  {user.photoURL || (user.avatar && user.avatar.startsWith('http')) ? (
                    <img src={user.photoURL || user.avatar} alt={user.name} className="w-full h-full object-cover" />
                  ) : (
                    user.avatar
                  )}
                </div>

                {/* Name */}
                <span className="hidden sm:inline font-medium text-silver max-w-[120px] truncate">
                  {user.name}
                </span>

                {/* Role Badge */}
                <span
                  className={`text-[9px] font-mono font-bold px-1.5 py-0.5 rounded uppercase tracking-wider hidden md:inline-block ${
                    user.role === 'CREATOR'
                      ? 'bg-amber/20 text-amber border border-amber/30'
                      : 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/30'
                  }`}
                >
                  {user.role === 'CREATOR' ? 'Director' : 'Patron'}
                </span>

                {/* Dropdown Chevron */}
                <svg
                  className={`w-3.5 h-3.5 text-silver-dim transition-transform duration-200 ${dropdownOpen ? 'rotate-180 text-amber' : ''}`}
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                </svg>
              </button>

              {/* Polished Cinema Dropdown Menu */}
              {dropdownOpen && (
                <div className="absolute right-0 top-full mt-2.5 w-72 bg-celluloid border border-white/[0.12] rounded-2xl shadow-[0_20px_50px_rgba(0,0,0,0.85)] backdrop-blur-xl p-3 z-50 animate-fadeIn space-y-3">
                  {/* User Profile Card */}
                  <div className="p-3 rounded-xl bg-black/40 border border-white/[0.08] space-y-2">
                    <div className="flex items-center gap-3">
                      <div className="h-10 w-10 rounded-xl bg-gradient-to-tr from-amber to-amber-bright text-ink font-cinema font-bold text-base flex items-center justify-center shadow-[0_0_12px_rgba(229,169,60,0.3)] overflow-hidden flex-shrink-0">
                        {user.photoURL || (user.avatar && user.avatar.startsWith('http')) ? (
                          <img src={user.photoURL || user.avatar} alt={user.name} className="w-full h-full object-cover" />
                        ) : (
                          user.avatar
                        )}
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <h4 className="font-bold text-silver text-sm truncate">{user.name}</h4>
                          <span
                            className={`text-[9px] font-mono font-bold px-1.5 py-0.2 rounded uppercase ${
                              user.role === 'CREATOR'
                                ? 'bg-amber/20 text-amber border border-amber/30'
                                : 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/30'
                            }`}
                          >
                            {user.role}
                          </span>
                        </div>
                        <p className="text-[11px] font-mono text-silver-dim truncate mt-0.5">{user.email}</p>
                      </div>
                    </div>
                    <p className="text-[11px] text-silver-faint font-sans border-t border-white/[0.06] pt-1.5 truncate">
                      {user.tag}
                    </p>
                  </div>

                  {/* Menu Links */}
                  <div className="space-y-1 font-mono text-xs">
                    <button
                      onClick={() => {
                        navigate('/pledges')
                        setDropdownOpen(false)
                      }}
                      className="w-full flex items-center justify-between px-3 py-2 rounded-lg text-silver hover:text-white hover:bg-white/[0.06] transition-colors group"
                    >
                      <span className="flex items-center gap-2.5">
                        <span className="text-sm">🎟️</span>
                        <span>My Pledges & Ledger</span>
                      </span>
                      {pledgeCount > 0 && (
                        <span className="px-1.5 py-0.2 rounded-full bg-amber/20 text-amber border border-amber/30 text-[10px] font-bold">
                          {pledgeCount}
                        </span>
                      )}
                    </button>

                    <button
                      onClick={() => {
                        onCreate()
                        setDropdownOpen(false)
                      }}
                      className="w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-silver hover:text-white hover:bg-white/[0.06] transition-colors"
                    >
                      <span className="text-sm">🎬</span>
                      <span>Submit Film Reel</span>
                    </button>

                    <button
                      onClick={() => {
                        navigate('/watch')
                        setDropdownOpen(false)
                      }}
                      className="w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-silver hover:text-white hover:bg-white/[0.06] transition-colors"
                    >
                      <span className="text-sm">📽️</span>
                      <span>Director Screening Room</span>
                    </button>

                    <button
                      onClick={() => openAuth('signin')}
                      className="w-full flex items-center justify-between px-3 py-2 rounded-lg text-amber hover:bg-amber/10 transition-colors border border-amber/20 mt-1"
                    >
                      <span className="flex items-center gap-2">
                        <span>⚡</span>
                        <span>Switch Role / Quick Demo</span>
                      </span>
                      <span className="text-[10px]">→</span>
                    </button>
                  </div>

                  {/* Sign Out Action */}
                  <div className="pt-2 border-t border-white/[0.08]">
                    <button
                      onClick={handleSignOut}
                      className="w-full py-2 px-3 rounded-lg border border-crimson/30 bg-crimson/10 hover:bg-crimson/20 text-crimson text-xs font-mono font-semibold transition-all flex items-center justify-center gap-2"
                    >
                      <span>Sign Out</span>
                    </button>
                  </div>
                </div>
              )}
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <button
                onClick={() => openAuth('signup')}
                className="hidden sm:inline-flex items-center gap-1 px-3 py-1.5 rounded-lg border border-white/10 bg-white/[0.04] hover:bg-white/[0.08] text-silver text-xs font-mono transition-all"
              >
                Create Account
              </button>
              <button
                onClick={() => openAuth('signin')}
                className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-lg border border-amber/40 bg-amber/10 hover:bg-amber/20 text-amber text-xs font-mono font-medium transition-all active:scale-95 shadow-[0_0_15px_rgba(229,169,60,0.15)] hover:shadow-[0_0_20px_rgba(229,169,60,0.3)]"
              >
                <span>🔑</span>
                <span>Sign In</span>
              </button>
            </div>
          )}
        </nav>
      </header>

      <AuthModal
        isOpen={authOpen}
        onClose={() => setAuthOpen(false)}
        initialTab={authTab}
      />
    </>
  )
}
