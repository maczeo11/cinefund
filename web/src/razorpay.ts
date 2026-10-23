// Razorpay Checkout must be loaded from Razorpay's own origin (it can't be
// bundled), so it is fetched only when someone is about to pay instead of on
// every page view.

const CHECKOUT_SRC = 'https://checkout.razorpay.com/v1/checkout.js'

export type RazorpayCheckout = new (options: Record<string, unknown>) => {
  open: () => void
  on: (event: string, handler: (res: { error?: { description?: string } }) => void) => void
}

declare global {
  interface Window {
    Razorpay?: RazorpayCheckout
  }
}

let loading: Promise<RazorpayCheckout> | null = null

export function loadRazorpay(): Promise<RazorpayCheckout> {
  if (window.Razorpay) return Promise.resolve(window.Razorpay)
  if (loading) return loading
  loading = new Promise<RazorpayCheckout>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = CHECKOUT_SRC
    script.async = true
    script.onload = () => (window.Razorpay ? resolve(window.Razorpay) : reject(new Error('Razorpay failed to initialise.')))
    script.onerror = () => {
      script.remove()
      loading = null // allow a retry
      reject(new Error('Could not load the Razorpay checkout. Check your connection and try again.'))
    }
    document.head.appendChild(script)
  })
  return loading
}
