import { useState, useEffect, useRef } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import AuthModal, { getActiveUser, logout, type UserProfile } from './AuthModal.tsx'
import { getUserPledges } from '../api'

type Props = {
  onHome: () => void
  onCreate: () => void
}

export default function Navbar({ onHome, onCreate }: Props) {
  const navigate = useNavigate()
  const location = useLocation()
  const [authOpen, setAuthOpen] = useState(false)
  const [authTab, setAuthTab] = useState<'signin' | 'signup'>('signin')
  const [dropdownOpen, setDropdownOpen] = useState(false)
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
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
    setMobileMenuOpen(false)
  }

  function openAuth(tab: 'signin' | 'signup' = 'signin') {
    setAuthTab(tab)
    setAuthOpen(true)
    setDropdownOpen(false)
    setMobileMenuOpen(false)
  }

  function handleCreateClick() {
    if (!user) {
      openAuth('signin')
      return
    }
    onCreate()
  }

  const navLinks = [
    { label: 'Films', path: '/' },
    { label: 'Screening Room', path: '/watch' },
    { label: 'Pledges & Escrow', path: '/pledges' },
    { label: 'Telemetry', path: '/dashboard' },
  ]

  return (
    <>
      <header className="sticky top-0 z-40 bg-[#0C0E14]/95 backdrop-blur-md border-b border-white/[0.08] px-4 sm:px-6 py-3 flex items-center justify-between">
        {/* Left: Brand Logo & Main Nav */}
        <div className="flex items-center gap-6 sm:gap-8">
          <button
            className="flex items-center gap-2.5 text-left group transition-transform active:scale-95 cursor-pointer"
            onClick={onHome}
            aria-label="CineFund Home"
          >
            <div className="h-8 w-8 rounded-lg bg-gradient-to-br from-amber via-amber-bright to-amber/80 flex items-center justify-center text-ink font-cinema font-bold text-sm shadow-[0_0_12px_rgba(229,169,60,0.3)] group-hover:scale-105 transition-transform">
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

          {/* Desktop Navigation Links */}
          <nav className="hidden md:flex items-center gap-1">
            {navLinks.map(link => {
              const isActive =
                location.pathname === link.path ||
                (link.path === '/' && location.pathname.startsWith('/campaigns/'))
              return (
                <button
                  key={link.path}
                  onClick={() => navigate(link.path)}
                  className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-all cursor-pointer ${
                    isActive
                      ? 'bg-amber/15 text-amber font-semibold shadow-[0_0_10px_rgba(229,169,60,0.15)]'
                      : 'text-silver-dim hover:text-silver hover:bg-white/[0.04]'
                  }`}
                >
                  {link.label}
                </button>
              )
            })}
          </nav>
        </div>

        {/* Right: Actions & User Menu */}
        <div className="flex items-center gap-3">
          <button
            onClick={handleCreateClick}
            className="text-xs font-semibold px-3.5 py-1.5 rounded-lg bg-amber hover:bg-amber-bright text-ink shadow-[0_0_12px_rgba(229,169,60,0.2)] hover:shadow-[0_0_16px_rgba(229,169,60,0.35)] transition-all active:scale-95 flex items-center gap-1.5 cursor-pointer"
          >
            <span>+</span>
            <span>Submit Film</span>
          </button>

          {/* User Section */}
          {user ? (
            <div className="relative" ref={dropdownRef}>
              <button
                onClick={() => setDropdownOpen(o => !o)}
                className={`flex items-center gap-2.5 px-2.5 py-1.5 rounded-xl border transition-all text-xs select-none cursor-pointer ${
                  dropdownOpen
                    ? 'border-amber bg-amber/10 shadow-[0_0_15px_rgba(229,169,60,0.2)]'
                    : 'border-white/10 bg-white/[0.04] hover:bg-white/[0.08] hover:border-amber/40'
                }`}
                title="Account Menu"
                aria-expanded={dropdownOpen}
              >
                {/* User Avatar */}
                <div className="h-6 w-6 rounded-full bg-amber text-ink font-cinema font-bold text-xs flex items-center justify-center overflow-hidden flex-shrink-0">
                  {user.photoURL || (user.avatar && user.avatar.startsWith('http')) ? (
                    <img src={user.photoURL || user.avatar} alt={user.name} className="w-full h-full object-cover" />
                  ) : (
                    user.avatar
                  )}
                </div>

                <span className="hidden sm:inline font-medium text-silver max-w-[120px] truncate">
                  {user.name}
                </span>

                <span
                  className={`text-[9px] font-mono font-bold px-1.5 py-0.5 rounded uppercase tracking-wider hidden md:inline-block ${
                    user.role === 'CREATOR'
                      ? 'bg-amber/20 text-amber border border-amber/30'
                      : 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/30'
                  }`}
                >
                  {user.role === 'CREATOR' ? 'Director' : 'Patron'}
                </span>

                {/* Chevron */}
                <svg
                  className={`w-3.5 h-3.5 text-silver-dim transition-transform duration-200 ${dropdownOpen ? 'rotate-180 text-amber' : ''}`}
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
                </svg>
              </button>

              {/* Account Dropdown Menu */}
              {dropdownOpen && (
                <div className="absolute right-0 top-full mt-2 w-72 bg-[#12151E] border border-white/[0.12] rounded-2xl shadow-2xl backdrop-blur-xl p-3 z-50 animate-fadeIn space-y-2.5">
                  {/* User Profile Card */}
                  <div className="p-3 rounded-xl bg-black/40 border border-white/[0.08] space-y-1.5">
                    <div className="flex items-center gap-2.5">
                      <div className="h-9 w-9 rounded-xl bg-amber text-ink font-cinema font-bold text-sm flex items-center justify-center overflow-hidden flex-shrink-0">
                        {user.photoURL || (user.avatar && user.avatar.startsWith('http')) ? (
                          <img src={user.photoURL || user.avatar} alt={user.name} className="w-full h-full object-cover" />
                        ) : (
                          user.avatar
                        )}
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                          <h4 className="font-bold text-silver text-xs truncate">{user.name}</h4>
                          <span className="text-[9px] font-mono px-1 py-0.2 rounded bg-amber/20 text-amber">
                            {user.role}
                          </span>
                        </div>
                        <p className="text-[11px] font-mono text-silver-dim truncate">{user.email}</p>
                      </div>
                    </div>
                  </div>

                  {/* Links */}
                  <div className="space-y-1 text-xs font-medium">
                    <button
                      onClick={() => {
                        navigate('/pledges')
                        setDropdownOpen(false)
                      }}
                      className="w-full flex items-center justify-between px-3 py-2 rounded-lg text-silver hover:text-white hover:bg-white/[0.06] transition-colors cursor-pointer"
                    >
                      <span className="flex items-center gap-2">
                        <span>🎟️</span>
                        <span>My Pledges</span>
                      </span>
                      {pledgeCount > 0 && (
                        <span className="px-1.5 py-0.2 rounded-full bg-amber/20 text-amber text-[10px] font-bold">
                          {pledgeCount}
                        </span>
                      )}
                    </button>

                    <button
                      onClick={() => {
                        onCreate()
                        setDropdownOpen(false)
                      }}
                      className="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-silver hover:text-white hover:bg-white/[0.06] transition-colors cursor-pointer"
                    >
                      <span>🎬</span>
                      <span>Submit Film</span>
                    </button>

                    <button
                      onClick={() => {
                        navigate('/watch')
                        setDropdownOpen(false)
                      }}
                      className="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-silver hover:text-white hover:bg-white/[0.06] transition-colors cursor-pointer"
                    >
                      <span>📽️</span>
                      <span>Screening Room</span>
                    </button>

                    <button
                      onClick={() => openAuth('signin')}
                      className="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-silver-dim hover:text-amber hover:bg-white/[0.06] transition-colors cursor-pointer"
                    >
                      <span>⚡</span>
                      <span>Switch Account / Demo</span>
                    </button>
                  </div>

                  {/* Sign Out */}
                  <div className="pt-2 border-t border-white/[0.08]">
                    <button
                      onClick={handleSignOut}
                      className="w-full py-2 px-3 rounded-lg border border-crimson/30 bg-crimson/10 hover:bg-crimson/20 text-crimson text-xs font-medium transition-all flex items-center justify-center gap-1.5 cursor-pointer"
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
                onClick={() => openAuth('signin')}
                className="px-3.5 py-1.5 rounded-lg border border-white/15 hover:border-amber/40 bg-white/[0.04] hover:bg-white/[0.08] text-silver hover:text-white text-xs font-medium transition-all cursor-pointer"
              >
                Sign In
              </button>
              <button
                onClick={() => openAuth('signup')}
                className="hidden sm:inline-flex px-3.5 py-1.5 rounded-lg bg-amber hover:bg-amber-bright text-ink text-xs font-semibold transition-all shadow-sm cursor-pointer"
              >
                Get Started
              </button>
            </div>
          )}

          {/* Mobile Menu Toggle Button */}
          <button
            onClick={() => setMobileMenuOpen(o => !o)}
            className="md:hidden p-1.5 rounded-lg text-silver-dim hover:text-white hover:bg-white/[0.05]"
            aria-label="Toggle Navigation"
          >
            <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              {mobileMenuOpen ? (
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              ) : (
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
              )}
            </svg>
          </button>
        </div>
      </header>

      {/* Mobile Navigation Drawer */}
      {mobileMenuOpen && (
        <div className="md:hidden bg-[#0C0E14] border-b border-white/10 px-4 py-3 space-y-1 animate-fadeIn">
          {navLinks.map(link => (
            <button
              key={link.path}
              onClick={() => {
                navigate(link.path)
                setMobileMenuOpen(false)
              }}
              className={`w-full text-left px-3 py-2 rounded-lg text-xs font-medium transition-colors ${
                location.pathname === link.path
                  ? 'bg-amber/15 text-amber font-semibold'
                  : 'text-silver-dim hover:text-white'
              }`}
            >
              {link.label}
            </button>
          ))}
        </div>
      )}

      <AuthModal
        isOpen={authOpen}
        onClose={() => setAuthOpen(false)}
        initialTab={authTab}
      />
    </>
  )
}
