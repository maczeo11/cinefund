import { BrowserRouter, Routes, Route, useNavigate, useParams } from 'react-router-dom'
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
  return (
    <div className="app">
      <Navbar onHome={() => nav('/')} onCreate={() => nav('/create')} />
      <div className="flex gap-4 px-[var(--gutter)] py-3 border-b border-white/10 bg-white/[0.02] text-xs overflow-x-auto">
        <button onClick={() => nav('/')} className="label hover:text-white transition-colors">
          Discovery
        </button>
        <span className="text-white/20">·</span>
        <button onClick={() => nav('/dashboard')} className="label hover:text-white transition-colors">
          Dashboard
        </button>
        <span className="text-white/20">·</span>
        <button onClick={() => nav('/pledges')} className="label hover:text-white transition-colors">
          Pledges / Ledger
        </button>
        <span className="text-white/20">·</span>
        <button onClick={() => nav('/watch')} className="label hover:text-white transition-colors">
          Watch
        </button>
        <span className="ml-auto hidden md:inline text-white/30">Lab / Archive — 35mm + ledger — separate features, not one page</span>
      </div>
      <HealthBar />
      <main className="content">
        <Routes>
          <Route path="/" element={<ListPage />} />
          <Route path="/campaigns/:id" element={<DetailPage />} />
          <Route path="/create" element={<CreatePage />} />
          <Route path="/dashboard" element={<Dashboard />} />
          <Route path="/pledges" element={<Pledges />} />
          <Route path="/watch" element={<Watch />} />
        </Routes>
      </main>
      <footer className="border-t border-white/10 mt-12 py-6 text-center">
        <p className="label">Go + Postgres + Redis + Kafka + MinIO + FFmpeg • React + TypeScript + Tailwind • HMAC + SETNX + SKIP LOCKED + GOP 48</p>
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
