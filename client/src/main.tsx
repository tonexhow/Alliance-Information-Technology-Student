import { Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import './global/site.css'
import { PageLoading } from './global/Loading'
import { installNativeFetchBridge } from './global/runtime'

installNativeFetchBridge()

const App = lazy(() => import('./index/App.tsx'))
const ApplicationPage = lazy(() => import('./index/ApplicationPage.tsx'))
const LoginPage = lazy(() => import('./login/LoginPage.tsx'))
const MemberPage = lazy(() => import('./user/MemberPage.tsx'))
const AdminPage = lazy(() => import('./admin/AdminPage.tsx'))
const ActivationPage = lazy(() => import('./index/ActivationPage.tsx'))
const AdminResetPage = lazy(() => import('./login/AdminResetPage.tsx'))

let path = window.location.pathname.replace(/\/+$/, '') || '/'
const runtimeWindow=window as Window & {aitsNative?:{platform?:string;installed?:boolean;bootMode?:string};Capacitor?:{isNativePlatform?:()=>boolean}}
const installedRuntime=Boolean(runtimeWindow.aitsNative?.installed)||Boolean(runtimeWindow.Capacitor?.isNativePlatform?.())||window.matchMedia?.('(display-mode: standalone)').matches||(navigator as Navigator&{standalone?:boolean}).standalone===true
const nativeFile=window.location.protocol==='file:'
const bootMode=runtimeWindow.aitsNative?.bootMode||localStorage.getItem('aits-last-mode')||'visitor'
if(installedRuntime&&(path==='/'||nativeFile)&&bootMode==='member'){path='/member';if(!nativeFile&&window.location.pathname!=='/member')history.replaceState({},'',`/member${window.location.search}`)}
else if(nativeFile)path='/'

if (!path.startsWith('/admin')) {
  void fetch('/api/analytics/event', { method: 'POST', credentials: 'include', keepalive: true, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ event_type: 'page_view', metadata: { path } }) }).catch(() => undefined)
}

const Screen =
  path === '/reset-password' ? AdminResetPage
  : path === '/activate-member' ? ActivationPage
  : path === '/apply' || path === '/application' ? ApplicationPage
  : path === '/login' ? LoginPage
  : path === '/member' || path.startsWith('/member/') || path === '/user' ? MemberPage
  : path === '/admin' || path === '/administrator' ? AdminPage
  : App

createRoot(document.getElementById('root')!).render(
  <Suspense fallback={<PageLoading label="Opening AITS"/>}>
    <Screen />
  </Suspense>,
)

if ('serviceWorker' in navigator && import.meta.env.PROD) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => undefined)
  })
}
