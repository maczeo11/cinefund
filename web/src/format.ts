// Paise ↔ INR — API speaks paise (int), UI shows rupees. Keep /100 at edge once.
const INR = new Intl.NumberFormat('en-IN', {
  style: 'currency',
  currency: 'INR',
  maximumFractionDigits: 0,
})

export const rupees = (paise: number | null | undefined): string => INR.format(((paise as number) || 0) / 100)

export const toPaise = (rupeeInput: string | number): number => Math.round(parseFloat(String(rupeeInput)) * 100)

export function percentOf(raised: number | null | undefined, goal: number | null | undefined): number {
  if (!goal || (goal as number) <= 0) return 0
  return Math.min(100, Math.round(((raised as number) / (goal as number)) * 100))
}

export const formatDate = (iso: string | null | undefined): string =>
  iso ? new Date(iso).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' }) : ''

export function daysLeft(deadline?: string | null): number | null {
  if (!deadline) return null
  return Math.ceil((new Date(deadline).getTime() - Date.now()) / 86400000)
}

export type Campaign = { raised_amount: number; goal_amount: number; deadline?: string }

export type Phase = 'live' | 'funded' | 'ended' | 'draft'

/**
 * What a visitor should be told about a campaign, derived from its status and
 * deadline. A campaign past its deadline reads as funded or ended even if the
 * server hasn't swept its status yet, so nobody is invited to back a closed one.
 */
export function campaignPhase(c: {
  status: string
  raised_amount: number
  goal_amount: number
  deadline?: string | null
}): Phase {
  switch (c.status) {
    case 'DRAFT':
    case 'IN_REVIEW':
      return 'draft'
    case 'FUNDED':
    case 'IN_PRODUCTION':
    case 'RELEASED':
      return 'funded'
    case 'LIVE': {
      const days = daysLeft(c.deadline)
      if (days !== null && days <= 0) return c.raised_amount >= c.goal_amount ? 'funded' : 'ended'
      return 'live'
    }
    default: // FAILED, CANCELLED, CLOSED, anything unknown
      return 'ended'
  }
}

export const PHASE_LABEL: Record<Phase, string> = {
  live: 'LIVE',
  funded: 'FUNDED',
  ended: 'ENDED',
  draft: 'DRAFT',
}

/** Remaining paise to the goal, never negative. */
export const remainingToGoal = (raised: number | null | undefined, goal: number | null | undefined): number =>
  Math.max(0, ((goal as number) || 0) - ((raised as number) || 0))
