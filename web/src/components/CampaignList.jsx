import { useState, useEffect } from 'react'
import { getCampaigns } from '../api'
import { rupees, percentOf, daysLeft } from '../format'

const CATEGORIES = ['ALL', 'DRAMA', 'COMEDY', 'DOCUMENTARY', 'ANIMATION', 'HORROR', 'SCIFI', 'EXPERIMENTAL']

function CampaignList({ onSelect }) {
  const [campaigns, setCampaigns] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [category, setCategory] = useState('ALL')

  useEffect(() => {
    getCampaigns()
      .then(data => setCampaigns(data || []))
      .catch(err => setError(err.message))
      .finally(() => setLoading(false))
  }, [])

  const shown = campaigns.filter(
    c => category === 'ALL' || c.category?.toUpperCase() === category,
  )

  return (
    <div>
      <div className="index-head">
        <h1>
          Films looking<br />for their <em>backers</em>
        </h1>
        <p>
          Independent shorts and features, funded in the open. Every rupee sits in
          escrow until the campaign closes.
        </p>
      </div>

      <div className="filters">
        {CATEGORIES.map(cat => (
          <button
            key={cat}
            className="filter"
            aria-pressed={category === cat}
            onClick={() => setCategory(cat)}
          >
            {cat === 'ALL' ? 'Everything' : cat}
          </button>
        ))}
      </div>

      {loading && <p className="status">Loading the index…</p>}
      {error && <p className="status notice">{error}</p>}
      {!loading && !error && shown.length === 0 && (
        <p className="status">Nothing here yet.</p>
      )}

      <ul className="entries">
        {shown.map((c, i) => {
          const days = daysLeft(c.deadline)
          return (
            <li key={c.id} className="entry">
              <button className="entry-btn" onClick={() => onSelect(c.id)}>
                <span className="entry-ord num">{String(i + 1).padStart(2, '0')}</span>

                <span>
                  <span className="entry-title">{c.title}</span>
                  <span className="entry-tagline">{c.tagline}</span>
                  <span className="entry-meta">
                    <span className="label">{c.category}</span>
                    <span className="dot">·</span>
                    <span className="label">{c.status}</span>
                    {days !== null && (
                      <>
                        <span className="dot">·</span>
                        <span className="label">
                          {days > 0 ? `${days} days left` : 'Closed'}
                        </span>
                      </>
                    )}
                  </span>
                </span>

                <span className="entry-figures">
                  <span className="entry-amount num">
                    {rupees(c.raised_amount)}
                    <small>of {rupees(c.goal_amount)}</small>
                  </span>
                  <span className="meter">
                    <span style={{ width: `${percentOf(c.raised_amount, c.goal_amount)}%` }} />
                  </span>
                </span>
              </button>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

export default CampaignList
