import { useState, useEffect, useRef } from 'react'
import { signInWithGoogle, signOutFirebase } from '../firebase'
import { ApiError, exchangeFirebaseToken, isBackendUnavailable, loginDemo } from '../api'

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
  // offline marks a sign-in made while the API was unreachable: the user
  // browses the offline demo vault with no session token.
  offline?: boolean
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
/**
 * Returns the currently signed-in user, or null if the visitor is a guest.
 * New visitors start unauthenticated for an intuitive, predictable experience.
 */
export function getActiveUser(): UserProfile | null {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (!saved || saved === 'null') return null
    const user = JSON.parse(saved) as UserProfile
    // Sessions with no token predate real sign-in (or came from the removed
    // email form, whose accounts the API never knew): treat them as signed out.
    if (!user.token && !user.offline) return null
    return user
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

// signInAsDemo signs in as one of the shared demo accounts with a real session
// token from the API. If the API is unreachable it signs in offline so the demo
// vault stays explorable.
export async function signInAsDemo(profile: UserProfile): Promise<UserProfile> {
  try {
    const session = await loginDemo(profile.role === 'CREATOR' ? 'creator' : 'backer')
    const user: UserProfile = { ...profile, id: session.user.id, token: session.token, offline: false }
    setActiveUser(user)
    return user
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      throw new Error('Demo accounts are turned off on this server.')
    }
    if (!isBackendUnavailable(err)) throw err
    const user: UserProfile = { ...profile, token: undefined, offline: true }
    setActiveUser(user)
    return user
  }
}

export function logout() {
  signOutFirebase()
  setActiveUser(null)
}

type Props = {
  isOpen: boolean
  onClose: () => void
}

export default function AuthModal({ isOpen, onClose }: Props) {
  const [current, setCurrent] = useState<UserProfile | null>(getActiveUser())
  const [role, setRole] = useState<UserRole>('CREATOR')
  const [signInError, setSignInError] = useState<string | null>(null)
  const [authSuccess, setAuthSuccess] = useState<string | null>(null)
  const [isGoogleLoading, setIsGoogleLoading] = useState(false)
  const [demoLoading, setDemoLoading] = useState<string | null>(null)

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
      setSignInError(null)
      setAuthSuccess(null)
    }
  }, [isOpen])

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

  async function handleGoogleSignIn() {
    setIsGoogleLoading(true)
    setSignInError(null)
    try {
      const { user, idToken } = await signInWithGoogle()
      const base: UserProfile = {
        id: user.uid,
        name: user.displayName || user.email?.split('@')[0] || 'Cinema Patron',
        email: user.email || '',
        role,
        avatar: user.photoURL || (user.displayName?.[0] || 'G').toUpperCase(),
        photoURL: user.photoURL || undefined,
        tag: role === 'CREATOR' ? 'Independent 35mm Director' : 'Film Patron & Backer',
      }

      // Wait for the CineFund session: until then there is no token the API
      // accepts, so the user is not signed in yet.
      let profile: UserProfile
      try {
        const session = await exchangeFirebaseToken(idToken, role)
        profile = { ...base, id: session.user.id, name: session.user.name || base.name, token: session.token }
      } catch (err) {
        if (!isBackendUnavailable(err)) {
          await signOutFirebase()
          throw err
        }
        profile = { ...base, offline: true }
      }

      setCurrent(profile)
      setActiveUser(profile)
      setAuthSuccess(`Welcome, ${profile.name}! Signed in successfully.`)
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

  async function handleQuickDemoSelect(demo: UserProfile) {
    setSignInError(null)
    setDemoLoading(demo.id)
    try {
      const user = await signInAsDemo(demo)
      setCurrent(user)
      setAuthSuccess(`Signed in as ${user.name} (${user.role === 'CREATOR' ? 'Director' : 'Backer'})`)
      scheduleClose(350)
    } catch (err) {
      setSignInError((err as Error).message)
    } finally {
      setDemoLoading(null)
    }
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
                {current ? 'Account Profile' : 'Sign in to CineFund'}
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
                onClick={() => setCurrent(null)}
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
            {/* Role for a new Google account */}
            <div>
              <label className="block text-xs text-silver-dim mb-2 font-medium">
                I'm joining as:
              </label>
              <div className="grid grid-cols-2 gap-2.5">
                <button
                  type="button"
                  onClick={() => setRole('CREATOR')}
                  className={`p-3 rounded-xl border text-left transition-all ${
                    role === 'CREATOR'
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
                  onClick={() => setRole('BACKER')}
                  className={`p-3 rounded-xl border text-left transition-all ${
                    role === 'BACKER'
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

            {/* 1-Click Google Sign In */}
            <button
              type="button"
              onClick={handleGoogleSignIn}
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

            {signInError && (
              <p className="text-xs text-crimson bg-crimson/10 p-2.5 rounded-lg border border-crimson/30">
                {signInError}
              </p>
            )}

            {/* Quick Demo Access Footer for Evaluators */}
            <div className="pt-4 border-t border-white/[0.08] text-center">
              <p className="text-[11px] text-silver-faint mb-2 font-mono">
                Evaluating CineFund? Shared demo accounts:
              </p>
              <p className="text-[10px] text-silver-faint mb-2">
                Public logins anyone can use, not private accounts.
              </p>
              <div className="flex justify-center gap-2">
                <button
                  type="button"
                  onClick={() => handleQuickDemoSelect(DEMO_USERS[0])}
                  disabled={demoLoading !== null}
                  className="px-2.5 py-1.5 rounded-lg bg-white/[0.04] hover:bg-amber/15 border border-white/10 hover:border-amber/40 text-xs text-silver hover:text-amber font-mono transition-all flex items-center gap-1.5"
                >
                  <span>🎬</span>
                  <span>Ava (Director)</span>
                </button>
                <button
                  type="button"
                  onClick={() => handleQuickDemoSelect(DEMO_USERS[1])}
                  disabled={demoLoading !== null}
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
