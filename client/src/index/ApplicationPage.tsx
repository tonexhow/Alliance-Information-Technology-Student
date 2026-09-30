import { useEffect, useMemo, useRef, useState, type ClipboardEvent } from 'react'
import './ApplicationPage.css'
import { PageLoading } from '../global/Loading'
import { Icon } from '../global/Icons'
import { CustomSelect } from '../global/CustomSelect'

type ThemeMode = 'light' | 'dark'
type OAuthProvider = 'google' | 'github'
type Stage = 'identity' | 'personal' | 'academic' | 'ready' | 'loading' | 'quiz' | 'result' | 'finish'
type AssessmentMode = 'discover' | 'codehex' | 'illustrados' | 'neo-tech'
type Feedback =
  | { kind: 'correct'; countdown: number; correctAnswer: string }
  | { kind: 'wrong'; countdown: number; correctAnswer: string }
  | { kind: 'timeout'; retryCountdown: number; correctAnswer: string }
  | null

type OAuthUser = { provider: OAuthProvider; provider_id: string; email: string; name: string; picture: string }
type Capabilities = { google_oauth: boolean; github_oauth: boolean; smtp: boolean }
type AssessmentContent = { type: 'text' | 'image' | 'audio' | 'video'; text?: string; url?: string; label?: string }
type Answer = { text: string; image?: string | null; type?: AssessmentContent['type']; url?: string; label?: string; value?: string }
type Question = {
  id: number
  category: string
  question: string
  image?: string | null
  questionContent?: AssessmentContent
  correctAnswer: Answer
  dummyAnswers: Answer[]
  enabled: boolean
  yearLevel?: number
}
type PreparedQuestion = Question & { renderedAnswers: Answer[]; allowedSeconds: number }
type AppConfig = {
  organization: string
  organizationShort: string
  questionsPerDivision: number
  specificDivisionQuestions?: number
  generalQuestions: number
  reserveQuestionsPerCategory: number
  secondsPerCharacter: number
  secondsPerImage: number
  overallPassingPercent: number
  divisionPassingPercent: number
  feedbackSeconds: number
  timeoutRetrySeconds: number
}
type Department = {
  id: string
  name: string
  courses: { id: string; name: string; yearLevels: string[] }[]
  yearLevels?: string[]
}
type Division = { id: Exclude<AssessmentMode, 'discover'>; name: string }
type ApplicationData = {
  config: AppConfig
  departments: Department[]
  divisions: Division[]
  questions: Question[]
  loadingMessages: string[]
  focusMessages: string[]
  correctMessages: string[]
  wrongMessages: string[]
  timeoutMessages: string[]
}
type FormState = {
  email: string
  fullName: string
  birthdate: string
  gender: string
  departmentId: string
  courseId: string
  schoolId: string
  yearLevel: string
}
type QuestionResult = {
  questionId: number
  category: string
  correct: boolean
  timedOut: boolean
  selected: string | null
  selectedKey: string | null
  speedPercent: number
}
type StoredApplication = {
  applicationNumber: string
  status: string
  submittedAt: string
  division: string
  overall: number
  confidence: number
  form: FormState
  assessmentMode?: AssessmentMode
  pendingId?: string
}

const emptyForm: FormState = {
  email: '', fullName: '', birthdate: '', gender: '', departmentId: '', courseId: '', schoolId: '', yearLevel: '',
}

const fallbackConfig: AppConfig = {
  organization: 'Alliance of Information Technology Students',
  organizationShort: 'AITS',
  questionsPerDivision: 5,
  specificDivisionQuestions: 15,
  generalQuestions: 5,
  reserveQuestionsPerCategory: 3,
  secondsPerCharacter: 2,
  secondsPerImage: 15,
  overallPassingPercent: 50,
  divisionPassingPercent: 50,
  feedbackSeconds: 5,
  timeoutRetrySeconds: 3,
}

function shuffle<T>(input: T[]): T[] {
  const items = [...input]
  for (let i = items.length - 1; i > 0; i -= 1) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[items[i], items[j]] = [items[j], items[i]]
  }
  return items
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}

function calculateAllowedSeconds(question: Question, config: AppConfig) {
  const answers = [question.correctAnswer, ...question.dummyAnswers.slice(0, 3)]
  const text = [question.question, ...answers.map((answer) => answer.text)].join(' ')
  const media = [question.questionContent?.type && question.questionContent.type !== 'text' ? question.questionContent.url : question.image, ...answers.map((answer) => answer.type && answer.type !== 'text' ? answer.url : answer.image)].filter(Boolean).length
  return Math.max(10, text.length * config.secondsPerCharacter + media * config.secondsPerImage)
}

function prepareQuestion(question: Question, config: AppConfig): PreparedQuestion {
  const dummyAnswers = shuffle(question.dummyAnswers).slice(0, 3)
  return {
    ...question,
    renderedAnswers: shuffle([question.correctAnswer, ...dummyAnswers]),
    allowedSeconds: calculateAllowedSeconds(question, config),
  }
}

function formatTime(seconds: number) {
  const safe = Math.max(0, seconds)
  return `${String(Math.floor(safe / 60)).padStart(2, '0')}:${String(safe % 60).padStart(2, '0')}`
}

function randomOf(items: string[], fallback: string) {
  return items[Math.floor(Math.random() * items.length)] || fallback
}

function applicationYearNumber(value: string) {
  const match = value.match(/[1-4]/)
  return match ? Number(match[0]) : 1
}


type PendingApplicationSubmission = {
  id: string
  queuedAt: string
  payload: Record<string, unknown>
}

const APPLICATION_OFFLINE_DB = 'aits-application-offline-v1'
const APPLICATION_OFFLINE_STORE = 'cache'

function openApplicationOfflineDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(APPLICATION_OFFLINE_DB, 1)
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(APPLICATION_OFFLINE_STORE)) request.result.createObjectStore(APPLICATION_OFFLINE_STORE)
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error || new Error('Offline cache is unavailable.'))
  })
}

async function offlineGet<T>(key: string): Promise<T | null> {
  if (!('indexedDB' in window)) return null
  const db = await openApplicationOfflineDB()
  return new Promise((resolve, reject) => {
    const tx = db.transaction(APPLICATION_OFFLINE_STORE, 'readonly')
    const request = tx.objectStore(APPLICATION_OFFLINE_STORE).get(key)
    request.onsuccess = () => resolve((request.result as T | undefined) ?? null)
    request.onerror = () => reject(request.error || new Error('Offline cache read failed.'))
    tx.oncomplete = () => db.close()
  })
}

async function offlinePut(key: string, value: unknown): Promise<void> {
  if (!('indexedDB' in window)) return
  const db = await openApplicationOfflineDB()
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(APPLICATION_OFFLINE_STORE, 'readwrite')
    tx.objectStore(APPLICATION_OFFLINE_STORE).put(value, key)
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error || new Error('Offline cache write failed.'))
  })
  db.close()
}

async function queueApplicationSubmission(payload: Record<string, unknown>) {
  const pending = (await offlineGet<PendingApplicationSubmission[]>('pending-submissions')) || []
  const item: PendingApplicationSubmission = {
    id: `pending-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`,
    queuedAt: new Date().toISOString(),
    payload,
  }
  pending.push(item)
  await offlinePut('pending-submissions', pending)
  return item
}

async function removeQueuedSubmission(id: string) {
  const pending = (await offlineGet<PendingApplicationSubmission[]>('pending-submissions')) || []
  await offlinePut('pending-submissions', pending.filter((item) => item.id !== id))
}

async function syncQueuedApplicationSubmissions() {
  const pending = (await offlineGet<PendingApplicationSubmission[]>('pending-submissions')) || []
  const remaining: PendingApplicationSubmission[] = []
  const synced: Array<{ item: PendingApplicationSubmission; response: Record<string, unknown> }> = []
  for (const item of pending) {
    try {
      const response = await fetch('/api/application/finish', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(item.payload),
      })
      const payload = await response.json().catch(() => ({})) as Record<string, unknown>
      if (!response.ok) {
        remaining.push(item)
        continue
      }
      synced.push({ item, response: payload })
    } catch {
      remaining.push(item)
    }
  }
  await offlinePut('pending-submissions', remaining)
  return synced
}

function focusPenaltyFromIncidents(incidents: number) {
  const events = Math.floor(Math.max(0, incidents) / 3)
  let penalty = 0
  let next = 1
  for (let index = 0; index < events; index += 1) {
    penalty += next
    if (penalty >= 100) return 100
    next *= 2
  }
  return penalty
}

function AssessmentMedia({ content, fallbackImage, compact = false }: { content?: AssessmentContent; fallbackImage?: string | null; compact?: boolean }) {
  const type = content?.type || (fallbackImage ? 'image' : 'text')
  const url = content?.url || fallbackImage || ''
  if (type === 'text' || !url) return null
  if (type === 'image') return <img className={compact ? 'answer-media' : 'question-image'} src={url.startsWith('/') ? url : url} alt={content?.label || 'Assessment reference'} draggable={false} />
  if (type === 'audio') return <audio className={compact ? 'answer-audio' : 'question-audio'} controls preload="metadata" src={url} />
  return <video className={compact ? 'answer-video' : 'question-video'} controls playsInline preload="metadata" src={url} />
}

function modeFromURL(): AssessmentMode {
  const raw = new URLSearchParams(window.location.search).get('division')?.toLowerCase().trim()
  if (raw === 'codehex' || raw === 'illustrados' || raw === 'neo-tech') return raw
  return 'discover'
}

function GoogleMark() {
  return <svg className="apply-oauth-icon" viewBox="0 0 24 24" aria-hidden="true"><path fill="#4285F4" d="M21.6 12.23c0-.71-.06-1.4-.19-2.07H12v3.92h5.38a4.6 4.6 0 0 1-2 3.02v2.54h3.24c1.9-1.75 2.98-4.33 2.98-7.41Z"/><path fill="#34A853" d="M12 22c2.7 0 4.97-.9 6.62-2.43l-3.24-2.54c-.9.6-2.05.96-3.38.96-2.61 0-4.82-1.76-5.61-4.13H3.04v2.62A10 10 0 0 0 12 22Z"/><path fill="#FBBC05" d="M6.39 13.86A6 6 0 0 1 6.08 12c0-.65.11-1.28.31-1.86V7.52H3.04A10 10 0 0 0 2 12c0 1.61.39 3.14 1.04 4.48l3.35-2.62Z"/><path fill="#EA4335" d="M12 6.01c1.47 0 2.79.5 3.83 1.49l2.87-2.87A9.65 9.65 0 0 0 12 2a10 10 0 0 0-8.96 5.52l3.35 2.62C7.18 7.77 9.39 6.01 12 6.01Z"/></svg>
}

function GitHubMark() {
  return <svg className="apply-oauth-icon github" viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M12 .7a11.5 11.5 0 0 0-3.64 22.41c.58.1.79-.25.79-.56v-2.02c-3.22.7-3.9-1.37-3.9-1.37-.53-1.34-1.29-1.7-1.29-1.7-1.05-.72.08-.71.08-.71 1.17.08 1.78 1.2 1.78 1.2 1.04 1.78 2.72 1.27 3.38.97.1-.75.4-1.27.74-1.56-2.57-.29-5.27-1.28-5.27-5.69 0-1.26.45-2.28 1.19-3.09-.12-.29-.52-1.47.11-3.05 0 0 .97-.31 3.16 1.18A10.9 10.9 0 0 1 12 6.32c.98 0 1.95.13 2.87.39 2.19-1.49 3.16-1.18 3.16-1.18.63 1.58.23 2.76.11 3.05.74.81 1.19 1.83 1.19 3.09 0 4.42-2.7 5.39-5.28 5.68.42.36.78 1.07.78 2.16v3.04c0 .31.21.67.8.56A11.5 11.5 0 0 0 12 .7Z"/></svg>
}

function BirthdatePicker({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const today = new Date()
  const initial = value ? new Date(`${value}T12:00:00`) : new Date(today.getFullYear() - 18, today.getMonth(), 1)
  const [open, setOpen] = useState(false)
  const [viewYear, setViewYear] = useState(initial.getFullYear())
  const [viewMonth, setViewMonth] = useState(initial.getMonth())
  const months = ['January','February','March','April','May','June','July','August','September','October','November','December']
  const daysInMonth = new Date(viewYear, viewMonth + 1, 0).getDate()
  const firstDay = new Date(viewYear, viewMonth, 1).getDay()
  const years = Array.from({ length: 101 }, (_, index) => today.getFullYear() - index)
  const selected = value ? new Date(`${value}T12:00:00`) : null
  const format = (date: Date) => `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,'0')}-${String(date.getDate()).padStart(2,'0')}`
  const choose = (day: number) => {
    const date = new Date(viewYear, viewMonth, day, 12)
    if (date > today) return
    onChange(format(date)); setOpen(false)
  }
  const moveMonth = (delta: number) => {
    const next = new Date(viewYear, viewMonth + delta, 1)
    setViewYear(next.getFullYear()); setViewMonth(next.getMonth())
  }
  return <div className="birthdate-picker">
    <button type="button" className={`birthdate-trigger ${value ? 'has-value' : ''}`} onClick={() => setOpen(v => !v)} aria-expanded={open}>
      <span className="birthdate-icon">◫</span><span><small>BIRTHDATE</small><strong>{selected ? selected.toLocaleDateString(undefined,{month:'long',day:'numeric',year:'numeric'}) : 'Select your birthdate'}</strong></span><b>{open ? '−' : '+'}</b>
    </button>
    {open && <div className="calendar-popover">
      <div className="calendar-toolbar"><button type="button" onClick={() => moveMonth(-1)}>‹</button><div><CustomSelect value={viewMonth} label="Month" options={months.map((m,i)=>({value:i,label:m}))} onChange={v=>setViewMonth(Number(v))}/><CustomSelect value={viewYear} label="Year" options={years.map(y=>({value:y,label:String(y)}))} onChange={v=>setViewYear(Number(v))}/></div><button type="button" onClick={() => moveMonth(1)}>›</button></div>
      <div className="calendar-weekdays">{['Su','Mo','Tu','We','Th','Fr','Sa'].map(d=><span key={d}>{d}</span>)}</div>
      <div className="calendar-days">{Array.from({length:firstDay},(_,i)=><i key={`blank-${i}`}/>) }{Array.from({length:daysInMonth},(_,i)=>i+1).map(day=>{const date=new Date(viewYear,viewMonth,day,12);const isSelected=selected && selected.getFullYear()===viewYear && selected.getMonth()===viewMonth && selected.getDate()===day;const future=date>today;return <button type="button" key={day} className={isSelected?'selected':''} disabled={future} onClick={()=>choose(day)}>{day}</button>})}</div>
      <div className="calendar-footer"><span>Choose the date shown on your school/personal records.</span>{value&&<button type="button" onClick={()=>{onChange('');setOpen(false)}}>Clear</button>}</div>
    </div>}
  </div>
}

function AitsMark() {
  return <span className="apply-brand-mark" aria-label="AITS logo"><img src="/library/images/branding/aits.png" alt=""/></span>
}

export default function ApplicationPage() {
  const [theme, setTheme] = useState<ThemeMode>(() => localStorage.getItem('aits-theme') === 'dark' ? 'dark' : 'light')
  const [now, setNow] = useState(new Date())
  const [data, setData] = useState<ApplicationData | null>(null)
  const [capabilities, setCapabilities] = useState<Capabilities>({ google_oauth: false, github_oauth: false, smtp: false })
  const [user, setUser] = useState<OAuthUser | null>(null)
  const [form, setForm] = useState<FormState>(() => {
    try { return { ...emptyForm, ...JSON.parse(localStorage.getItem('aits-application-draft') || '{}') } } catch { return emptyForm }
  })
  const [assessmentMode, setAssessmentMode] = useState<AssessmentMode>(modeFromURL)
  const [stage, setStage] = useState<Stage>('identity')
  const [questions, setQuestions] = useState<PreparedQuestion[]>([])
  const [reserves, setReserves] = useState<Record<string, PreparedQuestion[]>>({})
  const [currentIndex, setCurrentIndex] = useState(0)
  const [questionRun, setQuestionRun] = useState(0)
  const [remaining, setRemaining] = useState(0)
  const [deadline, setDeadline] = useState(0)
  const [feedback, setFeedback] = useState<Feedback>(null)
  const [results, setResults] = useState<QuestionResult[]>([])
  const [focusMessage, setFocusMessage] = useState<string | null>(null)
  const [focusIncidents, setFocusIncidents] = useState(0)
  const [focusCountdown, setFocusCountdown] = useState<number | null>(null)
  const [pausePending, setPausePending] = useState(false)
  const [paused, setPaused] = useState(false)
  const [windowWarning, setWindowWarning] = useState<string | null>(null)
  const [loadingMessage, setLoadingMessage] = useState('Preparing your assessment…')
  const [chosenDivision, setChosenDivision] = useState('')
  const [finishMessage, setFinishMessage] = useState('')
  const [storedApplication, setStoredApplication] = useState<StoredApplication | null>(() => {
    try { return JSON.parse(localStorage.getItem('aits-application-record') || 'null') } catch { return null }
  })
  const [showStatus, setShowStatus] = useState(false)
  const [cancelConfirm, setCancelConfirm] = useState(false)
  const feedbackLock = useRef(false)
  const reservesRef = useRef<Record<string, PreparedQuestion[]>>({})
  const questionsRef = useRef<PreparedQuestion[]>([])
  const currentIndexRef = useRef(0)
  const focusIncidentsRef = useRef(0)
  const resultsRef = useRef<QuestionResult[]>([])
  const awaySinceRef = useRef<number | null>(null)
  const awayAppliedRef = useRef(0)

  const config = data?.config || fallbackConfig
  const currentQuestion = questions[currentIndex]
  const specificDivision = data?.divisions.find((division) => division.id === assessmentMode)
  const isSpecific = assessmentMode !== 'discover'

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    localStorage.setItem('aits-theme', theme)
  }, [theme])

  useEffect(() => {
    const interval = window.setInterval(() => setNow(new Date()), 1000)
    return () => window.clearInterval(interval)
  }, [])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    if (params.get('oauth') !== 'handoff' || !params.get('handoff')) return
    const token = params.get('handoff') || ''
    params.delete('oauth')
    params.delete('provider')
    params.delete('handoff')
    const cleanURL = `${window.location.pathname}${params.toString() ? `?${params.toString()}` : ''}${window.location.hash}`
    void fetch('/api/auth/handoff', { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) })
      .then(async response => {
        const payload = await response.json().catch(() => ({}))
        if (!response.ok) throw new Error(payload.error || 'Visitor sign-in handoff failed.')
        window.history.replaceState({}, '', cleanURL)
        window.location.reload()
      })
      .catch(error => {
        setFinishMessage(error instanceof Error ? error.message : 'Visitor sign-in could not be completed.')
        window.history.replaceState({}, '', cleanURL)
      })
  }, [])

  useEffect(() => {
    const cacheKey = 'aits-application-content-v2'
    try {
      const cached = localStorage.getItem(cacheKey)
      if (cached) setData(JSON.parse(cached))
    } catch { /* ignore invalid cache */ }

    fetch('/application.json', { cache: 'no-cache' })
      .then((response) => response.ok ? response.json() : Promise.reject())
      .then((payload: ApplicationData) => {
        setData(payload)
        try { localStorage.setItem(cacheKey, JSON.stringify(payload)) } catch { /* storage may be unavailable */ }
      })
      .catch(() => undefined)

    Promise.all([
      fetch('/api/system/capabilities').then((response) => response.ok ? response.json() : null).catch(() => null),
      fetch('/api/auth/session', { credentials: 'include' }).then((response) => response.ok ? response.json() : null).catch(() => null),
      fetch('/api/application/current', { credentials: 'include', cache: 'no-store' }).then((response) => response.ok ? response.json() : null).catch(() => null),
    ]).then(([caps, session, current]) => {
      if (caps) setCapabilities(caps)
      if (session?.authenticated && session.user) {
        setUser(session.user)
        setForm((previous) => ({ ...previous, email: session.user.email || previous.email, fullName: previous.fullName || session.user.name || '' }))
        if (current?.application) {
          const record: StoredApplication = {
            applicationNumber: current.application.application_number,
            status: String(current.application.status || 'submitted').replace(/_/g, ' ').replace(/\b\w/g, (value: string) => value.toUpperCase()),
            submittedAt: current.application.submitted_at,
            division: current.application.division || current.application.division_code || 'AITS',
            overall: Number(current.application.overall || 0),
            confidence: Number(current.application.confidence || 0),
            form: {
              ...emptyForm,
              email: session.user.email || '',
              fullName: session.user.name || '',
              birthdate: current.application.birthdate || '',
              gender: current.application.gender || '',
              schoolId: current.application.school_id || '',
              yearLevel: current.application.year_level || '',
            },
            assessmentMode: current.application.assessment_mode === 'specific'
              ? ((current.application.division_code || '').toLowerCase().replace(/[^a-z-]/g, '') as AssessmentMode)
              : 'discover',
          }
          setStoredApplication(record)
          try { localStorage.setItem('aits-application-record', JSON.stringify(record)) } catch { /* optional local copy */ }
          setShowStatus(true)
        } else if (storedApplication && session.user.email && session.user.email.toLowerCase() === storedApplication.form.email.toLowerCase()) {
          setShowStatus(true)
        }
      }
    })
  }, [])

  useEffect(() => {
    const syncPending = () => {
      void syncQueuedApplicationSubmissions().then((synced) => {
        for (const result of synced) {
          const current = (() => {
            try { return JSON.parse(localStorage.getItem('aits-application-record') || 'null') as StoredApplication | null } catch { return null }
          })()
          if (!current || current.pendingId !== result.item.id) continue
          const applicationNumber = String(result.response.application_number || '')
          if (!applicationNumber) continue
          const next: StoredApplication = {
            ...current,
            applicationNumber,
            pendingId: undefined,
            status: 'Submitted',
            submittedAt: new Date().toISOString(),
            overall: Number(result.response.overall ?? current.overall),
            confidence: Number(result.response.confidence ?? current.confidence),
          }
          localStorage.setItem('aits-application-record', JSON.stringify(next))
          setStoredApplication(next)
          setFinishMessage('Your offline application was synchronized and validated by the AITS server.')
        }
      }).catch(() => undefined)
    }
    window.addEventListener('online', syncPending)
    if (navigator.onLine) syncPending()
    return () => window.removeEventListener('online', syncPending)
  }, [])

  useEffect(() => {
    localStorage.setItem('aits-application-draft', JSON.stringify(form))
  }, [form])

  useEffect(() => {
    if (stage !== 'quiz' || !currentQuestion || feedback || paused) return
    const nextDeadline = Date.now() + currentQuestion.allowedSeconds * 1000
    setDeadline(nextDeadline)
    setRemaining(currentQuestion.allowedSeconds)
    feedbackLock.current = false
  }, [stage, currentIndex, questionRun, paused])

  useEffect(() => {
    if (stage !== 'quiz' || !deadline || feedback || paused) return
    const timer = window.setInterval(() => {
      const seconds = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
      setRemaining(seconds)
      if (seconds <= 0 && !feedbackLock.current) {
        feedbackLock.current = true
        setFeedback({ kind: 'timeout', retryCountdown: config.timeoutRetrySeconds, correctAnswer: currentQuestion?.correctAnswer.text || '' })
      }
    }, 200)
    return () => window.clearInterval(timer)
  }, [stage, deadline, feedback, config.timeoutRetrySeconds, currentQuestion, paused])

  useEffect(() => {
    if (!feedback || feedback.kind === 'timeout') return
    const timer = window.setInterval(() => {
      setFeedback((current) => {
        if (!current || current.kind === 'timeout') return current
        if (current.countdown <= 1) {
          window.setTimeout(() => advanceQuestion(), 0)
          return null
        }
        return { ...current, countdown: current.countdown - 1 }
      })
    }, 1000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [feedback?.kind])

  useEffect(() => {
    if (!feedback || feedback.kind !== 'timeout' || feedback.retryCountdown <= 0) return
    const timer = window.setTimeout(() => {
      setFeedback((current) => current?.kind === 'timeout'
        ? { ...current, retryCountdown: Math.max(0, current.retryCountdown - 1) }
        : current)
    }, 1000)
    return () => window.clearTimeout(timer)
  }, [feedback])

  const selectedDepartment = data?.departments.find((department) => department.id === form.departmentId)
  const selectedCourse = selectedDepartment?.courses.find((course) => course.id === form.courseId)
    || (selectedDepartment?.courses.length === 1 ? selectedDepartment.courses[0] : undefined)
  const yearLevels = selectedCourse?.yearLevels || selectedDepartment?.yearLevels || []

  const testSummary = useMemo(() => {
    if (!data || results.length === 0) return null
    const categories = [...data.divisions.map((division) => division.id), 'general']
    const scores: Record<string, number> = {}
    const attempts: Record<string, number> = {}
    categories.forEach((category) => {
      const subset = results.filter((result) => result.category === category)
      attempts[category] = subset.length
      scores[category] = subset.length ? Math.round((subset.filter((result) => result.correct).length / subset.length) * 100) : 0
    })
    const rawOverall = Math.round((results.filter((result) => result.correct).length / Math.max(1, results.length)) * 100)
    const penaltyPoints = focusPenaltyFromIncidents(focusIncidents)
    const excessivePenalty = penaltyPoints >= Math.ceil(config.overallPassingPercent / 2)
    const overall = excessivePenalty ? 10 : clamp(rawOverall - penaltyPoints, 0, 100)
    const confidence = Math.round(results.reduce((sum, result) => sum + result.speedPercent, 0) / Math.max(1, results.length))
    const qualified = data.divisions.filter((division) => attempts[division.id] > 0 && scores[division.id] >= config.divisionPassingPercent)
    const max = qualified.length ? Math.max(...qualified.map((division) => scores[division.id])) : -1
    const suggested = qualified.filter((division) => scores[division.id] === max)
    return { scores, attempts, rawOverall, penaltyPoints, excessivePenalty, overall, confidence, passed: overall >= config.overallPassingPercent, qualified, suggested }
  }, [data, results, focusIncidents, config.divisionPassingPercent, config.overallPassingPercent])

  useEffect(() => {
    if (!testSummary) return
    if (assessmentMode !== 'discover') {
      if (testSummary.passed) setChosenDivision(assessmentMode)
      return
    }
    if (!chosenDivision && testSummary.suggested.length === 1) setChosenDivision(testSummary.suggested[0].id)
  }, [testSummary, assessmentMode, chosenDivision])

  const updateForm = (patch: Partial<FormState>) => setForm((previous) => ({ ...previous, ...patch }))

  const loginVisitor = (provider: OAuthProvider) => {
    const ready = provider === 'google' ? capabilities.google_oauth : capabilities.github_oauth
    if (!ready) return
    const suffix = assessmentMode === 'discover' ? '' : `?division=${encodeURIComponent(assessmentMode)}`
    window.location.href = `/api/auth/${provider}/start?return=${encodeURIComponent(`/apply${suffix}`)}&origin=${encodeURIComponent(window.location.origin)}`
  }

  const beginTest = async (mode: AssessmentMode = assessmentMode) => {
    if (!data) return
    setAssessmentMode(mode)
    setQuestions([])
    setResults([])
    resultsRef.current = []
    setCurrentIndex(0)
    setChosenDivision(mode === 'discover' ? '' : mode)
    setFocusIncidents(0)
    setPausePending(false)
    setPaused(false)
    setFeedback(null)
    setLoadingMessage(randomOf(data.loadingMessages, 'Preparing your assessment…'))
    setStage('loading')

    const selectedYear = applicationYearNumber(form.yearLevel)
    let bank: Question[] = []
    const bankCacheKey = `assessment-bank-year-${selectedYear}`
    try {
      const response = await fetch(`/api/application/questions?year_level=${encodeURIComponent(form.yearLevel || String(selectedYear))}`, { credentials: 'include', cache: 'no-store' })
      if (response.ok) {
        const payload = await response.json()
        if (Array.isArray(payload.questions)) {
          bank = payload.questions
          void offlinePut(bankCacheKey, bank).catch(() => undefined)
        }
      }
    } catch { /* use the predownloaded assessment bank below */ }
    if (!bank.length) {
      try { bank = (await offlineGet<Question[]>(bankCacheKey)) || [] } catch { /* IndexedDB is optional */ }
    }
    if (!bank.length) bank = data.questions.filter((question) => question.enabled && (question.yearLevel || 1) <= selectedYear)

    const primary: PreparedQuestion[] = []
    const nextReserves: Record<string, PreparedQuestion[]> = {}
    const counts: Record<string, number> = mode === 'discover'
      ? Object.fromEntries([...data.divisions.map((division) => [division.id, config.questionsPerDivision]), ['general', config.generalQuestions]])
      : { [mode]: config.specificDivisionQuestions ?? 15, general: config.generalQuestions }

    Object.entries(counts).forEach(([category, count]) => {
      const pool = shuffle(bank.filter((question) => question.enabled && question.category === category && (question.yearLevel || 1) <= selectedYear))
      const prepared = pool.slice(0, count * 3).map((question) => prepareQuestion(question, config))
      primary.push(...prepared.slice(0, count))
      nextReserves[category] = prepared.slice(count)
    })

    const shuffledPrimary = shuffle(primary)
    setQuestions(shuffledPrimary)
    setReserves(nextReserves)
    questionsRef.current = shuffledPrimary
    reservesRef.current = nextReserves
    focusIncidentsRef.current = 0
    awaySinceRef.current = null
    awayAppliedRef.current = 0
    sessionStorage.removeItem('aits-assessment-away-since')
    sessionStorage.removeItem('aits-assessment-away-applied')
    window.setTimeout(() => setStage('quiz'), 850)
  }

  const pullReserve = (preferred?: string) => {
    const next: Record<string, PreparedQuestion[]> = Object.fromEntries(Object.entries(reservesRef.current).map(([key,value]) => [key,[...value]]))
    const order = preferred ? [preferred, ...Object.keys(next).filter((key) => key !== preferred)] : Object.keys(next)
    let picked: PreparedQuestion | null = null
    for (const key of order) {
      if (next[key]?.length) { picked = next[key].shift() || null; break }
    }
    if (picked) { reservesRef.current = next; setReserves(next) }
    return picked
  }

  const applyFocusPenalty = () => {
    const index = currentIndexRef.current
    const active = questionsRef.current[index]
    if (!active) return
    const incident = focusIncidentsRef.current + 1
    focusIncidentsRef.current = incident
    setFocusIncidents(incident)
    const alreadyAnswered = resultsRef.current.some((result) => result.questionId === active.id)
    const replacement = alreadyAnswered ? null : pullReserve(active.category)
    if (replacement) {
      const nextQuestions = [...questionsRef.current]
      nextQuestions[index] = replacement
      questionsRef.current = nextQuestions
      setQuestions(nextQuestions)
      setFeedback(null)
      feedbackLock.current = false
      setQuestionRun((value) => value + 1)
    }
    if (incident % 3 === 0) {
      const extra = pullReserve()
      if (extra) {
        const expanded = [...questionsRef.current, extra]
        questionsRef.current = expanded
        setQuestions(expanded)
      }
      const penalty = focusPenaltyFromIncidents(incident)
      setFocusMessage(`Focus penalty: +1 question · -${penalty} cumulative score point${penalty === 1 ? '' : 's'}.`)
      window.setTimeout(() => setFocusMessage(null), 4200)
    }
  }

  useEffect(() => { questionsRef.current = questions }, [questions])
  useEffect(() => { reservesRef.current = reserves }, [reserves])
  useEffect(() => { currentIndexRef.current = currentIndex }, [currentIndex])
  useEffect(() => { focusIncidentsRef.current = focusIncidents }, [focusIncidents])
  useEffect(() => { resultsRef.current = results }, [results])

  useEffect(() => {
    if (stage !== 'quiz' || paused) { setFocusCountdown(null); setWindowWarning(null); return }
    const restoreAway = () => {
      const stored = Number(sessionStorage.getItem('aits-assessment-away-since') || 0)
      const applied = Number(sessionStorage.getItem('aits-assessment-away-applied') || 0)
      if (stored > 0) { awaySinceRef.current = stored; awayAppliedRef.current = Math.max(0, applied) }
    }
    restoreAway()
    const beginAway = () => {
      if (awaySinceRef.current) return
      const now = Date.now()
      awaySinceRef.current = now; awayAppliedRef.current = 0
      sessionStorage.setItem('aits-assessment-away-since', String(now))
      sessionStorage.setItem('aits-assessment-away-applied', '0')
      setFocusCountdown(10)
    }
    const reconcileAway = (returning = false) => {
      const since = awaySinceRef.current
      if (!since) return
      const elapsed = Math.max(0, Date.now() - since)
      const periods = Math.floor(elapsed / 10000)
      while (awayAppliedRef.current < periods) {
        applyFocusPenalty()
        awayAppliedRef.current += 1
        sessionStorage.setItem('aits-assessment-away-applied', String(awayAppliedRef.current))
      }
      setFocusCountdown(10 - (Math.floor(elapsed / 1000) % 10))
      if (returning) {
        awaySinceRef.current = null; awayAppliedRef.current = 0
        sessionStorage.removeItem('aits-assessment-away-since')
        sessionStorage.removeItem('aits-assessment-away-applied')
        setFocusCountdown(null)
        if (data) { setFocusMessage(randomOf(data.focusMessages, periods > 0 ? 'Your question was changed because the assessment lost focus.' : 'Return to the assessment before the 10-second focus timer expires.')); window.setTimeout(() => setFocusMessage(null), 4200) }
      }
    }
    const onVisibility = () => document.visibilityState === 'hidden' ? beginAway() : (document.hasFocus() && reconcileAway(true))
    const onBlur = () => beginAway()
    const onFocus = () => { if (document.visibilityState === 'visible') reconcileAway(true) }
    const onResize = () => {
      const desktop = window.screen.width >= 900
      const reduced = desktop && (window.innerWidth < window.screen.width * .62 || window.innerHeight < window.screen.height * .62)
      setWindowWarning(reduced ? 'Assessment window is unusually small. Restore a normal window size before continuing.' : null)
    }
    document.addEventListener('visibilitychange', onVisibility); window.addEventListener('blur', onBlur); window.addEventListener('focus', onFocus); window.addEventListener('resize', onResize)
    onResize()
    const tick = window.setInterval(() => { if (awaySinceRef.current) reconcileAway(false) }, 500)
    return () => { document.removeEventListener('visibilitychange', onVisibility); window.removeEventListener('blur', onBlur); window.removeEventListener('focus', onFocus); window.removeEventListener('resize', onResize); window.clearInterval(tick) }
    // Focus integrity intentionally reads live values through refs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stage, data, paused])

  const answerQuestion = (answer: Answer) => {
    if (!currentQuestion || feedback || feedbackLock.current) return
    feedbackLock.current = true
    const correct = (answer.value || answer.text) === (currentQuestion.correctAnswer.value || currentQuestion.correctAnswer.text)
    const speedPercent = clamp(Math.round((remaining / Math.max(1, currentQuestion.allowedSeconds)) * 100), 0, 100)
    const nextResults = [...resultsRef.current, {
      questionId: currentQuestion.id,
      category: currentQuestion.category,
      correct,
      timedOut: false,
      selected: answer.text,
      selectedKey: answer.value || answer.text,
      speedPercent,
    }]
    resultsRef.current = nextResults
    setResults(nextResults)
    setFeedback({ kind: correct ? 'correct' : 'wrong', countdown: config.feedbackSeconds, correctAnswer: currentQuestion.correctAnswer.text })
  }

  const advanceQuestion = () => {
    setFeedback(null)
    feedbackLock.current = false
    if (pausePending) {
      setPausePending(false)
      setPaused(true)
      return
    }
    if (currentIndex >= questions.length - 1) {
      setStage('result')
      return
    }
    setCurrentIndex((index) => index + 1)
  }

  const resumeAssessment = () => {
    setPaused(false)
    if (currentIndex >= questions.length - 1) {
      setStage('result')
      return
    }
    setCurrentIndex((index) => index + 1)
  }

  const timeoutNext = () => {
    if (!currentQuestion || feedback?.kind !== 'timeout') return
    const nextResults = [...resultsRef.current, {
      questionId: currentQuestion.id,
      category: currentQuestion.category,
      correct: false,
      timedOut: true,
      selected: null,
      selectedKey: null,
      speedPercent: 0,
    }]
    resultsRef.current = nextResults
    setResults(nextResults)
    advanceQuestion()
  }

  const timeoutAgain = () => {
    if (!currentQuestion || feedback?.kind !== 'timeout' || feedback.retryCountdown <= 0) return
    const pool = reserves[currentQuestion.category] || []
    const replacement = pool[0]
    if (!replacement) return
    setReserves((current) => ({ ...current, [currentQuestion.category]: current[currentQuestion.category].slice(1) }))
    setQuestions((current) => current.map((question, index) => index === currentIndex ? replacement : question))
    setFeedback(null)
    feedbackLock.current = false
    setQuestionRun((value) => value + 1)
  }

  const continueFromResult = () => {
    if (!testSummary || !chosenDivision) return
    setStage('finish')
  }

  const startAlternativeAssessment = (mode: AssessmentMode) => {
    window.history.replaceState({}, '', mode === 'discover' ? '/apply' : `/apply?division=${mode}`)
    beginTest(mode)
  }

  const finishApplication = async () => {
    if (!testSummary || !data) return
    const divisionName = data.divisions.find((division) => division.id === chosenDivision)?.name || chosenDivision
    const requestPayload: Record<string, unknown> = {
      email: form.email,
      name: form.fullName,
      division: divisionName,
      assessment_mode: assessmentMode,
      birthdate: form.birthdate,
      gender: form.gender,
      department: selectedDepartment?.name || '',
      course: selectedCourse?.name || '',
      year_level: form.yearLevel,
      school_id: form.schoolId,
      overall: testSummary.overall,
      confidence: testSummary.confidence,
      focus_incidents: focusIncidents,
      results,
    }
    setFinishMessage('Submitting your application…')
    try {
      const response = await fetch('/api/application/finish', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestPayload),
      })
      const payload = await response.json().catch(() => ({}))
      if (!response.ok) throw new Error(payload.error || 'Your application could not be submitted. Please try again.')
      const record: StoredApplication = {
        applicationNumber: payload.application_number,
        status: 'Submitted',
        submittedAt: new Date().toISOString(),
        division: divisionName,
        overall: Number(payload.overall ?? testSummary.overall),
        confidence: Number(payload.confidence ?? testSummary.confidence),
        form,
        assessmentMode,
      }
      localStorage.setItem('aits-application-record', JSON.stringify(record))
      setStoredApplication(record)
      setFinishMessage(payload.mailed ? `Application submitted. A confirmation was sent to ${form.email}.` : 'Application submitted successfully. The AITS server validated your assessment answers.')
    } catch (error) {
      if (!navigator.onLine || error instanceof TypeError) {
        try {
          const queued = await queueApplicationSubmission(requestPayload)
          const record: StoredApplication = {
            applicationNumber: 'Pending sync',
            pendingId: queued.id,
            status: 'Pending Sync',
            submittedAt: queued.queuedAt,
            division: divisionName,
            overall: testSummary.overall,
            confidence: testSummary.confidence,
            form,
            assessmentMode,
          }
          localStorage.setItem('aits-application-record', JSON.stringify(record))
          setStoredApplication(record)
          setFinishMessage('You are offline. Your completed assessment is stored on this device and will be submitted for server validation when the connection returns.')
          return
        } catch {
          // Fall through to the original submission error when local persistence is unavailable.
        }
      }
      setFinishMessage(error instanceof Error ? error.message : 'Your application could not be submitted. Please try again.')
    }
  }

  const cancelStoredApplication = async () => {
    if (!storedApplication) return
    setCancelConfirm(false)
    if (storedApplication.pendingId) {
      try { await removeQueuedSubmission(storedApplication.pendingId) } catch { /* local queue cleanup is best effort */ }
      localStorage.removeItem('aits-application-record')
      setStoredApplication(null)
      setFinishMessage('Pending offline application removed from this device.')
      setShowStatus(false)
      return
    }
    try {
      const response = await fetch('/api/application/cancel', {
        method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ application_number: storedApplication.applicationNumber }),
      })
      const payload = await response.json().catch(() => ({}))
      if (!response.ok) throw new Error(payload.error || 'Application could not be cancelled.')
      const next = { ...storedApplication, status: 'Cancelled' }
      localStorage.setItem('aits-application-record', JSON.stringify(next))
      setStoredApplication(next)
      setFinishMessage(payload.message || 'Application cancelled.')
    } catch (error) {
      setFinishMessage(error instanceof Error ? error.message : 'Application could not be cancelled.')
    }
  }

  const startFromStored = (reapply = false) => {
    if (reapply) {
      localStorage.removeItem('aits-application-record')
      setStoredApplication(null)
      setForm((current) => ({ ...emptyForm, email: user?.email || current.email, fullName: user?.name || current.fullName }))
    } else if (storedApplication) {
      setForm(storedApplication.form)
      setAssessmentMode(storedApplication.assessmentMode || 'discover')
    }
    setShowStatus(false)
    setStage('identity')
  }

  const onCopyDuringQuiz = (event: ClipboardEvent) => {
    if (stage !== 'quiz' || !currentQuestion) return
    event.preventDefault()
    const original = `${currentQuestion.question} ${currentQuestion.renderedAnswers.map((answer) => answer.text).join(' ')}`
    const scrambled = shuffle(original.split(/\s+/)).join(' ')
    event.clipboardData.setData('text/plain', `${scrambled}\n\nAITS assessment: copying test content is intentionally disrupted.`)
    setFocusMessage('Sorry, our organization prohibits cheating. Copying assessment content is intentionally disrupted.')
    window.setTimeout(() => setFocusMessage(null), 3800)
  }

  if (!data) {
    return <PageLoading label="Opening AITS application"/>
  }

  if (showStatus && storedApplication) {
    return <div className="application-page">
      <ApplicationHeader theme={theme} setTheme={setTheme} now={now} />
      <main className="status-page-wrap">
        <section className="application-status-view">
          <div className="status-heading">
            <div className="status-hero-mark"><AitsMark /></div>
            <div><p className="apply-eyebrow">My application</p><h1>{storedApplication.status === 'Cancelled' ? 'Application cancelled' : 'Application status'}</h1><p>Keep this reference available while AITS reviews your application.</p></div>
            <span className={`application-status-chip ${storedApplication.status.toLowerCase().replace(/\s+/g,'-')}`}>{storedApplication.status}</span>
          </div>
          <div className="status-reference"><span>APPLICATION NUMBER</span><strong>{storedApplication.applicationNumber}</strong></div>
          <div className="status-record-grid">
            <div><span>Division</span><strong>{storedApplication.division}</strong></div>
            <div><span>Year level</span><strong>{storedApplication.form.yearLevel || '—'}</strong></div>
            <div><span>Assessment</span><strong>{storedApplication.overall}%</strong><small>overall score</small></div>
            <div><span>Response</span><strong>{storedApplication.confidence}%</strong><small>speed score</small></div>
            <div><span>Submitted</span><strong>{new Date(storedApplication.submittedAt).toLocaleDateString()}</strong></div>
            <div><span>Account</span><strong>{storedApplication.form.email}</strong></div>
          </div>
          {finishMessage && <div className="status-message">{finishMessage}</div>}
          <div className="status-actions">
            <a className="apply-button primary" href="/">Back to AITS</a>
            <button className="apply-button secondary" onClick={() => startFromStored(true)}>New application</button>
            {storedApplication.status !== 'Cancelled' && <button className="apply-button danger ghost-danger" onClick={()=>setCancelConfirm(true)}>Cancel</button>}
          </div>
        </section>
        {cancelConfirm && <div className="application-modal-layer" role="dialog" aria-modal="true"><div className="application-confirm-modal"><div className="confirm-icon"><Icon name="info"/></div><h2>Cancel this application?</h2><p>Your current application will be marked as cancelled. You can start a new application later.</p><div><button className="apply-button secondary" onClick={()=>setCancelConfirm(false)}>Keep application</button><button className="apply-button danger" onClick={cancelStoredApplication}>Cancel application</button></div></div></div>}
      </main>
    </div>
  }

  const modeLabel = specificDivision?.name || 'Division discovery'
  const specificQuestionCount = config.specificDivisionQuestions ?? 15

  return (
    <div className="application-page" onCopy={onCopyDuringQuiz}>
      <ApplicationHeader theme={theme} setTheme={setTheme} now={now} />
      <main className="application-main">
        {stage !== 'quiz' && stage !== 'loading' && <ApplicationProgress stage={stage} />}

        {stage === 'identity' && (
          <section className="application-panel">
            <div className="panel-copy">
              <span className="step-badge">01 · Visitor account</span>
              <h1>Start your AITS application.</h1>
              <p>{isSpecific ? <>You are starting an application specifically for <strong>{modeLabel}</strong>. Continue with a visitor profile so your application can be identified and revisited.</> : <>Connect Google or GitHub so AITS can identify your application and let you revisit its status later.</>}</p>
              <div className="privacy-note"><span>🔒</span><div><strong>Visitor identity only</strong><small>Visitor sign-in is separate from your AITS member credentials.</small></div></div>
            </div>
            <div className="form-card">
              {user ? (
                <div className="connected-identity">
                  {user.picture ? <img src={user.picture} alt="" /> : <span>{user.name.slice(0, 1)}</span>}
                  <div><small>CONNECTED VISITOR</small><strong>{user.name}</strong><p>{user.email}</p></div>
                </div>
              ) : (
                <>
                  <h2>Sign in as a visitor</h2>
                  <p>Choose one provider. AITS requests only the identity information needed for applications and public interaction.</p>
                  <div className="apply-oauth-list">
                    <button onClick={() => loginVisitor('google')} disabled={!capabilities.google_oauth}><GoogleMark /><span><strong>Continue with Google</strong><small>{capabilities.google_oauth ? 'Name · email · profile image' : 'Google sign-in is currently unavailable'}</small></span><b>→</b></button>
                    <button onClick={() => loginVisitor('github')} disabled={!capabilities.github_oauth}><GitHubMark /><span><strong>Continue with GitHub</strong><small>{capabilities.github_oauth ? 'Name · email · profile image' : 'GitHub sign-in is currently unavailable'}</small></span><b>→</b></button>
                  </div>
                </>
              )}
              <label>Email<input value={form.email} readOnly placeholder="Sign in as a visitor to fill your email" /></label>
              <label>Full name<input value={form.fullName} onChange={(event) => updateForm({ fullName: event.target.value })} placeholder="Your full name" disabled={!user} /></label>
              <button className="apply-button primary wide" disabled={!user || !form.email || !form.fullName.trim()} onClick={() => setStage('personal')}>Continue <span>→</span></button>
            </div>
          </section>
        )}

        {stage === 'personal' && (
          <section className="application-panel compact-panel">
            <div className="panel-copy"><span className="step-badge">02 · Personal information</span><h1>Tell us a little about you.</h1><p>These details become part of your application record and help AITS communicate with you appropriately.</p></div>
            <div className="form-card">
              <BirthdatePicker value={form.birthdate} onChange={(birthdate) => updateForm({ birthdate })} />
              <fieldset><legend>Gender</legend><div className="choice-row">{['Male','Female','Prefer not to say'].map((gender) => <button type="button" key={gender} className={form.gender === gender ? 'selected' : ''} onClick={() => updateForm({ gender })}>{gender}</button>)}</div></fieldset>
              <div className="form-actions"><button className="apply-button secondary" onClick={() => setStage('identity')}>Back</button><button className="apply-button primary" disabled={!form.birthdate || !form.gender} onClick={() => setStage('academic')}>Continue →</button></div>
            </div>
          </section>
        )}

        {stage === 'academic' && (
          <section className="application-panel compact-panel">
            <div className="panel-copy"><span className="step-badge">03 · Academic information</span><h1>Connect your application to your school record.</h1><p>Select your current Buenavista Community College academic details so your application can be matched with your student record.</p></div>
            <div className="form-card form-grid">
              <div className="form-field"><span>Department</span><CustomSelect value={form.departmentId} label="Department" placeholder="Select department" options={[{value:'',label:'Select department'},...data.departments.map(department=>({value:department.id,label:department.name}))]} onChange={(value) => updateForm({ departmentId: value, courseId: '', yearLevel: '' })}/></div>
              <div className="form-field"><span>Course <small>Optional when the department has one course</small></span><CustomSelect value={form.courseId} label="Course" placeholder="Select course" options={selectedDepartment?.courses.length===1?selectedDepartment.courses.map(course=>({value:course.id,label:course.name})):[{value:'',label:'Select course'},...(selectedDepartment?.courses||[]).map(course=>({value:course.id,label:course.name}))]} onChange={(value) => updateForm({ courseId: value, yearLevel: '' })} disabled={!selectedDepartment || selectedDepartment.courses.length <= 1}/></div>
              <label>School ID number<input value={form.schoolId} onChange={(event) => updateForm({ schoolId: event.target.value })} placeholder="e.g. 2026-12345" /></label>
              <div className="form-field"><span>Year level</span><CustomSelect value={form.yearLevel} label="Year level" placeholder="Select year level" options={[{value:'',label:'Select year level'},...yearLevels.map(level=>({value:level,label:level}))]} onChange={(value) => updateForm({ yearLevel: value })}/></div>
              <div className="form-actions full"><button className="apply-button secondary" onClick={() => setStage('personal')}>Back</button><button className="apply-button primary" disabled={!form.departmentId || !form.schoolId.trim() || !form.yearLevel} onClick={() => setStage('ready')}>Continue →</button></div>
            </div>
          </section>
        )}

        {stage === 'ready' && (
          <section className="ready-stage">
            <div className="ready-visual"><div className="ready-orbit one"/><div className="ready-orbit two"/><AitsMark /><span className="ready-code">&lt;/ready&gt;</span></div>
            <p className="apply-eyebrow">04 · {isSpecific ? `${modeLabel} assessment` : 'Division-fit assessment'}</p>
            <h1>Alright, {form.gender === 'Male' ? 'Mr.' : form.gender === 'Female' ? 'Ms.' : ''} {form.fullName.split(' ')[0]} — are you ready?</h1>
            {isSpecific
              ? <p>You selected <strong>{modeLabel}</strong>. Your assessment contains {specificQuestionCount} questions from {modeLabel} and {config.generalQuestions} general/BCC questions, randomized from question banks up to your {form.yearLevel || 'current year level'}. There is no division recommendation at the end because you already chose your path.</p>
              : <p>You selected <strong>I don’t know yet</strong>. You will receive 20 randomized questions: 5 from CodeHex, 5 from Illustrados, 5 from Neo-Tech, and 5 general questions. Question difficulty is limited to banks at or below your {form.yearLevel || 'current year level'}.</p>}
            <div className="assessment-path-picker">
              <span>CHOOSE YOUR APPLICATION PATH</span>
              <div>
                {data.divisions.map((division) => <button type="button" key={division.id} className={assessmentMode === division.id ? 'selected' : ''} onClick={() => { setAssessmentMode(division.id); setChosenDivision(division.id); window.history.replaceState({}, '', `/apply?division=${division.id}`) }}><strong>{division.name}</strong><small>15 division questions + 5 general</small></button>)}
                <button type="button" className={assessmentMode === 'discover' ? 'selected' : ''} onClick={() => { setAssessmentMode('discover'); setChosenDivision(''); window.history.replaceState({}, '', '/apply') }}><strong>I don’t know yet</strong><small>Let the assessment help identify your strongest fit</small></button>
              </div>
            </div>
            <div className="ready-rules">
              <div><strong>20</strong><span>questions</span></div>
              <div><strong>2s</strong><span>per character</span></div>
              <div><strong>+15s</strong><span>per image</span></div>
              <div><strong>×3</strong><span>question reserve pool</span></div>
            </div>
            <div className="anti-cheat-card"><strong>Assessment integrity</strong><p>Questions are preloaded before the assessment. Leaving the tab/window starts a remembered 10-second focus timer; each completed interval replaces the current unanswered question. Every third swap adds one question and applies a doubling score penalty (1, 2, 4, 8…). Question and answer order is randomized.</p></div>
            <div className="ready-actions"><button className="apply-button secondary" onClick={() => setStage('academic')}>Back</button><button className="apply-button primary" onClick={() => beginTest()}>Start assessment →</button></div>
          </section>
        )}

        {stage === 'loading' && (
          <section className="test-loading-stage"><div className="binary-rain" aria-hidden="true">0101&nbsp;AITS&nbsp;1100&nbsp;CODE&nbsp;1010&nbsp;DESIGN&nbsp;0011&nbsp;NETWORK</div><div className="test-loader-core"><AitsMark /><div className="loader-rings"><i/><i/><i/></div><small>[ ASSESSMENT ENGINE ]</small><h1>{loadingMessage}</h1><p>Please wait a moment while your unique test is assembled.</p><div className="test-loader-bar"><span /></div></div></section>
        )}

        {stage === 'quiz' && currentQuestion && (
          <section className="quiz-shell">
            <div className="quiz-topbar"><div><span>Question</span><strong>{currentIndex + 1} <small>of {questions.length}</small></strong></div><div className={`quiz-timer ${remaining <= Math.min(15, currentQuestion.allowedSeconds * .15) ? 'danger' : ''}`}><span>TIME LEFT</span><strong>{formatTime(remaining)}</strong></div><div className="quiz-focus-tools"><span>Focus events</span><strong>{focusIncidents}</strong><button type="button" className={pausePending?'queued':''} onClick={()=>setPausePending(value=>!value)}>{pausePending?'Pause queued':'Pause after this question'}</button></div></div>
            {focusCountdown!==null&&<div className="focus-countdown"><strong>{focusCountdown}s</strong><span>Return focus before this reaches zero or this unanswered question will be replaced.</span></div>}
            {windowWarning&&<div className="focus-window-warning">⚠ {windowWarning}</div>}
            <div className="quiz-progress"><span style={{ width: `${((currentIndex + 1) / questions.length) * 100}%` }} /></div>
            <article className="quiz-card">
              <div className="quiz-category">{data.divisions.find((division) => division.id === currentQuestion.category)?.name || 'General · BCC'}</div>
              <h1>{currentQuestion.question}</h1>
              <AssessmentMedia content={currentQuestion.questionContent} fallbackImage={currentQuestion.image ? `/${currentQuestion.image}` : null} />
              <div className="answer-grid">{currentQuestion.renderedAnswers.map((answer, index) => <button key={`${answer.value || answer.text}-${index}`} onClick={() => answerQuestion(answer)} disabled={Boolean(feedback)}><span className="answer-letter">{String.fromCharCode(65 + index)}</span><span className="answer-content"><AssessmentMedia compact content={answer.type ? { type: answer.type, url: answer.url, label: answer.label, text: answer.text } : undefined} fallbackImage={answer.image ? `/${answer.image}` : null}/><strong>{answer.text}</strong></span></button>)}</div>
              <div className="quiz-integrity">AITS assessment · Copying is disrupted · Leaving this tab does not pause your timer.</div>
            </article>
            {focusMessage && <div className="focus-warning">⚠ {focusMessage}</div>}
            {feedback && <FeedbackOverlay feedback={feedback} config={config} data={data} onNext={feedback.kind === 'timeout' ? timeoutNext : advanceQuestion} onAgain={timeoutAgain} canRetry={feedback.kind === 'timeout' && feedback.retryCountdown > 0 && (reserves[currentQuestion.category]?.length || 0) > 0} />}
            {paused&&<div className="quiz-pause-layer"><div className="quiz-paused-card"><Icon name="pause" size={26}/><span>ASSESSMENT PAUSED</span><h2>Your next question has not started.</h2><p>The pause was applied only after the previous question finished. Focus monitoring and the next question timer resume when you continue.</p><button className="apply-button primary" onClick={resumeAssessment}>Resume assessment</button></div></div>}
          </section>
        )}

        {stage === 'result' && testSummary && (
          <section className="result-stage">
            <div className={`result-symbol ${testSummary.passed ? 'passed' : 'failed'}`}>{testSummary.passed ? '✓' : '!'}</div>
            <p className="apply-eyebrow">06 · Assessment result</p>

            {isSpecific ? (
              <>
                <h1>{testSummary.passed ? `Congratulations — you passed the ${modeLabel} assessment.` : `Your ${modeLabel} assessment is below the current passing grade.`}</h1>
                <p>{testSummary.passed ? `Your application can continue directly as a ${modeLabel} applicant. No division recommendation is needed because you chose this division before taking the test.` : 'Would you like to try a different path? Choose another division-specific assessment, or take the broader discovery test if you are not sure yet.'}</p>
                <div className="score-grid specific-score-grid">
                  <div><span>Response speed</span><strong>{testSummary.confidence}%</strong><small>average</small></div>
                  <div><span>{modeLabel}</span><strong>{testSummary.scores[assessmentMode]}%</strong><small>division score</small></div>
                  <div className="general-score"><span>General</span><strong>{testSummary.scores.general}%</strong><small>BCC / general</small></div>
                  <div className="total-score"><span>Total</span><strong>{testSummary.overall}%</strong><small>passing: {config.overallPassingPercent}%</small></div>
                </div>

                {testSummary.penaltyPoints>0&&<div className={`focus-score-penalty ${testSummary.excessivePenalty?'forced':''}`}><strong>Focus penalty: -{testSummary.penaltyPoints}</strong><span>{testSummary.excessivePenalty?'Penalty reached half of the 50% requirement, so the assessment score is forced to 10.':`Raw answer score ${testSummary.rawOverall}% · adjusted score ${testSummary.overall}%`}</span></div>}
                {testSummary.passed ? (
                  <div className="result-actions"><button className="apply-button primary" onClick={continueFromResult}>Yes, continue as {modeLabel} →</button></div>
                ) : (
                  <div className="alternative-assessment">
                    <span>TRY ANOTHER ASSESSMENT</span>
                    <div>
                      {data.divisions.map((division) => <button key={division.id} className={division.id === assessmentMode ? 'current' : ''} onClick={() => startAlternativeAssessment(division.id)}><strong>To be {division.name}</strong><small>{division.id === assessmentMode ? 'Retake this path' : '15 division + 5 general'}</small></button>)}
                      <button onClick={() => startAlternativeAssessment('discover')}><strong>I don’t know yet</strong><small>5 questions from every division + 5 general</small></button>
                    </div>
                  </div>
                )}
              </>
            ) : (
              <>
                <h1>{testSummary.passed ? 'Congratulations, you passed.' : testSummary.qualified.length ? 'Your overall score is low, but we found a strong division match.' : 'Your current result did not reach the qualifying mark.'}</h1>
                <p>{testSummary.passed ? 'Here is how your broader AITS assessment performed.' : testSummary.qualified.length ? 'You may continue with your strongest qualifying division, or take the assessment again.' : 'No division reached the current qualifying threshold, so another attempt is required before continuing.'}</p>
                <div className="score-grid">
                  <div><span>Response speed</span><strong>{testSummary.confidence}%</strong><small>average</small></div>
                  {data.divisions.map((division) => <div key={division.id}><span>{division.name}</span><strong>{testSummary.scores[division.id]}%</strong><small>{testSummary.scores[division.id] >= config.divisionPassingPercent ? 'qualified' : 'below mark'}</small></div>)}
                  <div className="general-score"><span>General</span><strong>{testSummary.scores.general}%</strong><small>BCC / general</small></div>
                  <div className="total-score"><span>Total</span><strong>{testSummary.overall}%</strong><small>passing: {config.overallPassingPercent}%</small></div>
                </div>

                {testSummary.suggested.length > 0 && (
                  <div className="recommendation-card"><span>Suggested division{testSummary.suggested.length > 1 ? 's' : ''}</span><h2>{testSummary.suggested.map((division) => division.name).join(' · ')}</h2><p>{testSummary.suggested.length > 1 ? 'These divisions share the same highest qualifying score. Choose one to continue.' : 'This division has your highest qualifying score.'}</p></div>
                )}

                {testSummary.suggested.length > 1 && <div className="division-choice-results">{testSummary.suggested.map((division) => <button key={division.id} className={chosenDivision === division.id ? 'selected' : ''} onClick={() => setChosenDivision(division.id)}><strong>{division.name}</strong><span>{testSummary.scores[division.id]}%</span></button>)}</div>}

                <div className="result-actions">
                  {testSummary.qualified.length === 0
                    ? <button className="apply-button primary" onClick={() => beginTest('discover')}>Try again</button>
                    : <>
                      {!testSummary.passed && <button className="apply-button secondary" onClick={() => beginTest('discover')}>Try again</button>}
                      <button className="apply-button primary" disabled={!chosenDivision} onClick={continueFromResult}>Yes, continue →</button>
                    </>}
                </div>
                {!testSummary.passed && testSummary.qualified.length > 0 && <div className="followup-note">Because your overall score is below the current passing grade, AITS may provide a follow-up assessment or interview before final acceptance.</div>}
              </>
            )}
          </section>
        )}

        {stage === 'finish' && testSummary && (
          <section className="finish-stage">
            <div className="finish-plane">➤</div>
            <p className="apply-eyebrow">07 · Finish</p>
            <h1>Congratulations, you completed the online test.</h1>
            <p>Please bear with us for a possible follow-up interview. Your application has been submitted for <strong>{data.divisions.find((division) => division.id === chosenDivision)?.name}</strong> applicant, and AITS appreciates the time, effort, and courage you gave to complete it.</p>
            <div className="finish-summary"><div><span>Applicant</span><strong>{form.fullName}</strong></div><div><span>Email</span><strong>{form.email}</strong></div><div><span>Division</span><strong>{data.divisions.find((division) => division.id === chosenDivision)?.name}</strong></div><div><span>Overall</span><strong>{testSummary.overall}%</strong></div></div>
            {storedApplication && <div className="application-number"><span>APPLICATION NUMBER</span><strong>{storedApplication.applicationNumber}</strong></div>}
            {finishMessage && <div className="finish-message">{finishMessage}</div>}
            <button className="apply-button primary finish-button" onClick={storedApplication ? () => { setShowStatus(true); window.scrollTo(0, 0) } : finishApplication}>{storedApplication ? 'View application' : 'Finish application'}</button>
          </section>
        )}
      </main>
    </div>
  )
}

function ApplicationHeader({ theme, setTheme, now }: { theme: ThemeMode; setTheme: (theme: ThemeMode) => void; now: Date }) {
  return <header className="application-header"><a href="/" className="apply-back"><Icon name="back" size={16}/> <span>Back to AITS</span></a><div className="apply-header-brand"><AitsMark /><span><strong>AITS Application</strong><small>Alliance of Information Technology Students</small></span></div><div className="apply-header-actions"><time>{now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</time><button onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')} aria-label="Toggle theme"><Icon name={theme === 'light' ? 'moon' : 'sun'} size={16}/></button></div></header>
}

function ApplicationProgress({ stage }: { stage: Stage }) {
  const stages: Stage[] = ['identity', 'personal', 'academic', 'ready', 'quiz', 'result', 'finish']
  const labels = ['Account', 'Personal', 'Academic', 'Ready', 'Quiz', 'Result', 'Finish']
  const current = Math.max(0, stages.indexOf(stage))
  return <div className="application-progress" aria-label="Application progress">{labels.map((label, index) => <div key={label} className={`${index === current ? 'active' : ''} ${index < current ? 'done' : ''}`}><span>{index < current ? '✓' : index + 1}</span><small>{label}</small></div>)}</div>
}

function FeedbackOverlay({ feedback, config, data, onNext, onAgain, canRetry }: { feedback: Exclude<Feedback, null>; config: AppConfig; data: ApplicationData; onNext: () => void; onAgain: () => void; canRetry: boolean }) {
  const source = feedback.kind === 'correct' ? data.correctMessages : feedback.kind === 'wrong' ? data.wrongMessages : data.timeoutMessages
  const fallback = feedback.kind === 'correct' ? 'Correct.' : feedback.kind === 'wrong' ? 'Keep going.' : 'Time ran out.'
  const message = source.length ? source[feedback.correctAnswer.length % source.length] : fallback
  return <div className="quiz-feedback-layer"><div className={`quiz-feedback ${feedback.kind}`}><div className="feedback-symbol">{feedback.kind === 'correct' ? '✓' : feedback.kind === 'wrong' ? '×' : '⌛'}</div><span>{feedback.kind === 'correct' ? 'CORRECT' : feedback.kind === 'wrong' ? 'INCORRECT' : 'TIMEOUT'}</span><h2>{message}</h2>{feedback.kind !== 'correct' && <p>The correct answer was <strong>{feedback.correctAnswer}</strong>.</p>}{feedback.kind === 'timeout' ? <div className="feedback-actions"><button className="apply-button secondary" disabled={!canRetry} onClick={onAgain}>Again {feedback.retryCountdown > 0 ? `(${feedback.retryCountdown})` : ''}</button><button className="apply-button primary" onClick={onNext}>Next</button></div> : <button className="apply-button primary wide" onClick={onNext}>Next ({feedback.countdown}s)</button>}<small>{feedback.kind === 'timeout' ? 'Next will not run automatically while you may be away.' : `Automatically continues after ${config.feedbackSeconds} seconds.`}</small></div></div>
}
