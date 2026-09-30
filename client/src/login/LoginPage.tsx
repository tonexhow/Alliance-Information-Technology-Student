import { useEffect, useState, type FormEvent } from 'react'
import './LoginPage.css'
import { Icon } from '../global/Icons'
import { Footer } from '../global/Footer'

type ThemeMode = 'light' | 'dark'
type RecoveryStep = 'signin' | 'request' | 'confirm'

function safeReturnPath() {
  const value = new URLSearchParams(window.location.search).get('return') || '/member'
  return value.startsWith('/') && !value.startsWith('//') ? value : '/member'
}

export default function LoginPage() {
  const [theme, setTheme] = useState<ThemeMode>(() => localStorage.getItem('aits-theme') === 'dark' ? 'dark' : 'light')
  const [step, setStep] = useState<RecoveryStep>('signin')
  const [identifier, setIdentifier] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [showNewPassword, setShowNewPassword] = useState(false)
  const [showConfirmPassword, setShowConfirmPassword] = useState(false)
  const [code, setCode] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    localStorage.setItem('aits-theme', theme)
  }, [theme])
  useEffect(() => {
    let active=true
    void fetch('/api/member/session',{credentials:'include',cache:'no-store'})
      .then(r=>r.ok?r.json():null)
      .then(session=>{if(active&&session?.authenticated)window.location.replace(safeReturnPath())})
      .catch(()=>undefined)
    return()=>{active=false}
  }, [])

  async function post(path: string, body: unknown) {
    const response = await fetch(path, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) { const detail=payload.restricted?`${payload.error||'This member account is restricted.'}${payload.reason?` Reason: ${payload.reason}.`:''}${payload.permanent?' Restriction: permanent.':payload.lifts_at?` Lifts: ${new Date(payload.lifts_at).toLocaleString()}.`:''}`:(payload.error||'Please try again.'); throw new Error(detail) }
    return payload
  }

  async function signIn(event: FormEvent) {
    event.preventDefault()
    if (!identifier.trim() || !password) { setMessage('Enter your username or email and password.'); return }
    setBusy(true)
    setMessage('Signing you in…')
    try {
      const payload = await post('/api/member/login', { identifier: identifier.trim(), password })
      try { localStorage.setItem('aits-member-profile-cache', JSON.stringify(payload.member)) } catch { /* optional */ }
      setMessage('Welcome back. Opening your member space…')
      window.setTimeout(() => { window.location.replace(safeReturnPath()) }, 120)
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Sign in was unsuccessful.')
    } finally {
      setBusy(false)
    }
  }

  async function requestRecovery(event: FormEvent) {
    event.preventDefault()
    if (!identifier.trim()) return setMessage('Enter your username or email first.')
    setBusy(true)
    try {
      const payload = await post('/api/member/recovery/request', { identifier: identifier.trim() })
      setMessage(payload.message || 'If an email is connected to this account, a verification code has been sent.')
      setStep('confirm')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Could not start account recovery.')
    } finally {
      setBusy(false)
    }
  }

  async function finishRecovery(event: FormEvent) {
    event.preventDefault()
    if (!code.trim()) return setMessage('Enter the verification code from your email.')
    if (newPassword.length < 10) return setMessage('Use a new password with at least 10 characters.')
    if (newPassword !== confirmPassword) return setMessage('The new passwords do not match.')
    setBusy(true)
    try {
      const payload = await post('/api/member/recovery/confirm', {
        identifier: identifier.trim(),
        code: code.trim(),
        newPassword,
      })
      setMessage(payload.message || 'Your password has been updated. You can sign in now.')
      setPassword('')
      setCode('')
      setNewPassword('')
      setConfirmPassword('')
      setStep('signin')
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Could not reset your password.')
    } finally {
      setBusy(false)
    }
  }

  return <div className="login-page">
    <header className="login-topbar">
      <a href="/" className="login-back"><Icon name="back" size={16}/> <span>AITS</span></a>
      <button className="login-theme" type="button" onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')} aria-label="Toggle theme">
        <Icon name={theme === 'light' ? 'moon' : 'sun'} size={16}/> <span>{theme === 'light' ? 'Dark' : 'Light'}</span>
      </button>
    </header>

    <main className="login-layout">
      <section className="login-intro" aria-hidden="true">
        <div className="login-logo"><img src="/logo/aits.png" alt="AITS"/></div>
        <span>MEMBER COMMUNITY</span>
        <h1>Your AITS space starts here.</h1>
        <p>Sign in to your AITS member account.</p>
        <div className="login-highlights">
          <span>Community posts</span><span>Member directory</span><span>Notifications</span><span>Profile & access</span>
        </div>
      </section>

      <section className="login-card">
        <div className="login-card-heading">
          <span>{step === 'signin' ? 'MEMBER SIGN IN' : 'ACCOUNT RECOVERY'}</span>
          <h2>{step === 'signin' ? 'Welcome back' : step === 'request' ? 'Recover your account' : 'Set a new password'}</h2>
          <p>{step === 'signin'
            ? 'Use your AITS member username or email and password.'
            : step === 'request'
              ? 'Enter your username or email. We’ll send a verification code to the email connected to your member account.'
              : 'Enter the verification code from your email, then choose a new password.'}</p>
        </div>

        {step === 'signin' && <form onSubmit={signIn} className="login-form">
          <label>Username or email<input autoFocus autoComplete="username" value={identifier} onChange={(e) => setIdentifier(e.target.value)} placeholder="Enter your account" /></label>
          <label>Password<div className="password-field"><input type={showPassword ? 'text' : 'password'} autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Enter your password" /><button type="button" aria-label={showPassword ? 'Hide password' : 'Show password'} onClick={() => setShowPassword(!showPassword)}><Icon name={showPassword ? 'eyeOff' : 'eye'} size={16}/></button></div></label>
          <button type="button" className="login-link" onClick={() => { setStep('request'); setMessage('') }}>Forgot password?</button>
          <button className="login-primary" disabled={busy}>{busy ? 'Signing in…' : 'Sign in'}</button>
        </form>}

        {step === 'request' && <form onSubmit={requestRecovery} className="login-form">
          <label>Username or email<input autoFocus autoComplete="username" value={identifier} onChange={(e) => setIdentifier(e.target.value)} placeholder="Enter your account" /></label>
          <button className="login-primary" disabled={busy || !identifier.trim()}>{busy ? 'Sending…' : 'Send verification code'}</button>
          <button type="button" className="login-secondary" onClick={() => { setStep('signin'); setMessage('') }}>Back to sign in</button>
        </form>}

        {step === 'confirm' && <form onSubmit={finishRecovery} className="login-form">
          <label>Verification code<input autoFocus inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))} placeholder="6-digit code" /></label>
          <label>New password<div className="password-field"><input type={showNewPassword ? 'text' : 'password'} autoComplete="new-password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} placeholder="At least 10 characters" /><button type="button" aria-label={showNewPassword ? 'Hide new password' : 'Show new password'} onClick={() => setShowNewPassword(!showNewPassword)}><Icon name={showNewPassword ? 'eyeOff' : 'eye'} size={16}/></button></div></label>
          <label>Confirm new password<div className="password-field"><input type={showConfirmPassword ? 'text' : 'password'} autoComplete="new-password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} placeholder="Repeat your new password" /><button type="button" aria-label={showConfirmPassword ? 'Hide confirmation password' : 'Show confirmation password'} onClick={() => setShowConfirmPassword(!showConfirmPassword)}><Icon name={showConfirmPassword ? 'eyeOff' : 'eye'} size={16}/></button></div></label>
          <button className="login-primary" disabled={busy || !code.trim() || newPassword.length < 10 || !confirmPassword}>{busy ? 'Updating…' : 'Update password'}</button>
          <button type="button" className="login-secondary" onClick={() => { setStep('request'); setMessage('') }}>Send another code</button>
        </form>}

        {message && <div className="login-message" role="status">{message}</div>}
        <div className="login-public-link">Not signing in as a member? <a href="/">Return to the public AITS site</a>.</div>
      </section>
    </main>
    <Footer/>
  </div>
}
