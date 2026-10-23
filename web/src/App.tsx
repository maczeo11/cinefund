import { Component, Suspense, lazy, type ErrorInfo, type ReactNode } from 'react'
import { BrowserRouter, Link, Routes, Route, useLocation, useNavigate, useParams } from 'react-router-dom'
import Navbar from './components/Navbar.tsx'
import CampaignList from './components/CampaignList.tsx'
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
  return <CampaignList onSelect={id => nav(`/campaigns/${id}`)} />
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
  return (
    <div className="py-24 text-center space-y-3">
      <p className="font-cinema text-2xl text-silver">Reel not found</p>
      <p className="text-sm text-silver-dim">That page doesn't exist.</p>
      <Link to="/" className="inline-block mt-2 px-4 py-2 rounded-lg bg-amber text-ink text-xs font-semibold">
        Back to the Film Vault
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
    <div className="app bg-[#0C0E14] text-silver min-h-screen flex flex-col font-sans selection:bg-amber selection:text-ink">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:m-2 focus:px-3 focus:py-2 focus:rounded-lg focus:bg-amber focus:text-ink"
      >
        Skip to content
      </a>
      <Navbar onHome={() => nav('/')} onCreate={() => nav('/create')} />

      <main id="main" className="content flex-1 px-4 sm:px-6 py-6 max-w-7xl mx-auto w-full">
        <ErrorBoundary resetKey={pathname}>
          <Suspense fallback={<Loading />}>
            <Routes>
              <Route path="/" element={<ListPage />} />
              <Route path="/campaigns/:id" element={<DetailPage />} />
              <Route path="/create" element={<CreatePage />} />
              <Route path="/dashboard" element={<Dashboard />} />
              <Route path="/pledges" element={<Pledges />} />
              <Route path="/watch" element={<Watch />} />
              <Route path="*" element={<NotFound />} />
            </Routes>
          </Suspense>
        </ErrorBoundary>
      </main>

      <footer className="border-t border-white/[0.08] mt-16 py-8 px-4 sm:px-6 bg-[#12151E]/40 text-center text-xs text-silver-dim">
        <div className="flex flex-col sm:flex-row items-center justify-between gap-4 max-w-7xl mx-auto">
          <div className="flex items-center gap-2">
            <span className="font-cinema font-bold text-silver">
              CINE<span className="text-amber">FUND</span>
            </span>
            <span className="text-silver-faint">·</span>
            <span>Celluloid Crowdfunding & HLS Streaming</span>
          </div>
          <p className="text-[11px] text-silver-faint font-mono">
            Go 1.26 + PostgreSQL + Redis + AWS S3 + HLS Streaming • React 19 + TypeScript
          </p>
        </div>
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
