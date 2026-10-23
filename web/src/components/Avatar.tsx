import { useState } from 'react'
import type { UserProfile } from '../auth'

type Props = {
  user: Pick<UserProfile, 'name' | 'avatar' | 'photoURL'> | null
  /** Size, shape and font size, e.g. "h-8 w-8 rounded-lg text-xs". */
  className?: string
}

function photoOf(user: Props['user']): string | null {
  const url = user?.photoURL || user?.avatar
  return url && url.startsWith('https://') ? url : null
}

function initialOf(user: Props['user']): string {
  if (!user) return '👤'
  if (user.avatar && !user.avatar.startsWith('http')) return user.avatar
  return (user.name?.[0] || '?').toUpperCase()
}

/** A user's profile photo, falling back to their initial. */
export default function Avatar({ user, className = 'h-8 w-8 rounded-full text-xs' }: Props) {
  const [failed, setFailed] = useState(false)
  const photo = failed ? null : photoOf(user)
  return (
    <span
      className={`bg-amber text-ink font-cinema font-bold flex items-center justify-center overflow-hidden shrink-0 ${className}`}
      aria-hidden={photo ? undefined : true}
    >
      {photo ? (
        // Google profile photos often 403 when a Referer is sent.
        <img
          src={photo}
          alt={user?.name ?? ''}
          referrerPolicy="no-referrer"
          onError={() => setFailed(true)}
          className="w-full h-full object-cover"
        />
      ) : (
        initialOf(user)
      )}
    </span>
  )
}
