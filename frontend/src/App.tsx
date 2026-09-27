import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { IncidentsPage } from './pages/IncidentsPage'
import { NetworkPage } from './pages/NetworkPage'
import { IncidentHistoryPage } from './pages/IncidentHistoryPage'

export function App() {
  return <Routes><Route element={<AppShell />}><Route index element={<NetworkPage />} /><Route path="incidents" element={<IncidentsPage />} /><Route path="incidents/:incidentId" element={<IncidentsPage />} /><Route path="incidents/history" element={<IncidentHistoryPage />} /><Route path="incidents/history/:incidentId" element={<IncidentHistoryPage />} /><Route path="*" element={<Navigate to="/" replace />} /></Route></Routes>
}
