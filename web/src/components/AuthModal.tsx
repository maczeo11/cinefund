import { useState, useEffect, useRef, useId } from 'react'
import { signInWithGoogle } from '../firebase'
import { completeGoogleSignIn, getActiveUser, logout, signInAsDemo, type UserProfile, type UserRole } from '../auth'
import Avatar from './Avatar.tsx'

type Props = {
  isOpen: boolean
  onClose: () => void
}

const errorText = (err: unknown, fallback: string) => (err instanceof Error && err.message) || fallback

export default function AuthModal({ isOpen, onClose }: Props) {
  const [current, setCurrent] = useState<UserProfile | null>(getActiveUser())
  const [role, setRole] = useState<UserRole>('BACKER')

  const [error, setError] = useState<string | null>(null)
  const [authSuccess, setAuthSuccess] = useState<string | null>(null)
  const [busy, setBusy] = useState<null | 'google' | 'creator' | 'backer'>(null)

  const closeTimer = useRef<number | null>(null)
  const dialogRef = useRef<HTMLDivElement>(null)
  const titleId = useId()

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
      setError(null)
      setAuthSuccess(null)
    }
  }, [isOpen])

  // Dialog behaviour: Escape closes, Tab stays inside, focus moves in on open
  // and returns to whatever opened the dialog on close, page doesn't scroll.
  useEffect(() => {
    if (!isOpen) return
    const opener = document.activeElement as HTMLElement | null
    const dialog = dialogRef.current
    const focusables = () =>
      Array.from(
        dialog?.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])') ?? []
      )
    focusables()[0]?.focus()

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onClose()
        return
      }
      if (e.key !== 'Tab') return
      const items = focusables()
      if (items.length === 0) return
      const first = items[0]
      const last = items[items.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }

    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    window.addEventListener('keydown', handleKeyDown)
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      document.body.style.overflow = prevOverflow
      opener?.focus?.()
    }
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
    setBusy('google')
    setError(null)
    try {
      const { user, idToken } = await signInWithGoogle()
      // Wait for the server session before reporting success, so the first
      // action after the modal closes is already authenticated.
      const profile = await completeGoogleSignIn(user, idToken, role)
      setCurrent(profile)
      setAuthSuccess(`Welcome, ${profile.name}! Signed in successfully.`)
      scheduleClose(350)
    } catch (err) {
      const code = (err as { code?: string })?.code
      setError(code === 'auth/popup-closed-by-user' ? 'Google sign-in was closed.' : errorText(err, 'Google sign-in could not be completed.'))
    } finally {
      setBusy(null)
    }
  }

  async function handleDemo(kind: 'creator' | 'backer') {
    setBusy(kind)
    setError(null)
    try {
      const user = await signInAsDemo(kind)
      setCurrent(user)
      setAuthSuccess(`Signed in as ${user.name} (${user.role === 'CREATOR' ? 'Filmmaker' : 'Backer'})`)
      scheduleClose(350)
    } catch (err) {
      setError(errorText(err, 'Demo sign-in failed.'))
    } finally {
      setBusy(null)
    }
  }

  if (!isOpen) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm animate-fadeIn"
      onClick={onClose}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="relative w-full max-w-md bg-celluloid border border-white/10 rounded-2xl shadow-2xl overflow-hidden flex flex-col max-h-[90vh]"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="px-6 pt-6 pb-4 border-b border-white/[0.08] flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="h-9 w-9 rounded-xl bg-amber/15 border border-amber/30 text-amber font-cinema font-bold text-sm flex items-center justify-center" aria-hidden="true">
              35
            </div>
            <div>
              <h2 id={titleId} className="font-cinema font-bold text-lg text-silver tracking-wide">
                {current ? 'Your account' : 'Sign in to CineFund'}
              </h2>
              <p className="text-xs text-silver-dim">
                {current ? 'Manage your session' : 'Back independent films and watch their reels'}
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
          <div role="status" className="mx-6 mt-4 p-3 rounded-xl bg-emerald-500/15 border border-emerald-500/30 text-emerald-300 text-xs font-medium flex items-center gap-2">
            <span className="h-2 w-2 rounded-full bg-emerald-400 animate-pulse" />
            <span>{authSuccess}</span>
          </div>
        )}

        {current ? (
          <div className="p-6 space-y-6">
            <div className="p-4 rounded-xl bg-black/40 border border-white/10 space-y-3">
              <div className="flex items-center gap-3">
                <Avatar user={current} className="h-12 w-12 rounded-xl text-lg" />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <h3 className="font-bold text-silver text-base truncate">{current.name}</h3>
                    <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-amber/20 text-amber font-semibold uppercase">
                      {current.role === 'CREATOR' ? 'Filmmaker' : current.role === 'BACKER' ? 'Backer' : current.role}
                    </span>
                  </div>
                  <p className="text-xs text-silver-dim truncate">{current.email}</p>
                  <p className="text-[11px] text-silver-faint truncate">
                    {current.offline ? 'Offline demo — the server could not be reached' : current.tag}
                  </p>
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <button
                type="button"
                onClick={onClose}
                className="w-full py-2.5 px-4 rounded-xl bg-amber hover:bg-amber-bright text-ink font-semibold text-sm transition-all shadow-md active:scale-[0.99]"
              >
                Continue
              </button>

              <button
                type="button"
                onClick={() => {
                  setCurrent(null)
                }}
                className="w-full py-2.5 px-4 rounded-xl bg-white/[0.05] hover:bg-white/[0.1] text-silver hover:text-white text-xs font-medium transition-all"
              >
                Switch account
              </button>

              <button
                type="button"
                onClick={() => {
                  logout()
                  setCurrent(null)
                  setAuthSuccess('Signed out.')
                }}
                className="w-full py-2.5 px-4 rounded-xl border border-crimson/30 bg-crimson/10 hover:bg-crimson/20 text-crimson text-xs font-medium transition-all"
              >
                Sign out
              </button>
            </div>
          </div>
        ) : (
          <div className="p-6 overflow-y-auto space-y-5">
            <fieldset>
              <legend className="block text-xs text-silver-dim mb-2 font-medium">I want to:</legend>
              <div className="grid grid-cols-2 gap-2.5">
                <button
                  type="button"
                  aria-pressed={role === 'BACKER'}
                  onClick={() => setRole('BACKER')}
                  className={`p-3 rounded-xl border text-left transition-all ${
                    role === 'BACKER'
                      ? 'border-emerald-500 bg-emerald-950/30 text-emerald-400'
                      : 'border-white/10 bg-black/30 text-silver-dim hover:border-white/20'
                  }`}
                >
                  <div className="flex items-center gap-1.5 font-bold text-xs mb-1">
                    <span aria-hidden="true">🎟️</span>
                    <span>Back films</span>
                  </div>
                  <p className="text-[11px] leading-snug opacity-80">Support projects and watch their reels</p>
                </button>

                <button
                  type="button"
                  aria-pressed={role === 'CREATOR'}
                  onClick={() => setRole('CREATOR')}
                  className={`p-3 rounded-xl border text-left transition-all ${
                    role === 'CREATOR'
                      ? 'border-amber bg-amber/15 text-amber'
                      : 'border-white/10 bg-black/30 text-silver-dim hover:border-white/20'
                  }`}
                >
                  <div className="flex items-center gap-1.5 font-bold text-xs mb-1">
                    <span aria-hidden="true">🎬</span>
                    <span>Make a film</span>
                  </div>
                  <p className="text-[11px] leading-snug opacity-80">Start campaigns and share your reels</p>
                </button>
              </div>
            </fieldset>

            <button
              type="button"
              onClick={handleGoogleSignIn}
              disabled={busy !== null}
              className="w-full py-3 px-4 rounded-xl bg-white text-gray-900 hover:bg-gray-100 font-medium text-sm transition-all flex items-center justify-center gap-3 shadow-md active:scale-[0.99] disabled:opacity-50"
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" aria-hidden="true">
                <path fill="#4285F4" d="M23.745 12.27c0-.7-.06-1.4-.19-2.07H12v4.51h6.6c-.29 1.52-1.14 2.82-2.4 3.68v3.05h3.88c2.27-2.09 3.66-5.17 3.66-9.17z" />
                <path fill="#34A853" d="M12 24c3.24 0 5.95-1.08 7.93-2.91l-3.88-3.05c-1.08.72-2.45 1.16-4.05 1.16-3.12 0-5.77-2.1-6.72-4.93H1.25v3.15C3.26 21.36 7.35 24 12 24z" />
                <path fill="#FBBC05" d="M5.28 14.27c-.25-.72-.38-1.49-.38-2.27s.13-1.55.38-2.27V6.58H1.25C.45 8.18 0 10.03 0 12s.45 3.82 1.25 5.42l4.03-3.15z" />
                <path fill="#EA4335" d="M12 4.75c1.77 0 3.35.61 4.6 1.8l3.42-3.42C17.95 1.19 15.24 0 12 0 7.35 0 3.26 2.64 1.25 6.58l4.03 3.15c.95-2.83 3.6-4.98 6.72-4.98z" />
              </svg>
              <span>{busy === 'google' ? 'Signing in with Google...' : 'Continue with Google'}</span>
            </button>

            {error && (
              <p role="alert" className="text-xs text-crimson bg-crimson/10 p-2.5 rounded-lg border border-crimson/30">
                {error}
              </p>
            )}

            {/* Shared demo accounts for trying the product without signing up */}
            <div className="pt-4 border-t border-white/[0.08] text-center">
              <p className="text-[11px] text-silver-dim mb-2">Just looking around? Try a shared demo account:</p>
              <div className="flex justify-center gap-2">
                <button
                  type="button"
                  disabled={busy !== null}
                  onClick={() => handleDemo('backer')}
                  className="px-3 py-1.5 rounded-lg bg-white/[0.04] hover:bg-emerald-500/15 border border-white/10 hover:border-emerald-500/40 text-xs text-silver hover:text-emerald-400 font-medium transition-all flex items-center gap-1.5 disabled:opacity-50"
                >
                  <span aria-hidden="true">🎟️</span>
                  <span>{busy === 'backer' ? 'Signing in…' : 'Ravi (backer)'}</span>
                </button>
                <button
                  type="button"
                  disabled={busy !== null}
                  onClick={() => handleDemo('creator')}
                  className="px-3 py-1.5 rounded-lg bg-white/[0.04] hover:bg-amber/15 border border-white/10 hover:border-amber/40 text-xs text-silver hover:text-amber font-medium transition-all flex items-center gap-1.5 disabled:opacity-50"
                >
                  <span aria-hidden="true">🎬</span>
                  <span>{busy === 'creator' ? 'Signing in…' : 'Ava (filmmaker)'}</span>
                </button>
              </div>
              <p className="mt-2 text-[11px] text-silver-faint">Demo accounts are shared: what they create is visible to everyone.</p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
