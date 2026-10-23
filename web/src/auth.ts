// Session state for the signed-in user.
//
// The profile (including the API session token) lives in localStorage so it
// survives reloads. Components read it through useActiveUser(), which
// re-renders on sign-in/out in this tab and in other tabs.

import { useMemo, useSyncExternalStore } from 'react'
import { ApiError, exchangeDemoLogin, exchangeFirebaseToken, isBackendUnavailable } from './api'
import { signOutFirebase } from './firebase'

export type UserRole = 'CREATOR' | 'BACKER' | 'ADMIN'

export type UserProfile = {
  id: string
  name: string
  email: string
  role: UserRole
  avatar: string
  tag: string
  photoURL?: string
  /** CineFund API session JWT. Absent for offline-only demo identities. */
  token?: string
  /**
   * Set when the user signed in while the API was unreachable: they browse the
   * offline demo vault with no session token. A profile with neither a token
   * nor this flag is treated as signed out.
   */
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

const AUTH_EVENT = 'cinefund_auth_change'

const tagFor = (role: UserRole) => (role === 'CREATOR' ? 'Independent 35mm Director' : 'Film Patron & Backer')

function parseUser(raw: string | null): UserProfile | null {
  if (!raw || raw === 'null') return null
  try {
    const user = JSON.parse(raw) as UserProfile
    // Sessions with no token and no offline flag predate real sign-in (or came
    // from the removed email form, whose accounts the API never knew): they
    // are signed out.
    if (!user.token && !user.offline) return null
    return user
  } catch {
    return null
  }
}

function readRaw(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

/**
 * Returns the currently signed-in user, or null if the visitor is a guest.
 * New visitors start unauthenticated for an intuitive, predictable experience.
 */
export function getActiveUser(): UserProfile | null {
  return parseUser(readRaw())
}

export function setActiveUser(user: UserProfile | null) {
  try {
    if (user === null) {
      localStorage.setItem(STORAGE_KEY, 'null')
    } else {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(user))
    }
  } catch {
    // storage unavailable (private mode): the session lasts for this page only
  }
  syncAccessTokenCookie(user)
  window.dispatchEvent(new Event(AUTH_EVENT))
}

/** Seconds until the JWT's exp claim, or null if it can't be read. */
function secondsUntilExpiry(token: string): number | null {
  try {
    const payload = JSON.parse(atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')))
    return typeof payload.exp === 'number' ? Math.max(0, payload.exp - Math.floor(Date.now() / 1000)) : null
  } catch {
    return null
  }
}

// Safari plays HLS natively, so it can't attach a Bearer header to playlist
// requests; the API also accepts the token from this cookie. Its lifetime
// tracks the token's own expiry so playback doesn't silently stop early.
function syncAccessTokenCookie(user: UserProfile | null) {
  try {
    if (typeof document === 'undefined') return
    const secure = window.location.protocol === 'https:' ? '; Secure' : ''
    if (user?.token) {
      const maxAge = secondsUntilExpiry(user.token) ?? 900
      document.cookie = `cf_at=${encodeURIComponent(user.token)}; Path=/api; SameSite=Strict; Max-Age=${maxAge}${secure}`
    } else {
      document.cookie = `cf_at=; Path=/api; SameSite=Strict; Max-Age=0${secure}`
    }
    // Clear the cookie older builds set on Path=/.
    document.cookie = 'cf_at=; Path=/; Max-Age=0'
  } catch {
    // ignore
  }
}

export function logout() {
  void signOutFirebase()
  setActiveUser(null)
}

function subscribe(onChange: () => void) {
  const onStorage = (e: StorageEvent) => {
    if (e.key === null || e.key === STORAGE_KEY) onChange()
  }
  window.addEventListener(AUTH_EVENT, onChange)
  window.addEventListener('storage', onStorage)
  return () => {
    window.removeEventListener(AUTH_EVENT, onChange)
    window.removeEventListener('storage', onStorage)
  }
}

/**
 * The signed-in user, kept in sync across components and browser tabs.
 *
 * The snapshot is the raw storage string (stable between renders) and is
 * parsed with useMemo; returning a freshly parsed object from getSnapshot
 * would make React re-render forever.
 */
export function useActiveUser(): UserProfile | null {
  const raw = useSyncExternalStore(subscribe, readRaw, () => null)
  return useMemo(() => parseUser(raw), [raw])
}

/**
 * Signs in as a seeded demo account with a real server-issued token. When the
 * backend is unreachable the demo profile is used offline (local data only).
 */
export async function signInAsDemo(kind: 'creator' | 'backer'): Promise<UserProfile> {
  const base = kind === 'creator' ? DEMO_USERS[0] : DEMO_USERS[1]
  try {
    const res = await exchangeDemoLogin(kind)
    const user: UserProfile = { ...base, id: res.user.id, token: res.token, offline: false }
    setActiveUser(user)
    return user
  } catch (err) {
    if (isBackendUnavailable(err)) {
      const offline: UserProfile = { ...base, token: undefined, offline: true }
      setActiveUser(offline)
      return offline
    }
    if (err instanceof ApiError && err.status === 404) {
      throw new Error('Demo sign-in is not enabled on this server.')
    }
    throw err
  }
}

/**
 * Exchanges a Firebase ID token for a CineFund session and stores the
 * resulting profile. The Firebase token itself is never used as an API token.
 */
export async function completeGoogleSignIn(
  firebaseUser: { uid: string; displayName: string | null; email: string | null; photoURL: string | null },
  idToken: string,
  role: UserRole,
): Promise<UserProfile> {
  const name = firebaseUser.displayName || firebaseUser.email?.split('@')[0] || 'Cinema Patron'
  const profile: UserProfile = {
    id: firebaseUser.uid,
    name,
    email: firebaseUser.email || '',
    role,
    avatar: firebaseUser.photoURL || (name[0] || 'G').toUpperCase(),
    photoURL: firebaseUser.photoURL || undefined,
    tag: tagFor(role),
  }
  try {
    const res = await exchangeFirebaseToken(idToken, role)
    const serverRole = (res.user.role as UserRole) || role
    Object.assign(profile, { id: res.user.id, token: res.token, role: serverRole, tag: tagFor(serverRole) })
  } catch (err) {
    // Offline: keep a local-only profile so the demo vault stays browsable.
    if (!isBackendUnavailable(err)) {
      void signOutFirebase()
      throw err
    }
    profile.offline = true
  }
  setActiveUser(profile)
  return profile
}
