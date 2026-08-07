import { useState, useEffect } from 'react'
import { getCampaign, createPledge } from '../api'

// hardcoded, no auth yet
const BACKER_ID = '00000000-0000-0000-0000-000000000002'

function CampaignDetail({ id, onBack }) {
  const [campaign, setCampaign] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  // pledge form state
  const [amount, setAmount] = useState('')
  const [message, setMessage] = useState('')
  const [pledging, setPledging] = useState(false)
  const [pledgeResult, setPledgeResult] = useState(null)
  const [pledgeError, setPledgeError] = useState(null)

  useEffect(() => {
    setLoading(true)
    getCampaign(id)
      .then(data => setCampaign(data))
      .catch(err => setError(err.message))
      .finally(() => setLoading(false))
  }, [id])

  async function handlePledge(e) {
    e.preventDefault()
    setPledgeError(null)
    setPledgeResult(null)

    if (!amount || parseFloat(amount) < 1) {
      setPledgeError('Enter an amount (min ₹1)')
      return
    }

    setPledging(true)
    try {
      const result = await createPledge(id, {
        backer_id: BACKER_ID,
        amount: Math.round(parseFloat(amount) * 100), // rupees to paise
        message: message || '',
        anonymous: false,
      })
      setPledgeResult(result)
      setAmount('')
      setMessage('')

      // refresh campaign data to see updated raised amount
      const updated = await getCampaign(id)
      setCampaign(updated)
    } catch (err) {
      setPledgeError(err.message)
    } finally {
      setPledging(false)
    }
  }

  if (loading) return <p className="center">Loading...</p>
  if (error) return <p className="center error">{error}</p>
  if (!campaign) return <p className="center">Campaign not found</p>

  const percent = campaign.goal_amount > 0
    ? Math.min(100, Math.round((campaign.raised_amount / campaign.goal_amount) * 100))
    : 0

  const deadlineStr = campaign.deadline
    ? new Date(campaign.deadline).toLocaleDateString()
    : 'No deadline'

  return (
    <div className="detail-container">
      <button onClick={onBack} className="back-btn">&larr; Back</button>

      <div className="detail-header">
        <h2>{campaign.title}</h2>
        <span className={`badge ${campaign.status === 'LIVE' ? 'badge-live' : ''}`}>
          {campaign.status}
        </span>
      </div>

      <p className="tagline">{campaign.tagline}</p>

      <div className="detail-stats">
        <div className="stat">
          <span className="stat-value">₹{(campaign.raised_amount / 100).toLocaleString()}</span>
          <span className="stat-label">raised of ₹{(campaign.goal_amount / 100).toLocaleString()}</span>
        </div>
        <div className="stat">
          <span className="stat-value">{campaign.backer_count}</span>
          <span className="stat-label">backers</span>
        </div>
        <div className="stat">
          <span className="stat-value">{percent}%</span>
          <span className="stat-label">funded</span>
        </div>
        <div className="stat">
          <span className="stat-value">{deadlineStr}</span>
          <span className="stat-label">deadline</span>
        </div>
      </div>

      <div className="progress-bar big">
        <div className="progress-fill" style={{ width: `${percent}%` }} />
      </div>

      <div className="detail-info">
        <span>Category: {campaign.category}</span>
      </div>

      {campaign.status === 'LIVE' && (
        <div className="pledge-section">
          <h3>Back this project</h3>
          <form onSubmit={handlePledge}>
            <label>
              Amount (₹)
              <input
                type="number"
                min="1"
                value={amount}
                onChange={e => setAmount(e.target.value)}
                placeholder="500"
              />
            </label>
            <label>
              Message (optional)
              <input
                type="text"
                value={message}
                onChange={e => setMessage(e.target.value)}
                placeholder="Good luck with the film!"
              />
            </label>
            {pledgeError && <p className="error">{pledgeError}</p>}
            {pledgeResult && (
              <div className="pledge-success">
                Pledge created! Order ID: {pledgeResult.order_id}
                <br />Status: {pledgeResult.status}
              </div>
            )}
            <button type="submit" disabled={pledging} className="btn-primary">
              {pledging ? 'Processing...' : 'Pledge'}
            </button>
          </form>
        </div>
      )}
    </div>
  )
}

export default CampaignDetail
