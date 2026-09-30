import { useState } from 'react'
import { Icon } from './Icons'
export type SelectOption={value:string|number;label:string;hint?:string;disabled?:boolean}
export function CustomSelect({value,options,onChange,label,placeholder='Choose an option',disabled=false}:{value:string|number;options:SelectOption[];onChange:(value:string)=>void;label?:string;placeholder?:string;disabled?:boolean}){
  const [open,setOpen]=useState(false)
  const active=options.find(o=>String(o.value)===String(value))
  return <div className="custom-select-wrap">
    {label&&<span className="custom-select-label">{label}</span>}
    <button type="button" className="custom-select-trigger" disabled={disabled} onClick={()=>setOpen(true)}><span>{active?.label||placeholder}</span><Icon name="chevronRight" size={15}/></button>
    {open&&<div className="custom-select-layer" onMouseDown={e=>{if(e.target===e.currentTarget)setOpen(false)}}><section className="custom-select-modal"><header><div><span>SELECT</span><strong>{label||placeholder}</strong></div><button type="button" onClick={()=>setOpen(false)} aria-label="Close"><Icon name="close"/></button></header><div>{options.map(o=><button type="button" key={String(o.value)} disabled={!!o.disabled} aria-disabled={!!o.disabled} className={`${String(o.value)===String(value)?'active':''} ${o.disabled?'disabled':''}`.trim()} onClick={()=>{if(o.disabled)return;onChange(String(o.value));setOpen(false)}}><span><strong>{o.label}</strong>{o.hint&&<small>{o.hint}</small>}</span>{String(o.value)===String(value)&&<Icon name="check" size={16}/>}</button>)}</div></section></div>}
  </div>
}
