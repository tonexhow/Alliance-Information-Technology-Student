export function PageLoading({label='Preparing AITS'}:{label?:string}){
  return <div className="global-page-loading" role="status" aria-live="polite"><div className="global-loader-mark"><img src="/logo/aits.png" alt=""/></div><div><strong>{label}</strong><span>Bringing your workspace together…</span></div><i/></div>
}
export function Skeleton({rows=3,className=''}:{rows?:number;className?:string}){
  return <div className={`global-skeleton ${className}`} aria-hidden="true">{Array.from({length:rows},(_,i)=><span key={i}/>)}</div>
}
