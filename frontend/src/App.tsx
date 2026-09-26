import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { IncidentsPage } from './pages/IncidentsPage'
import { JournalPage } from './pages/JournalPage'
import { NetworkPage } from './pages/NetworkPage'
import { RoutePage } from './pages/RoutePage'

export function App() {
  return <Routes><Route element={<AppShell />}><Route index element={<NetworkPage />} /><Route path="routes/:routeId" element={<RoutePage />} /><Route path="incidents" element={<IncidentsPage />} /><Route path="journal" element={<JournalPage />} /><Route path="*" element={<Navigate to="/" replace />} /></Route></Routes>
}
