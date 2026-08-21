// The API speaks paise everywhere. Converting at the edge, once, keeps the
// /100 out of the components.

const INR = new Intl.NumberFormat('en-IN', {
  style: 'currency',
  currency: 'INR',
  maximumFractionDigits: 0,
})

export const rupees = paise => INR.format((paise || 0) / 100)

export const toPaise = rupeeInput => Math.round(parseFloat(rupeeInput) * 100)

export function percentOf(raised, goal) {
  if (!goal || goal <= 0) return 0
  return Math.min(100, Math.round((raised / goal) * 100))
}

export function daysLeft(deadline) {
  if (!deadline) return null
  return Math.ceil((new Date(deadline) - new Date()) / 86400000)
}
