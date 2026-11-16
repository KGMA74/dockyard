import { useState } from 'react'
import { isAuthenticated } from './api'
import AppShell from './components/AppShell'
import LoginPage from './pages/LoginPage'

// The URL is left untouched across login: opening a deep link while signed
// out lands on the sign-in form, then on the page that was asked for.
export default function App() {
  const [authed, setAuthed] = useState(isAuthenticated)

  return authed ? <AppShell onLogout={() => setAuthed(false)} /> : <LoginPage onLogin={() => setAuthed(true)} />
}
