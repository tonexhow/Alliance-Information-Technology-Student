import { nativeServerURL } from './runtime'

export function websocketURL(path: string) {
  const normalized = path.startsWith('/') ? path : `/${path}`
  const nativeServer=nativeServerURL()
  if(nativeServer&&window.location.origin!==nativeServer){const target=new URL(nativeServer);return `${target.protocol==='https:'?'wss:':'ws:'}//${target.host}${normalized}`}
  const host = window.location.hostname.toLowerCase()
  const localVite = (host === '127.0.0.1' || host === 'localhost') && window.location.port === '3014'
  if (localVite) return `ws://127.0.0.1:3013${normalized}`
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${window.location.host}${normalized}`
}
