import { BrowserRouter, Routes, Route, useNavigate, useLocation, useParams } from 'react-router-dom'
import Navbar from './components/Navbar.tsx'
import CampaignList from './components/CampaignList.tsx'
import CampaignForm from './components/CampaignForm.tsx'
import CampaignDetail from './components/CampaignDetail.tsx'
import Dashboard from './components/Dashboard.tsx'
import Pledges from './components/Pledges.tsx'
import Watch from './components/Watch.tsx'
import HealthBar from './components/HealthBar.tsx'
import './App.css'

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

function AppShell() {
  const nav = useNavigate()
  const location = useLocation()

  const tabs = [
    { path: '/', label: 'Film Vault', tag: '35mm Reels' },
    { path: '/watch', label: 'Screening Room', tag: 'HLS Stream' },
    { path: '/pledges', label: 'Escrow Ledger', tag: 'Double-Entry' },
    { path: '/dashboard', label: 'System Telemetry', tag: 'Kafka & Queues' },
  ]

  return (
    <div className="app bg-obsidian text-silver min-h-screen flex flex-col font-sans">
      <Navbar onHome={() => nav('/')} onCreate={() => nav('/create')} />

      {/* Cinema Studio Secondary Toolbar */}
      <div className="flex gap-2 sm:gap-4 px-[var(--gutter)] py-2.5 border-b border-white/[0.06] bg-celluloid/40 text-xs font-mono overflow-x-auto items-center">
        {tabs.map(tab => {
          const isActive = location.pathname === tab.path || (tab.path === '/' && location.pathname.startsWith('/campaigns/'))
          return (
            <button
              key={tab.path}
              onClick={() => nav(tab.path)}
              className={`px-3 py-1.5 rounded-md transition-all flex items-center gap-2 whitespace-nowrap ${
                isActive
                  ? 'bg-amber/15 text-amber border border-amber/30 font-semibold shadow-[0_0_10px_rgba(229,169,60,0.15)]'
                  : 'text-silver-dim hover:text-white hover:bg-white/[0.04]'
              }`}
            >
              <span>{tab.label}</span>
              <span className="text-[10px] text-silver-faint opacity-80 hidden md:inline">({tab.tag})</span>
            </button>
          )
        })}
        <span className="ml-auto hidden lg:inline text-[11px] text-silver-faint font-mono">
          Strict GOP 48 Transcode • Skip-Locked Outbox • Balanced Paise Ledgers
        </span>
      </div>

      <HealthBar />

      <main className="content flex-1 px-[var(--gutter)] py-6 max-w-7xl mx-auto w-full">
        <Routes>
          <Route path="/" element={<ListPage />} />
          <Route path="/campaigns/:id" element={<DetailPage />} />
          <Route path="/create" element={<CreatePage />} />
          <Route path="/dashboard" element={<Dashboard />} />
          <Route path="/pledges" element={<Pledges />} />
          <Route path="/watch" element={<Watch />} />
        </Routes>
      </main>

      <footer className="border-t border-white/[0.08] mt-16 py-8 px-[var(--gutter)] bg-celluloid/30 text-center font-mono text-xs text-silver-dim">
        <div className="flex flex-col sm:flex-row items-center justify-between gap-4 max-w-7xl mx-auto">
          <div className="flex items-center gap-2">
            <span className="font-cinema font-bold text-silver">CINE<span className="text-amber">FUND</span></span>
            <span className="text-silver-faint">·</span>
            <span>Celluloid Crowdfunding & HLS Streaming Engine</span>
          </div>
          <p className="text-[11px] text-silver-faint">
            Go 1.26 + PostgreSQL 16 + Redis + Kafka + MinIO/S3 + FFmpeg • React 19 + TypeScript + Tailwind
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
