import { useSyncExternalStore } from 'react'
import { isDemoMode, subscribeDemoMode } from '../api'

// Shown only while the live server can't be reached and the app is serving its
// built-in sample films. Without it, a visitor could believe a simulated pledge
// was a real one.
export default function DemoBanner() {
  const demo = useSyncExternalStore(subscribeDemoMode, isDemoMode, () => false)
  if (!demo) return null
  return (
    <div role="status" className="border-b border-amber/25 bg-amber/10 px-4 py-2 text-center text-xs text-amber">
      <span className="font-semibold">Demo mode.</span>{' '}
      <span className="text-silver-dim">
        The live server isn&rsquo;t reachable, so you&rsquo;re seeing sample films. Nothing here is charged or saved to an account.
      </span>
    </div>
  )
}
