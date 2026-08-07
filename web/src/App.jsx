import { useState } from 'react'
import Navbar from './components/Navbar'
import CampaignList from './components/CampaignList'
import CampaignForm from './components/CampaignForm'
import CampaignDetail from './components/CampaignDetail'
import './App.css'

function App() {
  const [page, setPage] = useState('list')
  const [selectedId, setSelectedId] = useState(null)

  function goToDetail(id) {
    setSelectedId(id)
    setPage('detail')
  }

  return (
    <div className="app">
      <Navbar
        onHome={() => setPage('list')}
        onCreate={() => setPage('create')}
      />
      <main className="content">
        {page === 'list' && <CampaignList onSelect={goToDetail} />}
        {page === 'create' && <CampaignForm onDone={() => setPage('list')} />}
        {page === 'detail' && <CampaignDetail id={selectedId} onBack={() => setPage('list')} />}
      </main>
    </div>
  )
}

export default App
