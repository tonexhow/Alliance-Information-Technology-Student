declare global{
  interface Window{
    aitsNative?:{platform?:string;installed?:boolean;serverURL?:string;bootMode?:string}
  }
}

export function nativeServerURL(){
  const value=window.aitsNative?.serverURL?.trim()
  if(!value)return ''
  try{return new URL(value).origin}catch{return ''}
}

export function installNativeFetchBridge(){
  const server=nativeServerURL()
  if(!server||window.location.origin===server)return
  const original=window.fetch.bind(window)
  window.fetch=(input:RequestInfo|URL,init?:RequestInit)=>{
    if(typeof input==='string'&&input.startsWith('/'))return original(`${server}${input}`,init)
    if(input instanceof URL&&input.origin===window.location.origin&&input.pathname.startsWith('/'))return original(new URL(`${input.pathname}${input.search}`,server),init)
    if(typeof Request!=='undefined'&&input instanceof Request&&input.url.startsWith(`${window.location.origin}/`)){
      const current=new URL(input.url),target=new URL(`${current.pathname}${current.search}`,server)
      return original(new Request(target.toString(),input),init)
    }
    return original(input,init)
  }
}
