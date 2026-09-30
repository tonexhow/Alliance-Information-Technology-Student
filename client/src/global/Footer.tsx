import { useState } from 'react'

export function Footer(){
  const [open,setOpen]=useState(false)
  const [summary,setSummary]=useState('')
  const [details,setDetails]=useState('')
  const [sending,setSending]=useState(false)
  const [message,setMessage]=useState('')
  async function submit(){
    if(summary.trim().length<3||details.trim().length<8)return
    setSending(true);setMessage('')
    try{
      const r=await fetch('/api/bug-reports',{method:'POST',credentials:'include',headers:{'Content-Type':'application/json'},body:JSON.stringify({summary:summary.trim(),description:details.trim(),page_url:window.location.href})})
      const p=await r.json().catch(()=>({}))
      if(!r.ok)throw new Error(p.error||'Bug report could not be sent.')
      setSummary('');setDetails('');setMessage(`Report #${p.id||''} sent. Thank you.`)
      window.setTimeout(()=>{setOpen(false);setMessage('')},1400)
    }catch(e){setMessage(e instanceof Error?e.message:'Bug report could not be sent.')}
    finally{setSending(false)}
  }
  return <>
    <footer className="global-footer-shell">
      <div className="container global-footer-layout">
        <div className="global-footer-brand">
          <img className="global-footer-logo global-footer-logo-cite" src="/logo/cite.png" alt="CITE" width={40} height={40}/>
          <img className="global-footer-logo global-footer-logo-aits" src="/logo/aits.png" alt="AITS" width={40} height={40}/>
          <div className="global-footer-brand-copy"><strong>AITS</strong><small>Alliance of Information Technology Students</small></div>
        </div>
        <div className="global-footer-school"><strong>Buenavista Community College</strong><span>College of Information Technology Education</span></div>
        <div className="global-footer-credit"><span>Designed and Developed by <a href="https://jpano.dev" target="_blank" rel="noreferrer">Jhon Anthony Pano</a></span><span className="global-footer-bug-prompt">Did you notice anything off? <button type="button" className="global-footer-bug-link" onClick={()=>setOpen(true)}>Report a bug</button></span></div>
      </div>
    </footer>
    {open&&<div className="global-modal-layer" onMouseDown={e=>{if(e.target===e.currentTarget&&!sending)setOpen(false)}}>
      <section className="global-modal bug-report-global" role="dialog" aria-modal="true" aria-labelledby="global-bug-title">
        <header><div><small>HELP IMPROVE AITS</small><h3 id="global-bug-title">Report a bug</h3></div><button type="button" onClick={()=>setOpen(false)} aria-label="Close">×</button></header>
        <p>Describe what went wrong. The report will appear in Administration → Bug Reports.</p>
        <label><span>What happened?</span><input value={summary} onChange={e=>setSummary(e.target.value)} maxLength={220} placeholder="Short summary"/></label>
        <label><span>Details</span><textarea value={details} onChange={e=>setDetails(e.target.value)} rows={5} maxLength={12000} placeholder="What did you do, what happened, and what did you expect?"/></label>
        {message&&<div className="global-modal-note">{message}</div>}
        <footer><button type="button" className="button button-ghost" onClick={()=>setOpen(false)} disabled={sending}>Cancel</button><button type="button" className="button button-primary" onClick={()=>void submit()} disabled={sending||summary.trim().length<3||details.trim().length<8}>{sending?'Sending…':'Submit report'}</button></footer>
      </section>
    </div>}
  </>
}
