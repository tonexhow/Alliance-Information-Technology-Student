import { Icon } from './Icons'

export function isInstalledApp(){
  const standalone=(navigator as Navigator & {standalone?:boolean}).standalone===true
  const native=Boolean((window as Window & {aitsNative?:unknown}).aitsNative)
  const capacitor=Boolean((window as Window & {Capacitor?:{isNativePlatform?:()=>boolean}}).Capacitor?.isNativePlatform?.())
  return window.matchMedia?.('(display-mode: standalone)').matches || standalone || native || capacitor
}

export function ShareModal({open,onClose,url,title,onCopied}:{open:boolean;onClose:()=>void;url:string;title:string;onCopied?:()=>void}){
  if(!open)return null
  const enc=encodeURIComponent(url), text=encodeURIComponent(title)
  const shareTargets=[
    ['Facebook',`https://www.facebook.com/sharer/sharer.php?u=${enc}`],
    ['WhatsApp',`https://wa.me/?text=${text}%20${enc}`],
    ['Telegram',`https://t.me/share/url?url=${enc}&text=${text}`],
    ['X',`https://twitter.com/intent/tweet?text=${text}&url=${enc}`],
    ['Email',`mailto:?subject=${text}&body=${enc}`],
  ]
  async function copy(){
    try{await navigator.clipboard.writeText(url);onCopied?.()}
    catch{
      const input=document.createElement('textarea');input.value=url;input.style.position='fixed';input.style.opacity='0';document.body.appendChild(input);input.select();document.execCommand('copy');input.remove();onCopied?.()
    }
  }
  async function nativeShare(){
    if(navigator.share){
      try{await navigator.share({title,text:title,url});return}catch(error){if((error as DOMException)?.name==='AbortError')return}
    }
    await copy()
  }
  return <div className="share-modal-layer" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><section className="share-modal"><header><div><span>SHARE POST</span><h3>Send this post anywhere</h3><p>{isInstalledApp()?'Use your device share sheet or choose a service below.':'Use your browser or device share sheet, or choose a service below.'}</p></div><button type="button" onClick={onClose}><Icon name="close"/></button></header><div className="share-link-row"><input readOnly value={url}/><button type="button" onClick={copy}><Icon name="copy"/>Copy</button></div><button type="button" className="native-share-button" onClick={()=>void nativeShare()}><Icon name="share" size={18}/><span><strong>Share with apps</strong><small>Facebook, Messenger, Instagram and other apps supported by your device</small></span><Icon name="arrow" size={15}/></button><div className="share-grid"><div className="share-qr"><img alt="QR code for this post" src={`https://api.qrserver.com/v1/create-qr-code/?size=220x220&margin=8&data=${enc}`}/><span><Icon name="qr" size={15}/> Scan to open</span></div><div className="share-targets">{shareTargets.map(([label,href])=><a key={label} href={href} target="_blank" rel="noreferrer"><Icon name={label==='Email'?'mail':'share'} size={17}/><span>{label}</span></a>)}<button type="button" onClick={()=>void nativeShare()}><Icon name="share" size={17}/><span>Instagram / apps</span></button></div></div></section></div>
}
