import VideoPlayer from './VideoPlayer.tsx'
import { FALLBACK_POSTER_SVG } from '../api'

/**
 * What the film page can offer to watch right now.
 *  - server:  a processed reel on the API. Playlists need a signed-in user.
 *  - sample:  a public demo stream, only used when the backend is unreachable.
 *  - preview: the creator's just-uploaded local file, until processing is done.
 */
export type Reel =
  | { kind: 'loading' }
  | { kind: 'none' }
  | { kind: 'server'; src: string }
  | { kind: 'sample'; src: string }
  | { kind: 'preview'; src: string }

type Props = {
  reel: Reel
  title: string
  poster: string
  signedIn: boolean
  isCreator: boolean
  onSignIn: () => void
}

function Message({ poster, heading, children }: { poster: string; heading: string; children: React.ReactNode }) {
  return (
    <div className="relative aspect-video w-full overflow-hidden rounded-xl bg-black">
      <img
        src={poster}
        alt=""
        onError={e => {
          e.currentTarget.onerror = null
          e.currentTarget.src = FALLBACK_POSTER_SVG
        }}
        className="absolute inset-0 h-full w-full object-cover opacity-40"
      />
      <div className="absolute inset-0 bg-gradient-to-t from-obsidian via-obsidian/60 to-obsidian/30" />
      <div className="relative flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
        <p className="font-cinema text-lg sm:text-2xl font-bold text-silver">{heading}</p>
        {children}
      </div>
    </div>
  )
}

export default function ReelPanel({ reel, title, poster, signedIn, isCreator, onSignIn }: Props) {
  if (reel.kind === 'loading') {
    return <div className="skeleton aspect-video w-full rounded-xl" role="status" aria-label="Loading reel" />
  }

  if (reel.kind === 'none') {
    return (
      <Message poster={poster} heading="No reel yet">
        <p className="max-w-md text-sm text-silver-dim">
          {isCreator
            ? 'Upload your reel below so backers can see what they are funding.'
            : 'The filmmaker has not shared a reel yet. Check back soon.'}
        </p>
      </Message>
    )
  }

  if (reel.kind === 'server' && !signedIn) {
    return (
      <Message poster={poster} heading="Sign in to watch the reel">
        <p className="max-w-md text-sm text-silver-dim">It is free. Sign in and the reel starts right here on this page.</p>
        <button
          type="button"
          onClick={onSignIn}
          className="px-5 py-2.5 rounded-xl bg-amber hover:bg-amber-bright text-ink text-sm font-semibold transition-colors cursor-pointer"
        >
          Sign in to watch
        </button>
      </Message>
    )
  }

  const note = reel.kind === 'sample' ? 'Sample reel · demo mode' : reel.kind === 'preview' ? 'Local preview' : null
  return (
    <div className="relative">
      <VideoPlayer
        src={reel.src}
        poster={poster}
        title={`${title} — reel`}
        label={note ?? 'Reel'}
        withAuth={reel.kind === 'server'}
      />
      {note && (
        <p className="mt-2 text-[11px] font-mono text-silver-faint">
          {reel.kind === 'sample'
            ? 'This is a placeholder clip so you can try the player. The real reel appears once the server is reachable.'
            : 'Only you can see this. It is replaced by the processed reel once uploading and encoding finish.'}
        </p>
      )}
    </div>
  )
}
