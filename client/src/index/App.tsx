import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import './App.css'
import { Icon } from '../global/Icons'
import { Footer } from '../global/Footer'
import { ShareModal, isInstalledApp } from '../global/ShareModal'
import { Markdown } from '../global/Markdown'
import { websocketURL } from '../global/websocket'

type ThemeMode = 'light' | 'dark'
type StatusTone = 'info' | 'success' | 'warning' | 'danger'
type BlogFilter = 'all' | 'pinned' | 'aits' | 'divisions'
type DayPeriod = 'morning' | 'noon' | 'afternoon' | 'evening' | 'night'
type MediaType = 'image' | 'video' | 'audio' | 'file'

function detectInstallPlatform(){
  const ua=navigator.userAgent||''
  const platform=(navigator as Navigator & {userAgentData?:{platform?:string}}).userAgentData?.platform||navigator.platform||''
  if(/android/i.test(ua))return 'android' as const
  if(/iphone|ipad|ipod/i.test(ua)||(/mac/i.test(platform)&&navigator.maxTouchPoints>1))return 'ios' as const
  if(/win/i.test(platform)||/windows/i.test(ua))return 'windows' as const
  if(/mac/i.test(platform)||/mac os/i.test(ua))return 'macos' as const
  if(/linux/i.test(platform)||/linux/i.test(ua))return 'linux' as const
  return 'web' as const
}

function closeWebSocketQuietly(socket: WebSocket | null, reason = 'page cleanup') {
  if (!socket) return
  socket.onclose = null
  socket.onerror = null
  if (socket.readyState === WebSocket.OPEN) socket.close(1000, reason)
  else if (socket.readyState === WebSocket.CONNECTING) socket.onopen = () => socket.close(1000, reason)
}

type Announcement = {
  id: number
  type: string
  title: string
  description: string
  date: string
  status: StatusTone
  action: { label: string; href: string } | null
}

type MediaAttachment = {
  type: MediaType
  src: string
  name?: string
}

type Comment = {
  id: number
  accountId: number
  parentId?: number | null
  name: string
  avatar?: string
  body: string
  time: string
  reactions: number
  reactedByMe?: boolean
  tag?: 'VISITOR' | 'APPLICANT' | string
  edited?: boolean
  historyCount?: number
  canEdit?: boolean
  accountKind?: 'member' | 'visitor'
  division?: string
  access?: string
  membership?: string
}

type SocialMention = { account_id:number; name:string; username:string; avatar?:string; account_kind:'member'|'visitor'; division?:string }
type SocialProfile = { account_id:number; name:string; username?:string; email?:string; avatar?:string; account_kind:'member'|'visitor'; division?:string; access?:string; membership?:string; can_chat?:boolean }

type BlogPost = {
  id: number
  pinned: boolean
  author: string
  role: string
  date: string
  title: string
  body: string
  media?: MediaAttachment[]
  reactions: number
  reactedBy?: string[]
  comments: number
  commentItems?: Comment[]
  identity?: 'member'|'organization'|'division'
  avatar?: string
  division_logo?: string
  posted_by?: string
}

type AdministrationPerson = {
  id: number
  account_id: number
  name: string
  picture?: string
  gender?: string
  age?: number
  positions: string[]
  links: { facebook?: string; instagram?: string; github?: string; email?: string; contact?: string }
  sort_order: number
}

type PublicData = {
  announcements: Announcement[]
  blogs: BlogPost[]
  administration?: AdministrationPerson[]
  divisions?: { id: number; code: string; name: string; member_count: number; logo_url?: string }[]
}

type OAuthProvider = 'google' | 'github'

type OAuthUser = {
  provider: OAuthProvider
  provider_id: string
  email: string
  name: string
  picture: string
}

type PublicMemberViewer = {
  account_id: number
  name: string
  username: string
  email: string
  avatar: string
}

type Capabilities = {
  google_oauth: boolean
  github_oauth: boolean
  smtp: boolean
}

type ModalState =
  | { type: 'auth' }
  | { type: 'post'; post: BlogPost }
  | { type: 'reactions'; post: BlogPost }
  | { type: 'comments'; post: BlogPost }
  | { type: 'member-required'; returnPath: string }
  | { type: 'bug-report' }
  | null

type AppInstallPromptEvent = Event & {prompt:()=>Promise<void>;userChoice:Promise<{outcome:'accepted'|'dismissed';platform:string}>}
type SupportAttachment = { url:string; name:string; type:string; size:number }
type SupportMessage = { id:number; sender_account_id?:number; sender:string; avatar?:string; sender_kind:'visitor'|'admin'; body:string; time:string; reply_to?:number; attachments?:SupportAttachment[]; reactions?:number; reaction_summary?:Record<string,number>; seen?:boolean }

type GreetingLanguage = {
  name: string
  greeting: Record<DayPeriod, string>
}

const greetingLanguages: GreetingLanguage[] = [
  {
    name: 'English',
    greeting: {
      morning: 'Good morning', noon: 'Good noon', afternoon: 'Good afternoon',
      evening: 'Good evening', night: 'Good evening',
    },
  },
  {
    name: 'Filipino',
    greeting: {
      morning: 'Magandang umaga', noon: 'Magandang tanghali', afternoon: 'Magandang hapon',
      evening: 'Magandang gabi', night: 'Magandang gabi',
    },
  },
  {
    name: 'Cebuano',
    greeting: {
      morning: 'Maayong buntag', noon: 'Maayong udto', afternoon: 'Maayong hapon',
      evening: 'Maayong gabii', night: 'Maayong gabii',
    },
  },
  {name:'Spanish',greeting:{morning:'Buenos días',noon:'Buen día',afternoon:'Buenas tardes',evening:'Buenas noches',night:'Buenas noches'}},
  {name:'French',greeting:{morning:'Bonjour',noon:'Bonjour',afternoon:'Bon après-midi',evening:'Bonsoir',night:'Bonsoir'}},
  {name:'German',greeting:{morning:'Guten Morgen',noon:'Guten Tag',afternoon:'Guten Tag',evening:'Guten Abend',night:'Guten Abend'}},
  {name:'Italian',greeting:{morning:'Buongiorno',noon:'Buongiorno',afternoon:'Buon pomeriggio',evening:'Buonasera',night:'Buonasera'}},
  {name:'Japanese',greeting:{morning:'おはようございます',noon:'こんにちは',afternoon:'こんにちは',evening:'こんばんは',night:'こんばんは'}},
  {name:'Korean',greeting:{morning:'좋은 아침입니다',noon:'안녕하세요',afternoon:'안녕하세요',evening:'안녕하세요',night:'안녕하세요'}},
]

const divisions = [
  {
    name: 'CodeHex',
    short: 'CH',
    line: 'Build · Solve · Create',
    description: 'For students who enjoy programming, systems, software engineering, and solving problems through code.',
    icon: 'code' as const,
    focus: ['Programming', 'Systems', 'Problem Solving'],
    details: 'CodeHex is the development-focused division of AITS. It brings together students interested in software, algorithms, automation, and building reliable digital solutions through collaboration and practice.',
  },
  {
    name: 'Illustrados',
    short: 'IL',
    line: 'Design · Express · Imagine',
    description: 'For visual thinkers, designers, digital artists, storytellers, and students who connect creativity with technology.',
    icon: 'palette' as const,
    focus: ['Design', 'Digital Media', 'Creative Tech'],
    details: 'Illustrados connects technology with visual communication. Members can explore interface design, digital media, publication materials, branding, and creative ways to present technical ideas clearly and professionally.',
  },
  {
    name: 'Neo-Tech',
    short: 'NT',
    line: 'Explore · Connect · Advance',
    description: 'For students interested in hardware, networking, infrastructure, connected devices, and emerging technologies.',
    icon: 'network' as const,
    focus: ['Networks', 'Hardware', 'Emerging Tech'],
    details: 'Neo-Tech focuses on the systems behind connected environments—from hardware and networks to infrastructure and emerging platforms. It is a place for hands-on exploration, troubleshooting, and technical curiosity.',
  },
]

const navItems = [
  { id: 'overview', label: 'Overview' },
  { id: 'announcements', label: 'Announcements' },
  { id: 'blogs', label: 'Blogs' },
  { id: 'divisions', label: 'Divisions' },
  { id: 'administration', label: 'Administration' },
  { id: 'about', label: 'About' },
]

const fallbackData: PublicData = { announcements: [], blogs: [] }

function getDayPeriod(date: Date): DayPeriod {
  const hour = date.getHours()
  if (hour >= 5 && hour < 12) return 'morning'
  if (hour >= 12 && hour < 14) return 'noon'
  if (hour >= 14 && hour < 18) return 'afternoon'
  if (hour >= 18 && hour < 22) return 'evening'
  return 'night'
}

function DivisionMark({ index, size = 'normal', logo }: { index: number; size?: 'small' | 'normal' | 'large'; logo?: string }) {
  const division = divisions[index]
  return (
    <span className={`division-mark mark-${index} mark-${size}`} aria-label={`${division.name} logo`}>
      {logo ? <img src={logo} alt="" /> : <>
        {index === 0 && <><span>&lt;</span><b>H</b><span>/&gt;</span></>}
        {index === 1 && <><span className="mark-spark">✦</span><b>I</b><span className="mark-line" /></>}
        {index === 2 && <><i /><b>N</b><i /><i /></>}
      </>}
    </span>
  )
}


function AdministrationCards({ people }: { people: AdministrationPerson[] }) {
  const linkItems = (person: AdministrationPerson) => [
    ['facebook', person.links?.facebook, 'Facebook'], ['instagram', person.links?.instagram, 'Instagram'],
    ['github', person.links?.github, 'GitHub'], ['mail', person.links?.email ? `mailto:${person.links.email}` : '', 'Email'],
    ['phone', person.links?.contact ? `tel:${person.links.contact}` : '', 'Contact'],
  ] as const
  const arranged=[...people].sort((a,b)=>a.sort_order-b.sort_order||a.id-b.id)
  return <section className="administration-group administration-flat">
    {arranged.length ? <div className="administration-list administration-cards-grid">
      {arranged.map(person => <article className="administration-person administration-profile-card" key={person.id}>
        {person.picture ? <img src={person.picture} alt={`${person.name} profile`} /> : <span className="administration-avatar">{person.name.slice(0,1).toUpperCase()}</span>}
        <div className="administration-profile-copy"><strong>{person.name}</strong><div className="admin-position-chips">{(person.positions||[]).map(position=><span key={position}>{position}</span>)}</div>{(person.gender || person.age) && <p>{[person.gender, person.age ? `${person.age} years old` : ''].filter(Boolean).join(' · ')}</p>}</div>
        <footer className="administration-socials">{linkItems(person).map(([icon,href,label]) => href ? <a key={label} href={href} target={href.startsWith('http')?'_blank':undefined} rel="noreferrer" aria-label={label} title={label}><Icon name={icon} size={15}/></a> : <button key={label} type="button" disabled aria-label={`${label} unavailable`} title={`${label} unavailable`}><Icon name={icon} size={15}/></button>)}</footer>
      </article>)}
    </div> : <div className="administration-empty"><Icon name="users" size={22}/><div><strong>No administration profiles yet.</strong><span>Published profiles will appear here.</span></div></div>}
  </section>
}

function Pagination({ page, pages, onChange }: { page: number; pages: number; onChange: (page: number) => void }) {
  if (pages <= 1) return null
  return (
    <div className="pagination" aria-label="Pagination">
      <button type="button" onClick={(event) => { event.currentTarget.blur(); onChange(page - 1) }} disabled={page === 1} aria-label="Previous page"><Icon name="chevronLeft" size={17} /></button>
      {Array.from({ length: pages }, (_, index) => index + 1).map((value) => (
        <button type="button" key={value} className={page === value ? 'active' : ''} onClick={(event) => { event.currentTarget.blur(); onChange(value) }} aria-label={`Page ${value}`}>{value}</button>
      ))}
      <button type="button" onClick={(event) => { event.currentTarget.blur(); onChange(page + 1) }} disabled={page === pages} aria-label="Next page"><Icon name="chevronRight" size={17} /></button>
    </div>
  )
}

function AnnouncementSkeleton() {
  return <div className="announcement-grid skeleton-grid" aria-hidden="true">{Array.from({ length: 6 }, (_, index) => <div className="announcement-card skeleton-card" key={index}><span className="skeleton-index" /><div className="skeleton-copy"><i /><i /><i /></div></div>)}</div>
}

function BlogSkeleton() {
  return <div className="blog-feed skeleton-blog-feed" aria-hidden="true">{Array.from({ length: 3 }, (_, index) => <div className="post-card skeleton-post" key={index}><div className="skeleton-post-head"><span /><i /></div><div className="skeleton-lines"><i /><i /><i /></div><div className="skeleton-media" /></div>)}</div>
}

function GoogleMark() {
  return (
    <svg className="oauth-brand-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path fill="#4285F4" d="M21.6 12.23c0-.71-.06-1.24-.2-1.8H12v3.28h5.52a4.8 4.8 0 0 1-2.05 3.06l2.76 2.13c1.61-1.48 3.37-3.9 3.37-6.67Z" />
      <path fill="#34A853" d="M12 22c2.7 0 4.97-.89 6.63-2.42l-3.16-2.43c-.88.59-2 .94-3.47.94-2.6 0-4.8-1.75-5.59-4.11l-3.25 2.5A10 10 0 0 0 12 22Z" />
      <path fill="#FBBC05" d="M6.41 13.98A6 6 0 0 1 6.1 12c0-.69.12-1.35.31-1.98l-3.25-2.5A10 10 0 0 0 2 12c0 1.61.39 3.14 1.16 4.48l3.25-2.5Z" />
      <path fill="#EA4335" d="M12 5.91c1.85 0 3.12.8 3.84 1.46l2.86-2.78C16.95 2.96 14.7 2 12 2a10 10 0 0 0-8.84 5.52l3.25 2.5C7.2 7.66 9.4 5.91 12 5.91Z" />
    </svg>
  )
}

function GitHubMark() {
  return (
    <svg className="oauth-brand-icon github-mark" viewBox="0 0 24 24" aria-hidden="true">
      <path fill="currentColor" d="M12 .7a11.5 11.5 0 0 0-3.64 22.41c.58.11.79-.25.79-.56v-2.22c-3.23.7-3.91-1.37-3.91-1.37-.53-1.34-1.29-1.7-1.29-1.7-1.05-.72.08-.7.08-.7 1.16.08 1.78 1.2 1.78 1.2 1.04 1.77 2.72 1.26 3.38.96.1-.75.4-1.26.74-1.55-2.58-.29-5.29-1.29-5.29-5.68 0-1.26.45-2.29 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.16 1.18A10.9 10.9 0 0 1 12 6.14c.98 0 1.96.13 2.88.39 2.19-1.49 3.15-1.18 3.15-1.18.63 1.59.23 2.76.11 3.05.74.8 1.19 1.83 1.19 3.09 0 4.4-2.72 5.38-5.3 5.67.42.36.79 1.07.79 2.16v3.23c0 .31.21.68.8.56A11.5 11.5 0 0 0 12 .7Z" />
    </svg>
  )
}

function ProviderMark({ provider }: { provider: OAuthProvider }) {
  return provider === 'google' ? <GoogleMark /> : <GitHubMark />
}

function MediaGrid({ media = [], modal = false }: { media?: MediaAttachment[]; modal?: boolean }) {
  if (!media.length) return null
  if (modal) {
    return <div className="modal-media-list">{media.map((item,index)=>{
      const raw=item.src||''
      const src=raw.startsWith('/')?raw:`/${raw}`
      if(item.type==='image') return <figure key={`${raw}-${index}`}><img src={src} alt={item.name||'Post image'} loading="lazy"/>{item.name&&<figcaption>{item.name}</figcaption>}</figure>
      if(item.type==='video') return <figure key={`${raw}-${index}`}><video src={src} controls playsInline preload="metadata"/>{item.name&&<figcaption>{item.name}</figcaption>}</figure>
      if(item.type==='audio') return <figure key={`${raw}-${index}`}><audio src={src} controls/>{item.name&&<figcaption>{item.name}</figcaption>}</figure>
      return <a className="modal-media-file" href={src} target="_blank" rel="noreferrer" key={`${raw}-${index}`}><Icon name="file" size={20}/><span>{item.name||'Open attachment'}</span></a>
    })}</div>
  }
  const visible=media.slice(0,4)
  return <div className={`media-grid media-count-${Math.min(visible.length,4)}`}>
    {visible.map((item,index)=>{
      const raw=item.src||''; const src=raw.startsWith('/')?raw:`/${raw}`
      if(item.type==='image') return <img key={`${raw}-${index}`} src={src} alt={item.name||'Post attachment'} loading="lazy"/>
      if(item.type==='video') return <div className="media-file-tile" key={`${raw}-${index}`}><Icon name="play" size={24}/><strong>{item.name||'Video attachment'}</strong></div>
      if(item.type==='audio') return <div className="media-file-tile" key={`${raw}-${index}`}><Icon name="music" size={24}/><strong>{item.name||'Audio attachment'}</strong></div>
      return <div className="media-file-tile" key={`${raw}-${index}`}><Icon name="file" size={24}/><strong>{item.name||'File attachment'}</strong></div>
    })}
    {media.length>4&&<span className="media-more">+{media.length-4}</span>}
  </div>
}

function App() {
  const [theme, setTheme] = useState<ThemeMode>(() => localStorage.getItem('aits-theme') === 'dark' ? 'dark' : 'light')
  const [data, setData] = useState<PublicData>(fallbackData)
  const [activeSection, setActiveSection] = useState('overview')
  const [scrollProgress, setScrollProgress] = useState(0)
  const [menuOpen, setMenuOpen] = useState(false)
  const [chatOpen, setChatOpen] = useState(false)
  const [modal, setModal] = useState<ModalState>(null)
  const [toast, setToast] = useState<string | null>(null)
  const [announcementPage, setAnnouncementPage] = useState(1)
  const [blogPage, setBlogPage] = useState(1)
  const [blogFilter, setBlogFilter] = useState<BlogFilter>('all')
  const [selectedDivision, setSelectedDivision] = useState(0)
  const [divisionFlash, setDivisionFlash] = useState<number | null>(null)
  const [now, setNow] = useState(new Date())
  const [languageIndex, setLanguageIndex] = useState(() => Math.floor(Math.random() * greetingLanguages.length))
  const [user, setUser] = useState<OAuthUser | null>(null)
  const [memberViewer, setMemberViewer] = useState<PublicMemberViewer | null>(null)
  const [capabilities, setCapabilities] = useState<Capabilities>({ google_oauth: false, github_oauth: false, smtp: false })
  const [myReactions, setMyReactions] = useState<Set<number>>(new Set())
  const [profileMenuOpen, setProfileMenuOpen] = useState(false)
  const [supportMessages, setSupportMessages] = useState<SupportMessage[]>([])
  const [supportText, setSupportText] = useState('')
  const [supportReply, setSupportReply] = useState<SupportMessage|null>(null)
  const [supportFiles, setSupportFiles] = useState<SupportAttachment[]>([])
  const [supportUploading, setSupportUploading] = useState(false)
  const [supportTyping, setSupportTyping] = useState('')
  const [supportState, setSupportState] = useState<'offline' | 'connecting' | 'online' | 'blocked'>('offline')
  const [supportReactionFor, setSupportReactionFor] = useState<number | null>(null)
  const [supportMetaFor, setSupportMetaFor] = useState<number | null>(null)
  const [supportRecording, setSupportRecording] = useState(false)
  const [bugSummary, setBugSummary] = useState('')
  const [bugDescription, setBugDescription] = useState('')
  const [bugSending, setBugSending] = useState(false)
  const [commentText, setCommentText] = useState('')
  const [replyTo, setReplyTo] = useState<Comment | null>(null)
  const [publicMentions,setPublicMentions]=useState<SocialMention[]>([])
  const [socialProfile,setSocialProfile]=useState<SocialProfile|null>(null)
  const [socialProfileAnchor,setSocialProfileAnchor]=useState<{x:number;y:number}|null>(null)
  const [editingPublicComment,setEditingPublicComment]=useState<Comment|null>(null)
  const [editingPublicText,setEditingPublicText]=useState('')
  const [publicCommentHistory,setPublicCommentHistory]=useState<{id:number;body:string;time:string;editor:string}[]>([])
  const [shareTarget, setShareTarget] = useState<BlogPost | null>(null)
  const [installOpen,setInstallOpen]=useState(false)
  const [installPrompt,setInstallPrompt]=useState<AppInstallPromptEvent|null>(null)
  const [installFiles,setInstallFiles]=useState({checking:false,apk:false,exe:false,dmg:false,appimage:false})
  const supportSocketRef = useRef<WebSocket | null>(null)
  const supportMessagesRef = useRef<HTMLDivElement | null>(null)
  const supportRecorderRef = useRef<MediaRecorder | null>(null)
  const supportStreamRef = useRef<MediaStream | null>(null)
  const supportDiscardRecordingRef = useRef(false)
  const supportTypingLastSentRef = useRef(0)
  const publicSyncRef = useRef<WebSocket | null>(null)
  const publicSyncRetryRef = useRef<number | null>(null)
  const publicRefreshTimerRef = useRef<number | null>(null)
  const [announcementLoading, setAnnouncementLoading] = useState(false)
  const [blogLoading, setBlogLoading] = useState(false)
  const announcementTimerRef = useRef<number | null>(null)
  const blogTimerRef = useRef<number | null>(null)
  const profileMenuRef = useRef<HTMLDivElement | null>(null)
  const blogFilterRef = useRef<HTMLDivElement | null>(null)

  const applyPublicPayload = useCallback((payload: PublicData) => {
    setData(payload)
    try { localStorage.setItem('aits-public-content-v3', JSON.stringify(payload)) } catch { /* storage may be unavailable */ }
  }, [])

  const refreshPublicContent = useCallback(async () => {
    try {
      const response = await fetch('/api/content/public', { cache: 'no-store' })
      if (!response.ok) return false
      applyPublicPayload(await response.json() as PublicData)
      return true
    } catch {
      return false
    }
  }, [applyPublicPayload])

  useEffect(() => {
    try {
      const cached = localStorage.getItem('aits-public-content-v3')
      if (cached) setData(JSON.parse(cached))
    } catch { /* ignore invalid browser cache */ }
    void refreshPublicContent()
  }, [refreshPublicContent])

  useEffect(()=>{
    localStorage.setItem('aits-last-mode',localStorage.getItem('aits-last-mode')||'visitor')
    const beforeInstall=(event:Event)=>{event.preventDefault();setInstallPrompt(event as AppInstallPromptEvent)}
    const installed=()=>{setInstallPrompt(null);setToast('AITS web app installed.')}
    window.addEventListener('beforeinstallprompt',beforeInstall)
    window.addEventListener('appinstalled',installed)
    return()=>{window.removeEventListener('beforeinstallprompt',beforeInstall);window.removeEventListener('appinstalled',installed)}
  },[])

  useEffect(()=>{
    if(!installOpen)return
    let cancelled=false;setInstallFiles(current=>({...current,checking:true}))
    void Promise.all(['/downloads/AITS-Android.apk','/downloads/AITS-Windows.exe','/downloads/AITS-macOS.dmg','/downloads/AITS-Linux.AppImage'].map(path=>fetch(path,{method:'HEAD',cache:'no-store'}).then(response=>response.ok).catch(()=>false))).then(([apk,exe,dmg,appimage])=>{if(!cancelled)setInstallFiles({checking:false,apk,exe,dmg,appimage})})
    return()=>{cancelled=true}
  },[installOpen])

  useEffect(()=>{
    if(!chatOpen)return
    const updateViewport=()=>{const vv=window.visualViewport;const h=Math.round(vv?.height||window.innerHeight),top=Math.round(vv?.offsetTop||0);const keyboard=!!vv&&(window.innerHeight-vv.height-vv.offsetTop)>120;document.documentElement.style.setProperty('--aits-support-vh',`${h}px`);document.documentElement.style.setProperty('--aits-support-top',`${top}px`);document.documentElement.classList.toggle('aits-support-keyboard-open',keyboard)}
    updateViewport();window.visualViewport?.addEventListener('resize',updateViewport);window.visualViewport?.addEventListener('scroll',updateViewport);window.addEventListener('resize',updateViewport)
    return()=>{window.visualViewport?.removeEventListener('resize',updateViewport);window.visualViewport?.removeEventListener('scroll',updateViewport);window.removeEventListener('resize',updateViewport);document.documentElement.style.removeProperty('--aits-support-vh');document.documentElement.style.removeProperty('--aits-support-top');document.documentElement.classList.remove('aits-support-keyboard-open')}
  },[chatOpen])

  useEffect(() => {
    let disposed = false
    let heartbeatTimer: number | null = null
    const connect = () => {
      if (disposed) return
      const socket = new WebSocket(websocketURL('/ws/public'))
      publicSyncRef.current = socket
      socket.onopen = () => {
        if (disposed) { socket.close(1000, 'disposed'); return }
        if (heartbeatTimer) window.clearInterval(heartbeatTimer)
        heartbeatTimer = window.setInterval(() => { if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'ping' })) }, 25000)
      }
      socket.onmessage = (event) => {
        try {
          const payload = JSON.parse(event.data)
          if (payload.type === 'ready') return
          if (payload.type !== 'content_changed') return
          setAnnouncementPage(1); setBlogPage(1)
          if (publicRefreshTimerRef.current) window.clearTimeout(publicRefreshTimerRef.current)
          publicRefreshTimerRef.current = window.setTimeout(() => { void refreshPublicContent() }, 120)
        } catch { /* ignore malformed sync frames */ }
      }
      socket.onclose = () => {
        if (heartbeatTimer) { window.clearInterval(heartbeatTimer); heartbeatTimer = null }
        if (publicSyncRef.current === socket) publicSyncRef.current = null
        if (disposed) return
        publicSyncRetryRef.current = window.setTimeout(connect, 2400)
      }
    }
    connect()
    return () => {
      disposed = true
      if (publicSyncRetryRef.current) window.clearTimeout(publicSyncRetryRef.current)
      if (publicRefreshTimerRef.current) window.clearTimeout(publicRefreshTimerRef.current)
      if (heartbeatTimer) { window.clearInterval(heartbeatTimer); heartbeatTimer = null }
      const socket = publicSyncRef.current
      publicSyncRef.current = null
      closeWebSocketQuietly(socket)
    }
  }, [refreshPublicContent])

  const refreshVisitorSession = async (attempts = 14) => {
    for (let attempt = 0; attempt < attempts; attempt += 1) {
      const session = await fetch('/api/auth/session', { credentials: 'include', cache: 'no-store' }).then((r) => r.ok ? r.json() : null).catch(() => null)
      if (session?.authenticated && session?.user) { setUser(session.user); return session.user as OAuthUser }
      if (attempt < attempts - 1) await new Promise((resolve) => window.setTimeout(resolve, 180 + attempt * 160))
    }
    return null
  }

  useEffect(() => {
    Promise.all([
      fetch('/api/system/capabilities').then((r) => r.ok ? r.json() : null).catch(() => null),
      fetch('/api/auth/session', { credentials: 'include' }).then((r) => r.ok ? r.json() : null).catch(() => null),
      fetch('/api/member/session', { credentials: 'include', cache: 'no-store' }).then((r) => r.ok ? r.json() : null).catch(() => null),
    ]).then(([caps, session, memberSession]) => {
      if (caps) setCapabilities(caps)
      if (session?.authenticated && session?.user) setUser(session.user)
      if (memberSession?.authenticated && memberSession?.member) setMemberViewer(memberSession.member as PublicMemberViewer)
    })

    const params = new URLSearchParams(window.location.search)
    const oauthProvider = params.get('provider') === 'github' ? 'GitHub' : 'Google'
    if (params.get('oauth') === 'handoff' && params.get('handoff')) {
      const token = params.get('handoff') || ''
      void fetch('/api/auth/handoff', { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) })
        .then(async (response) => {
          const payload = await response.json().catch(() => ({}))
          if (!response.ok) throw new Error(payload.error || 'Visitor sign-in handoff failed.')
          const connected = await refreshVisitorSession(5)
          if (!connected) throw new Error('Visitor session could not be confirmed.')
          setModal(null)
          setToast(`${oauthProvider} account connected successfully.`)
        })
        .catch((error) => setToast(error instanceof Error ? error.message : `${oauthProvider} sign-in could not be completed.`))
      params.delete('oauth')
      params.delete('provider')
      params.delete('handoff')
      const query = params.toString()
      window.history.replaceState({}, '', `${window.location.pathname}${query ? `?${query}` : ''}${window.location.hash}`)
    } else if (params.get('oauth') === 'success') {
      void refreshVisitorSession().then((connected) => {
        if (connected) {
          setModal(null)
          setToast(`${oauthProvider} account connected successfully.`)
        } else {
          setToast(`${oauthProvider} sign-in completed, but the visitor session could not be confirmed. Please try again.`)
        }
      })
      params.delete('oauth')
      params.delete('provider')
      const query = params.toString()
      window.history.replaceState({}, '', `${window.location.pathname}${query ? `?${query}` : ''}${window.location.hash}`)
    }
    if (params.get('oauth') === 'error') {
      setToast(`${oauthProvider} sign-in could not be completed.`)
      params.delete('oauth')
      params.delete('provider')
      const query = params.toString()
      window.history.replaceState({}, '', `${window.location.pathname}${query ? `?${query}` : ''}${window.location.hash}`)
    }
  }, [])

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    localStorage.setItem('aits-theme', theme)
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#111214' : '#fffaf3')
  }, [theme])

  useEffect(() => {
    const tick = window.setInterval(() => setNow(new Date()), 1000)
    return () => window.clearInterval(tick)
  }, [])

  useEffect(() => {
    let frame = 0
    const update = () => {
      window.cancelAnimationFrame(frame)
      frame = window.requestAnimationFrame(() => {
        const max = document.documentElement.scrollHeight - window.innerHeight
        setScrollProgress(max > 0 ? Math.min(100, Math.max(0, (window.scrollY / max) * 100)) : 0)
        const header = document.querySelector<HTMLElement>('.site-header')?.offsetHeight || 72
        const probe = window.scrollY + header + 36
        let current = navItems[0].id
        for (const item of navItems) {
          const section = document.getElementById(item.id)
          if (section && section.offsetTop <= probe) current = item.id
        }
        setActiveSection(current)
      })
    }
    update()
    window.addEventListener('scroll', update, { passive: true })
    window.addEventListener('resize', update)
    return () => {
      window.cancelAnimationFrame(frame)
      window.removeEventListener('scroll', update)
      window.removeEventListener('resize', update)
    }
  }, [])


  useEffect(() => {
    if (!profileMenuOpen) return
    const close = (event: PointerEvent) => {
      if (profileMenuRef.current && !profileMenuRef.current.contains(event.target as Node)) setProfileMenuOpen(false)
    }
    document.addEventListener('pointerdown', close)
    return () => document.removeEventListener('pointerdown', close)
  }, [profileMenuOpen])

  useEffect(() => {
    document.body.classList.toggle('overlay-open', chatOpen || modal !== null || menuOpen)
    return () => document.body.classList.remove('overlay-open')
  }, [chatOpen, modal, menuOpen])

  useEffect(() => {
    if (!toast) return
    const timeout = window.setTimeout(() => setToast(null), 2800)
    return () => window.clearTimeout(timeout)
  }, [toast])

  useEffect(() => {
    const postId = Number(new URLSearchParams(window.location.search).get('post') || 0)
    if (!postId) return
    const preview = data.blogs.find((post) => post.id === postId)
    if (preview) void openPost(preview)
    else void fetchPostDetail(postId).then((detail) => { if (detail) setModal({ type: 'post', post: detail }) }).catch(() => undefined)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data.blogs.length])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    if (params.get('report') !== 'bug') return
    setModal({ type: 'bug-report' })
    params.delete('report')
    const query=params.toString()
    window.history.replaceState({},'',`${window.location.pathname}${query?`?${query}`:''}${window.location.hash}`)
  }, [])

  useEffect(() => {
    let disposed = false
    let serverBlocked = false
    let retryTimer: number | null = null
    let heartbeatTimer: number | null = null
    if (!chatOpen || !user) { const existing=supportSocketRef.current; supportSocketRef.current=null; closeWebSocketQuietly(existing, 'support closed'); setSupportState('offline'); return }
    const connect = () => {
      if (disposed) return
      const socket = new WebSocket(websocketURL('/ws/support?mode=visitor'))
      supportSocketRef.current = socket
      setSupportState('connecting')
      socket.onopen = () => { if (!disposed) setSupportState('online'); if (heartbeatTimer) window.clearInterval(heartbeatTimer); heartbeatTimer=window.setInterval(()=>{if(socket.readyState===WebSocket.OPEN)socket.send(JSON.stringify({type:'ping'}))},25000) }
      socket.onmessage = (event) => {
        try {
          const payload = JSON.parse(event.data)
          if (payload.type === 'history') { setSupportMessages(payload.messages || []); socket.send(JSON.stringify({type:'seen'})) }
          if (payload.type === 'message') { setSupportMessages((current) => current.some((item) => item.id === payload.message.id) ? current : [...current, payload.message]); if (payload.message?.sender_kind === 'admin') socket.send(JSON.stringify({type:'seen'})) }
          if (payload.type === 'typing') setSupportTyping(payload.typing===false?'':(payload.name || 'AITS administrator'))
          if (payload.type === 'reaction') setSupportMessages((current)=>current.map((item)=>item.id===payload.message_id?{...item,reactions:payload.count,reaction_summary:payload.summary||{}}:item))
          if (payload.type === 'seen' && payload.viewer_kind === 'admin') setSupportMessages((current)=>current.map((item)=>item.sender_kind==='visitor'?{...item,seen:true}:item))
          if (payload.type === 'clear') setSupportMessages([])
          if (payload.type === 'blocked') { serverBlocked = true; setSupportState('blocked'); setToast(payload.message || 'This support conversation is blocked.'); socket.close() }
        } catch { /* ignore malformed websocket frames */ }
      }
      socket.onclose = () => {
        if (heartbeatTimer) { window.clearInterval(heartbeatTimer); heartbeatTimer = null }
        if (disposed) return
        if (supportSocketRef.current === socket) supportSocketRef.current = null
        if (serverBlocked) { setSupportState('blocked'); return }
        setSupportState('offline')
        retryTimer = window.setTimeout(connect, 1800)
      }
    }
    connect()
    return () => { disposed = true; if (retryTimer) window.clearTimeout(retryTimer); if (heartbeatTimer) window.clearInterval(heartbeatTimer); const socket=supportSocketRef.current; supportSocketRef.current=null; closeWebSocketQuietly(socket, 'support cleanup') }
  }, [chatOpen, user])

  useEffect(() => {
    if (!chatOpen) return
    const box = supportMessagesRef.current
    if (!box) return
    requestAnimationFrame(() => box.scrollTo({ top: box.scrollHeight, behavior: 'smooth' }))
  }, [chatOpen, supportMessages.length, supportTyping, supportFiles.length])

  useEffect(() => () => {
    try { if (supportRecorderRef.current?.state === 'recording') supportRecorderRef.current.stop() } catch { /* recorder already stopped */ }
    supportStreamRef.current?.getTracks().forEach((track) => track.stop())
  }, [])

  const announcementPageSize = 8
  const blogPageSize = 10

  const filteredBlogs = useMemo(() => {
    switch (blogFilter) {
      case 'pinned': return data.blogs.filter((post) => post.pinned)
      case 'aits': return data.blogs.filter((post) => post.author === 'AITS')
      case 'divisions': return data.blogs.filter((post) => post.author !== 'AITS')
      default: return data.blogs
    }
  }, [data.blogs, blogFilter])

  const filterCounts = useMemo(() => ({
    all: data.blogs.length,
    pinned: data.blogs.filter((post) => post.pinned).length,
    aits: data.blogs.filter((post) => post.author === 'AITS').length,
    divisions: data.blogs.filter((post) => post.author !== 'AITS').length,
  }), [data.blogs])

  const announcementPages = Math.max(1, Math.ceil(data.announcements.length / announcementPageSize))
  const blogPages = Math.max(1, Math.ceil(filteredBlogs.length / blogPageSize))
  useEffect(() => { if (announcementPage > announcementPages) setAnnouncementPage(announcementPages) }, [announcementPage, announcementPages])
  useEffect(() => { if (blogPage > blogPages) setBlogPage(blogPages) }, [blogPage, blogPages])
  const visibleAnnouncements = useMemo(() => data.announcements.slice((announcementPage - 1) * announcementPageSize, announcementPage * announcementPageSize), [data.announcements, announcementPage])
  const visibleBlogs = useMemo(() => filteredBlogs.slice((blogPage - 1) * blogPageSize, blogPage * blogPageSize), [filteredBlogs, blogPage])



  useEffect(() => () => {
    if (announcementTimerRef.current) window.clearTimeout(announcementTimerRef.current)
    if (blogTimerRef.current) window.clearTimeout(blogTimerRef.current)
  }, [])

  const installPlatform=detectInstallPlatform()
  const currentGreetingLanguage = greetingLanguages[languageIndex]
  const greeting = currentGreetingLanguage.greeting[getDayPeriod(now)]
  const currentDivision = divisions[selectedDivision]
  const divisionMemberCount = (index: number) => data.divisions?.find((item) => item.code === divisions[index].name.toLowerCase().replace(/[^a-z0-9]+/g, '-'))?.member_count ?? 0
  const divisionLogo = (index: number) => data.divisions?.find((item) => item.code === divisions[index].name.toLowerCase().replace(/[^a-z0-9]+/g, '-'))?.logo_url || ''

  const changeAnnouncementPage = (page: number) => {
    if (announcementLoading || page === announcementPage || page < 1 || page > announcementPages) return
    setAnnouncementLoading(true)
    if (announcementTimerRef.current) window.clearTimeout(announcementTimerRef.current)
    announcementTimerRef.current = window.setTimeout(() => { setAnnouncementPage(page); window.setTimeout(()=>setAnnouncementLoading(false),160) }, 140)
  }

  const changeBlogPage = (page: number) => {
    if (blogLoading || page === blogPage || page < 1 || page > blogPages) return
    setBlogLoading(true)
    if (blogTimerRef.current) window.clearTimeout(blogTimerRef.current)
    blogTimerRef.current = window.setTimeout(() => { setBlogPage(page); window.setTimeout(()=>setBlogLoading(false),160) }, 140)
  }

  const changeBlogFilter = (value: BlogFilter) => {
    if (blogLoading || value === blogFilter) return
    setBlogLoading(true); setActiveSection('blogs')
    if (blogTimerRef.current) window.clearTimeout(blogTimerRef.current)
    blogTimerRef.current = window.setTimeout(() => { setBlogFilter(value); setBlogPage(1); window.setTimeout(()=>setBlogLoading(false),160) }, 140)
  }

  async function optimizeImage(file:File){
    if(!file.type.startsWith('image/')||file.size<900_000)return file
    const bitmap=await createImageBitmap(file);const max=1800;const scale=Math.min(1,max/Math.max(bitmap.width,bitmap.height));const canvas=document.createElement('canvas');canvas.width=Math.round(bitmap.width*scale);canvas.height=Math.round(bitmap.height*scale);canvas.getContext('2d')?.drawImage(bitmap,0,0,canvas.width,canvas.height)
    const blob=await new Promise<Blob|null>(resolve=>canvas.toBlob(resolve,'image/webp',.82));bitmap.close();return blob?new File([blob],file.name.replace(/\.[^.]+$/,'.webp'),{type:'image/webp'}):file
  }
  async function uploadSupport(files:FileList|null){
    if(!files?.length)return;setSupportUploading(true)
    try{for(const original of Array.from(files)){const file=await optimizeImage(original);const form=new FormData();form.append('file',file);const response=await fetch('/api/support/upload',{method:'POST',credentials:'include',body:form});const payload=await response.json().catch(()=>({}));if(!response.ok)throw new Error(payload.error||`Could not attach ${original.name}.`);setSupportFiles(current=>[...current,payload.attachment])}}catch(error){setToast(error instanceof Error?error.message:'Attachment could not be added.')}finally{setSupportUploading(false)}
  }
  const sendSupportTyping=(typing:boolean)=>{const ws=supportSocketRef.current;if(ws?.readyState!==WebSocket.OPEN)return;if(!typing){supportTypingLastSentRef.current=0;ws.send(JSON.stringify({type:'typing_stop'}));return}const now=performance.now();if(now-supportTypingLastSentRef.current>850){supportTypingLastSentRef.current=now;ws.send(JSON.stringify({type:'typing'}))}}
  const reactSupport=(id:number,reaction:string)=>{if(supportSocketRef.current?.readyState===WebSocket.OPEN)supportSocketRef.current.send(JSON.stringify({type:'react',message_id:id,reaction}))}
  const toggleSupportRecording = async () => {
    if (supportRecording) { supportDiscardRecordingRef.current = false; supportRecorderRef.current?.stop(); return }
    if (!user) { setModal({ type: 'auth' }); return }
    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === 'undefined') { setToast('Audio recording is not supported by this browser.'); return }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      supportStreamRef.current = stream
      const chunks: BlobPart[] = []
      const recorder = new MediaRecorder(stream)
      supportRecorderRef.current = recorder
      recorder.ondataavailable = (event) => { if (event.data.size) chunks.push(event.data) }
      recorder.onstop = () => {
        setSupportRecording(false)
        stream.getTracks().forEach((track) => track.stop())
        supportStreamRef.current = null
        supportRecorderRef.current = null
        const discard = supportDiscardRecordingRef.current
        supportDiscardRecordingRef.current = false
        const blob = new Blob(chunks, { type: recorder.mimeType || 'audio/webm' })
        if (discard || !blob.size) return
        const ext = blob.type.includes('ogg') ? 'ogg' : blob.type.includes('mp4') ? 'm4a' : 'webm'
        const file = new File([blob], `voice-${Date.now()}.${ext}`, { type: blob.type || 'audio/webm' })
        void (async () => {
          setSupportUploading(true)
          try {
            const form = new FormData(); form.append('file', file)
            const response = await fetch('/api/support/upload', { method: 'POST', credentials: 'include', body: form })
            const payload = await response.json().catch(() => ({}))
            if (!response.ok) throw new Error(payload.error || 'Voice message could not be uploaded.')
            setSupportFiles((current) => [...current, payload.attachment].slice(0, 8))
            setToast('Voice recording attached. Review it, then press Send.')
          } catch (error) { setToast(error instanceof Error ? error.message : 'Voice message could not be sent.') }
          finally { setSupportUploading(false) }
        })()
      }
      recorder.start()
      setSupportRecording(true)
    } catch { setToast('Microphone permission was not granted.') }
  }

  const closeSupportChat = () => {
    if (supportRecording && supportRecorderRef.current?.state === 'recording') {
      supportDiscardRecordingRef.current = true
      supportRecorderRef.current.stop()
    }
    setSupportReactionFor(null)
    setChatOpen(false)
  }

  const submitBugReport=async()=>{
    const summary=bugSummary.trim(),description=bugDescription.trim()
    if(summary.length<3||description.length<8){setToast('Add a short summary and enough detail to reproduce the problem.');return}
    setBugSending(true)
    try{const response=await fetch('/api/bug-reports',{method:'POST',credentials:'include',headers:{'Content-Type':'application/json'},body:JSON.stringify({summary,description,page_url:window.location.href,name:user?.name||'',email:user?.email||''})});const payload=await response.json().catch(()=>({}));if(!response.ok)throw new Error(payload.error||'Bug report could not be sent.');setBugSummary('');setBugDescription('');setModal(null);setToast(`Bug report #${payload.id} submitted.`)}catch(error){setToast(error instanceof Error?error.message:'Bug report could not be sent.')}finally{setBugSending(false)}
  }

  const sendSupportMessage = () => {
    const body = supportText.trim()
    if ((!body && supportFiles.length===0) || !user) { if (!user) { setChatOpen(false); setModal({ type: 'auth' }); } return }
    if (!supportSocketRef.current || supportSocketRef.current.readyState !== WebSocket.OPEN) { setToast('Visitor support is reconnecting.'); return }
    supportSocketRef.current.send(JSON.stringify({ type: 'send', body, reply_to:supportReply?.id||0, attachments:supportFiles }))
    setSupportText(''); setSupportFiles([]); setSupportReply(null); sendSupportTyping(false)
  }

  const toggleTheme = () => {
    const next = theme === 'light' ? 'dark' : 'light'
    setTheme(next)
    setToast(`${next === 'light' ? 'Light' : 'Dark'} theme enabled.`)
  }

  const goToSection = (id: string, behavior: ScrollBehavior = 'smooth') => {
    setMenuOpen(false)
    setActiveSection(id)
    const target = document.getElementById(id)
    if (!target) return
    const headerHeight = document.querySelector<HTMLElement>('.site-header')?.offsetHeight || 72
    const top = target.getBoundingClientRect().top + window.scrollY - headerHeight - 6
    window.scrollTo({ top: Math.max(0, top), behavior })
    window.history.replaceState({}, '', `#${id}`)
  }

  const goToDivision = (index: number) => {
    setSelectedDivision(index)
    setDivisionFlash(index)
    goToSection('divisions')
    window.setTimeout(() => setDivisionFlash(null), 1750)
  }

  const handleAnnouncementAction = (action: Announcement['action']) => {
    if (!action) return
    if (action.href === '#chat') return setChatOpen(true)
    if (action.href === '#apply') { window.location.href = '/apply'; return }
    if (action.href.startsWith('#')) return goToSection(action.href.slice(1))
    window.open(action.href, '_blank', 'noopener,noreferrer')
  }

  const loginWithProvider = (provider: OAuthProvider) => {
    const ready = provider === 'google' ? capabilities.google_oauth : capabilities.github_oauth
    if (!ready) {
      setToast(`${provider === 'google' ? 'Google' : 'GitHub'} sign-in is currently unavailable.`)
      return
    }
    const returnPath = `${window.location.pathname}${window.location.search}${window.location.hash}`
    window.location.href = `/api/auth/${provider}/start?return=${encodeURIComponent(returnPath)}&origin=${encodeURIComponent(window.location.origin)}`
  }

  const logout = async () => {
    await fetch('/api/auth/logout', { method: 'POST', credentials: 'include' }).catch(() => undefined)
    setUser(null)
    setSupportMessages([])
    setSupportFiles([])
    setSupportReply(null)
    setProfileMenuOpen(false)
    setModal(null)
    setToast('Signed out successfully.')
  }

  const togglePostReaction = async (post: BlogPost) => {
    if (!user && !memberViewer) { setModal({ type: 'auth' }); return }
    try {
      const response = await fetch(`/api/posts/${post.id}/reaction`, { method: 'POST', credentials: 'include' })
      const payload = await response.json()
      if (!response.ok) throw new Error(payload.error || 'Could not update reaction.')
      setMyReactions((previous) => { const next = new Set(previous); if (payload.reacted) next.add(post.id); else next.delete(post.id); return next })
      setData((current) => ({ ...current, blogs: current.blogs.map((item) => item.id === post.id ? { ...item, reactions: payload.count } : item) }))
      setModal((current) => current && 'post' in current && current.post.id === post.id ? { ...current, post: { ...current.post, reactions: payload.count } } : current)
      setToast(payload.reacted ? 'Reaction added.' : 'Reaction removed.')
    } catch (error) { setToast(error instanceof Error ? error.message : 'Could not update reaction.') }
  }

  const toggleCommentReaction = async (postId: number, commentId: number) => {
    if (!user && !memberViewer) { setModal({ type: 'auth' }); return }
    try {
      const response = await fetch(`/api/posts/${postId}/comments/${commentId}`, { method: 'POST', credentials: 'include' })
      const payload = await response.json()
      if (!response.ok) throw new Error(payload.error || 'Could not update reaction.')
      setModal((current) => {
        if (!current || current.type !== 'comments' || current.post.id !== postId) return current
        return { ...current, post: { ...current.post, commentItems: (current.post.commentItems || []).map((comment) => comment.id === commentId ? { ...comment, reactions: payload.count, reactedByMe: payload.reacted } : comment) } }
      })
    } catch (error) { setToast(error instanceof Error ? error.message : 'Could not update comment reaction.') }
  }

  const sharePost = (post: BlogPost) => {
    setShareTarget(post)
    void fetch('/api/analytics/event', { method: 'POST', credentials: 'include', keepalive: true, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ event_type: 'post_share', entity_type: 'post', entity_id: String(post.id) }) }).catch(() => undefined)
  }

  const normalizePostDetail = (payload: any): BlogPost => ({
    ...payload,
    date: payload.date,
    reactedBy: payload.reacted_by || payload.reactedBy || [],
    commentItems: (payload.comment_items || payload.commentItems || []).map((comment: any) => ({
      id: comment.id, accountId: Number(comment.account_id ?? comment.accountId ?? 0), parentId: comment.parent_id ?? comment.parentId ?? null, name: comment.name,
      avatar: comment.avatar, body: comment.body, time: comment.time, reactions: comment.reactions || 0,
      reactedByMe: !!(comment.reacted_by_me ?? comment.reactedByMe), tag: comment.tag || '', edited: !!comment.edited, historyCount: Number(comment.history_count ?? comment.historyCount ?? 0), canEdit: !!(comment.can_edit ?? comment.canEdit), accountKind: comment.account_kind, division: comment.division, access: comment.access, membership: comment.membership,
    })),
  })

  const fetchPostDetail = async (postId: number) => {
    const response = await fetch(`/api/posts/${postId}`, { credentials: 'include', cache: 'no-store' })
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) {
      if (payload.member_required) {
        const target = `/member/posts/${postId}`
        setModal({ type: 'member-required', returnPath: target })
        return null
      }
      throw new Error(payload.error || 'Post unavailable.')
    }
    return normalizePostDetail(payload.post)
  }


  const openReactions = async (post: BlogPost) => {
    try {
      const detail = await fetchPostDetail(post.id)
      if (!detail) return
      setModal({ type: 'reactions', post: detail })
    } catch (error) { setToast(error instanceof Error ? error.message : 'Reactions unavailable.') }
  }

  const openPost = async (post: BlogPost, comments = false) => {
    try {
      const detail = await fetchPostDetail(post.id)
      if (!detail) return
      if ((detail as any).reacted_by_me) setMyReactions((current) => new Set(current).add(post.id))
      setReplyTo(null); setCommentText('')
      setModal({ type: comments ? 'comments' : 'post', post: detail })
      const url = new URL(window.location.href); url.searchParams.set('post', String(post.id)); window.history.replaceState({}, '', `${url.pathname}${url.search}${url.hash}`)
    } catch (error) { setToast(error instanceof Error ? error.message : 'Post unavailable.') }
  }

  const submitPublicComment = async () => {
    if (!modal || modal.type !== 'comments') return
    if (!user && !memberViewer) { setModal({ type: 'auth' }); return }
    const body = commentText.trim(); if (!body) return
    try {
      const response = await fetch(`/api/posts/${modal.post.id}/comments`, { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ body, parent_id: replyTo?.id ?? null }) })
      const payload = await response.json().catch(() => ({})); if (!response.ok) throw new Error(payload.error || 'Could not save comment.')
      const detail = await fetchPostDetail(modal.post.id); if (!detail) return
      setData((current) => ({ ...current, blogs: current.blogs.map((item) => item.id === modal.post.id ? { ...item, comments: detail.comments } : item) }))
      setModal({ type: 'comments', post: detail }); setCommentText(''); setReplyTo(null)
    } catch (error) { setToast(error instanceof Error ? error.message : 'Could not save comment.') }
  }

  const lookupPublicMentions = async (value:string) => {
    const match=value.match(/(?:^|\s)@([\w .-]{0,40})$/)
    if(!match){setPublicMentions([]);return}
    try{const response=await fetch(`/api/social/mentions?q=${encodeURIComponent(match[1].trim())}`,{credentials:'include'});const payload=await response.json().catch(()=>({}));setPublicMentions(response.ok?(payload.results||[]):[])}catch{setPublicMentions([])}
  }
  const insertPublicMention=(mention:SocialMention)=>{setCommentText(current=>current.replace(/(?:^|\s)@[\w .-]{0,40}$/,match=>`${match.startsWith(' ')?' ':''}@[${mention.name}](account:${mention.account_id}) `));setPublicMentions([])}
  const showSocialProfile=async(accountId:number,event?:{currentTarget:HTMLElement})=>{if(accountId<1)return;const rect=event?.currentTarget.getBoundingClientRect();if(rect)setSocialProfileAnchor({x:Math.min(window.innerWidth-300,Math.max(8,rect.left)),y:Math.min(window.innerHeight-230,rect.bottom+6)});try{const response=await fetch(`/api/social/profile?id=${accountId}`,{credentials:'include'});const payload=await response.json().catch(()=>({}));if(response.ok)setSocialProfile(payload.profile||null)}catch{setSocialProfile(null)}}
  const savePublicCommentEdit=async()=>{if(!modal||modal.type!=='comments'||!editingPublicComment||!editingPublicText.trim())return;try{const response=await fetch(`/api/posts/${modal.post.id}/comments/${editingPublicComment.id}`,{method:'PATCH',credentials:'include',headers:{'Content-Type':'application/json'},body:JSON.stringify({body:editingPublicText.trim()})});const payload=await response.json().catch(()=>({}));if(!response.ok)throw new Error(payload.error||'Could not update comment.');const detail=await fetchPostDetail(modal.post.id);if(detail)setModal({type:'comments',post:detail});setEditingPublicComment(null);setEditingPublicText('');setToast('Comment updated.')}catch(error){setToast(error instanceof Error?error.message:'Could not update comment.')}}
  const deletePublicComment=async(comment:Comment)=>{if(!modal||modal.type!=='comments'||!comment.canEdit)return;try{const response=await fetch(`/api/posts/${modal.post.id}/comments/${comment.id}`,{method:'DELETE',credentials:'include'});const payload=await response.json().catch(()=>({}));if(!response.ok)throw new Error(payload.error||'Could not delete comment.');const detail=await fetchPostDetail(modal.post.id);if(detail){setModal({type:'comments',post:detail});setData(current=>({...current,blogs:current.blogs.map(post=>post.id===detail.id?{...post,comments:detail.comments}:post)}))}setToast('Comment deleted.')}catch(error){setToast(error instanceof Error?error.message:'Could not delete comment.')}}
  const loadPublicCommentHistory=async(comment:Comment)=>{if(!modal||modal.type!=='comments')return;try{const response=await fetch(`/api/posts/${modal.post.id}/comments/${comment.id}/history`,{credentials:'include'});const payload=await response.json().catch(()=>({}));if(!response.ok)throw new Error(payload.error||'History unavailable.');setPublicCommentHistory(payload.history||[])}catch(error){setToast(error instanceof Error?error.message:'History unavailable.')}}

  async function installWebApp(){
    if(isInstalledApp()){setToast('AITS is already running as an installed app.');return}
    if(installPrompt){await installPrompt.prompt();const choice=await installPrompt.userChoice;if(choice.outcome==='accepted')setInstallPrompt(null);return}
    const isiOS=/iPad|iPhone|iPod/.test(navigator.userAgent)
    setToast(isiOS?'On iPhone or iPad: Share → Add to Home Screen.':'Use your browser menu and choose Install app / Add to Home Screen.')
  }

  return (
    <div className="site-shell">
      <div className="scroll-progress" aria-hidden="true"><span style={{ width: `${scrollProgress}%` }} /></div>

      <header className="site-header">
        <div className="container header-inner">
          <a className="brand aits-brand" href="#overview" onClick={(event) => { event.preventDefault(); goToSection('overview') }} aria-label="AITS home">
            <span className="brand-mark brand-aits" aria-hidden="true"><img src="/logo/aits.png" alt="" width={38} height={38}/></span>
            <span className="brand-copy"><strong>AITS</strong><small>Alliance of Information Technology Students</small></span>
          </a>

          <nav className={`main-nav ${menuOpen ? 'open' : ''}`} aria-label="Main navigation">
            {navItems.map((item) => (
              <a key={item.id} href={`#${item.id}`} className={activeSection === item.id ? 'active' : ''} onClick={(event) => { event.preventDefault(); goToSection(item.id) }}>{item.label}</a>
            ))}
            <button type="button" className="mobile-nav-action" onClick={() => { setMenuOpen(false); setChatOpen(true) }}><Icon name="chat" size={17} /> Chat</button>
            <button type="button" className="mobile-nav-action" onClick={() => { setMenuOpen(false); if (user) setProfileMenuOpen(true); else setModal({ type: 'auth' }) }}><Icon name="users" size={17} /> Visitor profile</button>
            <button type="button" className="mobile-nav-action" onClick={() => { setMenuOpen(false); window.location.href = '/login' }}><Icon name="login" size={17} /> Member login</button>
            <button type="button" className="mobile-nav-action" onClick={() => { setMenuOpen(false); window.location.href = '/apply' }}><Icon name="bolt" size={17} /> Apply</button>
          </nav>

          <div className="header-actions">
            <button type="button" className="icon-button tooltip" data-tooltip={theme === 'light' ? 'Switch to dark theme' : 'Switch to light theme'} onClick={toggleTheme} aria-label="Toggle theme"><Icon name={theme === 'light' ? 'moon' : 'sun'} size={18} /></button>
            <button type="button" className="header-chat tooltip" data-tooltip="Open community chat" onClick={() => setChatOpen(true)}><Icon name="chat" size={16} /><span>Chat</span></button>
            {user ? (
              <div className="profile-menu-wrap" ref={profileMenuRef}>
                <button type="button" className="oauth-profile" onClick={() => setProfileMenuOpen((open) => !open)} aria-expanded={profileMenuOpen} aria-label="Open profile menu">
                  {user.picture ? <img src={user.picture} alt="" /> : <span className="profile-fallback">{user.name.slice(0, 1)}</span>}
                  <span>{user.name.split(' ')[0]}</span>
                </button>
                <div className={`profile-popover ${profileMenuOpen ? 'open' : ''}`}>
                  <div className="profile-popover-head">
                    {user.picture ? <img src={user.picture} alt="" /> : <span className="profile-fallback large">{user.name.slice(0, 1)}</span>}
                    <div><strong>{user.name}</strong><small>{user.email}</small><span><ProviderMark provider={user.provider} /> Connected with {user.provider === 'google' ? 'Google' : 'GitHub'}</span></div>
                  </div>
                  <button type="button" onClick={() => { setProfileMenuOpen(false); window.location.href = '/apply' }}><Icon name="file" size={16} /> My application</button>
                  <button type="button" className="logout-item" onClick={logout}><Icon name="login" size={16} /> Logout</button>
                </div>
              </div>
            ) : (
              <button type="button" className="visitor-profile tooltip" data-tooltip="Visitor sign in · Google or GitHub" onClick={() => setModal({ type: 'auth' })} aria-label="Open visitor profile"><Icon name="users" size={17} /></button>
            )}
            <button type="button" className="button button-ghost header-login" onClick={() => { window.location.href = '/login' }}>Login</button>
            <button type="button" className="button button-primary header-apply" onClick={() => { window.location.href = '/apply' }}>{user ? 'Application' : 'Apply'}</button>
            <button type="button" className={`mobile-menu-toggle ${menuOpen ? 'open' : ''}`} onClick={() => setMenuOpen((value) => !value)} aria-label="Open navigation" aria-expanded={menuOpen}><Icon name={menuOpen ? 'close' : 'menu'} size={19} /></button>
          </div>
        </div>
      </header>
      <button type="button" className={`nav-backdrop ${menuOpen ? 'open' : ''}`} onClick={() => setMenuOpen(false)} aria-label="Close navigation" />

      <main>
        <section id="overview" className="page-section hero-section">
          <div className="hero-tech-grid" aria-hidden="true" />
          <div className="hero-glow hero-glow-one" aria-hidden="true" />
          <div className="hero-glow hero-glow-two" aria-hidden="true" />
          <div className="container hero-layout">
            <div className="hero-copy">
              <button className="greeting-chip tooltip" data-tooltip={`Greeting language: ${currentGreetingLanguage.name}. Click to change.`} type="button" onClick={() => setLanguageIndex((current) => (current + 1) % greetingLanguages.length)}>
                <span className="greeting-pulse" /><strong>{greeting}{user ? `, ${user.name.split(' ')[0]}` : ''}!</strong><span>{currentGreetingLanguage.name}</span><Icon name="globe" size={14} />
              </button>
              <p className="eyebrow">Buenavista Community College · CITE</p>
              <h1>One alliance for <span>IT students</span> building what comes next.</h1>
              <p className="hero-lead">AITS connects BCC Information Technology students through projects, activities, leadership, and three student divisions.</p>
              <div className="hero-actions">
                <button type="button" className="button button-primary button-lg" onClick={() => { window.location.href = '/apply' }}>{user ? 'View my application' : 'Apply to AITS'} <Icon name="arrow" size={17} /></button>
                <button type="button" className="button button-outline-light button-lg" onClick={() => goToSection('divisions')}>Explore divisions</button>
              </div>
              <div className="hero-status-row">
                <div><Icon name="users" size={16} /><span><strong>One community</strong><small>Students connected by IT</small></span></div>
                <div><Icon name="network" size={16} /><span><strong>3 divisions</strong><small>Different paths to contribute</small></span></div>
                <div><Icon name="clock" size={16} /><span><strong>{now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</strong><small>Local campus time</small></span></div>
              </div>
            </div>

            <div className="hero-console" aria-label="Interactive AITS network overview">
              <div className="console-topbar"><span className="console-lights"><i /><i /><i /></span><span>https://aits.jpano.dev</span><span>ONLINE</span></div>
              <div className="console-body">
                <div className="console-title"><span><Icon name="network" size={15} /> AITS NETWORK MAP</span><small>Select a division to explore</small></div>
                <div className="solar-map">
                  <div className="solar-ring ring-one" /><div className="solar-ring ring-two" />
                  <button type="button" className="solar-core" onClick={() => goToSection('about')} aria-label="About AITS"><span className="brand-mark"><img src="/logo/aits.png" alt=""/></span><small>AITS</small></button>
                  {divisions.map((division, index) => (
                    <button type="button" key={division.name} className={`solar-node solar-node-${index}`} onClick={() => goToDivision(index)} aria-label={`Explore ${division.name}`}>
                      <DivisionMark index={index} size="small" logo={divisionLogo(index)} /><span>{division.name}</span>
                    </button>
                  ))}
                  <svg className="solar-lines" viewBox="0 0 420 280" preserveAspectRatio="none" aria-hidden="true"><path d="M210 140 L83 62 M210 140 L338 66 M210 140 L328 222" /><circle cx="210" cy="140" r="4" /></svg>
                </div>
                <div className="console-quick-grid">
                  <button type="button" onClick={() => goToSection('announcements')}><Icon name="bell" size={17} /><span><strong>Announcements</strong><small>AITS updates</small></span><Icon name="arrow" size={14} /></button>
                  <button type="button" onClick={() => goToSection('blogs')}><Icon name="spark" size={17} /><span><strong>Community stories</strong><small>Posts from AITS</small></span><Icon name="arrow" size={14} /></button>
                  <button type="button" onClick={() => setChatOpen(true)}><Icon name="chat" size={17} /><span><strong>Community chat</strong><small>{user ? `Signed in as ${user.name}` : 'Sign in to interact'}</small></span><Icon name="arrow" size={14} /></button>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section id="announcements" className="page-section section-shell announcements-section pattern-section">
          <div className="container">
            <div className="section-heading heading-with-action">
              <div><p className="eyebrow"><Icon name="bell" size={14} /> AITS updates</p><h2>Announcements</h2></div>
              <div className="section-count"><strong>{data.announcements.length}</strong><span>published notices</span></div>
            </div>

            <div className="pagination-stage announcement-stage">
              <div className="content-transition page-content-enter" key={`announcement-${announcementPage}`}>
                <div className="announcement-grid">
                  {visibleAnnouncements.map((announcement, index) => (
                    <article className={`announcement-card tone-${announcement.status}`} key={announcement.id}>
                      <div className="announcement-index">{String((announcementPage - 1) * announcementPageSize + index + 1).padStart(2, '0')}</div>
                      <div className="announcement-content">
                        <div className="announcement-meta"><span className="announcement-type">{announcement.type}</span><span><Icon name="calendar" size={12} /> {announcement.date}</span></div>
                        <h3>{announcement.title}</h3><p>{announcement.description}</p>
                        {announcement.action && <button type="button" className="text-action" onClick={() => handleAnnouncementAction(announcement.action)}>{announcement.action.label} <Icon name="arrow" size={14} /></button>}
                      </div>
                    </article>
                  ))}
                </div>
              </div>
              {announcementLoading && <div className="inline-loading-overlay"><AnnouncementSkeleton /></div>}
            </div>
            <div className="pagination-anchor"><Pagination page={announcementPage} pages={announcementPages} onChange={changeAnnouncementPage} /></div>
          </div>
        </section>

        <section id="blogs" className="page-section section-shell blogs-section pattern-section">
          <div className="container blog-layout">
            <aside className="blog-aside">
              <p className="eyebrow"><Icon name="spark" size={14} /> From the community</p>
              <h2>Blogs</h2>
              <p>Posts from AITS, its divisions, and members.</p>
              <div className="blog-filters" ref={blogFilterRef} role="group" aria-label="Filter posts">
                {([
                  ['all', 'All posts'], ['pinned', 'Pinned'], ['aits', 'AITS'], ['divisions', 'Divisions'],
                ] as [BlogFilter, string][]).map(([value, label]) => (
                  <button type="button" key={value} className={blogFilter === value ? 'active' : ''} onClick={(event) => { event.currentTarget.blur(); changeBlogFilter(value) }}>
                    <span>{label}</span><b>{filterCounts[value]}</b>
                  </button>
                ))}
              </div>
              <div className="blog-side-card">
                <Icon name="chat" size={20} /><strong>Join the public conversation.</strong>
                <p>AITS members can participate directly. Visitors can sign in with Google or GitHub.</p>
                {user ? <div className="side-user"><img src={user.picture} alt="" /><span><strong>{user.name}</strong><small>{user.email}</small></span><ProviderMark provider={user.provider} /></div> : <button type="button" onClick={() => setModal({ type: 'auth' })}>Visitor sign in <Icon name="arrow" size={14} /></button>}
              </div>
            </aside>

            <div className="blog-feed-wrap">
              <div className="pagination-stage blog-stage">
                <div className="blog-feed content-transition page-content-enter" key={`${blogFilter}-${blogPage}`}>
                {visibleBlogs.length === 0 && <div className="empty-state">No posts are available in this filter yet.</div>}
                {visibleBlogs.map((post) => {
                  const reacted = myReactions.has(post.id)
                  const reactionTotal = post.reactions
                  return (
                    <article id={`post-${post.id}`} className="post-card" key={post.id} onClick={() => openPost(post)} tabIndex={0} onKeyDown={(event) => { if (event.key === 'Enter') openPost(post) }}>
                      <header className="post-header">
                        <div className="post-avatar">{post.identity==='organization'?<img src="/logo/aits.png" alt="AITS"/>:post.identity==='division'&&post.division_logo?<img src={post.division_logo} alt=""/>:post.avatar?<img src={post.avatar} alt=""/>:post.author==='AITS'?<span>A</span>:<DivisionMark index={Math.max(0, divisions.findIndex((division) => division.name === post.author))} size="small" logo={divisionLogo(Math.max(0, divisions.findIndex((division) => division.name === post.author)))} />}</div>
                        <div className="post-identity"><strong>{post.author}</strong><span>{post.role} · {post.date}</span></div>
                        {post.pinned && <span className="pinned-badge"><Icon name="pin" size={11} /> Pinned</span>}
                      </header>
                      <div className="post-content"><h3>{post.title}</h3><p>{post.body}</p></div>
                      <MediaGrid media={post.media} />
                      <div className="post-metrics" onClick={(event) => event.stopPropagation()}>
                        <button type="button" onClick={() => void openReactions(post)}><Icon name="heart" size={13} /> {reactionTotal} reactions</button>
                        <button type="button" onClick={() => void openPost(post, true)}>{post.comments} comments</button>
                      </div>
                      <div className="post-actions" onClick={(event) => event.stopPropagation()}>
                        <button type="button" className={reacted ? 'reacted' : ''} onClick={() => togglePostReaction(post)}><Icon name="heart" size={16} /> {reacted ? 'Reacted' : 'React'}</button>
                        <button type="button" onClick={() => void openPost(post, true)}><Icon name="comment" size={16} /> Comment</button>
                        <button type="button" onClick={() => sharePost(post)}><Icon name="share" size={16} /> Share</button>
                      </div>
                    </article>
                  )
                })}
                </div>
                {blogLoading && <div className="inline-loading-overlay"><BlogSkeleton /></div>}
              </div>
              <div className="pagination-anchor"><Pagination page={blogPage} pages={blogPages} onChange={changeBlogPage} /></div>
            </div>
          </div>
        </section>

        <section id="divisions" className="page-section section-shell divisions-section">
          <div className="container">
            <div className="section-heading centered-heading"><p className="eyebrow"><Icon name="network" size={14} /> Choose your path</p><h2>Three divisions. Different strengths. One AITS.</h2><p>Each division has its own focus and identity.</p></div>

            <div className="division-grid">
              {divisions.map((division, index) => (
                <button type="button" id={`division-${index}`} className={`division-card ${selectedDivision === index ? 'active' : ''} ${divisionFlash === index ? 'flash' : ''}`} key={division.name} onClick={() => setSelectedDivision(index)}>
                  <div className="division-card-top"><DivisionMark index={index} logo={divisionLogo(index)} /><span className="division-member-count"><b>{divisionMemberCount(index)}</b><small>members</small></span></div>
                  <div className="division-icon"><Icon name={division.icon} size={23} /></div>
                  <p className="division-line">{division.line}</p><h3>{division.name}</h3><p>{division.description}</p>
                  <div className="division-tags">{division.focus.map((item) => <span key={item}>{item}</span>)}</div>
                  <span className="division-explore">Explore division <Icon name="arrow" size={14} /></span>
                </button>
              ))}
            </div>

            <div className={`division-detail ${divisionFlash === selectedDivision ? 'detail-flash' : ''}`} key={currentDivision.name}>
              <div className="division-detail-visual"><DivisionMark index={selectedDivision} size="large" logo={divisionLogo(selectedDivision)} /><div className="division-circuit" aria-hidden="true"><i /><i /><i /><i /></div></div>
              <div className="division-detail-copy"><span className="detail-kicker">Currently exploring</span><h3>{currentDivision.name}</h3><p>{currentDivision.details}</p><div className="detail-focus">{currentDivision.focus.map((item) => <span key={item}><Icon name="bolt" size={12} /> {item}</span>)}</div><button type="button" className="button button-primary" onClick={() => { window.location.href = `/apply?division=${encodeURIComponent(currentDivision.name.toLowerCase().replace(/[^a-z0-9]+/g, '-'))}` }}>Apply specifically for {currentDivision.name}</button></div>
            </div>
          </div>
        </section>


        <section id="administration" className="page-section section-shell administration-section">
          <div className="container">
            <div className="section-heading centered-heading">
              <p className="eyebrow"><Icon name="shield" size={14} /> AITS Administration</p>
              <h2>The people currently guiding and leading AITS.</h2>
              
            </div>

            <div className="administration-groups">
              <AdministrationCards people={data.administration || []} />
            </div>
          </div>
        </section>

        <section id="about" className="page-section section-shell about-section pattern-section">
          <div className="container about-layout">
            <div className="about-copy">
              <p className="eyebrow"><Icon name="info" size={14} /> About the organization</p>
              <h2>AITS at Buenavista Community College</h2>
              <p>AITS is the student organization for Information Technology learners at Buenavista Community College. Members connect, share skills, lead activities, and work together beyond the classroom.</p>
              <div className="institution-identity" aria-label="AITS institutional identity">
                <div><img src="/logo/aits.png" alt="Alliance of Information Technology Students"/><span><strong>AITS</strong><small>Student organization</small></span></div>
                <div><img src="/logo/cite.png" alt="Information Technology"/><span><strong>CITE</strong><small>Information Technology</small></span></div>
                <div><img src="/logo/bcc.png" alt="Buenavista Community College"/><span><strong>BCC</strong><small>Buenavista Community College</small></span></div>
              </div>
              <div className="about-points">
                <div><span>01</span><p><strong>Learn together.</strong> Exchange knowledge across software, design, hardware, networks, and emerging technologies.</p></div>
                <div><span>02</span><p><strong>Create together.</strong> Build activities, ideas, digital content, technical solutions, and student-led initiatives.</p></div>
                <div><span>03</span><p><strong>Lead together.</strong> Develop responsibility, teamwork, confidence, and stronger connections across the CITE community.</p></div>
              </div>
            </div>

            <div className="community-card">
              <div className="community-card-icon"><Icon name="group" size={24} /></div>
              <p className="eyebrow inverse">AITS Community</p><h3>There is room for your kind of IT.</h3>
              <p>Code, design, systems, leadership, and collaboration all have a place here.</p>
              <div className="community-actions"><button type="button" className="button button-light" onClick={() => { window.location.href = '/apply' }}>Start an application</button><button type="button" className="button button-outline-light" onClick={() => setChatOpen(true)}>Open community chat</button></div>
              <div className="contact-strip"><span><strong>Campus</strong> Buenavista Community College</span><span><strong>College</strong> Information Technology Education</span></div>
            </div>
          </div>
        </section>

        <section className="application-cta">
          <div className="container application-cta-inner"><div><p className="eyebrow inverse">Your next step</p><h2>Curious where you fit in AITS?</h2><p>Start an application and complete the division-fit assessment.</p></div><button type="button" className="button button-light button-lg" onClick={() => { window.location.href = '/apply' }}>Apply now <Icon name="arrow" size={17} /></button></div>
        </section>
      </main>

      <Footer />

      <button type="button" className="install-fab tooltip" data-tooltip="Install AITS" onClick={()=>setInstallOpen(true)} aria-label="Install AITS"><Icon name="download" size={20}/></button>

      <button type="button" className={`chat-backdrop ${chatOpen ? 'open' : ''}`} onClick={closeSupportChat} aria-label="Close support" />
      <aside className={`chat-canvas support-canvas ${chatOpen ? 'open' : ''}`} aria-hidden={!chatOpen}>
        <header className="support-panel-header">
          <button type="button" className="support-back" onClick={closeSupportChat} aria-label="Back"><Icon name="back" size={20} /></button>
          <div className="support-brand-avatar">{user?.picture?<img src={user.picture} alt={`${user.name} profile`}/>:<img src="/logo/aits.png" alt="AITS"/>}<i className={supportState} /></div>
          <div className="support-heading"><strong>AITS Visitor Support</strong><span>{supportState==='online'?'Online':supportState==='connecting'?'Connecting…':supportState==='blocked'?'Conversation blocked':'Offline'}</span></div>
          <button type="button" className="support-close" onClick={closeSupportChat} aria-label="Close support"><Icon name="close" size={18} /></button>
        </header>
        <div className="chat-messages support-messages global-scrollbar" ref={supportMessagesRef}>
          <div className="support-day"><span>Visitor support</span></div>
          {supportMessages.length===0&&<div className="support-empty"><span className="support-empty-icon"><Icon name="chat" size={23}/></span><strong>{user?'Start a conversation':'Visitor sign in required'}</strong><p>{user?'Send a message, file, image, or voice note.':'Sign in with Google or GitHub to keep your support thread.'}</p>{!user&&<button type="button" onClick={()=>{setChatOpen(false);setModal({type:'auth'})}}>Sign in to chat</button>}</div>}
          {supportMessages.map(message=><div className={`support-message ${message.sender_kind==='visitor'?'own':'reply'}`} key={message.id}>
            {message.sender_kind!=='visitor'&&<span className="message-avatar">{message.avatar?<img src={message.avatar} alt=""/>:<img src="/logo/aits.png" alt="AITS"/>}</span>}
            <div className="support-bubble-wrap">
              {message.sender_kind!=='visitor'&&<small className="support-sender-name">{message.sender}</small>}
              {message.reply_to? <div className="support-reply-ref">Replying to message #{message.reply_to}</div>:null}
              {message.body&&<div className="support-markdown" onClick={()=>setSupportMetaFor(current=>current===message.id?null:message.id)}><Markdown text={message.body}/></div>}
              {!!message.attachments?.length&&<div className="support-attachments">{message.attachments.map((a,i)=><a href={a.url} target="_blank" rel="noreferrer" key={`${a.url}-${i}`} className={`support-attachment ${a.type}`}>
                {a.type==='image'?<img src={a.url} alt={a.name}/>:a.type==='audio'?<audio controls src={a.url}/>:a.type==='video'?<video controls src={a.url}/>:<><Icon name="file" size={18}/><span><strong>{a.name}</strong><small>Open attachment</small></span></>}
              </a>)}</div>}
              {Object.keys(message.reaction_summary||{}).length>0&&<div className="support-reaction-row">{Object.entries(message.reaction_summary||{}).map(([emoji,count])=><span className="support-reaction-chip" key={emoji}>{emoji} {count}</span>)}</div>}
              <div className={`support-message-meta ${(supportMetaFor===message.id||(message.sender_kind==='visitor'&&!message.seen))?'meta-visible':''}`}>
                {(supportMetaFor===message.id||(message.sender_kind==='visitor'&&!message.seen))&&<><time>{message.time}</time>{message.sender_kind==='visitor'&&<span className={`support-delivery ${message.seen?'seen':'sent'}`}>{message.seen?'Seen':'Sent'}</span>}</>}
                <button type="button" onClick={()=>setSupportReply(message)}>Reply</button>
                <div className="support-reaction-control"><button type="button" onClick={()=>setSupportReactionFor(supportReactionFor===message.id?null:message.id)}>React</button>{supportReactionFor===message.id&&<div className="support-reaction-picker">{['👍','❤️','😂','🎉','😮','🙏'].map(emoji=><button type="button" key={emoji} onClick={()=>{reactSupport(message.id,emoji);setSupportReactionFor(null)}} aria-label={`React ${emoji}`}>{emoji}</button>)}</div>}</div>
              </div>
            </div>
          </div>)}
          {supportTyping&&<div className="support-typing"><span/><span/><span/><small>{supportTyping} is typing</small></div>}
        </div>
        {user&&<div className="chat-composer support-composer modern-support-composer">
          <>
            {supportReply&&<div className="support-compose-reply"><span>Replying to <b>{supportReply.sender_kind==='visitor'?'your message':supportReply.sender}</b></span><button type="button" onClick={()=>setSupportReply(null)} aria-label="Cancel reply"><Icon name="close" size={13}/></button></div>}
            {supportRecording&&<div className="support-recording-preview"><span className="recording-pulse"/><span><strong>Recording voice…</strong><small>Tap stop to attach the recording before sending.</small></span></div>}
            {!!supportFiles.length&&<div className="support-file-queue">{supportFiles.map((a,i)=><span key={`${a.url}-${i}`}><Icon name={a.type==='image'?'image':a.type==='audio'?'audio':a.type==='video'?'video':'file'} size={14}/><b>{a.name}</b><button type="button" onClick={()=>setSupportFiles(v=>v.filter((_,x)=>x!==i))} aria-label={`Remove ${a.name}`}>×</button></span>)}</div>}
            <div className="support-compose-row">
              <label className={`support-tool ${supportUploading?'busy':''}`} title="Attach file"><Icon name="paperclip" size={18}/><input type="file" multiple hidden onChange={e=>{void uploadSupport(e.target.files);e.currentTarget.value=''}}/></label>
              <div className="support-input-shell">
                <textarea value={supportText} onFocus={()=>sendSupportTyping(true)} onBlur={()=>sendSupportTyping(false)} onChange={(e)=>{setSupportText(e.target.value);sendSupportTyping(true)}} onKeyDown={e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();sendSupportMessage()}}} placeholder="Message AITS…" maxLength={6000} rows={1}/>
                <button type="button" className={`support-input-mic ${supportRecording?'recording':''}`} title={supportRecording?'Stop and attach recording':'Record voice message'} onClick={()=>void toggleSupportRecording()} disabled={supportUploading||supportState==='blocked'} aria-label={supportRecording?'Stop recording':'Record voice message'}><Icon name={supportRecording?'stop':'mic'} size={18}/></button>
              </div>
              <button type="button" className="support-send" onClick={sendSupportMessage} disabled={(!supportText.trim()&&!supportFiles.length)||supportState!=='online'||supportUploading} aria-label="Send"><Icon name="send" size={18}/></button>
            </div>
          </>
        </div>}
      </aside>

      {modal && (
        <div className="modal-layer" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setModal(null) }}>
          {modal.type === 'auth' && (
            <div className="modal auth-modal" role="dialog" aria-modal="true" aria-labelledby="auth-title">
              <button type="button" className="modal-close" onClick={() => setModal(null)} aria-label="Close"><Icon name="close" size={18} /></button>
              <div className="modal-icon"><Icon name="users" size={22} /></div><h3 id="auth-title">Visitor profile</h3>
              <p>Visitors can continue with Google or GitHub. AITS members should use Member Login.</p>
              <div className="oauth-options">
                <button type="button" className="oauth-provider google" onClick={() => loginWithProvider('google')} disabled={!capabilities.google_oauth}><GoogleMark /><span><strong>Sign in with Google</strong><small>{capabilities.google_oauth ? 'Use your Google visitor profile' : 'Currently unavailable'}</small></span><Icon name="arrow" size={16} /></button>
                <button type="button" className="oauth-provider github" onClick={() => loginWithProvider('github')} disabled={!capabilities.github_oauth}><GitHubMark /><span><strong>Sign in with GitHub</strong><small>{capabilities.github_oauth ? 'Use your GitHub visitor profile' : 'Currently unavailable'}</small></span><Icon name="arrow" size={16} /></button>
              </div>
              {(!capabilities.google_oauth || !capabilities.github_oauth) && <div className="modal-note"><Icon name="info" size={15} /><span>One or more visitor sign-in options are temporarily unavailable. You can still use any option shown as available.</span></div>}
              {user && <button type="button" className="text-danger-button" onClick={logout}>Sign out of {user.email}</button>}
            </div>
          )}

          {modal.type === 'member-required' && (
            <div className="modal auth-modal member-required-modal" role="dialog" aria-modal="true" aria-labelledby="member-required-title">
              <button type="button" className="modal-close" onClick={() => setModal(null)} aria-label="Close"><Icon name="close" size={18} /></button>
              <div className="modal-icon"><Icon name="shield" size={22} /></div><h3 id="member-required-title">Member access required</h3>
              <p>This post is limited to AITS members. Open the member login to continue to the protected post.</p>
              <div className="member-required-actions"><button type="button" className="button button-ghost" onClick={() => setModal(null)}>Cancel</button><button type="button" className="button button-primary" onClick={() => { const target=modal.returnPath; window.location.href=`/login?return=${encodeURIComponent(target)}` }}>Open member login</button></div>
            </div>
          )}

          {modal.type === 'bug-report' && (
            <div className="modal bug-report-modal" role="dialog" aria-modal="true" aria-labelledby="bug-report-title">
              <button type="button" className="modal-close" onClick={() => setModal(null)} aria-label="Close"><Icon name="close" size={18} /></button>
              <img className="bug-report-icon" src="/icons/report-bug.svg" alt=""/><h3 id="bug-report-title">Report a bug</h3>
              <p>Describe the problem. The current page and browser details are included automatically.</p>
              <label><span>Summary</span><input value={bugSummary} onChange={e=>setBugSummary(e.target.value)} maxLength={220} placeholder="What is not working?"/></label>
              <label><span>Details</span><textarea rows={6} value={bugDescription} onChange={e=>setBugDescription(e.target.value)} maxLength={12000} placeholder="What did you do, what happened, and what did you expect?"/></label>
              <div className="modal-note"><Icon name="info" size={15}/><span>Page: {window.location.pathname}. {user?`Report will include ${user.name}'s visitor profile.`:'You may submit anonymously.'}</span></div>
              <div className="modal-actions"><button type="button" className="button button-ghost" onClick={()=>setModal(null)}>Cancel</button><button type="button" className="button button-primary" disabled={bugSending||bugSummary.trim().length<3||bugDescription.trim().length<8} onClick={()=>void submitBugReport()}>{bugSending?'Sending…':'Submit report'}</button></div>
            </div>
          )}

          {modal.type === 'post' && (
            <div className="modal post-modal" role="dialog" aria-modal="true" aria-labelledby="post-modal-title">
              <button type="button" className="modal-close" onClick={() => setModal(null)} aria-label="Close"><Icon name="close" size={18} /></button>
              <header className="post-modal-header"><div className="post-avatar">{modal.post.author === 'AITS' ? <span>A</span> : <DivisionMark index={Math.max(0, divisions.findIndex((division) => division.name === modal.post.author))} size="small" logo={divisionLogo(Math.max(0, divisions.findIndex((division) => division.name === modal.post.author)))} />}</div><div><strong>{modal.post.author}</strong><span>{modal.post.role} · {modal.post.date}</span></div></header>
              <h3 id="post-modal-title">{modal.post.title}</h3><p>{modal.post.body}</p>
              <MediaGrid media={modal.post.media} modal />
              <div className="post-modal-actions"><button type="button" onClick={() => togglePostReaction(modal.post)}><Icon name="heart" size={16} /> React</button><button type="button" onClick={() => void openPost(modal.post, true)}><Icon name="comment" size={16} /> Comment</button><button type="button" onClick={() => sharePost(modal.post)}><Icon name="share" size={16} /> Share</button></div>
            </div>
          )}

          {modal.type === 'reactions' && (
            <div className="modal reaction-modal" role="dialog" aria-modal="true" aria-labelledby="reaction-title">
              <button type="button" className="modal-close" onClick={() => setModal(null)} aria-label="Close"><Icon name="close" size={18} /></button>
              <div className="modal-icon"><Icon name="heart" size={22} /></div><h3 id="reaction-title">People who reacted</h3>
              <div className="reactor-list">
                {myReactions.has(modal.post.id) && user && <div><img src={user.picture} alt="" /><span><strong>{user.name}</strong><small>You · {user.provider === 'google' ? 'Google' : 'GitHub'} visitor</small></span></div>}
                {(modal.post.reactedBy || []).filter((name) => !(myReactions.has(modal.post.id) && user && name === user.name)).map((name, index) => <div key={`${name}-${index}`}><span className="reactor-fallback">{name.slice(0, 1)}</span><span><strong>{name}</strong><small>AITS community</small></span></div>)}
                {(modal.post.reactedBy || []).length === 0 && !myReactions.has(modal.post.id) && <div className="empty-state">No reactions yet.</div>}
              </div>
            </div>
          )}

          {modal.type === 'comments' && (
            <div className="modal comments-modal" role="dialog" aria-modal="true" aria-labelledby="comments-title">
              <button type="button" className="modal-close" onClick={() => { setModal(null); setReplyTo(null); setCommentText('') }} aria-label="Close"><Icon name="close" size={18} /></button>
              <div className="comments-heading">
                <div><span>PUBLIC CONVERSATION</span><h3 id="comments-title">Comments & replies</h3></div>
                <b>{modal.post.comments}</b>
              </div>
              <p className="comments-intro">Public post · members and signed-in visitors can react, comment, reply, and tag profiles.</p>
              <div className="comment-list global-scrollbar">
                {(modal.post.commentItems || []).filter((comment) => !comment.parentId).map((comment) => {
                  const replies = (modal.post.commentItems || []).filter((reply) => reply.parentId === comment.id)
                  const reacted = !!comment.reactedByMe
                  return <div className="comment-thread" key={comment.id}>
                    <div className="comment-card">
                      {comment.avatar ? <img className="comment-avatar comment-avatar-image" src={comment.avatar} alt="" /> : <span className="comment-avatar">{comment.name.slice(0, 1)}</span>}
                      <div><div className="comment-bubble"><button type="button" className="comment-profile-trigger" onMouseEnter={event=>void showSocialProfile(comment.accountId,event)} onFocus={event=>void showSocialProfile(comment.accountId,event)}>{comment.name}{comment.tag&&<em className={`comment-role-tag ${comment.tag.toLowerCase()}`}>{comment.tag}</em>}</button><Markdown text={comment.body} onMention={showSocialProfile}/></div><div className="comment-meta"><span>{comment.time}{comment.edited?' · Edited':''}</span><button type="button" className={reacted ? 'reacted' : ''} onClick={() => void toggleCommentReaction(modal.post.id, comment.id)}>React · {comment.reactions}</button><button type="button" onClick={() => (user || memberViewer) ? setReplyTo(comment) : setModal({ type: 'auth' })}>Reply</button>{comment.canEdit&&<button type="button" onClick={()=>{setEditingPublicComment(comment);setEditingPublicText(comment.body)}}>Edit</button>}{comment.canEdit&&<button type="button" onClick={()=>void deletePublicComment(comment)}>Delete</button>}{(comment.historyCount||0)>0&&<button type="button" onClick={()=>void loadPublicCommentHistory(comment)}>History · {comment.historyCount}</button>}</div></div>
                    </div>
                    {replies.length > 0 && <div className="comment-replies">{replies.map((reply) => <div className="comment-card reply-card" key={reply.id}>
                      {reply.avatar ? <img className="comment-avatar comment-avatar-image" src={reply.avatar} alt="" /> : <span className="comment-avatar">{reply.name.slice(0, 1)}</span>}
                      <div><div className="comment-bubble"><button type="button" className="comment-profile-trigger" onMouseEnter={event=>void showSocialProfile(reply.accountId,event)} onFocus={event=>void showSocialProfile(reply.accountId,event)}>{reply.name}{reply.tag&&<em className={`comment-role-tag ${reply.tag.toLowerCase()}`}>{reply.tag}</em>}</button><Markdown text={reply.body} onMention={showSocialProfile}/></div><div className="comment-meta"><span>{reply.time}{reply.edited?' · Edited':''}</span><button type="button" className={reply.reactedByMe ? 'reacted' : ''} onClick={() => void toggleCommentReaction(modal.post.id, reply.id)}>React · {reply.reactions}</button><button type="button" onClick={() => (user || memberViewer) ? setReplyTo(comment) : setModal({ type: 'auth' })}>Reply</button>{reply.canEdit&&<button type="button" onClick={()=>{setEditingPublicComment(reply);setEditingPublicText(reply.body)}}>Edit</button>}{reply.canEdit&&<button type="button" onClick={()=>void deletePublicComment(reply)}>Delete</button>}{(reply.historyCount||0)>0&&<button type="button" onClick={()=>void loadPublicCommentHistory(reply)}>History · {reply.historyCount}</button>}</div></div>
                    </div>)}</div>}
                  </div>
                })}
                {(!modal.post.commentItems || modal.post.commentItems.length === 0) && <div className="empty-state">No comments yet. Be the first to start the conversation.</div>}
              </div>
              {(user || memberViewer) ? <div className="comment-composer">
                {replyTo && <div className="replying-to"><span>Replying to <strong>{replyTo.name}</strong></span><button type="button" onClick={() => setReplyTo(null)} aria-label="Cancel reply"><Icon name="close" size={13}/></button></div>}
                <div className="comment-compose-row">
                  {(user?.picture || memberViewer?.avatar) ? <img src={user?.picture || memberViewer?.avatar || undefined} alt="" /> : <span>{(user?.name || memberViewer?.name || 'M').slice(0,1)}</span>}
                  <div className="public-comment-input"><textarea value={commentText} onChange={(event) => {setCommentText(event.target.value);void lookupPublicMentions(event.target.value)}} placeholder={replyTo ? `Reply to ${replyTo.name}…` : 'Write a comment… Use @ to tag someone.'} maxLength={2000} />{publicMentions.length>0&&<div className="public-mention-results">{publicMentions.map(mention=><button type="button" key={mention.account_id} onClick={()=>insertPublicMention(mention)}>{mention.avatar?<img src={mention.avatar} alt=""/>:<span>{mention.name.slice(0,1)}</span>}<div><strong>{mention.name}</strong><small>@{mention.username} · {mention.account_kind}{mention.division?` · ${mention.division}`:''}</small></div></button>)}</div>}</div>
                  <button type="button" onClick={() => void submitPublicComment()} disabled={!commentText.trim()} aria-label="Post comment"><Icon name="send" size={16}/></button>
                </div>
              </div> : <div className="comment-composer-locked"><Icon name="users" size={16} /><span><strong>Join the conversation</strong><small>Sign in as an AITS member or connect a visitor profile to react, comment, and reply.</small></span><button type="button" onClick={() => setModal({ type: 'auth' })}>Sign in</button></div>}
            </div>
          )}
        </div>
      )}

      {socialProfile&&socialProfileAnchor&&<aside className="public-profile-card" style={{left:socialProfileAnchor.x,top:socialProfileAnchor.y}} onMouseLeave={()=>setSocialProfile(null)}><button type="button" className="public-profile-close" onClick={()=>setSocialProfile(null)}><Icon name="close" size={13}/></button><div className="public-profile-head">{socialProfile.avatar?<img src={socialProfile.avatar} alt=""/>:<span>{socialProfile.name.slice(0,1)}</span>}<div><strong>{socialProfile.name}</strong><small>{socialProfile.username?`@${socialProfile.username}`:socialProfile.account_kind}</small></div></div><div className="public-profile-facts"><span><small>Type</small><b>{socialProfile.account_kind==='member'?'Member':'Visitor'}</b></span>{socialProfile.division&&<span><small>Division</small><b>{socialProfile.division}</b></span>}{socialProfile.access&&<span><small>Access</small><b>{socialProfile.access}</b></span>}{socialProfile.membership&&<span><small>Membership</small><b>{socialProfile.membership}</b></span>}</div>{socialProfile.can_chat&&<button type="button" className="public-profile-chat" onClick={()=>window.location.href=`/member?chat_account=${socialProfile.account_id}`}><Icon name="chat" size={14}/>Chat</button>}</aside>}
      {editingPublicComment&&<div className="public-social-layer" onMouseDown={event=>{if(event.target===event.currentTarget)setEditingPublicComment(null)}}><section className="public-social-dialog"><button type="button" className="modal-close" onClick={()=>setEditingPublicComment(null)}><Icon name="close" size={16}/></button><span>EDIT COMMENT</span><h3>Update your comment</h3><textarea rows={5} value={editingPublicText} onChange={event=>setEditingPublicText(event.target.value)}/><footer><button type="button" onClick={()=>setEditingPublicComment(null)}>Cancel</button><button type="button" className="primary" disabled={!editingPublicText.trim()} onClick={()=>void savePublicCommentEdit()}>Save</button></footer></section></div>}
      {publicCommentHistory.length>0&&<div className="public-social-layer" onMouseDown={event=>{if(event.target===event.currentTarget)setPublicCommentHistory([])}}><section className="public-social-dialog history"><button type="button" className="modal-close" onClick={()=>setPublicCommentHistory([])}><Icon name="close" size={16}/></button><span>EDIT HISTORY</span><h3>Previous versions</h3><div>{publicCommentHistory.map(item=><article key={item.id}><Markdown text={item.body}/><small>{item.time} · {item.editor}</small></article>)}</div></section></div>}

      {installOpen&&<div className="install-modal-layer" onMouseDown={event=>{if(event.target===event.currentTarget)setInstallOpen(false)}}><section className="install-modal"><header><div><span>INSTALL AITS</span><h3>Use AITS your way</h3><p>Installation is optional. You can always keep using AITS safely from this website URL without installing anything.</p></div><button type="button" onClick={()=>setInstallOpen(false)} aria-label="Close"><Icon name="close"/></button></header><div className="install-device-note"><Icon name="system" size={18}/><span><strong>Detected: {installPlatform==='android'?'Android':installPlatform==='ios'?'iPhone / iPad':installPlatform==='windows'?'Windows':installPlatform==='macos'?'macOS':installPlatform==='linux'?'Linux':'Web browser'}</strong><small>Recommended option is shown first. Web App is the safest cross-platform installation.</small></span></div><div className="install-options"><button type="button" className={`install-option ${['android','ios','windows','macos','linux','web'].includes(installPlatform)?'recommended':''}`} onClick={()=>void installWebApp()}><span className="install-option-icon"><Icon name="spark"/></span><span><strong>Web App</strong><small>{isInstalledApp()?'Already installed on this device':'Recommended · installs from your browser · offline app shell supported'}</small></span><b>{isInstalledApp()?'Installed':installPrompt?'Install':'Recommended'}</b></button>{installFiles.apk?<a className={`install-option ${installPlatform==='android'?'device-match':''}`} href="/downloads/AITS-Android.apk" download><span className="install-option-icon"><Icon name="phone"/></span><span><strong>Android</strong><small>APK package · local SQLite cache for installed Android devices</small></span><b>{installPlatform==='android'?'For this device':'Download APK'}</b></a>:<div className={`install-option unavailable ${installPlatform==='android'?'device-match':''}`}><span className="install-option-icon"><Icon name="phone"/></span><span><strong>Android</strong><small>{installFiles.checking?'Checking published builds…':'APK is not published yet. Web App remains available.'}</small></span><b>Coming later</b></div>}{installFiles.exe?<a className={`install-option ${installPlatform==='windows'?'device-match':''}`} href="/downloads/AITS-Windows.exe" download><span className="install-option-icon"><Icon name="system"/></span><span><strong>Windows</strong><small>Desktop EXE · Electron wrapper with local SQLite cache</small></span><b>{installPlatform==='windows'?'For this device':'Download EXE'}</b></a>:<div className={`install-option unavailable ${installPlatform==='windows'?'device-match':''}`}><span className="install-option-icon"><Icon name="system"/></span><span><strong>Windows</strong><small>{installFiles.checking?'Checking published builds…':'EXE is not published yet. Web App remains available.'}</small></span><b>Coming later</b></div>}<div className={`install-option unavailable ${installPlatform==='ios'?'device-match':''}`}><span className="install-option-icon"><Icon name="phone"/></span><span><strong>iOS / iPadOS</strong><small>Native iOS package is not published yet. Use Web App → Add to Home Screen.</small></span><b>{installPlatform==='ios'?'Use Web App':'Coming later'}</b></div>{installFiles.dmg?<a className={`install-option ${installPlatform==='macos'?'device-match':''}`} href="/downloads/AITS-macOS.dmg" download><span className="install-option-icon"><Icon name="system"/></span><span><strong>macOS</strong><small>Desktop DMG · Electron wrapper with local SQLite cache</small></span><b>{installPlatform==='macos'?'For this device':'Download DMG'}</b></a>:<div className={`install-option unavailable ${installPlatform==='macos'?'device-match':''}`}><span className="install-option-icon"><Icon name="system"/></span><span><strong>macOS</strong><small>{installFiles.checking?'Checking published builds…':'DMG is not published yet. Web App remains available.'}</small></span><b>Coming later</b></div>}{installFiles.appimage?<a className={`install-option ${installPlatform==='linux'?'device-match':''}`} href="/downloads/AITS-Linux.AppImage" download><span className="install-option-icon"><Icon name="system"/></span><span><strong>Linux</strong><small>AppImage · Electron wrapper with local SQLite cache</small></span><b>{installPlatform==='linux'?'For this device':'Download AppImage'}</b></a>:<div className={`install-option unavailable ${installPlatform==='linux'?'device-match':''}`}><span className="install-option-icon"><Icon name="system"/></span><span><strong>Linux</strong><small>{installFiles.checking?'Checking published builds…':'AppImage is not published yet. Web App remains available.'}</small></span><b>Coming later</b></div>}</div><footer><Icon name="shield" size={14}/><span><strong>No installation required.</strong> AITS continues to work from the website URL. Installed versions are optional conveniences and keep local member/visitor state; supported offline actions queue locally and synchronize with the PostgreSQL-backed server when connectivity returns.</span></footer></section></div>}

      <ShareModal open={!!shareTarget} onClose={() => setShareTarget(null)} url={shareTarget ? `${window.location.origin}/?post=${shareTarget.id}` : window.location.href} title={shareTarget?.title || 'AITS post'} onCopied={() => setToast('Post link copied.')} />

      <div className={`toast ${toast ? 'show' : ''}`} role="status"><span className="toast-dot" /> {toast}</div>
    </div>
  )
}

export default App
