import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { NetworkPage } from './pages/NetworkPage'

export function App() {
  return <Routes><Route element={<AppShell />}><Route index element={<NetworkPage />} /><Route path="*" element={<Navigate to="/" replace />} /></Route></Routes>
}
