import { useState } from 'react'
import { createCampaign, publishCampaign, addTier } from '../api'
import { toPaise } from '../format'

const CATEGORIES = ['DRAMA', 'COMEDY', 'DOCUMENTARY', 'ANIMATION', 'HORROR', 'SCIFI', 'EXPERIMENTAL']
const CREATOR_ID = '00000000-0000-0000-0000-000000000001'

function CampaignForm({ onDone }) {
  const [film, setFilm] = useState({ title: '', tagline: '', synopsis: '', category: 'DRAMA', goal: '' })
  const [reward, setReward] = useState({ title: '', min: '', description: '' })
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState(null)

  const set = (obj, setter) => e => setter({ ...obj, [e.target.name]: e.target.value })

  async function submit(e) {
    e.preventDefault()
    setError(null)

    if (!film.title || !film.tagline || !film.goal) {
      setError('Title, tagline and goal are required.')
      return
    }

    setSubmitting(true)
    try {
      const campaign = await createCampaign({
        creator_id: CREATOR_ID,
        title: film.title,
        tagline: film.tagline,
        synopsis: film.synopsis || film.tagline,
        category: film.category,
        goal: toPaise(film.goal),
      })

      if (reward.title && reward.min) {
        await addTier(campaign.id, {
          title: reward.title,
          description: reward.description,
          min_amount: toPaise(reward.min),
          quantity_limit: null,
        })
      }

      await publishCampaign(campaign.id)
      onDone()
    } catch (err) {
      setError(err.message)
      setSubmitting(false)
    }
  }

  return (
    <div className="form-page">
      <h1>Submit a film</h1>

      <form onSubmit={submit} className="form">
        <div className="field">
          <label className="label" htmlFor="title">Title</label>
          <input id="title" name="title" value={film.title} onChange={set(film, setFilm)}
            placeholder="The Last Frame" />
        </div>

        <div className="field">
          <label className="label" htmlFor="tagline">Tagline</label>
          <input id="tagline" name="tagline" value={film.tagline} onChange={set(film, setFilm)}
            placeholder="One line that sells the whole thing" />
        </div>

        <div className="field">
          <label className="label" htmlFor="synopsis">Synopsis</label>
          <textarea id="synopsis" name="synopsis" rows={5} value={film.synopsis}
            onChange={set(film, setFilm)}
            placeholder="The story, the crew, where the money goes." />
        </div>

        <div className="pair">
          <div className="field">
            <label className="label" htmlFor="category">Genre</label>
            <select id="category" name="category" value={film.category} onChange={set(film, setFilm)}>
              {CATEGORIES.map(c => <option key={c} value={c}>{c}</option>)}
            </select>
          </div>
          <div className="field">
            <label className="label" htmlFor="goal">Goal (₹)</label>
            <input id="goal" name="goal" type="number" min="1000" value={film.goal}
              onChange={set(film, setFilm)} placeholder="50000" />
          </div>
        </div>

        <fieldset className="fieldset">
          <legend className="label">First reward tier — optional</legend>

          <div className="pair">
            <div className="field">
              <label className="label" htmlFor="rtitle">Tier name</label>
              <input id="rtitle" name="title" value={reward.title} onChange={set(reward, setReward)}
                placeholder="Screen credit" />
            </div>
            <div className="field">
              <label className="label" htmlFor="rmin">From (₹)</label>
              <input id="rmin" name="min" type="number" min="1" value={reward.min}
                onChange={set(reward, setReward)} placeholder="1000" />
            </div>
          </div>

          <div className="field">
            <label className="label" htmlFor="rdesc">What backers get</label>
            <input id="rdesc" name="description" value={reward.description}
              onChange={set(reward, setReward)} placeholder="Name in the end credits, digital release" />
          </div>
        </fieldset>

        {error && <p className="notice">{error}</p>}

        <div className="actions">
          <button type="submit" className="btn" disabled={submitting}>
            {submitting ? 'Publishing…' : 'Publish'}
          </button>
          <button type="button" className="link" onClick={onDone} disabled={submitting}>
            Cancel
          </button>
        </div>
      </form>
    </div>
  )
}

export default CampaignForm
