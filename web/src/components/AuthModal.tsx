import { useState, useEffect, useRef } from 'react'
import { signInWithGoogle, signOutFirebase } from '../firebase'

export type UserRole = 'CREATOR' | 'BACKER' | 'ADMIN'

export type UserProfile = {
  id: string
  name: string
  email: string
  role: UserRole
  avatar: string
  tag: string
  photoURL?: string
  token?: string
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
    tag: 'Film Patron & Collector',
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
    const idx = list.findIndex(
      u => u.id === user.id || (u.email && user.email && u.email.toLowerCase() === user.email.toLowerCase())
    )
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

/**
 * Returns the currently signed-in user, or null if the visitor is a guest.
 * New visitors start unauthenticated for an intuitive, predictable experience.
 */
export function getActiveUser(): UserProfile | null {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (!saved || saved === 'null') return null
    return JSON.parse(saved)
  } catch {
    return null
  }
}

export function setActiveUser(user: UserProfile | null) {
  if (user === null) {
    localStorage.setItem(STORAGE_KEY, 'null')
  } else {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(user))
  }
  syncAccessTokenCookie(user)
  window.dispatchEvent(new Event('cinefund_auth_change'))
}

function syncAccessTokenCookie(user: UserProfile | null) {
  try {
    if (typeof document === 'undefined') return
    if (user?.token) {
      document.cookie = `cf_at=${encodeURIComponent(user.token)}; Path=/; SameSite=Lax; Max-Age=900`
    } else {
      document.cookie = 'cf_at=; Path=/; Max-Age=0'
    }
  } catch {
    // ignore
  }
}

export function logout() {
  signOutFirebase()
  setActiveUser(null)
}

type Props = {
  isOpen: boolean
  onClose: () => void
  initialTab?: 'signin' | 'signup'
}

export default function AuthModal({ isOpen, onClose, initialTab = 'signin' }: Props) {
  const [tab, setTab] = useState<'signin' | 'signup'>(initialTab)
  const [current, setCurrent] = useState<UserProfile | null>(getActiveUser())

  // Sign In fields
  const [signInEmail, setSignInEmail] = useState('')
  const [signInPassword, setSignInPassword] = useState('')
  const [signInError, setSignInError] = useState<string | null>(null)

  // Sign Up fields
  const [signUpName, setSignUpName] = useState('')
  const [signUpEmail, setSignUpEmail] = useState('')
  const [signUpPassword, setSignUpPassword] = useState('')
  const [signUpRole, setSignUpRole] = useState<UserRole>('CREATOR')
  const [signUpError, setSignUpError] = useState<string | null>(null)

  const [showPassword, setShowPassword] = useState(false)
  const [authSuccess, setAuthSuccess] = useState<string | null>(null)
  const [isGoogleLoading, setIsGoogleLoading] = useState(false)

  const closeTimer = useRef<number | null>(null)

  function scheduleClose(ms: number) {
    if (closeTimer.current !== null) {
      window.clearTimeout(closeTimer.current)
    }
    closeTimer.current = window.setTimeout(() => {
      closeTimer.current = null
      onClose()
    }, ms)
  }

  useEffect(() => {
    if (isOpen) {
      setCurrent(getActiveUser())
      setTab(initialTab)
      setSignInError(null)
      setSignUpError(null)
      setAuthSuccess(null)
    }
  }, [isOpen, initialTab])

  // Close on Escape key
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape' && isOpen) {
        onClose()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  useEffect(() => {
    return () => {
      if (closeTimer.current !== null) {
        window.clearTimeout(closeTimer.current)
        closeTimer.current = null
      }
    }
  }, [])

  async function handleGoogleSignIn(selectedRole?: UserRole) {
    try {
      setIsGoogleLoading(true)
      setSignInError(null)
      setSignUpError(null)

      const { user, idToken } = await signInWithGoogle()
      const assignedRole: UserRole = selectedRole || (tab === 'signup' ? signUpRole : 'CREATOR')

      const googleProfile: UserProfile = {
        id: user.uid,
        name: user.displayName || user.email?.split('@')[0] || 'Cinema Patron',
        email: user.email || '',
        role: assignedRole,
        avatar: user.photoURL || (user.displayName?.[0] || 'G').toUpperCase(),
        photoURL: user.photoURL || undefined,
        tag: assignedRole === 'CREATOR' ? 'Independent 35mm Director' : 'Film Patron & Backer',
        token: idToken,
      }

      saveRegisteredUser(googleProfile)
      setCurrent(googleProfile)
      setActiveUser(googleProfile)
      setAuthSuccess(`Welcome, ${googleProfile.name}! Signed in successfully.`)

      // Background sync to backend
      fetch('/api/v1/auth/firebase', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id_token: idToken,
          role: assignedRole,
        }),
      })
        .then(async resp => {
          if (resp.ok) {
            const data = await resp.json()
            if (data.token) {
              googleProfile.token = data.token
              if (data.user?.id) googleProfile.id = data.user.id
              saveRegisteredUser(googleProfile)
              setActiveUser(googleProfile)
            }
          }
        })
        .catch(() => {})

      scheduleClose(350)
    } catch (err: any) {
      if (err?.code === 'auth/popup-closed-by-user') {
        setSignInError('Google sign-in was closed.')
      } else {
        setSignInError(err?.message || 'Google sign-in could not be completed.')
      }
    } finally {
      setIsGoogleLoading(false)
    }
  }

  function handleQuickDemoSelect(user: UserProfile) {
    setCurrent(user)
    setActiveUser(user)
    setAuthSuccess(`Signed in as ${user.name} (${user.role === 'CREATOR' ? 'Director' : 'Backer'})`)
    scheduleClose(350)
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
      setAuthSuccess(`Welcome back, ${matched.name}!`)
      scheduleClose(400)
    } else {
      // Auto-provision demo account for testing with entered email
      const namePart = emailTrim.split('@')[0]
      const formattedName = namePart.charAt(0).toUpperCase() + namePart.slice(1)
      const role: UserRole = emailTrim.includes('director') || emailTrim.includes('film') ? 'CREATOR' : 'BACKER'
      const newUser: UserProfile = {
        id: `user-${Date.now()}`,
        name: formattedName || 'Cinema Member',
        email: emailTrim,
        role,
        avatar: (formattedName[0] || 'C').toUpperCase(),
        tag: role === 'CREATOR' ? 'Independent 35mm Director' : 'Film Patron & Backer',
      }
      saveRegisteredUser(newUser)
      setCurrent(newUser)
      setActiveUser(newUser)
      setAuthSuccess(`Welcome to CineFund, ${newUser.name}!`)
      scheduleClose(400)
    }
  }

  function handleSignUpSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSignUpError(null)

    const nameTrim = signUpName.trim()
    const emailTrim = signUpEmail.trim().toLowerCase()

    if (!nameTrim) {
      setSignUpError('Please enter your full name.')
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
    scheduleClose(450)
  }

  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm animate-fadeIn"
      onClick={onClose}
    >
      <div
        className="relative w-full max-w-md bg-[#12151E] border border-white/10 rounded-2xl shadow-2xl overflow-hidden flex flex-col max-h-[90vh]"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="px-6 pt-6 pb-4 border-b border-white/[0.08] flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="h-9 w-9 rounded-xl bg-amber/15 border border-amber/30 text-amber font-cinema font-bold text-sm flex items-center justify-center">
              35
            </div>
            <div>
              <h2 className="font-cinema font-bold text-lg text-silver tracking-wide">
                {current ? 'Account Profile' : tab === 'signin' ? 'Welcome Back' : 'Create an Account'}
              </h2>
              <p className="text-xs text-silver-dim">
                {current
                  ? 'Manage your CineFund identity & pledges'
                  : 'Crowdfund and stream independent cinema'}
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="h-8 w-8 rounded-lg bg-white/[0.05] hover:bg-white/[0.12] text-silver-dim hover:text-white transition-all flex items-center justify-center text-sm font-semibold"
            aria-label="Close"
          >
            ✕
          </button>
        </div>

        {/* Success Banner */}
        {authSuccess && (
          <div className="mx-6 mt-4 p-3 rounded-xl bg-emerald-500/15 border border-emerald-500/30 text-emerald-300 text-xs font-medium flex items-center gap-2">
            <span className="h-2 w-2 rounded-full bg-emerald-400 animate-pulse" />
            <span>{authSuccess}</span>
          </div>
        )}

        {/* If user is already authenticated and viewing this modal */}
        {current ? (
          <div className="p-6 space-y-6">
            <div className="p-4 rounded-xl bg-black/40 border border-white/10 space-y-3">
              <div className="flex items-center gap-3">
                <div className="h-12 w-12 rounded-xl bg-amber text-ink font-cinema font-bold text-lg flex items-center justify-center overflow-hidden flex-shrink-0">
                  {current.photoURL || (current.avatar && current.avatar.startsWith('http')) ? (
                    <img src={current.photoURL || current.avatar} alt={current.name} className="w-full h-full object-cover" />
                  ) : (
                    current.avatar
                  )}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <h3 className="font-bold text-silver text-base truncate">{current.name}</h3>
                    <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-amber/20 text-amber font-semibold uppercase">
                      {current.role}
                    </span>
                  </div>
                  <p className="text-xs text-silver-dim truncate">{current.email}</p>
                  <p className="text-[11px] text-silver-faint truncate">{current.tag}</p>
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <button
                type="button"
                onClick={onClose}
                className="w-full py-2.5 px-4 rounded-xl bg-amber hover:bg-amber-bright text-ink font-semibold text-sm transition-all shadow-md active:scale-[0.99]"
              >
                Continue Browsing
              </button>

              <button
                type="button"
                onClick={() => {
                  setCurrent(null)
                  setTab('signin')
                }}
                className="w-full py-2.5 px-4 rounded-xl bg-white/[0.05] hover:bg-white/[0.1] text-silver hover:text-white text-xs font-mono transition-all"
              >
                Switch to Another Account
              </button>

              <button
                type="button"
                onClick={() => {
                  logout()
                  setCurrent(null)
                  setAuthSuccess('Signed out of session.')
                }}
                className="w-full py-2.5 px-4 rounded-xl border border-crimson/30 bg-crimson/10 hover:bg-crimson/20 text-crimson text-xs font-mono font-medium transition-all"
              >
                Sign Out
              </button>
            </div>
          </div>
        ) : (
          <div className="p-6 overflow-y-auto space-y-5">
            {/* Tab Selector */}
            <div className="flex p-1 rounded-xl bg-black/40 border border-white/[0.08]">
              <button
                type="button"
                onClick={() => {
                  setTab('signin')
                  setSignInError(null)
                }}
                className={`flex-1 py-2 rounded-lg text-xs font-medium transition-all ${
                  tab === 'signin'
                    ? 'bg-amber text-ink font-bold shadow-sm'
                    : 'text-silver-dim hover:text-silver'
                }`}
              >
                Sign In
              </button>
              <button
                type="button"
                onClick={() => {
                  setTab('signup')
                  setSignUpError(null)
                }}
                className={`flex-1 py-2 rounded-lg text-xs font-medium transition-all ${
                  tab === 'signup'
                    ? 'bg-amber text-ink font-bold shadow-sm'
                    : 'text-silver-dim hover:text-silver'
                }`}
              >
                Create Account
              </button>
            </div>

            {/* 1-Click Google Sign In */}
            <button
              type="button"
              onClick={() => handleGoogleSignIn()}
              disabled={isGoogleLoading}
              className="w-full py-3 px-4 rounded-xl bg-white text-gray-900 hover:bg-gray-100 font-medium text-sm transition-all flex items-center justify-center gap-3 shadow-md active:scale-[0.99] disabled:opacity-50"
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24">
                <path fill="#4285F4" d="M23.745 12.27c0-.7-.06-1.4-.19-2.07H12v4.51h6.6c-.29 1.52-1.14 2.82-2.4 3.68v3.05h3.88c2.27-2.09 3.66-5.17 3.66-9.17z" />
                <path fill="#34A853" d="M12 24c3.24 0 5.95-1.08 7.93-2.91l-3.88-3.05c-1.08.72-2.45 1.16-4.05 1.16-3.12 0-5.77-2.1-6.72-4.93H1.25v3.15C3.26 21.36 7.35 24 12 24z" />
                <path fill="#FBBC05" d="M5.28 14.27c-.25-.72-.38-1.49-.38-2.27s.13-1.55.38-2.27V6.58H1.25C.45 8.18 0 10.03 0 12s.45 3.82 1.25 5.42l4.03-3.15z" />
                <path fill="#EA4335" d="M12 4.75c1.77 0 3.35.61 4.6 1.8l3.42-3.42C17.95 1.19 15.24 0 12 0 7.35 0 3.26 2.64 1.25 6.58l4.03 3.15c.95-2.83 3.6-4.98 6.72-4.98z" />
              </svg>
              <span>{isGoogleLoading ? 'Signing in with Google...' : 'Continue with Google'}</span>
            </button>

            {/* Divider */}
            <div className="flex items-center gap-3 text-xs text-silver-faint font-mono">
              <div className="flex-1 h-[1px] bg-white/[0.08]" />
              <span>or with email</span>
              <div className="flex-1 h-[1px] bg-white/[0.08]" />
            </div>

            {/* TAB 1: SIGN IN FORM */}
            {tab === 'signin' ? (
              <form onSubmit={handleSignInSubmit} className="space-y-4">
                <div>
                  <label className="block text-xs text-silver-dim mb-1 font-medium" htmlFor="signin-email">
                    Email Address
                  </label>
                  <input
                    id="signin-email"
                    type="email"
                    value={signInEmail}
                    onChange={e => setSignInEmail(e.target.value)}
                    placeholder="you@example.com"
                    className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
                  />
                </div>

                <div>
                  <div className="flex items-center justify-between mb-1">
                    <label className="text-xs text-silver-dim font-medium" htmlFor="signin-password">
                      Password
                    </label>
                    <button
                      type="button"
                      onClick={() => setShowPassword(p => !p)}
                      className="text-[11px] text-silver-faint hover:text-silver"
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
                  <p className="text-xs text-crimson bg-crimson/10 p-2.5 rounded-lg border border-crimson/30">
                    {signInError}
                  </p>
                )}

                <button
                  type="submit"
                  className="w-full py-3 rounded-xl bg-amber hover:bg-amber-bright text-ink font-semibold text-sm transition-all shadow-md active:scale-[0.99]"
                >
                  Sign In
                </button>

                <p className="text-xs text-silver-dim text-center pt-1">
                  Don't have an account?{' '}
                  <button
                    type="button"
                    onClick={() => {
                      setTab('signup')
                      setSignInError(null)
                    }}
                    className="text-amber hover:underline font-medium ml-1"
                  >
                    Create an account
                  </button>
                </p>
              </form>
            ) : (
              /* TAB 2: SIGN UP FORM */
              <form onSubmit={handleSignUpSubmit} className="space-y-4">
                <div>
                  <label className="block text-xs text-silver-dim mb-1 font-medium" htmlFor="signup-name">
                    Full Name
                  </label>
                  <input
                    id="signup-name"
                    type="text"
                    value={signUpName}
                    onChange={e => setSignUpName(e.target.value)}
                    placeholder="Maya Lin"
                    className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
                  />
                </div>

                <div>
                  <label className="block text-xs text-silver-dim mb-1 font-medium" htmlFor="signup-email">
                    Email Address
                  </label>
                  <input
                    id="signup-email"
                    type="email"
                    value={signUpEmail}
                    onChange={e => setSignUpEmail(e.target.value)}
                    placeholder="maya@example.com"
                    className="w-full px-3.5 py-2.5 rounded-xl bg-black/40 border border-white/10 text-sm text-silver placeholder-silver-faint focus:outline-none focus:border-amber focus:ring-1 focus:ring-amber transition-all"
                  />
                </div>

                <div>
                  <label className="block text-xs text-silver-dim mb-1 font-medium" htmlFor="signup-password">
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

                {/* Role Selector */}
                <div>
                  <label className="block text-xs text-silver-dim mb-2 font-medium">
                    I want to:
                  </label>
                  <div className="grid grid-cols-2 gap-2.5">
                    <button
                      type="button"
                      onClick={() => setSignUpRole('CREATOR')}
                      className={`p-3 rounded-xl border text-left transition-all ${
                        signUpRole === 'CREATOR'
                          ? 'border-amber bg-amber/15 text-amber'
                          : 'border-white/10 bg-black/30 text-silver-dim hover:border-white/20'
                      }`}
                    >
                      <div className="flex items-center gap-1.5 font-bold text-xs mb-1">
                        <span>🎬</span>
                        <span>Filmmaker</span>
                      </div>
                      <p className="text-[11px] leading-snug opacity-80">
                        Launch campaigns & stream film reels
                      </p>
                    </button>

                    <button
                      type="button"
                      onClick={() => setSignUpRole('BACKER')}
                      className={`p-3 rounded-xl border text-left transition-all ${
                        signUpRole === 'BACKER'
                          ? 'border-emerald-500 bg-emerald-950/30 text-emerald-400'
                          : 'border-white/10 bg-black/30 text-silver-dim hover:border-white/20'
                      }`}
                    >
                      <div className="flex items-center gap-1.5 font-bold text-xs mb-1">
                        <span>🎟️</span>
                        <span>Film Patron</span>
                      </div>
                      <p className="text-[11px] leading-snug opacity-80">
                        Back indie projects & stream workprints
                      </p>
                    </button>
                  </div>
                </div>

                {signUpError && (
                  <p className="text-xs text-crimson bg-crimson/10 p-2.5 rounded-lg border border-crimson/30">
                    {signUpError}
                  </p>
                )}

                <button
                  type="submit"
                  className="w-full py-3 rounded-xl bg-amber hover:bg-amber-bright text-ink font-semibold text-sm transition-all shadow-md active:scale-[0.99]"
                >
                  Create Account
                </button>

                <p className="text-xs text-silver-dim text-center pt-1">
                  Already have an account?{' '}
                  <button
                    type="button"
                    onClick={() => {
                      setTab('signin')
                      setSignUpError(null)
                    }}
                    className="text-amber hover:underline font-medium ml-1"
                  >
                    Sign in here
                  </button>
                </p>
              </form>
            )}

            {/* Quick Demo Access Footer for Evaluators */}
            <div className="pt-4 border-t border-white/[0.08] text-center">
              <p className="text-[11px] text-silver-faint mb-2 font-mono">
                Evaluating CineFund? 1-Click Demo Profiles:
              </p>
              <div className="flex justify-center gap-2">
                <button
                  type="button"
                  onClick={() => handleQuickDemoSelect(DEMO_USERS[0])}
                  className="px-2.5 py-1.5 rounded-lg bg-white/[0.04] hover:bg-amber/15 border border-white/10 hover:border-amber/40 text-xs text-silver hover:text-amber font-mono transition-all flex items-center gap-1.5"
                >
                  <span>🎬</span>
                  <span>Ava (Director)</span>
                </button>
                <button
                  type="button"
                  onClick={() => handleQuickDemoSelect(DEMO_USERS[1])}
                  className="px-2.5 py-1.5 rounded-lg bg-white/[0.04] hover:bg-emerald-500/15 border border-white/10 hover:border-emerald-500/40 text-xs text-silver hover:text-emerald-400 font-mono transition-all flex items-center gap-1.5"
                >
                  <span>🎟️</span>
                  <span>Ravi (Backer)</span>
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
