import { useCallback, useState } from 'react'
import { isAuthenticated } from './api'
import AppShell from './components/AppShell'
import LoginPage from './pages/LoginPage'

// The URL is left untouched across login: opening a deep link while signed
// out lands on the sign-in form, then on the page that was asked for.
//
// Coming back from single sign-on (#oidc_code / #oidc_error in the URL) always
// goes through the login page, even with an older session still stored: that
// is where the new sign-in is completed — or its failure explained.
export default function App() {
  const [authed, setAuthed] = useState(() => isAuthenticated() && !/[#&]oidc_(code|error)=/.test(window.location.hash))
  const onLogin = useCallback(() => setAuthed(true), [])

  return authed ? <AppShell onLogout={() => setAuthed(false)} /> : <LoginPage onLogin={onLogin} />
}
