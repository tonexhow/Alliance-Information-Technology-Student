import { useEffect, useState, type FormEvent } from 'react'
import './LoginPage.css'
import { Icon } from '../global/Icons'
import { Footer } from '../global/Footer'

type ThemeMode='light'|'dark'

export default function AdminResetPage(){
  const [theme,setTheme]=useState<ThemeMode>(()=>localStorage.getItem('aits-theme')==='dark'?'dark':'light')
  const [checking,setChecking]=useState(true)
  const [valid,setValid]=useState(false)
  const [name,setName]=useState('AITS member')
  const [message,setMessage]=useState('Checking your one-time reset link…')
  const [password,setPassword]=useState('')
  const [confirm,setConfirm]=useState('')
  const [showPassword,setShowPassword]=useState(false)
  const [showConfirm,setShowConfirm]=useState(false)
  const [busy,setBusy]=useState(false)
  const [complete,setComplete]=useState(false)
  const token=new URLSearchParams(window.location.search).get('token')?.trim()||''

  useEffect(()=>{document.documentElement.dataset.theme=theme;localStorage.setItem('aits-theme',theme)},[theme])
  useEffect(()=>{let active=true;if(!token){setChecking(false);setMessage('This password reset link is missing its one-time token.');return()=>{active=false}};void fetch(`/api/member/admin-reset?token=${encodeURIComponent(token)}`,{credentials:'include',cache:'no-store'}).then(async response=>{const payload=await response.json().catch(()=>({}));if(!active)return;if(!response.ok){setMessage(payload.error||'This password reset link is invalid or expired.');setValid(false)}else{setName(payload.name||'AITS member');setMessage('This one-time link is valid for 15 minutes. Choose a new password below.');setValid(true)}}).catch(()=>{if(active)setMessage('The reset link could not be verified. Check your connection and try again.')}).finally(()=>{if(active)setChecking(false)});return()=>{active=false}},[token])

  async function update(event:FormEvent){
    event.preventDefault()
    if(password.length<10)return setMessage('Use a password with at least 10 characters.')
    if(password!==confirm)return setMessage('The passwords do not match.')
    setBusy(true)
    try{
      const response=await fetch('/api/member/admin-reset',{method:'POST',credentials:'include',headers:{'Content-Type':'application/json'},body:JSON.stringify({token,new_password:password})})
      const payload=await response.json().catch(()=>({}))
      if(!response.ok)throw new Error(payload.error||'Password could not be updated.')
      setComplete(true);setValid(false);setPassword('');setConfirm('');setMessage(payload.message||'Password updated. You can sign in now.')
    }catch(error){setMessage(error instanceof Error?error.message:'Password could not be updated.')}finally{setBusy(false)}
  }

  return <div className="login-page admin-reset-page">
    <header className="login-topbar"><a href="/" className="login-back"><Icon name="back" size={16}/><span>AITS</span></a><button className="login-theme" type="button" onClick={()=>setTheme(theme==='light'?'dark':'light')} aria-label="Toggle theme"><Icon name={theme==='light'?'moon':'sun'} size={16}/><span>{theme==='light'?'Dark':'Light'}</span></button></header>
    <main className="login-layout">
      <section className="login-intro" aria-hidden="true"><div className="login-logo"><img src="/logo/aits.png" alt="AITS"/></div><span>SECURE ACCOUNT RECOVERY</span><h1>Choose a new password.</h1><p>This one-time link was issued by AITS Administration and becomes unusable immediately after a successful reset.</p><div className="login-highlights"><span>One-time link</span><span>15-minute expiry</span><span>Sessions revoked</span></div></section>
      <section className="login-card reset-password-card"><div className="login-card-heading"><span>PASSWORD RESET</span><h2>{complete?'Password updated':checking?'Verifying link…':valid?`Reset for ${name}`:'Link unavailable'}</h2><p>{message}</p></div>
        {valid&&!complete&&<form className="login-form" onSubmit={update}><label>New password<div className="password-field"><input autoFocus type={showPassword?'text':'password'} autoComplete="new-password" value={password} onChange={e=>setPassword(e.target.value)} placeholder="At least 10 characters"/><button type="button" aria-label={showPassword?'Hide password':'Show password'} onClick={()=>setShowPassword(v=>!v)}><Icon name={showPassword?'eyeOff':'eye'} size={16}/></button></div></label><label>Confirm password<div className="password-field"><input type={showConfirm?'text':'password'} autoComplete="new-password" value={confirm} onChange={e=>setConfirm(e.target.value)} placeholder="Repeat new password"/><button type="button" aria-label={showConfirm?'Hide confirmation password':'Show confirmation password'} onClick={()=>setShowConfirm(v=>!v)}><Icon name={showConfirm?'eyeOff':'eye'} size={16}/></button></div></label><button className="login-primary" disabled={busy||password.length<10||!confirm}>{busy?'Updating…':'UPDATE PASSWORD'}</button></form>}
        {!checking&&(!valid||complete)&&<div className="reset-link-actions"><a className="login-primary reset-signin-link" href="/login">{complete?'Sign in':'Open member sign in'}</a></div>}
      </section>
    </main><Footer/>
  </div>
}
