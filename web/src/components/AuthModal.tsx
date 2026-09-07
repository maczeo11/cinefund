import { useState, useEffect } from 'react'

export type UserRole = 'CREATOR' | 'BACKER' | 'ADMIN'

export type UserProfile = {
  id: string
  name: string
  email: string
  role: UserRole
  avatar: string
  tag: string
}

export const DEMO_USERS: UserProfile[] = [
  {
    id: '00000000-0000-0000-0000-000000000001',
    name: 'Ava Chen',
    email: 'creator@cinefund.dev',
    role: 'CREATOR',
    avatar: 'A',
    tag: 'Independent 35mm Director',
  },
  {
    id: '00000000-0000-0000-0000-000000000002',
    name: 'Ravi Patel',
    email: 'backer@cinefund.dev',
    role: 'BACKER',
    avatar: 'R',
    tag: 'Film Patron & Tier Collector',
  },
]

const STORAGE_KEY = 'cinefund_current_user'
const USERS_KEY = 'cinefund_registered_users'

export function getRegisteredUsers(): UserProfile[] {
  try {
    const saved = localStorage.getItem(USERS_KEY)
    if (saved) {
      const parsed: UserProfile[] = JSON.parse(saved)
      const ids = new Set(DEMO_USERS.map(u => u.id))
      const additional = parsed.filter(u => !ids.has(u.id))
      return [...DEMO_USERS, ...additional]
    }
  } catch {
    // fallback
  }
  return DEMO_USERS
}

export function saveRegisteredUser(user: UserProfile) {
  try {
    const list = getRegisteredUsers()
    const idx = list.findIndex(u => u.id === user.id || u.email.toLowerCase() === user.email.toLowerCase())
    if (idx >= 0) {
      list[idx] = user
    } else {
      list.push(user)
    }
    localStorage.setItem(USERS_KEY, JSON.stringify(list))
  } catch {
    // ignore
  }
}

export function getActiveUser(): UserProfile | null {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'null') return null
    if (saved) return JSON.parse(saved)
  } catch {
    // fallback
  }
  // Default to Ava Chen (Filmmaker) for immediate interactive demo review
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
  initialTab?: 'signin' | 'signup' | 'architecture'
}

export default function AuthModal({ isOpen, onClose, initialTab = 'signin' }: Props) {
  const [tab, setTab] = useState<'signin' | 'signup' | 'architecture'>(initialTab)
  const [current, setCurrent] = useState<UserProfile | null>(getActiveUser())

  // Sign In form fields
  const [signInEmail, setSignInEmail] = useState('')
  const [signInPassword, setSignInPassword] = useState('')
  const [signInError, setSignInError] = useState<string | null>(null)

  // Sign Up form fields
  const [signUpName, setSignUpName] = useState('')
  const [signUpEmail, setSignUpEmail] = useState('')
  const [signUpPassword, setSignUpPassword] = useState('')
  const [signUpRole, setSignUpRole] = useState<UserRole>('CREATOR')
  const [signUpError, setSignUpError] = useState<string | null>(null)

  const [showPassword, setShowPassword] = useState(false)
  const [authSuccess, setAuthSuccess] = useState<string | null>(null)

  useEffect(() => {
    if (isOpen) {
      setCurrent(getActiveUser())
      setTab(initialTab)
      setSignInError(null)
      setSignUpError(null)
      setAuthSuccess(null)
    }
  }, [isOpen, initialTab])

  // ESC key to close
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape' && isOpen) {
        onClose()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  if (!isOpen) return null

  function handleQuickDemoSelect(user: UserProfile) {
    setCurrent(user)
    setActiveUser(user)
    setAuthSuccess(`Welcome, ${user.name}! Switched to ${user.role} profile.`)
    setTimeout(() => {
      onClose()
    }, 450)
  }

  function handleSignInSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSignInError(null)

    const emailTrim = signInEmail.trim().toLowerCase()
    if (!emailTrim) {
      setSignInError('Please enter your email address.')
      return
    }
    if (!signInPassword) {
      setSignInError('Please enter your password.')
      return
    }

    const allUsers = getRegisteredUsers()
    const matched = allUsers.find(u => u.email.toLowerCase() === emailTrim)

    if (matched) {
      setCurrent(matched)
      setActiveUser(matched)
      setAuthSuccess(`Authenticated as ${matched.name}!`)
      setTimeout(() => {
        onClose()
      }, 500)
    } else {
      // Auto-provision demo account for recruiter testing with entered email
      const namePart = emailTrim.split('@')[0]
      const formattedName = namePart.charAt(0).toUpperCase() + namePart.slice(1)
      const isDirector = emailTrim.includes('director') || emailTrim.includes('creator') || emailTrim.includes('film')
      const role: UserRole = isDirector ? 'CREATOR' : 'BACKER'
      const newUser: UserProfile = {
        id: `user-${Date.now()}`,
        name: formattedName || 'Cinema Member',
        email: emailTrim,
        role,
        avatar: (formattedName[0] || 'C').toUpperCase(),
        tag: role === 'CREATOR' ? 'Independent 35mm Filmmaker' : 'Film Patron & Collector',
      }
      saveRegisteredUser(newUser)
      setCurrent(newUser)
      setActiveUser(newUser)
      setAuthSuccess(`Welcome to CineFund, ${newUser.name}!`)
      setTimeout(() => {
        onClose()
      }, 500)
    }
  }

  function handleSignUpSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSignUpError(null)

    const nameTrim = signUpName.trim()
    const emailTrim = signUpEmail.trim().toLowerCase()

    if (!nameTrim) {
      setSignUpError('Please enter your director or patron name.')
      return
    }
    if (!emailTrim || !emailTrim.includes('@')) {
      setSignUpError('Please enter a valid email address.')
      return
    }
    if (!signUpPassword || signUpPassword.length < 4) {
      setSignUpError('Password must be at least 4 characters.')
      return
    }

    const newUser: UserProfile = {
      id: `user-${Date.now()}`,
      name: nameTrim,
      email: emailTrim,
      role: signUpRole,
      avatar: nameTrim.charAt(0).toUpperCase(),
      tag: signUpRole === 'CREATOR' ? 'Independent 35mm Director' : 'Film Patron & Backer',
    }

    saveRegisteredUser(newUser)
    setCurrent(newUser)
    setActiveUser(newUser)
    setAuthSuccess(`Account created! Welcome, ${newUser.name}.`)
    setTimeout(() => {
      onClose()
    }, 550)
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-3 sm:p-4 bg-black/85 backdrop-blur-md animate-fadeIn"
      onClick={onClose}
    >
      {/* Outer film frame container */}
      <div
        className="relative w-full max-w-lg bg-celluloid border border-white/[0.12] rounded-2xl shadow-[0_25px_60px_rgba(0,0,0,0.8)] overflow-hidden flex flex-col max-h-[92vh]"
        onClick={e => e.stopPropagation()}
      >
        {/* Top 35mm Sprocket Strip */}
        <div className="bg-black/60 px-4 py-1.5 border-b border-white/[0.08] flex items-center justify-between text-[9px] font-mono text-silver-faint select-none">
          <div className="flex gap-3 items-center">
            <span className="inline-block w-2.5 h-1.5 rounded-[1px] bg-white/20 border border-white/30" />
            <span className="inline-block w-2.5 h-1.5 rounded-[1px] bg-white/20 border border-white/30" />
            <span className="inline-block w-2.5 h-1.5 rounded-[1px] bg-white/20 border border-white/30 hidden sm:inline-block" />
            <span className="text-amber tracking-widest uppercase font-semibold">CINEFUND 35MM REEL AUTH</span>
          </div>
          <div className="flex gap-3 items-center">
            <span className="text-silver-dim">KODAK 5219 VISION3</span>
            <span className="inline-block w-2.5 h-1.5 rounded-[1px] bg-white/20 border border-white/30" />
            <span className="inline-block w-2.5 h-1.5 rounded-[1px] bg-white/20 border border-white/30" />
          </div>
        </div>

        {/* Modal Header */}
        <div className="px-6 pt-5 pb-3 flex items-center justify-between border-b border-white/[0.06]">
          <div className="flex items-center gap-2.5">
            <div className="h-7 w-7 rounded-lg bg-amber/15 border border-amber/30 flex items-center justify-center text-amber font-cinema font-bold text-xs">
              35
            </div>
            <div>
              <h2 className="font-cinema font-bold text-base text-silver tracking-wide">
                Arthouse Cinema Identity
              </h2>
              <p className="text-[11px] font-mono text-silver-dim">
                Role-based access • Double-entry escrow • S3 film reels
              </p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="text-silver-dim hover:text-white transition-colors h-8 w-8 rounded-lg bg-white/[0.04] hover:bg-white/[0.08] flex items-center justify-center text-sm"
            aria-label="Close modal"
          >
            ✕
          </button>
        </div>

        {/* Navigation Tabs */}
        <div className="px-6 pt-3 pb-2 bg-black/20 border-b border-white/[0.06] flex gap-1 text-xs font-mono">
          <button
            onClick={() => setTab('signin')}
            className={`flex-1 py-2 px-3 rounded-lg transition-all text-center flex items-center justify-center gap-1.5 ${
              tab === 'signin'
                ? 'bg-amber text-ink font-bold shadow-[0_0_12px_rgba(229,169,60,0.3)]'
                : 'text-silver-dim hover:text-silver hover:bg-white/[0.04]'
            }`}
          >
            <span>Sign In</span>
          </button>

          <button
            onClick={() => setTab('signup')}
            className={`flex-1 py-2 px-3 rounded-lg transition-all text-center flex items-center justify-center gap-1.5 ${
              tab === 'signup'
                ? 'bg-amber text-ink font-bold shadow-[0_0_12px_rgba(229,169,60,0.3)]'
                : 'text-silver-dim hover:text-silver hover:bg-white/[0.04]'
            }`}
          >
            <span>Create Account</span>
          </button>

          <button
            onClick={() => setTab('architecture')}
            className={`py-2 px-3 rounded-lg transition-all text-center text-xs ${
              tab === 'architecture'
                ? 'bg-white/15 text-white font-semibold'
                : 'text-silver-faint hover:text-silver-dim'
            }`}
            title="Backend Security Specifications"
          >
            <span>Security Specs</span>
          </button>
        </div>

        {/* Modal Body */}
        <div className="p-6 overflow-y-auto space-y-5">
          {/* Success Banner */}
          {authSuccess && (
            <div className="p-3.5 rounded-xl bg-emerald-950/40 border border-emerald-500/40 text-emerald-300 text-xs font-mono flex items-center gap-2.5 animate-fadeIn">
              <span className="h-2 w-2 rounded-full bg-emerald-400 animate-pulse" />
              <span>{authSuccess}</span>
            </div>
          )}

          {/* TAB 1: SIGN IN */}
          {tab === 'signin' && (
            <div className="space-y-5">
              {/* 1-Click Quick Demo Login Section */}
              <div className="p-4 rounded-xl bg-gradient-to-b from-amber/[0.08] to-transparent border border-amber/30 space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="h-1.5 w-1.5 rounded-full bg-amber animate-ping" />
                    <span className="text-[11px] font-mono uppercase tracking-wider text-amber font-semibold">
                      1-Click Quick Demo Login
                    </span>
                  </div>
                  <span className="text-[10px] font-mono text-silver-faint">Reviewer Fast Pass</span>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
                  {/* Ava Button */}
                  <button
                    type="button"
                    onClick={() => handleQuickDemoSelect(DEMO_USERS[0])}
                    className={`p-3 rounded-xl border text-left transition-all group flex items-center gap-3 ${
                      current?.id === DEMO_USERS[0].id
                        ? 'border-amber bg-amber/15 shadow-[0_0_15px_rgba(229,169,60,0.2)]'
                        : 'border-white/10 bg-black/40 hover:border-amber/50 hover:bg-white/[0.04]'
                    }`}
                  >
                    <div className="h-10 w-10 rounded-lg bg-amber/20 text-amber font-cinema font-bold text-sm flex items-center justify-center border border-amber/30 group-hover:scale-105 transition-transform">
                      A
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-bold text-silver group-hover:text-amber transition-colors">
                          Ava Chen
                        </span>
                        <span className="text-[9px] font-mono px-1.5 py-0.5 rounded bg-amber/20 text-amber font-semibold">
                          CREATOR
                        </span>
                      </div>
                      <p className="text-[11px] text-silver-dim truncate">Independent Director</p>
                      <p className="text-[10px] font-mono text-silver-faint truncate">creator@cinefund.dev</p>
                    </div>
                  </button>

                  {/* Ravi Button */}
                  <button
                    type="button"
                    onClick={() => handleQuickDemoSelect(DEMO_USERS[1])}
                    className={`p-3 rounded-xl border text-left transition-all group flex items-center gap-3 ${
                      current?.id === DEMO_USERS[1].id
                        ? 'border-emerald-500 bg-emerald-950/30 shadow-[0_0_15px_rgba(16,185,129,0.2)]'
                        : 'border-white/10 bg-black/40 hover:border-emerald-500/50 hover:bg-white/[0.04]'
                    }`}
                  >
                    <div className="h-10 w-10 rounded-lg bg-emerald-900/30 text-emerald-400 font-cinema font-bold text-sm flex items-center justify-center border border-emerald-500/30 group-hover:scale-105 transition-transform">
                      R
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-bold text-silver group-hover:text-emerald-400 transition-colors">
                          Ravi Patel
                        </span>
                        <span className="text-[9px] font-mono px-1.5 py-0.5 rounded bg-emerald-500/20 text-emerald-400 font-semibold">
                          BACKER
                        </span>
                      </div>
                      <p className="text-[11px] text-silver-dim truncate">Film Patron & Collector</p>
                      <p className="text-[10px] font-mono text-silver-faint truncate">backer@cinefund.dev</p>
                    </div>
                  </button>
                </div>
              </div>

              {/* Divider */}
              <div className="flex items-center gap-3 my-4 text-xs font-mono text-silver-faint">
                <div className="flex-1 h-[1px] bg-white/[0.08]" />
                <span>OR SIGN IN WITH CREDENTIALS</span>
                <div className="flex-1 h-[1px] bg-white/[0.08]" />
              </div>

              {/* Form */}
              <form onSubmit={handleSignInSubmit} className="space-y-4">
                <div>
                  <label className="block text-xs font-mono text-silver-dim mb-1.5" htmlFor="signin-email">
                    Email Address
                  </label>
                  <input
                    id="signin-email"
                    type="email"
                    value={signInEmail}
                    onChange={e => setSignInEmail(e.target.value)}
                    placeholder="director@cinefund.dev or any email"
                    className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
                  />
                </div>

                <div>
                  <div className="flex items-center justify-between mb-1.5">
                    <label className="text-xs font-mono text-silver-dim" htmlFor="signin-password">
                      Password
                    </label>
                    <button
                      type="button"
                      onClick={() => setShowPassword(p => !p)}
                      className="text-[11px] font-mono text-silver-faint hover:text-silver transition-colors"
                    >
                      {showPassword ? 'Hide' : 'Show'}
                    </button>
                  </div>
                  <input
                    id="signin-password"
                    type={showPassword ? 'text' : 'password'}
                    value={signInPassword}
                    onChange={e => setSignInPassword(e.target.value)}
                    placeholder="••••••••"
                    className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
                  />
                </div>

                {signInError && (
                  <p className="text-xs font-mono text-crimson bg-crimson/10 p-2.5 rounded-lg border border-crimson/30">
                    {signInError}
                  </p>
                )}

                <button
                  type="submit"
                  className="w-full py-3 rounded-xl bg-amber hover:bg-amber-bright text-ink font-cinema font-bold text-sm tracking-wider shadow-[0_0_20px_rgba(229,169,60,0.25)] transition-all active:scale-[0.99]"
                >
                  Sign In to CineFund
                </button>

                <p className="text-[11px] font-mono text-silver-faint text-center">
                  Quick demo accounts: <code className="text-silver">creator@cinefund.dev</code> / <code className="text-silver">backer@cinefund.dev</code>
                </p>
              </form>

              <div className="text-center pt-2">
                <p className="text-xs text-silver-dim font-sans">
                  New to CineFund?{' '}
                  <button
                    type="button"
                    onClick={() => setTab('signup')}
                    className="text-amber hover:underline font-medium font-mono ml-1"
                  >
                    Create an account →
                  </button>
                </p>
              </div>
            </div>
          )}

          {/* TAB 2: CREATE ACCOUNT */}
          {tab === 'signup' && (
            <form onSubmit={handleSignUpSubmit} className="space-y-4">
              <div>
                <label className="block text-xs font-mono text-silver-dim mb-1.5" htmlFor="signup-name">
                  Full Name / Director Moniker
                </label>
                <input
                  id="signup-name"
                  type="text"
                  value={signUpName}
                  onChange={e => setSignUpName(e.target.value)}
                  placeholder="e.g. Maya Lin or Christopher Nolan"
                  className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
                />
              </div>

              <div>
                <label className="block text-xs font-mono text-silver-dim mb-1.5" htmlFor="signup-email">
                  Email Address
                </label>
                <input
                  id="signup-email"
                  type="email"
                  value={signUpEmail}
                  onChange={e => setSignUpEmail(e.target.value)}
                  placeholder="name@indiefilm.studio"
                  className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
                />
              </div>

              <div>
                <label className="block text-xs font-mono text-silver-dim mb-1.5" htmlFor="signup-password">
                  Password
                </label>
                <input
                  id="signup-password"
                  type="password"
                  value={signUpPassword}
                  onChange={e => setSignUpPassword(e.target.value)}
                  placeholder="At least 4 characters"
                  className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
                />
              </div>

              {/* Role Selection Cards */}
              <div>
                <label className="block text-xs font-mono text-silver-dim mb-2">
                  Select Your Platform Role
                </label>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  {/* Creator Card */}
                  <div
                    onClick={() => setSignUpRole('CREATOR')}
                    className={`p-3.5 rounded-xl border cursor-pointer transition-all ${
                      signUpRole === 'CREATOR'
                        ? 'border-amber bg-amber/15 shadow-[0_0_15px_rgba(229,169,60,0.2)]'
                        : 'border-white/10 bg-black/30 hover:border-white/20'
                    }`}
                  >
                    <div className="flex items-center justify-between mb-1.5">
                      <div className="flex items-center gap-2">
                        <span className="text-lg">🎬</span>
                        <span className="font-cinema font-bold text-silver text-xs">Filmmaker / Creator</span>
                      </div>
                      <input
                        type="radio"
                        name="role"
                        checked={signUpRole === 'CREATOR'}
                        onChange={() => setSignUpRole('CREATOR')}
                        className="accent-amber"
                      />
                    </div>
                    <p className="text-[11px] text-silver-dim font-sans leading-relaxed">
                      Launch 35mm film campaigns, upload master reels straight to S3, manage tiered backer rewards.
                    </p>
                  </div>

                  {/* Backer Card */}
                  <div
                    onClick={() => setSignUpRole('BACKER')}
                    className={`p-3.5 rounded-xl border cursor-pointer transition-all ${
                      signUpRole === 'BACKER'
                        ? 'border-emerald-500 bg-emerald-950/30 shadow-[0_0_15px_rgba(16,185,129,0.2)]'
                        : 'border-white/10 bg-black/30 hover:border-white/20'
                    }`}
                  >
                    <div className="flex items-center justify-between mb-1.5">
                      <div className="flex items-center gap-2">
                        <span className="text-lg">🎟️</span>
                        <span className="font-cinema font-bold text-silver text-xs">Patron / Backer</span>
                      </div>
                      <input
                        type="radio"
                        name="role"
                        checked={signUpRole === 'BACKER'}
                        onChange={() => setSignUpRole('BACKER')}
                        className="accent-emerald-500"
                      />
                    </div>
                    <p className="text-[11px] text-silver-dim font-sans leading-relaxed">
                      Back indie auteur projects into double-entry escrow, claim limited film frames and premiere seats.
                    </p>
                  </div>
                </div>
              </div>

              {signUpError && (
                <p className="text-xs font-mono text-crimson bg-crimson/10 p-2.5 rounded-lg border border-crimson/30">
                  {signUpError}
                </p>
              )}

              <button
                type="submit"
                className="w-full py-3 rounded-xl bg-amber hover:bg-amber-bright text-ink font-cinema font-bold text-sm tracking-wider shadow-[0_0_20px_rgba(229,169,60,0.25)] transition-all active:scale-[0.99]"
              >
                Create Cinema Account
              </button>

              <div className="text-center pt-2">
                <p className="text-xs text-silver-dim font-sans">
                  Already have an account?{' '}
                  <button
                    type="button"
                    onClick={() => setTab('signin')}
                    className="text-amber hover:underline font-medium font-mono ml-1"
                  >
                    Sign in here →
                  </button>
                </p>
              </div>
            </form>
          )}

          {/* TAB 3: ARCHITECTURE */}
          {tab === 'architecture' && (
            <div className="space-y-3.5 text-xs">
              <div className="p-3.5 bg-black/40 border border-white/10 rounded-xl space-y-1.5">
                <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-amber/15 text-amber border border-amber/30">
                  PostgreSQL Migration 0003
                </span>
                <h4 className="font-semibold text-silver text-sm">Refresh Token Family Rotation</h4>
                <p className="text-silver-dim leading-relaxed font-sans">
                  Tokens are stored in PostgreSQL <code className="text-amber font-mono">refresh_token_families</code>. Rotation uses atomic CAS on <code className="text-amber font-mono">current_jti</code>. If a consumed token is reused, the entire token family is burned immediately.
                </p>
              </div>

              <div className="p-3.5 bg-black/40 border border-white/10 rounded-xl space-y-1.5">
                <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-amber/15 text-amber border border-amber/30">
                  Go Backend Architecture
                </span>
                <h4 className="font-semibold text-silver text-sm">Dual 32-Byte Cryptographic Secrets</h4>
                <p className="text-silver-dim leading-relaxed font-sans">
                  Enforced by <code className="text-amber font-mono">config.go:Validate()</code>: <code className="text-silver font-mono">JWT_ACCESS_SECRET</code> (15m expiry) and <code className="text-silver font-mono">JWT_REFRESH_SECRET</code> (720h HTTP-only secure cookie).
                </p>
              </div>

              <div className="p-3.5 bg-black/40 border border-white/10 rounded-xl space-y-1.5">
                <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-amber/15 text-amber border border-amber/30">
                  Dual-Entry Escrow Ledger
                </span>
                <h4 className="font-semibold text-silver text-sm">PostgreSQL Deferred Invariant</h4>
                <p className="text-silver-dim leading-relaxed font-sans">
                  Every <code className="text-silver font-mono">CAPTURED</code> pledge generates balanced DEBIT and CREDIT transactions. Transaction isolation guarantees escrow safety before transcode workers trigger.
                </p>
              </div>
            </div>
          )}
        </div>

        {/* Modal Footer */}
        <div className="px-6 py-3.5 bg-black/40 border-t border-white/[0.08] flex items-center justify-between text-xs font-mono">
          <div className="text-silver-faint text-[11px] truncate max-w-[240px]">
            {current ? (
              <span>Session: <strong className="text-silver font-sans">{current.name}</strong> ({current.role})</span>
            ) : (
              <span>Session: <span className="text-amber">Guest / Signed Out</span></span>
            )}
          </div>

          <div className="flex gap-2">
            {current && (
              <button
                type="button"
                onClick={() => {
                  logout()
                  setCurrent(null)
                  setAuthSuccess('Signed out of session.')
                  setTimeout(() => onClose(), 400)
                }}
                className="px-3 py-1.5 rounded-lg border border-crimson/30 bg-crimson/10 hover:bg-crimson/20 text-crimson text-xs transition-colors"
              >
                Sign Out
              </button>
            )}
            <button
              type="button"
              onClick={onClose}
              className="px-3 py-1.5 rounded-lg bg-white/10 text-silver hover:bg-white/20 text-xs transition-colors"
            >
              Close
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
