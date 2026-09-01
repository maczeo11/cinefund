type Props = {
  onHome: () => void
  onCreate: () => void
}

export default function Navbar({ onHome, onCreate }: Props) {
  return (
    <header className="masthead sticky top-0 z-10 bg-ink/95 backdrop-blur supports-[backdrop-filter]:bg-ink/80">
      <button className="masthead-brand" onClick={onHome} aria-label="Home">
        Cine<em>Fund</em>
      </button>
      <nav className="masthead-nav items-center">
        <button className="link" onClick={onHome}>
          Index
        </button>
        <button
          className="link link-accent bg-paper text-ink px-3 py-1.5 hover:bg-accent hover:text-paper hover:border-accent transition-colors"
          onClick={onCreate}
        >
          Submit a film
        </button>
        <span className="hidden md:inline-flex tw-badge">TS + Tailwind • React 19</span>
      </nav>
    </header>
  )
}
