function Navbar({ onHome, onCreate }) {
  return (
    <header className="masthead">
      <button className="masthead-brand" onClick={onHome}>
        Cine<em>Fund</em>
      </button>
      <nav className="masthead-nav">
        <button className="link" onClick={onHome}>Index</button>
        <button className="link link-accent" onClick={onCreate}>Submit a film</button>
      </nav>
    </header>
  )
}

export default Navbar
