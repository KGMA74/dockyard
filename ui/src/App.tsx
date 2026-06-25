import { useState } from 'react'
import { isAuthenticated } from './api'
import LoginPage from './pages/LoginPage'
import Dashboard from './pages/Dashboard'

export default function App() {
  const [authed, setAuthed] = useState(isAuthenticated)

  return authed
    ? <Dashboard onLogout={() => setAuthed(false)} />
    : <LoginPage onLogin={() => setAuthed(true)} />
}
