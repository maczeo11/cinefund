import { Component, Suspense, lazy, useEffect, type ErrorInfo, type ReactNode } from 'react'
import { BrowserRouter, Link, Routes, Route, useLocation, useNavigate, useParams } from 'react-router-dom'
import Navbar from './components/Navbar.tsx'
import CampaignList from './components/CampaignList.tsx'
import DemoBanner from './components/DemoBanner.tsx'
import { usePageTitle } from './usePageTitle'
import './App.css'

// The landing page is eager; everything else (and the heavy bits behind it —
// the video player, Firebase, the upload form) loads when first visited.
const CampaignForm = lazy(() => import('./components/CampaignForm.tsx'))
const CampaignDetail = lazy(() => import('./components/CampaignDetail.tsx'))
const Dashboard = lazy(() => import('./components/Dashboard.tsx'))
const Pledges = lazy(() => import('./components/Pledges.tsx'))
const Watch = lazy(() => import('./components/Watch.tsx'))

function ListPage() {
  const nav = useNavigate()
  return <CampaignList onSelect={id => nav(`/campaigns/${id}`)} onStartCampaign={() => nav('/create')} />
}

// A new page should open at its top, but in-page links (#films, #watch) and
// back/forward navigation keep their own scroll behaviour.
function ScrollToTop() {
  const { pathname, hash } = useLocation()
  useEffect(() => {
    if (!hash) window.scrollTo(0, 0)
  }, [pathname, hash])
  return null
}

function DetailPage() {
  const { id } = useParams<{ id: string }>()
  const nav = useNavigate()
  if (!id) return null
  return <CampaignDetail id={id} onBack={() => nav('/')} />
}

function CreatePage() {
  const nav = useNavigate()
  return <CampaignForm onDone={() => nav('/')} />
}

function NotFound() {
  usePageTitle('Page not found')
  return (
    <div className="py-24 text-center space-y-3">
      <p className="font-cinema text-2xl text-silver">Page not found</p>
      <p className="text-sm text-silver-dim">That page doesn&rsquo;t exist or has moved.</p>
      <Link to="/" className="inline-block mt-2 px-4 py-2 rounded-lg bg-amber text-ink text-xs font-semibold">
        Browse films
      </Link>
    </div>
  )
}

function Loading() {
  return (
    <div className="py-16 text-center text-sm font-mono text-silver-dim" role="status">
      <span className="inline-block animate-spin mr-2" aria-hidden="true">✦</span>
      Loading…
    </div>
  )
}

type BoundaryProps = { children: ReactNode; resetKey: string }

// Catches render errors below it. `resetKey` is the route path, so navigating
// away from a crashed page clears the error.
class ErrorBoundary extends Component<BoundaryProps, { error: Error | null; key: string }> {
  state = { error: null as Error | null, key: this.props.resetKey }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  static getDerivedStateFromProps(props: BoundaryProps, state: { error: Error | null; key: string }) {
    return props.resetKey !== state.key ? { error: null, key: props.resetKey } : null
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('Unhandled UI error', error, info.componentStack)
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="py-24 text-center space-y-3" role="alert">
        <p className="font-cinema text-2xl text-silver">Something snapped on the reel</p>
        <p className="text-sm text-silver-dim">This page hit an unexpected error. Your session and pledges are safe.</p>
        <button
          type="button"
          onClick={() => window.location.reload()}
          className="mt-2 px-4 py-2 rounded-lg bg-amber text-ink text-xs font-semibold cursor-pointer"
        >
          Reload
        </button>
      </div>
    )
  }
}

function AppShell() {
  const nav = useNavigate()
  const { pathname } = useLocation()

  return (
    <div className="app cinema-grade text-silver min-h-screen flex flex-col font-sans selection:bg-amber selection:text-ink">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:m-2 focus:px-3 focus:py-2 focus:rounded-lg focus:bg-amber focus:text-ink"
      >
        Skip to content
      </a>
      <ScrollToTop />
      <Navbar onHome={() => nav('/')} onCreate={() => nav('/create')} />
      <DemoBanner />

      <main id="main" className="content flex-1 px-4 sm:px-6 py-6 max-w-7xl mx-auto w-full">
        <ErrorBoundary resetKey={pathname}>
          <Suspense fallback={<Loading />}>
            <Routes>
              <Route path="/" element={<ListPage />} />
              <Route path="/campaigns/:id" element={<DetailPage />} />
              <Route path="/create" element={<CreatePage />} />
              <Route path="/status" element={<Dashboard />} />
              <Route path="/dashboard" element={<Dashboard />} />
              <Route path="/pledges" element={<Pledges />} />
              <Route path="/watch" element={<Watch />} />
              <Route path="*" element={<NotFound />} />
            </Routes>
          </Suspense>
        </ErrorBoundary>
      </main>

      <footer className="border-t border-white/[0.08] mt-16 bg-celluloid/40 px-4 sm:px-6 py-10 text-sm text-silver-dim">
        <div className="max-w-7xl mx-auto grid gap-8 sm:grid-cols-[1.4fr_1fr_1fr]">
          <div>
            <p className="font-cinema font-bold text-lg text-silver">
              CINE<span className="text-amber">FUND</span>
            </p>
            <p className="mt-2 max-w-xs leading-relaxed">
              Fund independent films and watch them come to life. Back a project, and get your money back if it falls short.
            </p>
          </div>
          <nav aria-label="Explore" className="space-y-2">
            <p className="label">Explore</p>
            <Link to="/" className="block hover:text-silver">Browse films</Link>
            <Link to="/watch" className="block hover:text-silver">Watch</Link>
            <Link to="/create" className="block hover:text-silver">Start a campaign</Link>
          </nav>
          <nav aria-label="Account" className="space-y-2">
            <p className="label">Account</p>
            <Link to="/pledges" className="block hover:text-silver">My pledges</Link>
            <Link to="/status" className="block hover:text-silver">System status</Link>
          </nav>
        </div>
        <p className="max-w-7xl mx-auto mt-8 pt-6 border-t border-white/[0.06] text-xs text-silver-faint">
          © {new Date().getFullYear()} CineFund. Payments are processed securely by Razorpay.
        </p>
      </footer>
    </div>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <AppShell />
    </BrowserRouter>
  )
}
