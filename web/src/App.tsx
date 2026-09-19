import { BrowserRouter, Routes, Route, useNavigate, useParams } from 'react-router-dom'
import Navbar from './components/Navbar.tsx'
import CampaignList from './components/CampaignList.tsx'
import CampaignForm from './components/CampaignForm.tsx'
import CampaignDetail from './components/CampaignDetail.tsx'
import Dashboard from './components/Dashboard.tsx'
import Pledges from './components/Pledges.tsx'
import Watch from './components/Watch.tsx'
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
    <div className="app bg-[#0C0E14] text-silver min-h-screen flex flex-col font-sans selection:bg-amber selection:text-ink">
      <Navbar onHome={() => nav('/')} onCreate={() => nav('/create')} />

      <main className="content flex-1 px-4 sm:px-6 py-6 max-w-7xl mx-auto w-full">
        <Routes>
          <Route path="/" element={<ListPage />} />
          <Route path="/campaigns/:id" element={<DetailPage />} />
          <Route path="/create" element={<CreatePage />} />
          <Route path="/dashboard" element={<Dashboard />} />
          <Route path="/pledges" element={<Pledges />} />
          <Route path="/watch" element={<Watch />} />
        </Routes>
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
