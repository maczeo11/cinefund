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

export function daysLeft(deadline?: string | null): number | null {
  if (!deadline) return null
  return Math.ceil((new Date(deadline).getTime() - Date.now()) / 86400000)
}

export type Campaign = { raised_amount: number; goal_amount: number; deadline?: string }
