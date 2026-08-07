function Navbar({ onHome, onCreate }) {
  return (
    <nav className="navbar">
      <h1 onClick={onHome} style={{ cursor: 'pointer' }}>CineFund</h1>
      <div>
        <button onClick={onHome}>Campaigns</button>
        <button onClick={onCreate}>+ New Campaign</button>
      </div>
    </nav>
  )
}

export default Navbar
