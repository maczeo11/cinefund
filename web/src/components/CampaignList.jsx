import { useState, useEffect } from 'react'
import { getCampaigns } from '../api'

function CampaignList({ onSelect }) {
  const [campaigns, setCampaigns] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  useEffect(() => {
    getCampaigns()
      .then(data => setCampaigns(data))
      .catch(err => setError(err.message))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <p className="center">Loading campaigns...</p>
  if (error) return <p className="center error">Error: {error}</p>
  if (campaigns.length === 0) return <p className="center">No campaigns yet. Create one!</p>

  return (
    <div className="campaign-grid">
      {campaigns.map(c => {
        const percent = c.goal_amount > 0
          ? Math.min(100, Math.round((c.raised_amount / c.goal_amount) * 100))
          : 0

        return (
          <div key={c.id} className="campaign-card" onClick={() => onSelect(c.id)}>
            <div className="card-header">
              <span className={`badge ${c.status === 'LIVE' ? 'badge-live' : ''}`}>
                {c.status}
              </span>
              <span className="category">{c.category}</span>
            </div>
            <h3>{c.title}</h3>
            <p className="tagline">{c.tagline}</p>
            <div className="progress-bar">
              <div className="progress-fill" style={{ width: `${percent}%` }} />
            </div>
            <div className="card-stats">
              <span>₹{(c.raised_amount / 100).toLocaleString()} / ₹{(c.goal_amount / 100).toLocaleString()}</span>
              <span>{c.backer_count} backers</span>
            </div>
          </div>
        )
      })}
    </div>
  )
}

export default CampaignList
