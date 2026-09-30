import type { CapacitorConfig } from '@capacitor/cli'
const appURL=(process.env.AITS_APP_URL||process.env.DOMAIN||'https://aits.jpano.dev').replace(/\/$/,'')
const host=new URL(appURL).hostname
const config:CapacitorConfig={
  appId:'dev.jpano.aits',
  appName:'AITS',
  webDir:'web',
  bundledWebRuntime:false,
  server:{url:appURL,cleartext:false,allowNavigation:[host],errorPath:'offline.html'},
  android:{allowMixedContent:false},
  plugins:{CapacitorCookies:{enabled:true},CapacitorHttp:{enabled:false}}
}
export default config
