import { useState } from 'react'
import { createCampaign, publishCampaign } from '../api'

const CATEGORIES = ['DRAMA', 'COMEDY', 'DOCUMENTARY', 'ANIMATION', 'HORROR', 'SCIFI', 'EXPERIMENTAL']

// hardcoded, no auth yet
const CREATOR_ID = '00000000-0000-0000-0000-000000000001'

function CampaignForm({ onDone }) {
  const [title, setTitle] = useState('')
  const [tagline, setTagline] = useState('')
  const [synopsis, setSynopsis] = useState('')
  const [category, setCategory] = useState('DRAMA')
  const [goal, setGoal] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState(null)

  async function handleSubmit(e) {
    e.preventDefault()
    setError(null)

    if (!title || !tagline || !goal) {
      setError('Fill in all required fields')
      return
    }

    setSubmitting(true)
    try {
      const campaign = await createCampaign({
        creator_id: CREATOR_ID,
        title,
        tagline,
        synopsis: synopsis || 'No synopsis provided.',
        category,
        goal: Math.round(parseFloat(goal) * 100), // rupees to paise
      })

      // auto-publish so it shows up in the list
      await publishCampaign(campaign.id)
      onDone()
    } catch (err) {
      setError(err.message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="form-container">
      <h2>Create Campaign</h2>
      <form onSubmit={handleSubmit}>
        <label>
          Title *
          <input
            type="text"
            value={title}
            onChange={e => setTitle(e.target.value)}
            placeholder="My Awesome Film"
          />
        </label>

        <label>
          Tagline *
          <input
            type="text"
            value={tagline}
            onChange={e => setTagline(e.target.value)}
            placeholder="a short description"
          />
        </label>

        <label>
          Synopsis
          <textarea
            value={synopsis}
            onChange={e => setSynopsis(e.target.value)}
            rows={3}
            placeholder="Tell backers about your film..."
          />
        </label>

        <label>
          Category
          <select value={category} onChange={e => setCategory(e.target.value)}>
            {CATEGORIES.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
        </label>

        <label>
          Goal Amount (₹) *
          <input
            type="number"
            min="1000"
            value={goal}
            onChange={e => setGoal(e.target.value)}
            placeholder="50000"
          />
        </label>

        {error && <p className="error">{error}</p>}

        <div className="form-actions">
          <button type="button" onClick={onDone} disabled={submitting}>Cancel</button>
          <button type="submit" disabled={submitting} className="btn-primary">
            {submitting ? 'Creating...' : 'Create & Publish'}
          </button>
        </div>
      </form>
    </div>
  )
}

export default CampaignForm
