import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ChangeEvent, type FocusEvent as ReactFocusEvent, type KeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from 'react'
import { createPortal } from 'react-dom'
import './MemberPage.css'
import { Icon } from '../global/Icons'
import { Footer } from '../global/Footer'
import { PageLoading, Skeleton } from '../global/Loading'
import { ShareModal } from '../global/ShareModal'
import { Markdown } from '../global/Markdown'
import { CustomSelect } from '../global/CustomSelect'
import { websocketURL } from '../global/websocket'
import { cacheConversations, cacheMessages, cachePosts, clearMemberCache, getMeta, listOutbox, putMeta, queueOutbox, readConversations, readMessages, readPosts, removeCachedMessage, removeOutbox } from '../global/deviceCache'

type ThemeMode = 'light' | 'dark'
type MemberTab = 'posts' | 'chat' | 'members' | 'notifications' | 'profile'
type Visibility = 'public' | 'organization' | 'division'
type PostIdentity = 'member' | 'organization' | 'division'
type ChatScope = 'private' | 'group' | 'division' | 'organization'

type MemberProfile = {
  account_id:number; name:string; username:string; email:string; avatar?:string; role:string; role_name:string
  division:string; division_id:number; membership_level:string; is_admin:boolean
  member_tags?:string[]; pending_division_request?:{division_id:number;division:string}|null; pending_membership_request?:string
  department?:string; course?:string; year_level?:string; biography?:string; gender?:string; birth_date?:string
  first_name?:string; middle_name?:string; last_name?:string; extension_name?:string; school_id?:string
  contact_number?:string; facebook_url?:string; instagram_url?:string; github_url?:string
}
type FeedPost = {id:number;author:string;role:string;time:string;title?:string;text:string;media?:Attachment[];reactions:number;reaction_summary?:Record<string,number>;my_reaction?:string;comments:number;visibility:Visibility;division?:string;status?:'published'|'pending';identity?:PostIdentity;avatar?:string;division_logo?:string;posted_by?:string;edited?:boolean;history_count?:number;can_edit?:boolean}
type DirectoryMember = {account_id:number;name:string;username:string;avatar:string;division:string;access:string;membership:string;tags:string[];online:boolean;online_seconds?:number;straight_online_seconds?:number;connected_at?:string;last_seen_at?:string}
type NotificationItem = {id:number;type:string;title:string;body:string;time:string;read:boolean;action_url?:string}
type DivisionOption = {id:number;code:string;name:string;description:string}
type CommentItem = {id:number;parent_id:number|null;account_id:number;name:string;avatar:string;body:string;time:string;edited?:boolean;edited_time?:string;history_count?:number;can_edit?:boolean;reactions:number;reacted_by_me:boolean;reaction_summary?:Record<string,number>;my_reaction?:string;tag?:string;account_kind?:'member'|'visitor';division?:string;access?:string;membership?:string}
type PostDetail = FeedPost & {body:string;date:string;media?:Attachment[];reacted_by_me:boolean;comment_items:CommentItem[]}
type Conversation = {id:number;scope:ChatScope;name:string;avatar:string;last:string;time:string;time_iso?:string;unread:boolean;unread_count:number;peer_account_id?:number;active_call?:{call_id:string;mode:CallMode;host_account_id:number;participant_count:number}|null}
type Attachment = {url:string;src?:string;name:string;type:string;size:number}
type ChatMessage = {id:number;sender_account_id:number;sender:string;avatar?:string;body:string;attachments:Attachment[];reply_to:number;time:string;mine?:boolean;seen?:boolean;reactions:number;reaction_summary?:Record<string,number>;my_reaction?:string;client_nonce?:string;queued?:boolean;edited?:boolean;edited_time?:string;history_count?:number;unsent?:boolean;pinned?:boolean;pinned_by?:number;view_limit?:number;view_count?:number;expires_at?:string;poll_id?:number}
type ChatPollOption={id:number;text:string;votes:number}
type ChatPoll={id:number;creator_account_id:number;question:string;created_at:string;closes_at?:string;closed:boolean;my_option_id:number;total_votes:number;options:ChatPollOption[]}
type ChatParticipant={account_id:number;name:string;username?:string;avatar?:string}
type ChatSearchResult={id:number;sender_account_id:number;sender:string;body:string;time:string;mine?:boolean;attachment_match?:boolean}
type ProfileCardData={account_id:number;name:string;username?:string;email?:string;avatar?:string;account_kind:'member'|'visitor';division?:string;access?:string;membership?:string;can_chat?:boolean}
type MentionResult={account_id:number;name:string;username:string;avatar?:string;account_kind:'member'|'visitor';division?:string}
type CallMode='audio'|'video'
const SOCIAL_REACTIONS=['👍','❤️','😂','🎉','😮','😢','😡','👏','🙏','🔥','💯','👀'] as const
type IncomingCall={conversation_id:number;call_id:string;mode:CallMode;from_account_id:number;from_name:string;from_avatar?:string;group?:boolean;host_account_id?:number;participant_count?:number}
type ActiveCall={conversation_id:number;call_id:string;mode:CallMode;started_by_me:boolean;group:boolean;host_account_id:number}
type RemoteCallState={muted:boolean;camera_off:boolean}

type ProfileForm = {
  first_name:string;middle_name:string;last_name:string;extension_name:string;username:string;year_level:string
  birth_date:string;gender:string;course:string;department:string;school_id:string
}

const blankProfile:ProfileForm={first_name:'',middle_name:'',last_name:'',extension_name:'',username:'',year_level:'',birth_date:'',gender:'',course:'',department:'',school_id:''}
function mergeProfileForm(current:MemberProfile,form:ProfileForm):MemberProfile{const name=[form.first_name,form.middle_name,form.last_name,form.extension_name].map(x=>x.trim()).filter(Boolean).join(' ');return {...current,...form,name:name||current.name}}
const membershipOptions=['Member','Core Member','Officer']

function readPendingSecurity(key:string):{until:number;target?:string}|null{try{const value=JSON.parse(sessionStorage.getItem(key)||'null') as {until?:number;target?:string}|null;if(!value||!Number.isFinite(Number(value.until))||Number(value.until)<=Date.now()){sessionStorage.removeItem(key);return null}return {until:Number(value.until),target:String(value.target||'')}}catch{return null}}
function initials(name:string){return name.split(/\s+/).filter(Boolean).slice(0,2).map(x=>x[0]).join('').toUpperCase()||'A'}
function MemberAvatar({src,label}:{src?:string;label:string}){return <img src={src?.trim()||'/logo/aits.png'} alt={src?.trim()?`${label} profile`:'AITS'} loading="lazy"/>}
function closeSocketQuietly(socket:WebSocket|null,reason='replaced'){if(!socket)return;socket.onclose=null;socket.onerror=null;if(socket.readyState===WebSocket.OPEN)socket.close(1000,reason);else if(socket.readyState===WebSocket.CONNECTING)socket.onopen=()=>socket.close(1000,reason)}
function formatDuration(seconds=0){
  let value=Math.max(0,Math.floor(seconds)); const years=Math.floor(value/31536000); value%=31536000; const months=Math.floor(value/2592000); value%=2592000; const days=Math.floor(value/86400); value%=86400; const hours=Math.floor(value/3600); value%=3600; const mins=Math.floor(value/60); const parts:string[]=[]
  if(years)parts.push(`${years}Y`); if(months)parts.push(`${months}MO`); if(days)parts.push(`${days}D`); if(hours)parts.push(`${hours}H`); if(mins||!parts.length)parts.push(`${mins}M`); return parts.slice(0,4).join(' ')
}
function elapsedSince(value?:string){const t=value?Date.parse(value):NaN;return Number.isFinite(t)?Math.max(0,Math.floor((Date.now()-t)/1000)):0}
function formatSecurityTime(seconds:number){const value=Math.max(0,Math.floor(seconds));const minutes=Math.floor(value/60);const remainder=value%60;return `${minutes}:${String(remainder).padStart(2,'0')}`}
function relativeActivity(iso:string|undefined,fallback:string,now=Date.now()){const t=iso?Date.parse(iso):NaN;if(!Number.isFinite(t))return fallback||'';const seconds=Math.max(0,Math.floor((now-t)/1000));if(seconds<45)return'now';if(seconds<3600)return`${Math.floor(seconds/60)}m ago`;if(seconds<86400)return`${Math.floor(seconds/3600)}h ago`;if(seconds<604800)return`${Math.floor(seconds/86400)}d ago`;return fallback||new Date(t).toLocaleDateString()}
function ageFromBirthdate(value?:string){if(!value)return null;const birth=new Date(`${value}T12:00:00`);if(Number.isNaN(birth.getTime()))return null;const now=new Date();let age=now.getFullYear()-birth.getFullYear();const m=now.getMonth()-birth.getMonth();if(m<0||(m===0&&now.getDate()<birth.getDate()))age--;return age>=0?age:null}
function makeNonce(prefix:string){return `${prefix}-${Date.now().toString(36)}-${crypto.randomUUID?.()||Math.random().toString(36).slice(2)}`.slice(0,96)}
function normalizeChatMessageOwnership(message:ChatMessage,accountID?:number){
  const selfID=Number(accountID||0),senderID=Number(message.sender_account_id||0)
  return {...message,mine:selfID>0&&senderID>0?senderID===selfID:!!message.mine}
}
function normalizeChatMessagesOwnership(messages:ChatMessage[],accountID?:number){return messages.map(message=>normalizeChatMessageOwnership(message,accountID))}

async function optimizeImage(file:File){
  if(!file.type.startsWith('image/')||file.size<900_000)return file
  const bitmap=await createImageBitmap(file).catch(()=>null);if(!bitmap)return file
  const scale=Math.min(1,1920/Math.max(bitmap.width,bitmap.height));const canvas=document.createElement('canvas');canvas.width=Math.max(1,Math.round(bitmap.width*scale));canvas.height=Math.max(1,Math.round(bitmap.height*scale));const ctx=canvas.getContext('2d');if(!ctx)return file;ctx.drawImage(bitmap,0,0,canvas.width,canvas.height);bitmap.close()
  const blob=await new Promise<Blob|null>(resolve=>canvas.toBlob(resolve,'image/jpeg',.82));if(!blob||blob.size>=file.size)return file
  const name=file.name.replace(/\.[^.]+$/,'.jpg');return new File([blob],name,{type:'image/jpeg',lastModified:Date.now()})
}
function maxFileSize(file:File){if(file.type.startsWith('image/'))return 5<<20;if(file.type.startsWith('audio/'))return 20<<20;if(file.type.startsWith('video/'))return 40<<20;return 10<<20}
function blobDataURL(blob:Blob){return new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result||''));reader.onerror=()=>reject(reader.error||new Error('Unable to read file'));reader.readAsDataURL(blob)})}

export default function MemberPage(){
  const [theme,setTheme]=useState<ThemeMode>(()=>localStorage.getItem('aits-theme')==='dark'?'dark':'light')
  const [tab,setTabState]=useState<MemberTab>('posts')
  const tabRef=useRef<MemberTab>('posts')
  const lastTabRef=useRef<MemberTab>('posts')
  const [profile,setProfile]=useState<MemberProfile|null>(null)
  const profileRef=useRef<MemberProfile|null>(null)
  const [loading,setLoading]=useState(true)
  const [toast,setToast]=useState('')
  const [memberConfirm,setMemberConfirm]=useState<{title:string;text:string;action:()=>void}|null>(null)
  const [posts,setPosts]=useState<FeedPost[]>([])
  const [feedLoading,setFeedLoading]=useState(false)
  const [hasMore,setHasMore]=useState(true)
  const [cursor,setCursor]=useState<number|null>(null)
  const [postTitle,setPostTitle]=useState('')
  const [postBody,setPostBody]=useState('')
  const [visibility,setVisibility]=useState<Visibility>('organization')
  const [postIdentity,setPostIdentity]=useState<PostIdentity>('member')
  const publishLockRef=useRef(false)
  const [postMediaFiles,setPostMediaFiles]=useState<File[]>([])
  const postMediaRef=useRef<HTMLInputElement|null>(null)
  const [detail,setDetail]=useState<PostDetail|null>(null)
  const detailRef=useRef<PostDetail|null>(null)
  const [commentText,setCommentText]=useState('')
  const [replyToComment,setReplyToComment]=useState<CommentItem|null>(null)
  const [shareTarget,setShareTarget]=useState<FeedPost|null>(null)
  const [members,setMembers]=useState<DirectoryMember[]>([])
  const [onlineNames,setOnlineNames]=useState<Set<string>>(new Set())
  const [memberSearch,setMemberSearch]=useState('')
  const [divisionFilter,setDivisionFilter]=useState('all')
  const [accessFilter,setAccessFilter]=useState('all')
  const [membershipFilter,setMembershipFilter]=useState('all')
  const [notifications,setNotifications]=useState<NotificationItem[]>([])
  const [divisions,setDivisions]=useState<DivisionOption[]>([])
  const [profileForm,setProfileForm]=useState<ProfileForm>(blankProfile)
  const [profileEditing,setProfileEditing]=useState(false)
  const [nameModal,setNameModal]=useState(false)
  const [divisionChoice,setDivisionChoice]=useState(0)
  const [membershipChoice,setMembershipChoice]=useState('')
  const [saving,setSaving]=useState(false)
  const [showEmailPassword,setShowEmailPassword]=useState(false)
  const [showNewPassword,setShowNewPassword]=useState(false)
  const [showConfirmPassword,setShowConfirmPassword]=useState(false)
  const initialEmailSecurity=readPendingSecurity('aits-email-change-pending')
  const initialPasswordSecurity=readPendingSecurity('aits-password-change-pending')
  const [emailStage,setEmailStage]=useState<'request'|'confirm'>(initialEmailSecurity&&initialEmailSecurity.until>Date.now()?'confirm':'request')
  const [emailForm,setEmailForm]=useState({email:initialEmailSecurity?.target||'',password:'',code:''})
  const [passwordStage,setPasswordStage]=useState<'request'|'confirm'>(initialPasswordSecurity&&initialPasswordSecurity.until>Date.now()?'confirm':'request')
  const [passwordForm,setPasswordForm]=useState({code:'',newPassword:'',confirmPassword:''})
  const [emailCodeUntil,setEmailCodeUntil]=useState(initialEmailSecurity?.until||0)
  const [passwordCodeUntil,setPasswordCodeUntil]=useState(initialPasswordSecurity?.until||0)
  const [securityNow,setSecurityNow]=useState(Date.now())
  const presenceRef=useRef<WebSocket|null>(null)
  const mountedRef=useRef(true)
  const inboxRef=useRef<WebSocket|null>(null)
  const inboxRetryRef=useRef<number|null>(null)

  const [conversations,setConversations]=useState<Conversation[]>([])
  const [chatLoading,setChatLoading]=useState(false)
  const [activeConversation,setActiveConversation]=useState<Conversation|null>(null)
  const [chatMessages,setChatMessages]=useState<ChatMessage[]>([])
  const [chatHasMore,setChatHasMore]=useState(false)
  const [chatBefore,setChatBefore]=useState<number|null>(null)
  const [chatText,setChatText]=useState('')
  const [chatEditing,setChatEditing]=useState<ChatMessage|null>(null)
  const [chatAttachments,setChatAttachments]=useState<Attachment[]>([])
  const [chatReply,setChatReply]=useState<ChatMessage|null>(null)
  const [typingNames,setTypingNames]=useState<string[]>([])
  const [chatConnection,setChatConnection]=useState<'connecting'|'online'|'offline'>('offline')
  const [privatePicker,setPrivatePicker]=useState(false)
  const [groupModal,setGroupModal]=useState(false)
  const [groupName,setGroupName]=useState('')
  const [groupMembers,setGroupMembers]=useState<number[]>([])
  const [chatFilter,setChatFilter]=useState<'all'|'private'|'groups'>('all')
  const [chatSearch,setChatSearch]=useState('')
  const [uploading,setUploading]=useState(false)
  const [chatRecording,setChatRecording]=useState(false)
  const [chatParticipants,setChatParticipants]=useState<ChatParticipant[]>([])
  const [profileCard,setProfileCard]=useState<ProfileCardData|null>(null)
  const [profileCardAnchor,setProfileCardAnchor]=useState<{x:number;y:number}|null>(null)
  const profileRequestRef=useRef(0)
  useEffect(()=>{const close=()=>{profileRequestRef.current+=1;setProfileCard(null);setProfileCardAnchor(null)};window.addEventListener('aits-close-floating-profile',close);return()=>window.removeEventListener('aits-close-floating-profile',close)},[])
  const [mentionResults,setMentionResults]=useState<MentionResult[]>([])
  const [editingComment,setEditingComment]=useState<CommentItem|null>(null)
  const [commentHistory,setCommentHistory]=useState<{id:number;body:string;time:string;editor:string}[]>([])
  const [incomingCall,setIncomingCall]=useState<IncomingCall|null>(null)
  const [groupCallNotice,setGroupCallNotice]=useState<IncomingCall|null>(null)
  const [activeCall,setActiveCall]=useState<ActiveCall|null>(null)
  const [remoteStreams,setRemoteStreams]=useState<Record<number,MediaStream>>({})
  const [remoteCallStates,setRemoteCallStates]=useState<Record<number,RemoteCallState>>({})
  const [callMuted,setCallMuted]=useState(false)
  const [callCameraOff,setCallCameraOff]=useState(false)
  const chatSocketRef=useRef<WebSocket|null>(null)
  const activeConversationRef=useRef<number>(0)
  const chatReconnectRef=useRef<number|null>(null)
  const typingTimerRef=useRef<number|null>(null)
  const typingLastSentRef=useRef(0)
  const fileInputRef=useRef<HTMLInputElement|null>(null)
  const audioInputRef=useRef<HTMLInputElement|null>(null)
  const chatRecorderRef=useRef<MediaRecorder|null>(null)
  const chatStreamRef=useRef<MediaStream|null>(null)
  const soundContextRef=useRef<AudioContext|null>(null)
  const notificationUnreadRef=useRef(0)
  const localCallStreamRef=useRef<MediaStream|null>(null)
  const peerConnectionsRef=useRef<Map<number,RTCPeerConnection>>(new Map())
  const activeCallRef=useRef<typeof activeCall>(null)
  const pendingJoinCallRef=useRef<{conversation_id:number;call_id:string;mode:CallMode}|null>(null)
  const flushRunningRef=useRef(false)

  useEffect(()=>{document.documentElement.dataset.theme=theme;localStorage.setItem('aits-theme',theme)},[theme])
  useEffect(()=>{if(!toast)return;const id=window.setTimeout(()=>setToast(''),3200);return()=>clearTimeout(id)},[toast])
  useEffect(()=>{if(emailCodeUntil<=Date.now()&&passwordCodeUntil<=Date.now())return;const id=window.setInterval(()=>setSecurityNow(Date.now()),1000);return()=>window.clearInterval(id)},[emailCodeUntil,passwordCodeUntil])
  useEffect(()=>{if(emailStage==='confirm'&&emailCodeUntil>0&&securityNow>=emailCodeUntil){sessionStorage.removeItem('aits-email-change-pending');setEmailCodeUntil(0);setEmailStage('request');setEmailForm(current=>({...current,password:'',code:''}))}},[emailStage,emailCodeUntil,securityNow])
  useEffect(()=>{if(passwordStage==='confirm'&&passwordCodeUntil>0&&securityNow>=passwordCodeUntil){sessionStorage.removeItem('aits-password-change-pending');setPasswordCodeUntil(0);setPasswordStage('request');setPasswordForm({code:'',newPassword:'',confirmPassword:''})}},[passwordStage,passwordCodeUntil,securityNow])
  useEffect(()=>{const updateViewport=()=>{const vv=window.visualViewport;const viewportHeight=Math.round(vv?.height||window.innerHeight),viewportTop=Math.round(vv?.offsetTop||0);const keyboardOpen=!!vv&&(window.innerHeight-vv.height-vv.offsetTop)>120;document.documentElement.style.setProperty('--aits-visual-height',`${viewportHeight}px`);document.documentElement.style.setProperty('--aits-visual-top',`${viewportTop}px`);document.documentElement.classList.toggle('aits-keyboard-open',keyboardOpen)};const unlockSound=()=>{try{const Ctx=window.AudioContext||(window as typeof window&{webkitAudioContext?:typeof AudioContext}).webkitAudioContext;if(Ctx&&!soundContextRef.current)soundContextRef.current=new Ctx();void soundContextRef.current?.resume()}catch{/* audio is optional */}};updateViewport();window.visualViewport?.addEventListener('resize',updateViewport);window.visualViewport?.addEventListener('scroll',updateViewport);window.addEventListener('resize',updateViewport);window.addEventListener('pointerdown',unlockSound,{once:true});return()=>{window.visualViewport?.removeEventListener('resize',updateViewport);window.visualViewport?.removeEventListener('scroll',updateViewport);window.removeEventListener('resize',updateViewport);window.removeEventListener('pointerdown',unlockSound);document.documentElement.style.removeProperty('--aits-visual-height');document.documentElement.style.removeProperty('--aits-visual-top');document.documentElement.classList.remove('aits-keyboard-open');void soundContextRef.current?.close().catch(()=>undefined);soundContextRef.current=null}},[])
  useEffect(()=>{activeCallRef.current=activeCall},[activeCall])
  useEffect(()=>{if(!incomingCall||incomingCall.group)return;playCallRing();const id=window.setInterval(playCallRing,1350);return()=>window.clearInterval(id)},[incomingCall?.call_id,incomingCall?.group])
  useEffect(()=>{if(!groupCallNotice||activeCallRef.current)return;playCallRing();const id=window.setInterval(playCallRing,4000);return()=>window.clearInterval(id)},[groupCallNotice?.call_id])
  useEffect(()=>{detailRef.current=detail},[detail])
  useEffect(()=>{tabRef.current=tab},[tab])
  useEffect(()=>{mountedRef.current=true;void initialize();const onVisibility=()=>{if(document.visibilityState!=='visible'||!mountedRef.current)return;if(navigator.onLine){if(!presenceRef.current||presenceRef.current.readyState>WebSocket.OPEN)connectPresence();if(!inboxRef.current||inboxRef.current.readyState>WebSocket.OPEN)connectInbox();void flushOutbox()}const socket=chatSocketRef.current;if(activeConversationRef.current&&socket?.readyState===WebSocket.OPEN)socket.send(JSON.stringify({type:'seen'}));void loadChats();void loadNotifications()};const onOnline=()=>{connectPresence();connectInbox();if(activeConversationRef.current)connectChat(activeConversationRef.current);void flushOutbox();void loadFeed();void loadChats()};const onPop=()=>{if(detailRef.current){detailRef.current=null;setDetail(null);setReplyToComment(null);return}if(activeConversationRef.current){closeConversation(false);return}if(tabRef.current==='chat'){setTabState(lastTabRef.current==='chat'?'posts':lastTabRef.current)}};document.addEventListener('visibilitychange',onVisibility);window.addEventListener('focus',onVisibility);window.addEventListener('online',onOnline);window.addEventListener('popstate',onPop);return()=>{mountedRef.current=false;document.removeEventListener('visibilitychange',onVisibility);window.removeEventListener('focus',onVisibility);window.removeEventListener('online',onOnline);window.removeEventListener('popstate',onPop);const presence=presenceRef.current;presenceRef.current=null;closeSocketQuietly(presence,'member page cleanup');const inbox=inboxRef.current;inboxRef.current=null;closeSocketQuietly(inbox,'member page cleanup');if(inboxRetryRef.current)window.clearTimeout(inboxRetryRef.current);activeConversationRef.current=0;if(chatReconnectRef.current)window.clearTimeout(chatReconnectRef.current);const socket=chatSocketRef.current;closeSocketQuietly(socket,'member page cleanup');if(chatRecorderRef.current?.state==='recording')chatRecorderRef.current.stop();chatStreamRef.current?.getTracks().forEach(t=>t.stop());endCall(false)}},[])

  async function api(path:string,init?:RequestInit){const r=await fetch(path,{credentials:'include',cache:'no-store',...init});const p=await r.json().catch(()=>({}));if(!r.ok)throw new Error(p.error||'Please try again.');return p}
  function setTab(next:MemberTab){if(next==='chat'&&tab!=='chat'){lastTabRef.current=tab;history.pushState({memberTab:'chat'},'', '/member?tab=chat')}if(next!=='chat'){lastTabRef.current=next;if(tab==='chat')closeConversation(false);if(location.pathname.startsWith('/member/chat/')||location.search.includes('tab=chat'))history.replaceState({},'', '/member')}setTabState(next);if(next==='chat')void loadChats()}
  function closeChatSocket(){if(chatReconnectRef.current){window.clearTimeout(chatReconnectRef.current);chatReconnectRef.current=null}const socket=chatSocketRef.current;chatSocketRef.current=null;closeSocketQuietly(socket,'chat closed');setChatConnection('offline')}
  function closeConversation(updateHistory=true){activeConversationRef.current=0;closeChatSocket();setActiveConversation(null);setChatMessages([]);setTypingNames([]);setChatReply(null);setChatParticipants([]);if(updateHistory&&location.pathname.startsWith('/member/chat/'))history.replaceState({memberTab:'chat'},'', '/member?tab=chat')}
  function backFromChat(){closeConversation(false);setTabState(lastTabRef.current==='chat'?'posts':lastTabRef.current);history.replaceState({},'', '/member')}

  async function initialize(){
    const session=await fetch('/api/member/session',{credentials:'include',cache:'no-store'}).then(r=>r.ok?r.json():null).catch(()=>null)
    if(!session?.authenticated){
      const cachedProfile=await getMeta<MemberProfile>('member-profile').catch(()=>null)
      if(!navigator.onLine&&cachedProfile){
        profileRef.current=cachedProfile;setProfile(cachedProfile);localStorage.setItem('aits-last-mode','member')
        const cachedConversations=await readConversations<Conversation>().catch(()=>[])
        setPosts(await readPosts<FeedPost>(25).catch(()=>[]));setMembers(await getMeta<DirectoryMember[]>('member-directory').catch(()=>null)||[]);setDivisions(await getMeta<DivisionOption[]>('member-divisions').catch(()=>null)||[]);setConversations(cachedConversations)
        const chatMatch=location.pathname.match(/^\/member\/chat\/(\d+)$/)
        if(chatMatch){const chatID=Number(chatMatch[1]);const conversation=cachedConversations.find(c=>c.id===chatID);if(conversation){setTabState('chat');tabRef.current='chat';activeConversationRef.current=chatID;setActiveConversation(conversation);setChatMessages(normalizeChatMessagesOwnership(await readMessages<ChatMessage>(chatID).catch(()=>[]),cachedProfile.account_id));setChatConnection('offline')}}
        else {const requestedTab=new URLSearchParams(location.search).get('tab') as MemberTab|null;if(requestedTab&&['posts','chat','members','notifications','profile'].includes(requestedTab)){setTabState(requestedTab);tabRef.current=requestedTab}}
        setLoading(false);return
      }
      location.replace(`/login?return=${encodeURIComponent(location.pathname+location.search)}`);return
    }
    await Promise.allSettled([loadProfile(),loadFeed(),loadDirectory(),loadNotifications(),loadDivisions(),loadChats()])
    connectPresence();connectInbox();void flushOutbox();setLoading(false)
    const requestedTab=new URLSearchParams(location.search).get('tab') as MemberTab|null;if(requestedTab&&['posts','chat','members','notifications','profile'].includes(requestedTab)){setTabState(requestedTab);tabRef.current=requestedTab}
    const postMatch=location.pathname.match(/^\/member\/posts\/(\d+)$/);if(postMatch){setTabState('posts');tabRef.current='posts';void openPost(Number(postMatch[1]),false)}
    const chatMatch=location.pathname.match(/^\/member\/chat\/(\d+)$/);if(chatMatch){const chatID=Number(chatMatch[1]);const cached=(await readConversations<Conversation>().catch(()=>[])).find(c=>c.id===chatID);if(cached){setTabState('chat');tabRef.current='chat';void openConversation(cached,false)}}
    const requestedAccount=Number(new URLSearchParams(location.search).get('chat_account')||0);if(requestedAccount>0){setTabState('chat');tabRef.current='chat';history.replaceState({memberTab:'chat'},'', '/member?tab=chat');void createConversation('private',requestedAccount)}
  }
  async function loadProfile(){const p=await api('/api/member/me');const m=p.member as MemberProfile;profileRef.current=m;setProfile(m);setProfileForm({first_name:m.first_name||m.name.split(' ')[0]||'',middle_name:m.middle_name||'',last_name:m.last_name||m.name.split(' ').slice(1).join(' ')||'',extension_name:m.extension_name||'',username:m.username||'',year_level:m.year_level||'',birth_date:m.birth_date||'',gender:m.gender||'',course:m.course||'',department:m.department||'',school_id:m.school_id||''});setEmailForm({email:m.email||'',password:'',code:''});setMembershipChoice(m.membership_level||'Member');localStorage.setItem('aits-last-mode','member');void putMeta('member-profile',m);void putMeta('last-mode','member');try{localStorage.setItem('aits-member-profile-cache',JSON.stringify(m))}catch{/* optional cache */}}
  async function loadFeed(){const cached=await readPosts<FeedPost>(25).catch(()=>[]);if(cached.length)setPosts(cached);setFeedLoading(true);try{const p=await api('/api/member/feed?limit=10');setPosts(p.posts||[]);setHasMore(!!p.has_more);setCursor(p.next_cursor??null);void cachePosts(p.posts||[])}catch{if(!cached.length)setToast('Community posts are temporarily unavailable.')}finally{setFeedLoading(false)}}
  async function loadMore(){if(feedLoading||!hasMore)return;setFeedLoading(true);try{const p=await api(`/api/member/feed?limit=10${cursor?`&before=${cursor}`:''}`);setPosts(current=>{const map=new Map(current.map(x=>[x.id,x]));(p.posts||[]).forEach((x:FeedPost)=>map.set(x.id,x));return[...map.values()].sort((a,b)=>b.id-a.id)});setHasMore(!!p.has_more);setCursor(p.next_cursor??null);void cachePosts(p.posts||[])}catch(e){setToast(e instanceof Error?e.message:'Earlier posts could not be loaded.')}finally{setFeedLoading(false)}}
  async function loadDirectory(){try{const p=await api('/api/member/directory');const rows=p.members||[];setMembers(rows);void putMeta('member-directory',rows)}catch{const cached=await getMeta<DirectoryMember[]>('member-directory').catch(()=>null);if(cached)setMembers(cached)}}
  async function loadNotifications(){try{const p=await api('/api/member/notifications');const items=(p.notifications||[]) as NotificationItem[];setNotifications(items);notificationUnreadRef.current=items.filter(n=>!n.read).length}catch{/* retain current notifications */}}
  async function loadDivisions(){try{const p=await api('/api/member/divisions');const rows=p.divisions||[];setDivisions(rows);void putMeta('member-divisions',rows)}catch{const cached=await getMeta<DivisionOption[]>('member-divisions').catch(()=>null);if(cached)setDivisions(cached)}}
  function connectPresence(){
    if(!mountedRef.current)return
    const old=presenceRef.current;presenceRef.current=null;closeSocketQuietly(old,'presence replaced')
    const ws=new WebSocket(websocketURL('/ws/members'));presenceRef.current=ws
    let heartbeat:number|undefined
    ws.onopen=()=>{heartbeat=window.setInterval(()=>{if(ws.readyState===WebSocket.OPEN)ws.send(JSON.stringify({type:'ping'}))},25000)}
    ws.onmessage=e=>{try{const p=JSON.parse(e.data);if(p.type==='presence'){setOnlineNames(new Set((p.members||[]).map((x:{username:string})=>String(x.username||'').toLowerCase())));void loadDirectory()}}catch{/* ignore malformed event */}}
    ws.onclose=()=>{if(heartbeat)window.clearInterval(heartbeat);if(presenceRef.current===ws)presenceRef.current=null;if(!mountedRef.current)return;window.setTimeout(()=>{if(mountedRef.current&&document.visibilityState==='visible')connectPresence()},1800)}
  }
  function playMemberTone(kind:'message'|'notification'){
    try{
      const Ctx=window.AudioContext||(window as typeof window&{webkitAudioContext?:typeof AudioContext}).webkitAudioContext
      if(!Ctx)return
      const ctx=soundContextRef.current||(soundContextRef.current=new Ctx())
      if(ctx.state==='suspended')void ctx.resume()
      if(ctx.state!=='running')return
      const oscillator=ctx.createOscillator(),gain=ctx.createGain();oscillator.type='sine';oscillator.frequency.value=kind==='message'?660:880;gain.gain.setValueAtTime(.0001,ctx.currentTime);gain.gain.exponentialRampToValueAtTime(.055,ctx.currentTime+.012);gain.gain.exponentialRampToValueAtTime(.0001,ctx.currentTime+.16);oscillator.connect(gain);gain.connect(ctx.destination);oscillator.start();oscillator.stop(ctx.currentTime+.17)
    }catch{/* notification sound is optional */}
  }
  function playCallRing(){
    try{
      const Ctx=window.AudioContext||(window as typeof window&{webkitAudioContext?:typeof AudioContext}).webkitAudioContext
      if(!Ctx)return
      const ctx=soundContextRef.current||(soundContextRef.current=new Ctx())
      if(ctx.state==='suspended')void ctx.resume()
      if(ctx.state!=='running')return
      ;[0,0.23].forEach((offset,index)=>{const oscillator=ctx.createOscillator(),gain=ctx.createGain();oscillator.type='sine';oscillator.frequency.value=index?784:659;const start=ctx.currentTime+offset;gain.gain.setValueAtTime(.0001,start);gain.gain.exponentialRampToValueAtTime(.075,start+.018);gain.gain.exponentialRampToValueAtTime(.0001,start+.19);oscillator.connect(gain);gain.connect(ctx.destination);oscillator.start(start);oscillator.stop(start+.2)})
    }catch{/* ringing is optional when browser audio is blocked */}
  }
  function connectInbox(){
    if(!mountedRef.current)return
    if(inboxRetryRef.current){window.clearTimeout(inboxRetryRef.current);inboxRetryRef.current=null}
    const old=inboxRef.current;inboxRef.current=null;closeSocketQuietly(old,'inbox replaced')
    const ws=new WebSocket(websocketURL('/ws/member/inbox'));inboxRef.current=ws
    let heartbeat:number|undefined
    ws.onopen=()=>{heartbeat=window.setInterval(()=>{if(ws.readyState===WebSocket.OPEN)ws.send(JSON.stringify({type:'ping'}))},25000)}
    ws.onmessage=e=>{
      try{
        const p=JSON.parse(e.data)
        if(p.type==='conversation_update'||p.type==='conversation_created'){
          if(p.type==='conversation_update'&&p.sender_account_id&&p.sender_account_id!==profileRef.current?.account_id)playMemberTone('message')
          void loadChats()
          if(p.conversation_id===activeConversationRef.current&&chatSocketRef.current?.readyState!==WebSocket.OPEN)connectChat(activeConversationRef.current)
        }
        if(p.type==='notification_update'){playMemberTone('notification');void loadNotifications()}
        if(p.type==='call_start'&&Number(p.from_account_id)!==profileRef.current?.account_id){
          const notice:IncomingCall={conversation_id:Number(p.conversation_id),call_id:String(p.call_id||''),mode:p.mode==='video'?'video':'audio',from_account_id:Number(p.from_account_id),from_name:String(p.from_name||'AITS member'),from_avatar:String(p.from_avatar||''),group:!!p.group,host_account_id:Number(p.host_account_id||p.from_account_id||0),participant_count:Number(p.participant_count||1)}
          if(notice.group){playMemberTone('notification');setGroupCallNotice(notice)}
          else if(!activeCallRef.current){setIncomingCall(notice)}
          void loadChats()
        }
        if(p.type==='call_end'){
          if(activeCallRef.current?.call_id===String(p.call_id||''))endCall(false)
          setIncomingCall(current=>current?.call_id===String(p.call_id||'')?null:current)
          setGroupCallNotice(current=>current?.call_id===String(p.call_id||'')?null:current)
          void loadChats()
        }
        if((p.type==='ready'||p.type==='pong')&&typeof p.notification_unread==='number'){
          const next=Math.max(0,p.notification_unread)
          if(p.type==='pong'&&next>notificationUnreadRef.current){playMemberTone('notification');void loadNotifications()}
          notificationUnreadRef.current=next
        }
      }catch{/* ignore malformed event */}
    }
    ws.onclose=()=>{if(heartbeat)window.clearInterval(heartbeat);if(inboxRef.current===ws)inboxRef.current=null;if(!mountedRef.current)return;inboxRetryRef.current=window.setTimeout(()=>{inboxRetryRef.current=null;if(mountedRef.current&&document.visibilityState==='visible')connectInbox()},1800)}
  }

  async function sendPostNow(nonce:string,title:string,body:string,audience:Visibility,identity:PostIdentity,files:File[]){const media:Attachment[]=[];for(const original of files.slice(0,12)){let file=original;if(file.type.startsWith('image/'))file=await optimizeImage(file);const fd=new FormData();fd.append('file',file);const uploaded=await api('/api/member/media',{method:'POST',body:fd});if(uploaded.media)media.push(uploaded.media)}return api('/api/member/posts',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({title,body:body||'Shared media',visibility:audience,identity,media,client_nonce:nonce})})}
  async function publish(){if(publishLockRef.current||(!postBody.trim()&&!postMediaFiles.length))return;publishLockRef.current=true;const nonce=makeNonce('post');const title=postTitle.trim(),body=postBody.trim(),files=[...postMediaFiles],audience=visibility,identity=postIdentity;let accepted=false;setUploading(true);try{if(!navigator.onLine)throw new TypeError('offline');const p=await sendPostNow(nonce,title,body,audience,identity,files);accepted=true;setToast(p.status==='pending'?'Your public post was sent for review.':'Post published.');await loadFeed()}catch(e){if(!navigator.onLine||e instanceof TypeError){await queueOutbox({nonce,kind:'post',created_at:Date.now(),payload:{title,body,audience,identity,files}}).catch(()=>undefined);const local:FeedPost={id:-Date.now(),author:profileRef.current?.name||'You',role:profileRef.current?.role_name||'Member',time:'Queued · offline',title,text:body||'Shared media',reactions:0,comments:0,visibility:audience,status:audience==='public'?'pending':'published',identity};setPosts(current=>[local,...current]);void cachePosts([local]);accepted=true;setToast('Post saved on this device and will sync when you are online.')}else setToast(e instanceof Error?e.message:'Your post could not be published.')}finally{if(accepted){setPostTitle('');setPostBody('');setPostMediaFiles([]);if(postMediaRef.current)postMediaRef.current.value=''}setUploading(false);publishLockRef.current=false}}
  function choosePostMedia(event:ChangeEvent<HTMLInputElement>){const raw=(Array.from(event.target.files||[]) as File[]).filter(f=>f.type.startsWith('image/')||f.type.startsWith('video/'));const files=raw.filter(file=>{const ok=file.size<=maxFileSize(file);if(!ok)setToast(`${file.name} exceeds the allowed upload size.`);return ok});setPostMediaFiles(current=>{const all=[...current];for(const file of files){const duplicate=all.some(item=>item.name===file.name&&item.size===file.size&&item.lastModified===file.lastModified);if(!duplicate&&all.length<12)all.push(file)}if(current.length+files.length>12)setToast('A post can contain up to 12 images or videos.');return all});event.currentTarget.value=''}
  async function uploadAvatar(file:File){setSaving(true);let selected=file;try{if(file.type.startsWith('image/'))selected=await optimizeImage(file);if(!navigator.onLine)throw new TypeError('offline');const fd=new FormData();fd.append('file',selected);const result=await api('/api/member/avatar',{method:'POST',body:fd});if(result?.url)setProfile(current=>current?{...current,avatar:String(result.url)}:current);setToast('Profile image updated.');await Promise.all([loadProfile(),loadDirectory(),loadFeed(),loadChats()])}catch(e){if(!navigator.onLine||e instanceof TypeError){const nonce=makeNonce('avatar');await queueOutbox({nonce,kind:'avatar',created_at:Date.now(),payload:{file:selected}}).catch(()=>undefined);const current=profileRef.current;if(current){const localAvatar=await blobDataURL(selected).catch(()=>current.avatar||'');const next={...current,avatar:localAvatar||current.avatar};profileRef.current=next;setProfile(next);void putMeta('member-profile',next);try{localStorage.setItem('aits-member-profile-cache',JSON.stringify(next))}catch{/* optional cache */}}setToast('Profile image saved on this device and will sync when online.')}else setToast(e instanceof Error?e.message:'Profile image could not be updated.')}finally{setSaving(false)}}
  async function updateOwnPost(post:FeedPost,updates:{title:string;body:string;visibility:Visibility;identity:PostIdentity}){
    try{const result=await api(`/api/posts/${post.id}`,{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify(updates)});setToast(result.status==='pending'?'Post updated and sent for public review.':'Post updated.');await loadFeed();if(detailRef.current?.id===post.id)await openPost(post.id,false)}catch(e){setToast(e instanceof Error?e.message:'Post could not be updated.');throw e}
  }
  async function deleteOwnPost(post:FeedPost){
    try{await api(`/api/posts/${post.id}`,{method:'DELETE'});setPosts(current=>current.filter(item=>item.id!==post.id));setToast('Post deleted.');if(detailRef.current?.id===post.id){setDetail(null);detailRef.current=null;history.replaceState({},'', '/member?tab=profile')}}catch(e){setToast(e instanceof Error?e.message:'Post could not be deleted.');throw e}
  }
  async function loadPostHistory(postID:number){const p=await api(`/api/posts/${postID}/history`);return (p.history||[]) as {id:number;title:string;body:string;visibility:Visibility;identity:PostIdentity;time:string}[]}
  async function openPost(id:number,push=true,focusComment=false){try{const target=Number(new URLSearchParams(location.search).get('comment')||0);const p=await api(`/api/posts/${id}`);const next={...p.post,text:p.post.body,comment_items:p.post.comment_items||[]} as PostDetail;detailRef.current=next;setDetail(next);setReplyToComment(null);setCommentText('');if(push)history.pushState({},'',target?`/member/posts/${id}?comment=${target}`:`/member/posts/${id}`);if(target>0)window.setTimeout(()=>document.getElementById(`comment-${target}`)?.scrollIntoView({behavior:'smooth',block:'center'}),180);else if(focusComment)window.setTimeout(()=>document.getElementById('member-comment-input')?.focus(),180)}catch(e){setToast(e instanceof Error?e.message:'This post is unavailable.')}}
  function closePost(){detailRef.current=null;setDetail(null);setReplyToComment(null);if(location.pathname.startsWith('/member/posts/'))history.pushState({},'','/member')}
  async function reactPost(id:number,reaction='❤️'){try{const p=await api(`/api/posts/${id}/reaction`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({reaction})});setPosts(ps=>ps.map(x=>x.id===id?{...x,reactions:p.count,reaction_summary:p.summary,my_reaction:p.my_reaction}:x));if(detail?.id===id)setDetail({...detail,reactions:p.count,reacted_by_me:p.reacted,reaction_summary:p.summary,my_reaction:p.my_reaction})}catch(e){setToast(e instanceof Error?e.message:'Reaction could not be updated.')}}
  async function submitComment(){if(!detail||!commentText.trim())return;const nonce=makeNonce('comment');const body=commentText.trim(),postId=detail.id,parentId=replyToComment?.id??null;try{if(!navigator.onLine)throw new TypeError('offline');await api(`/api/posts/${postId}/comments`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({body,parent_id:parentId,client_nonce:nonce})});setCommentText('');setReplyToComment(null);await openPost(postId,false);setPosts(ps=>ps.map(x=>x.id===postId?{...x,comments:x.comments+1}:x))}catch(e){if(!navigator.onLine||e instanceof TypeError){await queueOutbox({nonce,kind:'comment',created_at:Date.now(),payload:{postId,body,parentId}}).catch(()=>undefined);const temp:CommentItem={id:-Date.now(),parent_id:parentId,account_id:profileRef.current?.account_id||0,name:profileRef.current?.name||'You',avatar:profileRef.current?.avatar||'',body,time:'Queued · offline',reactions:0,reacted_by_me:false,can_edit:false};setDetail(current=>current?{...current,comment_items:[...current.comment_items,temp],comments:current.comments+1}:current);setCommentText('');setReplyToComment(null);setToast('Comment saved on this device and will sync when online.')}else setToast(e instanceof Error?e.message:'Comment could not be added.')}}
  async function reactComment(commentId:number,reaction='❤️'){if(!detail)return;try{const p=await api(`/api/posts/${detail.id}/comments/${commentId}`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({reaction})});setDetail({...detail,comment_items:detail.comment_items.map(c=>c.id===commentId?{...c,reactions:p.count,reacted_by_me:p.reacted,reaction_summary:p.summary,my_reaction:p.my_reaction}:c)})}catch{/* keep current */}}
  async function saveCommentEdit(body:string){if(!detail||!editingComment||!body.trim())return;try{await api(`/api/posts/${detail.id}/comments/${editingComment.id}`,{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify({body:body.trim()})});setEditingComment(null);await openPost(detail.id,false);setToast('Comment updated.')}catch(e){setToast(e instanceof Error?e.message:'Comment could not be updated.')}}
  async function deleteComment(item:CommentItem){if(!detail)return;setMemberConfirm({title:'Delete comment',text:'Delete this comment? Its edit history will remain available to your account records.',action:()=>{setMemberConfirm(null);void (async()=>{try{await api(`/api/posts/${detail.id}/comments/${item.id}`,{method:'DELETE'});await openPost(detail.id,false);setPosts(current=>current.map(post=>post.id===detail.id?{...post,comments:Math.max(0,post.comments-1)}:post));setToast('Comment deleted.')}catch(e){setToast(e instanceof Error?e.message:'Comment could not be deleted.')}})()}})}
  async function showCommentHistory(item:CommentItem){if(!detail)return;try{const p=await api(`/api/posts/${detail.id}/comments/${item.id}/history`);setCommentHistory(p.history||[])}catch(e){setToast(e instanceof Error?e.message:'Comment history is unavailable.')}}
  async function lookupMentions(value:string){const match=value.match(/(?:^|\s)@([\w .-]{0,40})$/);if(!match){setMentionResults([]);return}try{const p=await api(`/api/social/mentions?q=${encodeURIComponent(match[1].trim())}`);setMentionResults(p.results||[])}catch{setMentionResults([])}}
  function insertMention(result:MentionResult){setCommentText(current=>current.replace(/(?:^|\s)@[\w .-]{0,40}$/,match=>`${match.startsWith(' ')?' ':''}@[${result.name}](account:${result.account_id}) `));setMentionResults([])}
  async function showProfile(accountID:number,event?:ReactMouseEvent<HTMLElement>|ReactFocusEvent<HTMLElement>){if(accountID<1)return;const requestID=++profileRequestRef.current;const rect=event?.currentTarget.getBoundingClientRect();if(rect)setProfileCardAnchor({x:Math.min(window.innerWidth-280,Math.max(8,rect.left)),y:Math.min(window.innerHeight-210,rect.bottom+6)});try{const p=await api(`/api/social/profile?id=${accountID}`);if(profileRequestRef.current===requestID)setProfileCard(p.profile||null)}catch{if(profileRequestRef.current===requestID)setProfileCard(null)}}

  async function markAllRead(){try{await api('/api/member/notifications',{method:'POST'});setNotifications(x=>x.map(n=>({...n,read:true})));setToast('Notifications marked as read.')}catch(e){setToast(e instanceof Error?e.message:'Notifications could not be updated.')}}
  async function clearNotifications(){setMemberConfirm({title:'Clear notifications',text:'Remove all notifications from your account?',action:()=>{setMemberConfirm(null);void (async()=>{try{await api('/api/member/notifications/clear',{method:'DELETE'});setNotifications([]);setToast('Notifications cleared.')}catch(e){setToast(e instanceof Error?e.message:'Notifications could not be cleared.')} })()}})}

  async function saveProfile(){setSaving(true);const payload={...profileForm};try{if(!navigator.onLine)throw new TypeError('offline');const p=await api('/api/member/me',{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});setToast(p.message||'Profile saved.');setProfileEditing(false);await Promise.all([loadProfile(),loadDirectory(),loadFeed(),loadChats()])}catch(e){if(!navigator.onLine||e instanceof TypeError){const nonce=makeNonce('profile');await queueOutbox({nonce,kind:'profile',created_at:Date.now(),payload}).catch(()=>undefined);const current=profileRef.current;if(current){const next=mergeProfileForm(current,payload);profileRef.current=next;setProfile(next);void putMeta('member-profile',next);try{localStorage.setItem('aits-member-profile-cache',JSON.stringify(next))}catch{/* optional cache */}}setProfileEditing(false);setToast('Profile changes saved on this device and will sync when online.')}else setToast(e instanceof Error?e.message:'Profile could not be saved.')}finally{setSaving(false)}}
  async function requestDivision(){if(!divisionChoice)return setToast('Choose a division first.');setSaving(true);try{const p=await api('/api/member/division-request',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({division_id:divisionChoice})});setToast(p.message||'Division request submitted.');await loadProfile()}catch(e){setToast(e instanceof Error?e.message:'Division request could not be submitted.')}finally{setSaving(false)}}
  async function requestMembership(){if(!membershipChoice||membershipChoice===profile?.membership_level)return setToast('Choose a different membership classification.');setSaving(true);try{const p=await api('/api/member/membership-request',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({membership:membershipChoice})});setToast(p.message||'Membership request submitted.');await loadProfile()}catch(e){setToast(e instanceof Error?e.message:'Membership request could not be submitted.')}finally{setSaving(false)}}
  async function requestEmailChange(){if(!emailForm.email.trim()||!emailForm.password)return setToast('Enter your new email and current password.');setSaving(true);try{const p=await api('/api/member/security/email/request',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({email:emailForm.email,password:emailForm.password})});const seconds=Number(p.expires_in_seconds||900),until=Date.now()+seconds*1000;setSecurityNow(Date.now());setEmailCodeUntil(until);sessionStorage.setItem('aits-email-change-pending',JSON.stringify({until,target:emailForm.email.trim()}));setToast(p.message||'Verification code sent.');setEmailStage('confirm')}catch(e){setToast(e instanceof Error?e.message:'Verification could not be started.')}finally{setSaving(false)}}
  async function confirmEmailChange(){if(!emailForm.code.trim())return;setSaving(true);try{const p=await api('/api/member/security/email/confirm',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({code:emailForm.code})});setToast(p.message||'Email updated.');sessionStorage.removeItem('aits-email-change-pending');setEmailCodeUntil(0);setEmailStage('request');setEmailForm({email:'',password:'',code:''});await loadProfile()}catch(e){setToast(e instanceof Error?e.message:'Email could not be updated.')}finally{setSaving(false)}}
  async function requestPasswordCode(){setSaving(true);try{const p=await api('/api/member/security/password/request',{method:'POST'});const seconds=Number(p.expires_in_seconds||900),until=Date.now()+seconds*1000;setSecurityNow(Date.now());setPasswordCodeUntil(until);sessionStorage.setItem('aits-password-change-pending',JSON.stringify({until}));setToast(p.message||'Verification code sent.');setPasswordStage('confirm')}catch(e){setToast(e instanceof Error?e.message:'Verification code could not be sent.')}finally{setSaving(false)}}
  async function confirmPasswordChange(){if(passwordForm.newPassword.length<10)return setToast('Use at least 10 characters for your new password.');if(passwordForm.newPassword!==passwordForm.confirmPassword)return setToast('The new passwords do not match.');setSaving(true);try{const p=await api('/api/member/security/password/confirm',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({code:passwordForm.code,newPassword:passwordForm.newPassword})});setToast(p.message||'Password updated.');sessionStorage.removeItem('aits-password-change-pending');setPasswordCodeUntil(0);setPasswordStage('request');setPasswordForm({code:'',newPassword:'',confirmPassword:''})}catch(e){setToast(e instanceof Error?e.message:'Password could not be updated.')}finally{setSaving(false)}}
  async function logout(){await fetch('/api/member/logout',{method:'POST',credentials:'include'}).catch(()=>undefined);localStorage.removeItem('aits-member-profile-cache');localStorage.setItem('aits-last-mode','visitor');await clearMemberCache();location.replace('/login')}

  async function loadChats(){setChatLoading(true);try{const p=await api('/api/member/chats');const rows=p.conversations||[];setConversations(rows);void cacheConversations(rows)}catch(e){const cached=await readConversations<Conversation>().catch(()=>[]);if(cached.length)setConversations(cached);else if(navigator.onLine)setToast(e instanceof Error?e.message:'Chats are unavailable.')}finally{setChatLoading(false)}}
  async function createConversation(scope:ChatScope,target=0){try{const p=await api('/api/member/chats',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({scope,target_account_id:target})});await loadChats();const id=Number(p.conversation_id);const convo=(await api('/api/member/chats')).conversations?.find((x:Conversation)=>x.id===id) as Conversation|undefined;if(convo)void openConversation(convo);setPrivatePicker(false)}catch(e){setToast(e instanceof Error?e.message:'Conversation could not be opened.')}}
  async function createCustomGroup(){if(!groupName.trim()||groupMembers.length<1)return setToast('Add a group name and at least one member.');try{const p=await api('/api/member/chats',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({scope:'group',title:groupName.trim(),member_ids:groupMembers})});setGroupModal(false);setGroupName('');setGroupMembers([]);await loadChats();const all=(await api('/api/member/chats')).conversations||[];const convo=all.find((x:Conversation)=>x.id===Number(p.conversation_id));if(convo){setTabState('chat');void openConversation(convo)}}catch(e){setToast(e instanceof Error?e.message:'Group could not be created.')}}
  function chatPageLimit(conversation:Conversation){return conversation.scope==='private'?30:50}
  async function openConversation(conversation:Conversation,push=true){
    activeConversationRef.current=conversation.id
    if(push&&location.pathname!==`/member/chat/${conversation.id}`)history.pushState({conversationID:conversation.id},'',`/member/chat/${conversation.id}`)
    setActiveConversation(conversation);setChatMessages([]);setChatParticipants([]);setChatLoading(true);setTypingNames([]);setChatEditing(null)
    try{
      const cached=await readMessages<ChatMessage>(conversation.id).catch(()=>[]);if(cached.length)setChatMessages(normalizeChatMessagesOwnership(cached.slice(-chatPageLimit(conversation)),profileRef.current?.account_id))
      const p=await api(`/api/member/chats/${conversation.id}?limit=${chatPageLimit(conversation)}`)
      const rows=normalizeChatMessagesOwnership((p.messages||[]) as ChatMessage[],profileRef.current?.account_id);const participants=(p.participants||[]) as ChatParticipant[];setChatMessages(rows);setChatParticipants(participants);setChatHasMore(!!p.has_more);setChatBefore(p.next_before??null);void cacheMessages(conversation.id,rows)
      if(p.active_call&&conversation.scope!=='private'&&!activeCallRef.current){const host=participants.find(member=>member.account_id===Number(p.active_call.host_account_id));setGroupCallNotice({conversation_id:conversation.id,call_id:String(p.active_call.call_id||''),mode:p.active_call.mode==='video'?'video':'audio',from_account_id:Number(p.active_call.host_account_id||0),from_name:host?.name||'Group host',from_avatar:host?.avatar||'',group:true,host_account_id:Number(p.active_call.host_account_id||0),participant_count:Number(p.active_call.participant_count||1)})}
      setConversations(current=>current.map(c=>c.id===conversation.id?{...c,unread:false,unread_count:0}:c))
      connectChat(conversation.id)
    }catch(e){const cached=await readMessages<ChatMessage>(conversation.id).catch(()=>[]);if(cached.length){setChatMessages(cached.slice(-chatPageLimit(conversation)));setChatConnection('offline')}else setToast(e instanceof Error?e.message:'Conversation could not be opened.')}finally{setChatLoading(false)}
  }
  async function loadEarlierChat(){
    const conversation=activeConversation;if(!conversation||!chatHasMore||!chatBefore||chatLoading)return
    setChatLoading(true)
    try{
      const p=await api(`/api/member/chats/${conversation.id}?limit=${chatPageLimit(conversation)}&before=${chatBefore}`)
      const older=normalizeChatMessagesOwnership((p.messages||[]) as ChatMessage[],profileRef.current?.account_id)
      setChatMessages(current=>{const map=new Map<number,ChatMessage>();[...older,...current].forEach(message=>map.set(message.id,message));return [...map.values()].sort((a,b)=>a.id-b.id)})
      setChatHasMore(!!p.has_more);setChatBefore(p.next_before??null);void cacheMessages(conversation.id,older)
    }catch(e){setToast(e instanceof Error?e.message:'Earlier messages could not be loaded.')}finally{setChatLoading(false)}
  }
  async function refreshActiveChat(){
    const conversation=activeConversation
    if(!conversation||activeConversationRef.current!==conversation.id)return
    try{
      const p=await api(`/api/member/chats/${conversation.id}?limit=${chatPageLimit(conversation)}`)
      setChatMessages(normalizeChatMessagesOwnership((p.messages||[]) as ChatMessage[],profileRef.current?.account_id));setChatParticipants(p.participants||[]);setChatHasMore(!!p.has_more);setChatBefore(p.next_before??null)
    }catch{/* keep the current view */}
  }
  async function revealChatMessage(messageID:number){
    const conversation=activeConversation
    if(!conversation||messageID<1||activeConversationRef.current!==conversation.id)return
    if(chatMessages.some(message=>message.id===messageID))return
    try{
      const limit=chatPageLimit(conversation)
      const p=await api(`/api/member/chats/${conversation.id}?limit=${limit}&before=${messageID+1}`)
      const around=normalizeChatMessagesOwnership((p.messages||[]) as ChatMessage[],profileRef.current?.account_id)
      setChatMessages(current=>{const map=new Map<number,ChatMessage>();[...around,...current].forEach(message=>map.set(message.id,message));return [...map.values()].sort((a,b)=>a.id-b.id)})
      if(p.next_before)setChatBefore(current=>current===null?Number(p.next_before):Math.min(current,Number(p.next_before)))
      setChatHasMore(current=>current||!!p.has_more)
    }catch{/* the original message may have expired or been removed */}
  }
  function connectChat(conversationID:number){
    if(activeConversationRef.current!==conversationID)return
    if(chatReconnectRef.current){window.clearTimeout(chatReconnectRef.current);chatReconnectRef.current=null}
    const previous=chatSocketRef.current;chatSocketRef.current=null;closeSocketQuietly(previous,'conversation changed')
    setChatConnection('connecting')
    const ws=new WebSocket(`${websocketURL('/ws/member/chat')}?conversation_id=${conversationID}`);chatSocketRef.current=ws
    let heartbeat:number|undefined
    ws.onopen=()=>{
      if(activeConversationRef.current!==conversationID){ws.close();return}
      setChatConnection('online');ws.send(JSON.stringify({type:'seen'}))
      const pending=pendingJoinCallRef.current
      if(pending&&pending.conversation_id===conversationID){ws.send(JSON.stringify({type:'call_join',call_id:pending.call_id}));pendingJoinCallRef.current=null;window.setTimeout(sendCurrentCallState,0)}
      heartbeat=window.setInterval(()=>{if(ws.readyState===WebSocket.OPEN)ws.send(JSON.stringify({type:'ping'}))},25000)
    }
    ws.onmessage=e=>{
      try{
        const p=JSON.parse(e.data)
        if(p.type==='message'&&p.message){
          const incoming=normalizeChatMessageOwnership(p.message as ChatMessage,profileRef.current?.account_id)
          setChatMessages(current=>{const match=current.find(m=>m.id===incoming.id||(incoming.client_nonce&&m.client_nonce===incoming.client_nonce));return match?current.map(m=>(m.id===match.id)?{...m,...incoming,queued:false}:m):[...current,incoming]})
          setConversations(current=>current.map(c=>c.id===conversationID?{...c,last:incoming.body||incoming.attachments?.[0]?.name||'Attachment',time:incoming.time,unread:false}:c))
          if(incoming.sender_account_id!==profileRef.current?.account_id&&document.visibilityState==='visible'&&ws.readyState===WebSocket.OPEN)ws.send(JSON.stringify({type:'seen'}))
          void cacheMessages(conversationID,[incoming]);void loadChats()
        }
        if(p.type==='message_available')void refreshActiveChat()
        if(p.type==='message_edit')setChatMessages(current=>current.map(m=>m.id===Number(p.message_id)?{...m,body:String(p.body||''),edited:true,edited_time:String(p.edited_time||'')}:m))
        if(p.type==='message_delete'){const messageID=Number(p.message_id);setChatMessages(current=>current.filter(m=>m.id!==messageID));void removeCachedMessage(conversationID,messageID)}
        if(p.type==='message_unsent'){const messageID=Number(p.message_id);setChatMessages(current=>current.map(m=>m.id===messageID?{...m,body:'Message unsent',attachments:[],reply_to:0,edited:false,history_count:0,pinned:false,pinned_by:0,poll_id:0,unsent:true,expires_at:String(p.expires_at||new Date(Date.now()+86400000).toISOString())}:m))}
        if(p.type==='message_pin')setChatMessages(current=>current.map(m=>m.id===Number(p.message_id)?{...m,pinned:!!p.pinned,pinned_by:Number(p.pinned_by||0)}:m))
        if(p.type==='typing'&&p.account_id!==profileRef.current?.account_id)setTypingNames(current=>Array.from(new Set([...current,String(p.name||'Member')])))
        if(p.type==='typing_stop')setTypingNames(current=>current.filter(x=>x!==p.name))
        if(p.type==='reaction')setChatMessages(current=>current.map(m=>m.id===Number(p.message_id)?{...m,reactions:Number(p.count||0),reaction_summary:p.summary||{},my_reaction:Number(p.account_id)===profileRef.current?.account_id?String(p.reaction||''):m.my_reaction}:m))
        if(p.type==='poll_update')window.dispatchEvent(new CustomEvent('aits-chat-poll-update',{detail:{poll_id:Number(p.poll_id||0)}}))
        if(p.type==='seen'){const ids=new Set<number>((p.message_ids||[]).map((x:unknown)=>Number(x)));setChatMessages(current=>current.map(m=>ids.has(m.id)?{...m,seen:true}:m))}
        if(p.type==='call_start'&&Number(p.from_account_id)!==profileRef.current?.account_id&&!activeCallRef.current){
          const notice:IncomingCall={conversation_id:conversationID,call_id:String(p.call_id||''),mode:p.mode==='video'?'video':'audio',from_account_id:Number(p.from_account_id),from_name:String(p.from_name||'AITS member'),from_avatar:String(p.from_avatar||''),group:!!p.group,host_account_id:Number(p.host_account_id||p.from_account_id||0),participant_count:Number(p.participant_count||1)}
          if(notice.group)setGroupCallNotice(notice);else setIncomingCall(notice)
        }
        if(p.type==='call_active'&&activeConversationRef.current===conversationID){
          const existingID=String(p.call_id||'')
          if(activeCallRef.current&&activeCallRef.current.call_id!==existingID)endCall(false)
          const host=chatParticipants.find(member=>member.account_id===Number(p.host_account_id))
          const notice:IncomingCall={conversation_id:conversationID,call_id:existingID,mode:p.mode==='video'?'video':'audio',from_account_id:Number(p.host_account_id||0),from_name:host?.name||'Group host',from_avatar:host?.avatar||'',group:!!p.group,host_account_id:Number(p.host_account_id||0),participant_count:Number(p.participant_count||1)}
          if(p.group)setGroupCallNotice(notice)
        }
        if(p.type==='call_join'&&activeCallRef.current?.call_id===String(p.call_id||'')&&Number(p.account_id)!==profileRef.current?.account_id){
          void offerCallTo(Number(p.account_id))
          const stream=localCallStreamRef.current
          sendCallSocket({type:'call_state',call_id:activeCallRef.current.call_id,muted:!!stream?.getAudioTracks().length&&stream.getAudioTracks().every(track=>!track.enabled),camera_off:!stream?.getVideoTracks().length||stream.getVideoTracks().every(track=>!track.enabled)})
        }
        if(p.type==='call_leave'&&activeCallRef.current?.call_id===String(p.call_id||'')){removeCallPeer(Number(p.account_id));setRemoteCallStates(current=>{const next={...current};delete next[Number(p.account_id)];return next})}
        if(p.type==='call_end'){
          if(activeCallRef.current?.call_id===String(p.call_id||''))endCall(false)
          setGroupCallNotice(current=>current?.call_id===String(p.call_id||'')?null:current)
        }
        if(p.type==='call_state'&&activeCallRef.current?.call_id===String(p.call_id||'')){setRemoteCallStates(current=>({...current,[Number(p.account_id)]:{muted:!!p.muted,camera_off:!!p.camera_off}}))}
        if(p.type==='call_signal'&&Number(p.target_account_id)===profileRef.current?.account_id)void handleCallSignal(p)
      }catch{/* ignore malformed event */}
    }
    ws.onclose=()=>{
      if(heartbeat)window.clearInterval(heartbeat)
      if(chatSocketRef.current===ws)chatSocketRef.current=null
      setTypingNames([])
      if(activeConversationRef.current!==conversationID)return
      setChatConnection('offline')
      chatReconnectRef.current=window.setTimeout(()=>connectChat(conversationID),1600)
    }
    ws.onerror=()=>setChatConnection('offline')
  }

  function sendCallSocket(payload:Record<string,unknown>){const ws=chatSocketRef.current;if(ws?.readyState===WebSocket.OPEN)ws.send(JSON.stringify(payload))}
  function sendCurrentCallState(){const call=activeCallRef.current,stream=localCallStreamRef.current;if(!call)return;sendCallSocket({type:'call_state',call_id:call.call_id,muted:!!stream?.getAudioTracks().length&&stream.getAudioTracks().every(track=>!track.enabled),camera_off:!stream?.getVideoTracks().length||stream.getVideoTracks().every(track=>!track.enabled)})}
  async function ensureCallMedia(mode:CallMode){
    if(localCallStreamRef.current)return localCallStreamRef.current
    if(!navigator.mediaDevices?.getUserMedia)throw new Error('Audio/video calls are not supported by this browser.')
    const stream=await navigator.mediaDevices.getUserMedia({audio:true,video:mode==='video'})
    localCallStreamRef.current=stream;setCallMuted(false);setCallCameraOff(mode!=='video');return stream
  }
  function removeCallPeer(accountID:number){const peer=peerConnectionsRef.current.get(accountID);peer?.close();peerConnectionsRef.current.delete(accountID);setRemoteStreams(current=>{const next={...current};delete next[accountID];return next})}
  function callPeer(accountID:number){
    const existing=peerConnectionsRef.current.get(accountID);if(existing)return existing
    const peer=new RTCPeerConnection({iceServers:[{urls:'stun:stun.l.google.com:19302'},{urls:'stun:stun1.l.google.com:19302'}]})
    localCallStreamRef.current?.getTracks().forEach(track=>peer.addTrack(track,localCallStreamRef.current!))
    peer.onicecandidate=event=>{if(event.candidate&&activeCallRef.current)sendCallSocket({type:'call_signal',call_id:activeCallRef.current.call_id,target_account_id:accountID,signal:{candidate:event.candidate.toJSON()}})}
    peer.ontrack=event=>{const stream=event.streams[0]||new MediaStream([event.track]);setRemoteStreams(current=>({...current,[accountID]:stream}))}
    peer.onconnectionstatechange=()=>{if(['failed','closed','disconnected'].includes(peer.connectionState))removeCallPeer(accountID)}
    peerConnectionsRef.current.set(accountID,peer);return peer
  }
  async function offerCallTo(accountID:number){if(accountID<1||!activeCallRef.current)return;const peer=callPeer(accountID);const offer=await peer.createOffer();await peer.setLocalDescription(offer);sendCallSocket({type:'call_signal',call_id:activeCallRef.current.call_id,target_account_id:accountID,signal:{description:peer.localDescription}})}
  async function handleCallSignal(payload:{call_id?:string;from_account_id?:number;signal?:unknown}){
    if(!activeCallRef.current||String(payload.call_id||'')!==activeCallRef.current.call_id)return
    const from=Number(payload.from_account_id);if(from<1||from===profileRef.current?.account_id)return
    const raw=typeof payload.signal==='string'?JSON.parse(payload.signal):payload.signal
    if(!raw||typeof raw!=='object')return
    const signal=raw as {description?:RTCSessionDescriptionInit;candidate?:RTCIceCandidateInit}
    const peer=callPeer(from)
    if(signal.description){await peer.setRemoteDescription(signal.description);if(signal.description.type==='offer'){const answer=await peer.createAnswer();await peer.setLocalDescription(answer);sendCallSocket({type:'call_signal',call_id:activeCallRef.current.call_id,target_account_id:from,signal:{description:peer.localDescription}})}}
    if(signal.candidate)await peer.addIceCandidate(signal.candidate).catch(()=>undefined)
  }
  async function startCall(mode:CallMode){
    const conversation=activeConversation
    if(!conversation)return
    if(chatConnection!=='online')return setToast('Reconnect to the chat before starting a call.')
    if(groupCallNotice?.conversation_id===conversation.id)return setToast('A group call is already active. Join it instead.')
    try{
      await ensureCallMedia(mode)
      const group=conversation.scope!=='private'
      const selfID=profileRef.current?.account_id||0
      const call:ActiveCall={conversation_id:conversation.id,call_id:makeNonce('call'),mode,started_by_me:true,group,host_account_id:selfID}
      setActiveCall(call);activeCallRef.current=call;setGroupCallNotice(null)
      sendCallSocket({type:'call_start',call_id:call.call_id,mode})
    }catch(e){setToast(e instanceof Error?e.message:'Camera or microphone could not be opened.')}
  }
  async function acceptIncomingCall(){
    const incoming=incomingCall
    if(!incoming)return
    try{
      if(activeCallRef.current&&activeCallRef.current.call_id!==incoming.call_id)endCall(true)
      await ensureCallMedia(incoming.mode)
      const call:ActiveCall={conversation_id:incoming.conversation_id,call_id:incoming.call_id,mode:incoming.mode,started_by_me:false,group:false,host_account_id:incoming.host_account_id||incoming.from_account_id}
      setActiveCall(call);activeCallRef.current=call
      let conversation=conversations.find(c=>c.id===incoming.conversation_id)
      if(!conversation){const p=await api('/api/member/chats');conversation=(p.conversations||[]).find((c:Conversation)=>c.id===incoming.conversation_id)}
      if(!conversation)throw new Error('The call conversation is unavailable.')
      setTabState('chat');tabRef.current='chat';pendingJoinCallRef.current={conversation_id:incoming.conversation_id,call_id:incoming.call_id,mode:incoming.mode};setIncomingCall(null)
      await openConversation(conversation)
      if(chatSocketRef.current?.readyState===WebSocket.OPEN){sendCallSocket({type:'call_join',call_id:incoming.call_id});pendingJoinCallRef.current=null;sendCurrentCallState()}
    }catch(e){endCall(false);setIncomingCall(null);setToast(e instanceof Error?e.message:'The call could not be joined.')}
  }
  async function joinGroupCall(){
    const incoming=groupCallNotice
    if(!incoming)return
    try{
      if(activeCallRef.current&&activeCallRef.current.call_id!==incoming.call_id)endCall(true)
      await ensureCallMedia(incoming.mode)
      const call:ActiveCall={conversation_id:incoming.conversation_id,call_id:incoming.call_id,mode:incoming.mode,started_by_me:false,group:true,host_account_id:incoming.host_account_id||incoming.from_account_id}
      setActiveCall(call);activeCallRef.current=call
      let conversation=conversations.find(c=>c.id===incoming.conversation_id)
      if(!conversation){const p=await api('/api/member/chats');conversation=(p.conversations||[]).find((c:Conversation)=>c.id===incoming.conversation_id)}
      if(!conversation)throw new Error('The group call is unavailable.')
      setTabState('chat');tabRef.current='chat'
      await openConversation(conversation)
      if(chatSocketRef.current?.readyState===WebSocket.OPEN){sendCallSocket({type:'call_join',call_id:incoming.call_id});sendCurrentCallState()}
      else pendingJoinCallRef.current={conversation_id:incoming.conversation_id,call_id:incoming.call_id,mode:incoming.mode}
    }catch(e){endCall(false);setToast(e instanceof Error?e.message:'The group call could not be joined.')}
  }
  function declineIncomingCall(){setIncomingCall(null)}
  function endCall(notify=true){
    const call=activeCallRef.current
    if(notify&&call){
      if(call.group&&!call.started_by_me)sendCallSocket({type:'call_leave',call_id:call.call_id})
      else sendCallSocket({type:'call_end',call_id:call.call_id})
    }
    peerConnectionsRef.current.forEach(peer=>peer.close());peerConnectionsRef.current.clear()
    localCallStreamRef.current?.getTracks().forEach(track=>track.stop());localCallStreamRef.current=null
    setRemoteStreams({});setRemoteCallStates({});setActiveCall(null);activeCallRef.current=null;setCallMuted(false);setCallCameraOff(false)
  }
  function toggleCallMute(){
    const stream=localCallStreamRef.current
    if(!stream)return
    const muted=!callMuted
    stream.getAudioTracks().forEach(track=>{track.enabled=!muted})
    setCallMuted(muted)
    const call=activeCallRef.current
    if(call)sendCallSocket({type:'call_state',call_id:call.call_id,muted,camera_off:callCameraOff})
  }
  function toggleCallCamera(){
    const stream=localCallStreamRef.current
    if(!stream)return
    const off=!callCameraOff
    stream.getVideoTracks().forEach(track=>{track.enabled=!off})
    setCallCameraOff(off)
    const call=activeCallRef.current
    if(call)sendCallSocket({type:'call_state',call_id:call.call_id,muted:callMuted,camera_off:off})
  }
  function sendTyping(active=true){const ws=chatSocketRef.current;if(!ws||ws.readyState!==WebSocket.OPEN)return;if(!active){if(typingTimerRef.current){clearTimeout(typingTimerRef.current);typingTimerRef.current=null}ws.send(JSON.stringify({type:'typing_stop'}));typingLastSentRef.current=0;return}const now=performance.now();if(now-typingLastSentRef.current>850){ws.send(JSON.stringify({type:'typing'}));typingLastSentRef.current=now}if(typingTimerRef.current)clearTimeout(typingTimerRef.current);typingTimerRef.current=window.setTimeout(()=>{if(ws.readyState===WebSocket.OPEN)ws.send(JSON.stringify({type:'typing_stop'}));typingLastSentRef.current=0;typingTimerRef.current=null},1100)}
  async function uploadFiles(files:FileList|File[]){const list=Array.from(files).slice(0,8);if(!list.length)return[];setUploading(true);const out:Attachment[]=[];try{for(const original of list){let file=original;if(file.type.startsWith('image/'))file=await optimizeImage(file);if(file.size>maxFileSize(file)){setToast(`${file.name} exceeds the allowed size.`);continue}const form=new FormData();form.append('file',file);const p=await api('/api/member/chat-media',{method:'POST',body:form});if(p.attachment)out.push(p.attachment)}return out}finally{setUploading(false)}}
  function expandChatCommand(value:string){
    const raw=value.trim()
    const command=raw.toLowerCase()
    const commandBody=(prefix:string)=>raw.slice(prefix.length).trim()
    const quoteLines=(message:string)=>message.split(/\r?\n/).map(line=>`> ${line}`).join('\n')
    if(command==='/commands'||command==='/usecommand'||command==='/poll'||command==='/markdown'||command==='/markup')return raw
    if(/^\/warning\s+/i.test(raw)){const message=commandBody('/warning');return `> [!WARNING]\n${quoteLines(message)}`}
    if(/^\/note\s+/i.test(raw)){const message=commandBody('/note');return `> [!NOTE]\n${quoteLines(message)}`}
    if(/^\/quote\s+/i.test(raw))return quoteLines(commandBody('/quote'))
    if(/^\/checklist\s+/i.test(raw)){const items=commandBody('/checklist').split(/\s*\|\s*/).filter(Boolean);return items.map(item=>`- [ ] ${item}`).join('\n')}
    if(command==='/code')return '```text\nPaste code here\n```'
    if(command==='/checklist')return '- [ ] First task'
    if(command==='/warning')return '> [!WARNING]\n> Add your warning here.'
    if(command==='/quote')return '> Add quoted text here.'
    if(command==='/meeting')return '### Meeting notes\n- **Topic:** \n- **Decision:** \n- **Owner:** \n- [ ] Follow-up task'
    if(command==='/shrug')return '¯\\_(ツ)_/¯'
    if(command==='/table')return '| Column 1 | Column 2 |\n| --- | --- |\n| Value 1 | Value 2 |'
    if(command==='/note')return '> [!NOTE]\n> Add your note here.'
    return raw
  }
  async function sendChat(draftText?:string){
    let body=expandChatCommand((draftText??chatText).trim())
    const conversationID=activeConversationRef.current
    if(!conversationID)return
    if(chatEditing){
      if(!body)return
      try{
        const p=await api(`/api/member/chats/${conversationID}/messages/${chatEditing.id}`,{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify({body})})
        setChatMessages(current=>current.map(m=>m.id===chatEditing.id?{...m,body,edited:true,edited_time:p.message?.edited_time||'Edited just now'}:m))
        setChatEditing(null);setChatText('');setToast('Message updated.')
      }catch(e){setToast(e instanceof Error?e.message:'Message could not be updated.')}
      return
    }
    const attachments=[...chatAttachments]
    if(!body&&!attachments.length)return
    const nonce=makeNonce('chat')
    const replyTo=chatReply?.id??0
    const optimistic:ChatMessage={id:-Date.now(),sender_account_id:profileRef.current?.account_id||0,sender:profileRef.current?.name||'You',avatar:profileRef.current?.avatar,body,attachments,reply_to:replyTo,time:navigator.onLine?'Sending…':'Queued · offline',mine:true,seen:false,reactions:0,reaction_summary:{},client_nonce:nonce,queued:!navigator.onLine}
    setChatMessages(current=>[...current,optimistic]);void cacheMessages(conversationID,[optimistic])
    setChatText('');setChatAttachments([]);setChatReply(null)
    const ws=chatSocketRef.current;if(ws?.readyState===WebSocket.OPEN)ws.send(JSON.stringify({type:'typing_stop'}))
    try{
      if(!navigator.onLine)throw new TypeError('offline')
      const p=await api(`/api/member/chats/${conversationID}/messages`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({body,reply_to:replyTo,attachments,client_nonce:nonce})})
      if(p.message){const sent=normalizeChatMessageOwnership(p.message as ChatMessage,profileRef.current?.account_id);setChatMessages(current=>current.map(m=>m.client_nonce===nonce?{...sent,queued:false}:m));void cacheMessages(conversationID,[sent])}
    }catch(e){
      if(!navigator.onLine||e instanceof TypeError){
        await queueOutbox({nonce,kind:'chat',created_at:Date.now(),payload:{conversationID,body,attachments,replyTo}}).catch(()=>undefined)
        setChatMessages(current=>current.map(m=>m.client_nonce===nonce?{...m,queued:true,time:'Queued · offline'}:m))
        setToast('Message queued. It will send when you are back online.')
      }else{
        setChatMessages(current=>current.filter(m=>m.client_nonce!==nonce))
        setToast(e instanceof Error?e.message:'Message could not be sent.')
      }
    }
  }
  async function flushOutbox(){if(flushRunningRef.current||!navigator.onLine)return;flushRunningRef.current=true;let changedFeed=false,changedChats=false,changedProfile=false;try{const actions=await listOutbox().catch(()=>[]);for(const action of actions){try{if(action.kind==='post'){const p=action.payload as {title:string;body:string;audience:Visibility;identity?:PostIdentity;files:File[]};await sendPostNow(action.nonce,p.title,p.body,p.audience,p.identity||'member',p.files||[]);changedFeed=true}else if(action.kind==='comment'){const p=action.payload as {postId:number;body:string;parentId:number|null};await api(`/api/posts/${p.postId}/comments`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({body:p.body,parent_id:p.parentId,client_nonce:action.nonce})});changedFeed=true}else if(action.kind==='chat'){const p=action.payload as {conversationID:number;body:string;attachments:Attachment[];replyTo:number};await api(`/api/member/chats/${p.conversationID}/messages`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({body:p.body,reply_to:p.replyTo,attachments:p.attachments||[],client_nonce:action.nonce})});changedChats=true}else if(action.kind==='profile'){await api('/api/member/me',{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify(action.payload as ProfileForm)});changedProfile=true}else if(action.kind==='avatar'){const p=action.payload as {file:File};if(!p.file)throw new Error('queued avatar is unavailable');const fd=new FormData();fd.append('file',p.file);await api('/api/member/avatar',{method:'POST',body:fd});changedProfile=true}await removeOutbox(action.nonce)}catch{break}}if(changedProfile)await loadProfile();if(changedFeed)await loadFeed();if(changedChats){await loadChats();if(activeConversationRef.current){const active=conversations.find(c=>c.id===activeConversationRef.current);if(active)await openConversation(active)}}}finally{flushRunningRef.current=false}}
  async function chooseChatFiles(event:ChangeEvent<HTMLInputElement>){if(!event.target.files?.length)return;const files=await uploadFiles(event.target.files);event.target.value='';if(files.length)setChatAttachments(current=>[...current,...files].slice(0,8))}
  async function addDroppedChatFiles(files:File[]){if(!files.length)return;const uploaded=await uploadFiles(files);if(uploaded.length)setChatAttachments(current=>[...current,...uploaded].slice(0,8))}
  function chatKeyDown(event:KeyboardEvent<HTMLTextAreaElement>){if(event.key==='Enter'&&!event.shiftKey){event.preventDefault();void sendChat()}}
  function reactChat(message:ChatMessage,reaction='👍'){
    const ws=chatSocketRef.current;if(message.id<1)return
    setChatMessages(current=>current.map(item=>{if(item.id!==message.id)return item;const summary={...(item.reaction_summary||{})};const previous=item.my_reaction||'';if(previous&&summary[previous]){summary[previous]=Math.max(0,summary[previous]-1);if(!summary[previous])delete summary[previous]}summary[reaction]=(summary[reaction]||0)+1;return {...item,my_reaction:reaction,reaction_summary:summary,reactions:Object.values(summary).reduce((a,b)=>a+b,0)}}))
    if(ws?.readyState===WebSocket.OPEN)ws.send(JSON.stringify({type:'react',message_id:message.id,reaction}))
  }
  function beginEditChat(message:ChatMessage){
    if(!message.mine||message.id<1)return
    setChatEditing(message);setChatText(message.body);setChatReply(null);setChatAttachments([])
  }
  function cancelEditChat(){setChatEditing(null);setChatText('')}
  function deleteChatMessage(message:ChatMessage){
    if(!message.mine||message.id<1)return
    const conversationID=activeConversationRef.current
    if(!conversationID)return
    setMemberConfirm({title:'Unsend message?',text:'Everyone will see “Message unsent” for 24 hours, then the placeholder is removed.',action:()=>{setMemberConfirm(null);void (async()=>{try{await api(`/api/member/chats/${conversationID}/messages/${message.id}`,{method:'DELETE'});setChatMessages(current=>current.map(m=>m.id===message.id?{...m,body:'Message unsent',attachments:[],reply_to:0,edited:false,history_count:0,pinned:false,pinned_by:0,poll_id:0,unsent:true,expires_at:new Date(Date.now()+86400000).toISOString()}:m));await loadChats();setToast('Message unsent. The placeholder disappears after 24 hours.')}catch(e){setToast(e instanceof Error?e.message:'Message could not be unsent.')}})()}})
  }
  async function pinChatMessage(message:ChatMessage){
    const conversationID=activeConversationRef.current
    if(!conversationID||message.id<1)return
    try{
      const pinned=!message.pinned
      await api(`/api/member/chats/${conversationID}/messages/${message.id}/pin`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({pinned})})
      setChatMessages(current=>current.map(m=>m.id===message.id?{...m,pinned,pinned_by:pinned?(profileRef.current?.account_id||0):0}:m))
    }catch(e){setToast(e instanceof Error?e.message:'Pin could not be changed.')}
  }
  async function toggleChatRecording(){
    if(chatRecording){chatRecorderRef.current?.stop();return}
    if(!navigator.mediaDevices?.getUserMedia||typeof MediaRecorder==='undefined'){audioInputRef.current?.click();setToast('Live recording is not supported here. Choose an audio file instead.');return}
    try{
      const stream=await navigator.mediaDevices.getUserMedia({audio:true});chatStreamRef.current=stream;const chunks:BlobPart[]=[];const recorder=new MediaRecorder(stream);chatRecorderRef.current=recorder
      recorder.ondataavailable=e=>{if(e.data.size)chunks.push(e.data)}
      recorder.onstop=()=>{setChatRecording(false);stream.getTracks().forEach(t=>t.stop());chatStreamRef.current=null;chatRecorderRef.current=null;const blob=new Blob(chunks,{type:recorder.mimeType||'audio/webm'});if(!blob.size)return;const ext=blob.type.includes('ogg')?'ogg':blob.type.includes('mp4')?'m4a':'webm';const file=new File([blob],`voice-${Date.now()}.${ext}`,{type:blob.type||'audio/webm'});void (async()=>{setUploading(true);try{const form=new FormData();form.append('file',file);const p=await api('/api/member/chat-media',{method:'POST',body:form});if(p.attachment){setChatAttachments(current=>[...current,p.attachment].slice(0,8));setToast('Voice recording attached. Review it, then press Send.')}}catch(e){setToast(e instanceof Error?e.message:'Voice recording could not be attached.')}finally{setUploading(false)}})()}
      recorder.start();setChatRecording(true)
    }catch{setToast('Microphone permission was not granted.')}
  }

  const unread=notifications.filter(n=>!n.read).length
  const chatUnread=conversations.reduce((total,c)=>total+Math.max(0,c.unread_count||(c.unread?1:0)),0)
  const divisionsForFilter=useMemo(()=>Array.from(new Set(members.map(m=>m.division).filter(Boolean))).sort(),[members])
  const accessesForFilter=useMemo(()=>Array.from(new Set(members.map(m=>m.access).filter(Boolean))).sort(),[members])
  const membershipsForFilter=useMemo(()=>Array.from(new Set(members.map(m=>m.membership).filter(Boolean))).sort(),[members])
  const filteredMembers=useMemo(()=>members.filter(m=>{
    const online=m.online||onlineNames.has(m.username.toLowerCase());void online
    const q=memberSearch.trim().toLowerCase();if(q&&!`${m.name} ${m.username} ${m.division} ${m.access} ${m.membership} ${(m.tags||[]).join(' ')}`.toLowerCase().includes(q))return false
    if(divisionFilter!=='all'&&m.division!==divisionFilter)return false;if(accessFilter!=='all'&&m.access!==accessFilter)return false;if(membershipFilter!=='all'&&m.membership!==membershipFilter)return false;return true
  }).sort((a,b)=>Number(b.online||onlineNames.has(b.username.toLowerCase()))-Number(a.online||onlineNames.has(a.username.toLowerCase()))||a.name.localeCompare(b.name)),[members,memberSearch,divisionFilter,accessFilter,membershipFilter,onlineNames])
  const groupedComments=useMemo(()=>detail?detail.comment_items.filter(c=>!c.parent_id).map(c=>({...c,replies:detail.comment_items.filter(r=>r.parent_id===c.id)})):[],[detail])
  const ownPosts=useMemo(()=>posts.filter(p=>p.can_edit),[posts])
  const age=ageFromBirthdate(profile?.birth_date)
  const mobileTabs:[MemberTab,string,string][]=[['posts','Post','posts'],['chat','Chat','chat'],['members','Members','users'],['notifications','Notifications','bell']]
  const desktopTabs:[MemberTab,string,string][]=[['posts','Posts','posts'],['chat','Chats','chat'],['members','Members','users'],['notifications','Notifications','bell'],['profile','Profile','user']]

  if(loading||!profile)return <PageLoading label="Opening your AITS member space"/>

  if(tab==='chat')return <div className="member-shell chat-mode">
    <header className="member-header chat-desktop-header">
      <button type="button" className="member-brand" onClick={()=>setTab('posts')} aria-label="AITS member home"><span className="global-brand-mark"><img src="/logo/aits.png" alt="AITS"/></span><div><strong>AITS</strong><small>Member Community</small></div></button>
      <nav className="member-desktop-nav">{desktopTabs.map(([id,label,icon])=><button key={id} className={tab===id?'active':''} onClick={()=>setTab(id)}><Icon name={icon}/><span>{label}</span>{id==='chat'&&chatUnread>0&&<b>{chatUnread>99?'99+':chatUnread}</b>}{id==='notifications'&&unread>0&&<b>{unread>99?'99+':unread}</b>}</button>)}</nav>
      <div className="member-header-actions"><button className="icon-button" onClick={()=>setTheme(theme==='light'?'dark':'light')} aria-label="Toggle theme" title="Theme"><Icon name={theme==='light'?'moon':'sun'}/></button>{profile.is_admin&&<button className="admin-shortcut" onClick={()=>location.href='/admin'} title="Administration"><Icon name="shield"/><span>Administration</span></button>}<button className="icon-button member-profile-cta" onClick={()=>setTab('profile')} aria-label="Profile" title="Profile">{profile.avatar?<img src={profile.avatar} alt=""/>:<Icon name="user"/>}</button><button className="icon-button member-logout-button" onClick={()=>void logout()} aria-label="Logout" title="Logout"><Icon name="logout"/></button></div>
    </header>
    <ChatPage currentAccountID={profile.account_id} conversations={conversations} active={activeConversation} messages={chatMessages} participants={chatParticipants} loading={chatLoading} typing={typingNames} connection={chatConnection} text={chatText} reply={chatReply} editing={chatEditing} attachments={chatAttachments} uploading={uploading} recording={chatRecording} filter={chatFilter} search={chatSearch} hasMore={chatHasMore} groupCall={groupCallNotice?.conversation_id===activeConversation?.id?groupCallNotice:null} onFilter={setChatFilter} onSearch={setChatSearch} onBack={backFromChat} onOpen={openConversation} onCloseConversation={closeConversation} onText={(value)=>sendTyping(Boolean(value.trim()))} onKeyDown={chatKeyDown} onSend={(draft)=>void sendChat(draft)} onAttach={()=>fileInputRef.current?.click()} onDropFiles={files=>void addDroppedChatFiles(files)} onAudio={()=>void toggleChatRecording()} onReply={setChatReply} onCancelReply={()=>setChatReply(null)} onEdit={beginEditChat} onCancelEdit={cancelEditChat} onDelete={deleteChatMessage} onPin={message=>void pinChatMessage(message)} onLoadMore={()=>void loadEarlierChat()} onRevealMessage={revealChatMessage} onRemoveAttachment={index=>setChatAttachments(current=>current.filter((_,i)=>i!==index))} onReact={reactChat} onAddGroup={()=>setGroupModal(true)} onNewPrivate={()=>setPrivatePicker(true)} onAudioCall={()=>void startCall('audio')} onVideoCall={()=>void startCall('video')} onJoinGroupCall={()=>void joinGroupCall()} onProfile={showProfile}/>
    <input ref={fileInputRef} hidden type="file" multiple onChange={chooseChatFiles}/><input ref={audioInputRef} hidden type="file" accept="audio/*" multiple onChange={chooseChatFiles}/>
    {privatePicker&&<div className="member-modal-layer" onMouseDown={e=>{if(e.target===e.currentTarget)setPrivatePicker(false)}}><section className="member-modal private-picker"><button className="modal-x" onClick={()=>setPrivatePicker(false)}><Icon name="close"/></button><span>PRIVATE CHAT</span><h2>Choose a member</h2><div className="private-member-list">{members.filter(m=>m.account_id!==profile.account_id).map(m=><button key={m.account_id} onClick={()=>void createConversation('private',m.account_id)}><span className="directory-avatar"><MemberAvatar src={m.avatar} label={m.name}/></span><div><strong>{m.name}</strong><small>@{m.username} · {m.division}</small></div></button>)}</div></section></div>}
    {groupModal&&<GroupModal members={members.filter(m=>m.account_id!==profile.account_id)} name={groupName} selected={groupMembers} onName={setGroupName} onToggle={id=>setGroupMembers(current=>current.includes(id)?current.filter(x=>x!==id):[...current,id])} onClose={()=>{setGroupModal(false);setGroupName('');setGroupMembers([])}} onSave={()=>void createCustomGroup()}/>} 
    {memberConfirm&&<div className="member-modal-layer" onMouseDown={e=>{if(e.target===e.currentTarget)setMemberConfirm(null)}}><section className="member-modal member-confirm-modal"><button className="modal-x" onClick={()=>setMemberConfirm(null)}><Icon name="close"/></button><span>CONFIRM ACTION</span><h2>{memberConfirm.title}</h2><p>{memberConfirm.text}</p><footer><button className="button-secondary" onClick={()=>setMemberConfirm(null)}>Cancel</button><button className="primary-button" onClick={memberConfirm.action}>Continue</button></footer></section></div>}
    {profileCard&&profileCardAnchor&&<FloatingProfileCard profile={profileCard} anchor={profileCardAnchor} onClose={()=>setProfileCard(null)} onChat={profileCard.can_chat?()=>void createConversation('private',profileCard.account_id):undefined}/>} 
    {incomingCall&&<IncomingCallModal call={incomingCall} onAccept={()=>void acceptIncomingCall()} onDecline={declineIncomingCall}/>} 
    {activeCall&&<CallOverlay call={activeCall} self={profile} participants={chatParticipants} localStream={localCallStreamRef.current} remoteStreams={remoteStreams} remoteStates={remoteCallStates} muted={callMuted} cameraOff={callCameraOff} onMute={toggleCallMute} onCamera={toggleCallCamera} onEnd={()=>endCall(true)}/>} 
    <Footer/>
    <div className={`toast ${toast?'show':''}`}><span className="toast-dot"/>{toast}</div>
  </div>

  return <div className="member-shell">
    <header className="member-header">
      <button type="button" className="member-brand" onClick={()=>setTab('posts')} aria-label="AITS member home"><span className="global-brand-mark"><img src="/logo/aits.png" alt="AITS"/></span><div><strong>AITS</strong><small>Member Community</small></div></button>
      <nav className="member-desktop-nav">{desktopTabs.map(([id,label,icon])=><button key={id} className={tab===id?'active':''} onClick={()=>setTab(id)}><Icon name={icon}/><span>{label}</span>{id==='chat'&&chatUnread>0&&<b>{chatUnread>99?'99+':chatUnread}</b>}{id==='notifications'&&unread>0&&<b>{unread>99?'99+':unread}</b>}</button>)}</nav>
      <div className="member-header-actions"><button className="icon-button" onClick={()=>setTheme(theme==='light'?'dark':'light')} aria-label="Toggle theme" title="Theme"><Icon name={theme==='light'?'moon':'sun'}/></button>{profile.is_admin&&<button className="admin-shortcut" onClick={()=>location.href='/admin'} title="Administration"><Icon name="shield"/><span>Administration</span></button>}<button className={`icon-button member-profile-cta ${tab==='profile'?'active':''}`} onClick={()=>setTab('profile')} aria-label="Profile" title="Profile">{profile.avatar?<img src={profile.avatar} alt=""/>:<Icon name="user"/>}</button><button className="icon-button member-logout-button" onClick={()=>void logout()} aria-label="Logout" title="Logout"><Icon name="logout"/></button></div>
    </header>



    <main className={`member-main member-tab-${tab}`}>
      {tab==='posts'&&<PostsPage profile={profile} posts={posts} hasMore={hasMore} loading={feedLoading} publishing={uploading} title={postTitle} body={postBody} visibility={visibility} identity={postIdentity} onlineCount={onlineNames.size} mediaFiles={postMediaFiles} mediaInputRef={postMediaRef} onMedia={choosePostMedia} onRemoveMedia={i=>setPostMediaFiles(current=>current.filter((_,idx)=>idx!==i))} onTitle={setPostTitle} onBody={setPostBody} onVisibility={setVisibility} onIdentity={setPostIdentity} onPublish={publish} onOpen={openPost} onReact={reactPost} onShare={setShareTarget} onMore={loadMore} onProfile={()=>setTab('profile')} onMembers={()=>setTab('members')}/>} 
      {tab==='members'&&<MembersPage members={filteredMembers} onlineNames={onlineNames} search={memberSearch} divisionFilter={divisionFilter} accessFilter={accessFilter} membershipFilter={membershipFilter} divisions={divisionsForFilter} accesses={accessesForFilter} memberships={membershipsForFilter} onSearch={setMemberSearch} onDivision={setDivisionFilter} onAccess={setAccessFilter} onMembership={setMembershipFilter} onChat={member=>{setTabState('chat');void createConversation('private',member.account_id)}}/>} 
      {tab==='notifications'&&<NotificationsPage notifications={notifications} unread={unread} onReadAll={markAllRead} onClear={clearNotifications}/>} 
      {tab==='profile'&&<ProfilePage profile={profile} age={age} ownPosts={ownPosts} editing={profileEditing} form={profileForm} saving={saving} divisions={divisions} divisionChoice={divisionChoice} membershipChoice={membershipChoice} emailStage={emailStage} emailForm={emailForm} emailSeconds={Math.max(0,Math.ceil((emailCodeUntil-securityNow)/1000))} passwordStage={passwordStage} passwordForm={passwordForm} passwordSeconds={Math.max(0,Math.ceil((passwordCodeUntil-securityNow)/1000))} showEmailPassword={showEmailPassword} showNewPassword={showNewPassword} showConfirmPassword={showConfirmPassword} onEdit={()=>setProfileEditing(v=>!v)} onNameEdit={()=>setNameModal(true)} onForm={setProfileForm} onSave={saveProfile} onDivision={setDivisionChoice} onDivisionRequest={requestDivision} onMembership={setMembershipChoice} onMembershipRequest={requestMembership} onEmailForm={setEmailForm} onEmailRequest={requestEmailChange} onEmailConfirm={confirmEmailChange} onPasswordForm={setPasswordForm} onPasswordRequest={requestPasswordCode} onPasswordConfirm={confirmPasswordChange} onToggleEmailPassword={()=>setShowEmailPassword(v=>!v)} onToggleNewPassword={()=>setShowNewPassword(v=>!v)} onToggleConfirmPassword={()=>setShowConfirmPassword(v=>!v)} onOpenPost={openPost} onEditPost={updateOwnPost} onDeletePost={deleteOwnPost} onPostHistory={loadPostHistory} onAvatar={file=>void uploadAvatar(file)}/>} 
    </main>

    <Footer/>
    <nav className="member-mobile-tabs">{mobileTabs.map(([id,label,icon])=><button key={id} className={tab===id?'active':''} onClick={()=>setTab(id)}><Icon name={icon}/><span>{label}</span>{id==='chat'&&chatUnread>0&&<b>{chatUnread>99?'99+':chatUnread}</b>}{id==='notifications'&&unread>0&&<b>{unread>99?'99+':unread}</b>}</button>)}</nav>

    {nameModal&&<div className="member-modal-layer" onMouseDown={e=>{if(e.target===e.currentTarget)setNameModal(false)}}><section className="member-modal name-editor"><button className="modal-x" onClick={()=>setNameModal(false)}><Icon name="close"/></button><span>EDIT FULL NAME</span><h2>How should your name appear?</h2><div className="name-grid"><label>First name<input value={profileForm.first_name} onChange={e=>setProfileForm({...profileForm,first_name:e.target.value})}/></label><label>Middle name <small>Optional</small><input value={profileForm.middle_name} onChange={e=>setProfileForm({...profileForm,middle_name:e.target.value})}/></label><label>Last name<input value={profileForm.last_name} onChange={e=>setProfileForm({...profileForm,last_name:e.target.value})}/></label><label>Extension <small>Optional</small><input value={profileForm.extension_name} onChange={e=>setProfileForm({...profileForm,extension_name:e.target.value})} placeholder="Jr., III…"/></label></div><footer><button className="button-secondary" onClick={()=>setNameModal(false)}>Done</button></footer></section></div>}

    {detail&&<PostModal detail={detail} comments={groupedComments} text={commentText} reply={replyToComment} mentionResults={mentionResults} onClose={closePost} onReactPost={reaction=>reactPost(detail.id,reaction)} onText={value=>{setCommentText(value);void lookupMentions(value)}} onSubmit={submitComment} onReply={setReplyToComment} onCancelReply={()=>setReplyToComment(null)} onReactComment={reactComment} onShare={()=>setShareTarget(detail)} onMention={insertMention} onProfile={showProfile} onEdit={setEditingComment} onDelete={item=>void deleteComment(item)} onHistory={item=>void showCommentHistory(item)}/>} 
    {editingComment&&<CommentEditDialog key={editingComment.id} item={editingComment} onClose={()=>setEditingComment(null)} onSave={body=>void saveCommentEdit(body)}/>}
    {commentHistory.length>0&&<CommentHistoryModal history={commentHistory} onClose={()=>setCommentHistory([])}/>}
    {memberConfirm&&<div className="member-modal-layer" onMouseDown={e=>{if(e.target===e.currentTarget)setMemberConfirm(null)}}><section className="member-modal member-confirm-modal"><button className="modal-x" onClick={()=>setMemberConfirm(null)}><Icon name="close"/></button><span>CONFIRM ACTION</span><h2>{memberConfirm.title}</h2><p>{memberConfirm.text}</p><footer><button className="button-secondary" onClick={()=>setMemberConfirm(null)}>Cancel</button><button className="primary-button" onClick={memberConfirm.action}>Continue</button></footer></section></div>}
    {shareTarget&&<ShareModal open={true} title={shareTarget.title||'AITS post'} url={shareTarget.visibility==='public'?`${location.origin}/?post=${shareTarget.id}`:`${location.origin}/member/posts/${shareTarget.id}`} onClose={()=>setShareTarget(null)} onCopied={()=>setToast('Post link copied.')}/>} 
    {profileCard&&profileCardAnchor&&<FloatingProfileCard profile={profileCard} anchor={profileCardAnchor} onClose={()=>setProfileCard(null)} onChat={profileCard.can_chat?()=>{setTabState('chat');void createConversation('private',profileCard.account_id)}:undefined}/>} 
    {incomingCall&&<IncomingCallModal call={incomingCall} onAccept={()=>void acceptIncomingCall()} onDecline={declineIncomingCall}/>} 
    {activeCall&&<CallOverlay call={activeCall} self={profile} participants={chatParticipants} localStream={localCallStreamRef.current} remoteStreams={remoteStreams} remoteStates={remoteCallStates} muted={callMuted} cameraOff={callCameraOff} onMute={toggleCallMute} onCamera={toggleCallCamera} onEnd={()=>endCall(true)}/>} 
    <div className={`toast ${toast?'show':''}`}><span className="toast-dot"/>{toast}</div>
  </div>
}

function IdentityChips({profile}:{profile:MemberProfile}){return <div className="identity-chips"><span><small>Division</small><strong>{profile.division||'AITS'}</strong></span><span><small>Access</small><strong>{profile.role_name||'Member'}</strong></span><span><small>Membership</small><strong>{profile.membership_level||'Member'}</strong></span></div>}

function PostsPage({profile,posts,hasMore,loading,publishing,title,body,visibility,identity,onlineCount,mediaFiles,mediaInputRef,onMedia,onRemoveMedia,onTitle,onBody,onVisibility,onIdentity,onPublish,onOpen,onReact,onShare,onMore,onProfile,onMembers}:{profile:MemberProfile;posts:FeedPost[];hasMore:boolean;loading:boolean;publishing:boolean;title:string;body:string;visibility:Visibility;identity:PostIdentity;onlineCount:number;mediaFiles:File[];mediaInputRef:{current:HTMLInputElement|null};onMedia:(e:ChangeEvent<HTMLInputElement>)=>void;onRemoveMedia:(i:number)=>void;onTitle:(v:string)=>void;onBody:(v:string)=>void;onVisibility:(v:Visibility)=>void;onIdentity:(v:PostIdentity)=>void;onPublish:()=>void;onOpen:(id:number)=>void;onReact:(id:number)=>void;onShare:(p:FeedPost)=>void;onMore:()=>void;onProfile:()=>void;onMembers:()=>void}){
  return <section className="member-feed-layout"><aside className="profile-snapshot"><div className="snapshot-avatar">{profile.avatar?<img src={profile.avatar} alt=""/>:initials(profile.name)}</div><div className="snapshot-name"><h2>{profile.name}</h2><p>@{profile.username}</p></div><IdentityChips profile={profile}/>{(profile.member_tags||[]).length>0&&<div className="snapshot-tags">{profile.member_tags!.map(t=><span key={t}>{t}</span>)}</div>}<button onClick={onProfile}>View profile <Icon name="arrow"/></button></aside><div className="member-feed"><header className="feed-heading"><span>COMMUNITY FEED</span><h1>Latest from AITS</h1><p>Updates and conversations from AITS, newest first.</p></header><section className="post-composer"><div className="composer-author"><span>{profile.avatar?<img src={profile.avatar} alt=""/>:initials(profile.name)}</span><div><strong>{profile.name}</strong><small>@{profile.username}</small></div></div><input value={title} disabled={publishing} onChange={e=>onTitle(e.target.value)} placeholder="Post title (optional)"/><textarea value={body} disabled={publishing} onChange={e=>onBody(e.target.value)} placeholder="Share an update, idea, question, or activity…"/>{mediaFiles.length>0&&<div className="post-media-drafts">{mediaFiles.map((file,i)=><div key={`${file.name}-${i}`}><MediaDraft file={file}/><button onClick={()=>onRemoveMedia(i)} aria-label={`Remove ${file.name}`}><Icon name="close"/></button></div>)}</div>}<input ref={mediaInputRef} hidden type="file" accept="image/*,video/*" multiple onChange={onMedia}/><div className="composer-tools"><button type="button" disabled={publishing} onClick={()=>mediaInputRef.current?.click()}><Icon name="image"/> Photo / video</button><span>{mediaFiles.length?`${mediaFiles.length} selected · max 12`:'Images and videos supported'}</span></div><footer><CustomSelect value={identity} options={[{value:'member',label:`Personal · ${profile.membership_level||'Member'}${profile.role_name&&profile.role_name!=='Member'?` · ${profile.role_name}`:''}`},{value:'organization',label:'AITS Organization',disabled:!(profile.membership_level==='Officer'||profile.role!=='member'),hint:!(profile.membership_level==='Officer'||profile.role!=='member')?'Officer/elevated access required':undefined},{value:'division',label:`My Division · ${profile.division||'AITS'}`,disabled:!(profile.membership_level==='Officer'||profile.role!=='member')||!profile.division_id,hint:!profile.division_id?'No division assigned':!(profile.membership_level==='Officer'||profile.role!=='member')?'Officer/elevated access required':undefined}]} onChange={v=>onIdentity(v as PostIdentity)} label="POST AS"/><CustomSelect value={visibility} options={[{value:'organization',label:'AITS Organization'},{value:'division',label:`My Division · ${profile.division||'AITS'}`,disabled:!profile.division_id,hint:!profile.division_id?'No division assigned':undefined},{value:'public',label:'Public'}]} onChange={v=>onVisibility(v as Visibility)} label="AUDIENCE"/><button disabled={publishing||(!body.trim()&&!mediaFiles.length)} onClick={onPublish}>{publishing?'Publishing…':visibility==='public'&&!profile.is_admin?'Submit for review':'Publish'}</button></footer></section>{posts.map(post=><article className="feed-post" key={post.id}><header><span className="feed-avatar">{post.identity==='organization'?<img src="/logo/aits.png" alt="AITS"/>:post.identity==='division'&&post.division_logo?<img src={post.division_logo} alt=""/>:post.avatar?<img src={post.avatar} alt=""/>:initials(post.author)}</span><div><strong>{post.author}</strong><small>{post.role} · {post.time}{post.edited&&<span className="edited-post-badge">Edited</span>}</small></div><span className={`visibility-pill ${post.visibility}`}><Icon name={post.visibility==='public'?'globe':post.visibility==='division'?'network':'lock'} size={13}/>{post.visibility==='public'?'Public':post.visibility==='division'?post.division||'Division':'Organization'}</span>{post.status==='pending'&&<span className="post-review-pill">Pending review</span>}</header>{post.title&&<h2>{post.title}</h2>}<Markdown text={post.text}/><PostMediaGrid media={post.media||[]} onOpen={()=>onOpen(post.id)}/><div className="post-counts"><span>{post.reactions} reactions</span>{post.status==='pending'?<span>Awaiting approval</span>:<button onClick={()=>onOpen(post.id)}>{post.comments} comments</button>}</div>{post.status==='pending'?<footer className="pending-post-footer"><span>Waiting for Administration approval.</span></footer>:<footer><button onClick={()=>onReact(post.id)}><Icon name="heart"/>React</button><button onClick={()=>onOpen(post.id)}><Icon name="comment"/>Comment</button><button onClick={()=>onShare(post)}><Icon name="share"/>Share</button></footer>}</article>)}{loading&&<section className="feed-loading"><Skeleton rows={4}/></section>}<div className="view-more">{hasMore?<button disabled={loading} onClick={onMore}>{loading?'Loading…':'View more'}</button>:<strong>You’re caught up.</strong>}<small>{hasMore?'Load the next 10 posts when you’re ready.':'End of feed.'}</small></div></div><aside className="feed-side"><section><span>ONLINE NOW</span><strong>{onlineCount}</strong><p>Members online now.</p><button onClick={onMembers}>Browse members</button></section><section><span>POST AUDIENCES</span><p><b>Public</b> can be viewed and discussed by signed-in visitors and members.</p><p><b>Organization</b> is for AITS members.</p><p><b>Division</b> stays within your division.</p></section></aside></section>
}
function MediaDraft({file}:{file:File}){const [url,setURL]=useState('');useEffect(()=>{const u=URL.createObjectURL(file);setURL(u);return()=>URL.revokeObjectURL(u)},[file]);return file.type.startsWith('video/')?<video src={url} muted playsInline/>:<img src={url} alt={file.name}/>}
function PostMediaGrid({media,onOpen}:{media:Attachment[];onOpen:()=>void}){
  const [lightbox,setLightbox]=useState<number|null>(null)
  const visual=media.filter(item=>{const src=item.url||item.src||'',type=item.type||'';return !!src&&(type.startsWith('image')||type==='image'||type.startsWith('video')||type==='video'||/\.(png|jpe?g|webp|gif|mp4|webm|mov)(\?|$)/i.test(src))})
  if(!visual.length)return null
  const visible=visual.slice(0,4)
  const isVideo=(item:Attachment)=>{const src=item.url||item.src||'',type=item.type||'';return type.startsWith('video')||type==='video'||/\.(mp4|webm|mov)(\?|$)/i.test(src)}
  return <><div className={`member-post-media count-${Math.min(visible.length,4)}`}>{visible.map((item,i)=>{const src=item.url||item.src||'';const extra=i===3?visual.length-4:0;return <button type="button" key={`${src}-${i}`} onClick={event=>{event.stopPropagation();window.dispatchEvent(new Event('aits-close-floating-profile'));setLightbox(i)}} className="post-media-tile">{isVideo(item)?<video src={src} muted playsInline preload="metadata"/>:<img src={src} alt={item.name||''}/>} {extra>0&&<span className="post-media-more">+{extra}</span>}</button>})}</div>{lightbox!==null&&visual[lightbox]&&<div className="post-lightbox feed-lightbox" onMouseDown={event=>{if(event.target===event.currentTarget)setLightbox(null)}}><button className="post-lightbox-close" onClick={()=>setLightbox(null)} aria-label="Close media viewer"><Icon name="close"/></button>{isVideo(visual[lightbox])?<video controls autoPlay playsInline src={visual[lightbox].url||visual[lightbox].src}/>:<img src={visual[lightbox].url||visual[lightbox].src} alt={visual[lightbox].name||''}/>}<div className="post-lightbox-count">{lightbox+1} / {visual.length}</div>{visual.length>1&&<><button className="lightbox-prev" onClick={()=>setLightbox((lightbox-1+visual.length)%visual.length)}><Icon name="back"/></button><button className="lightbox-next" onClick={()=>setLightbox((lightbox+1)%visual.length)}><Icon name="arrow"/></button></>}<button className="post-lightbox-open-post" onClick={()=>{setLightbox(null);onOpen()}}>Open post</button></div>}</>
}

function MembersPage({members,onlineNames,search,divisionFilter,accessFilter,membershipFilter,divisions,accesses,memberships,onSearch,onDivision,onAccess,onMembership,onChat}:{members:DirectoryMember[];onlineNames:Set<string>;search:string;divisionFilter:string;accessFilter:string;membershipFilter:string;divisions:string[];accesses:string[];memberships:string[];onSearch:(v:string)=>void;onDivision:(v:string)=>void;onAccess:(v:string)=>void;onMembership:(v:string)=>void;onChat:(m:DirectoryMember)=>void}){
  const [presenceTick,setPresenceTick]=useState(0); useEffect(()=>{const id=window.setInterval(()=>setPresenceTick(x=>x+1),60000);return()=>window.clearInterval(id)},[])
  return <section className="members-page"><header className="page-heading compact"><div><span>MEMBER DIRECTORY</span><h1>Members</h1><p>Find people by division, access, or membership.</p></div><div className="online-summary"><i/><strong>{members.filter(m=>m.online||onlineNames.has(m.username.toLowerCase())).length}</strong><span>online</span></div></header><div className="member-filterbar"><label className="search-box"><Icon name="search"/><input value={search} onChange={e=>onSearch(e.target.value)} placeholder="Search name, username, tag…"/></label><CustomSelect value={divisionFilter} options={[{value:'all',label:'All divisions'},...divisions.map(v=>({value:v,label:v}))]} onChange={onDivision}/><CustomSelect value={accessFilter} options={[{value:'all',label:'All access'},...accesses.map(v=>({value:v,label:v}))]} onChange={onAccess}/><CustomSelect value={membershipFilter} options={[{value:'all',label:'All memberships'},...memberships.map(v=>({value:v,label:v}))]} onChange={onMembership}/></div><div className="member-directory-grid">{members.map(m=>{const online=m.online||onlineNames.has(m.username.toLowerCase());return <article key={m.account_id} className={online?'online':''}><header><span className="directory-avatar"><MemberAvatar src={m.avatar} label={m.name}/>{online&&<i/>}</span><div><strong>{m.name}</strong><small>@{m.username}</small></div></header><IdentityMini division={m.division} access={m.access} membership={m.membership}/>{m.tags?.length>0&&<div className="member-tags">{m.tags.map(t=><span key={t}>{t}</span>)}</div>}<footer><div><span className={online?'online-text':''}>{online?`Online ${formatDuration((m.straight_online_seconds||0)+presenceTick*60)} straight`:`Offline ${formatDuration(elapsedSince(m.last_seen_at))} ago`}</span><small>Total {formatDuration((m.online_seconds||0)+(online?presenceTick*60:0))} online</small></div><button className="directory-chat" onClick={()=>onChat(m)}><Icon name="chat"/>Chat</button></footer></article>})}</div>{members.length===0&&<div className="empty-state"><Icon name="users"/><strong>No members found</strong><span>Try a different search or filter.</span></div>}</section>
}
function IdentityMini({division,access,membership}:{division:string;access:string;membership:string}){return <div className="identity-mini"><span><small>Division</small><b>{division||'AITS'}</b></span><span><small>Access</small><b>{access||'Member'}</b></span><span><small>Membership</small><b>{membership||'Member'}</b></span></div>}


function NotificationsPage({notifications,unread,onReadAll,onClear}:{notifications:NotificationItem[];unread:number;onReadAll:()=>void;onClear:()=>void}){return <section className="notifications-page"><header className="page-heading compact"><div><span>ACTIVITY CENTER</span><h1>Notifications</h1><p>Replies, approvals, account changes, and notices.</p></div><div className="heading-actions">{unread>0&&<button onClick={onReadAll}><Icon name="check"/>Mark all read</button>}{notifications.length>0&&<button className="danger-soft" onClick={onClear}><Icon name="trash"/>Clear</button>}</div></header><div className="notification-list">{notifications.map(n=><article key={n.id} role={n.action_url?'button':undefined} tabIndex={n.action_url?0:undefined} onClick={()=>{if(n.action_url)location.href=n.action_url}} onKeyDown={e=>{if(n.action_url&&(e.key==='Enter'||e.key===' '))location.href=n.action_url}} className={`${n.read?'':'unread'} ${n.action_url?'clickable':''}`}><span className="notification-icon"><Icon name={n.type==='post'?'comment':n.type==='security'?'shield':'bell'}/></span><div><strong>{n.title}</strong><p>{n.body}</p><small>{n.time}</small></div>{!n.read&&<i/>}</article>)}</div>{notifications.length===0&&<div className="empty-state"><Icon name="bell"/><strong>You’re all caught up</strong><span>New activity will appear here.</span></div>}</section>}

function ProfilePage({profile,age,ownPosts,editing,form,saving,divisions,divisionChoice,membershipChoice,emailStage,emailForm,emailSeconds,passwordStage,passwordForm,passwordSeconds,showEmailPassword,showNewPassword,showConfirmPassword,onEdit,onNameEdit,onForm,onSave,onDivision,onDivisionRequest,onMembership,onMembershipRequest,onEmailForm,onEmailRequest,onEmailConfirm,onPasswordForm,onPasswordRequest,onPasswordConfirm,onToggleEmailPassword,onToggleNewPassword,onToggleConfirmPassword,onOpenPost,onEditPost,onDeletePost,onPostHistory,onAvatar}:{profile:MemberProfile;age:number|null;ownPosts:FeedPost[];editing:boolean;form:ProfileForm;saving:boolean;divisions:DivisionOption[];divisionChoice:number;membershipChoice:string;emailStage:'request'|'confirm';emailForm:{email:string;password:string;code:string};emailSeconds:number;passwordStage:'request'|'confirm';passwordForm:{code:string;newPassword:string;confirmPassword:string};passwordSeconds:number;showEmailPassword:boolean;showNewPassword:boolean;showConfirmPassword:boolean;onEdit:()=>void;onNameEdit:()=>void;onForm:(v:ProfileForm)=>void;onSave:()=>void;onDivision:(v:number)=>void;onDivisionRequest:()=>void;onMembership:(v:string)=>void;onMembershipRequest:()=>void;onEmailForm:(v:{email:string;password:string;code:string})=>void;onEmailRequest:()=>void;onEmailConfirm:()=>void;onPasswordForm:(v:{code:string;newPassword:string;confirmPassword:string})=>void;onPasswordRequest:()=>void;onPasswordConfirm:()=>void;onToggleEmailPassword:()=>void;onToggleNewPassword:()=>void;onToggleConfirmPassword:()=>void;onOpenPost:(id:number)=>void;onEditPost:(post:FeedPost,updates:{title:string;body:string;visibility:Visibility;identity:PostIdentity})=>Promise<void>;onDeletePost:(post:FeedPost)=>Promise<void>;onPostHistory:(id:number)=>Promise<{id:number;title:string;body:string;visibility:Visibility;identity:PostIdentity;time:string}[]>;onAvatar:(file:File)=>void}){
  const avatarRef=useRef<HTMLInputElement|null>(null);
  return <section className="profile-page"><div className="profile-hero"><div className="profile-avatar-wrap"><div className="profile-avatar-large">{profile.avatar?<img src={profile.avatar} alt=""/>:initials(profile.name)}</div><button type="button" className="profile-avatar-change" title="Change photo" aria-label="Change photo" onClick={()=>avatarRef.current?.click()}><Icon name="image"/>Change photo</button><input ref={avatarRef} hidden type="file" accept="image/*" onChange={e=>{const f=e.target.files?.[0];if(f)onAvatar(f);e.target.value=""}}/></div><div className="profile-heading"><span>MEMBER PROFILE</span><h1>{profile.name}</h1><p>@{profile.username}</p><IdentityChips profile={profile}/>{(profile.member_tags||[]).length>0&&<div className="member-tags">{profile.member_tags!.map(t=><span key={t}>{t}</span>)}</div>}</div><button className="profile-edit-button" onClick={onEdit}><Icon name="edit"/>{editing?'Close editor':'Edit profile'}</button></div><div className="profile-facts"><Fact label="Age" value={age!==null?`${age} years old`:'Not set'}/><Fact label="Gender" value={profile.gender||'Not set'}/><Fact label="Year Level" value={profile.year_level||'Not set'}/><Fact label="ID" value={profile.school_id||'Not set'}/><Fact label="Department" value={profile.department||'Not set'} wide/><Fact label="Course" value={profile.course||'Not set'} wide/></div>{editing&&<section className="profile-editor"><header><div><span>EDIT PROFILE</span><h2>Personal & academic information</h2></div><button onClick={onNameEdit}><Icon name="edit"/>Edit full name</button></header><div className="profile-form-grid"><label>Username<input value={form.username} onChange={e=>onForm({...form,username:e.target.value})}/></label><label>Year level<input value={form.year_level} onChange={e=>onForm({...form,year_level:e.target.value})} placeholder="4th Year"/></label><label>Birthdate<input type="date" max={new Date().toISOString().slice(0,10)} value={form.birth_date} onChange={e=>onForm({...form,birth_date:e.target.value})}/></label><label>Gender<CustomSelect value={form.gender} options={[{value:'',label:'Prefer not to specify'},{value:'Female',label:'Female'},{value:'Male',label:'Male'},{value:'Non-binary',label:'Non-binary'},{value:'Other',label:'Other'}]} onChange={v=>onForm({...form,gender:v})}/></label><label>Student ID<input value={form.school_id} onChange={e=>onForm({...form,school_id:e.target.value})}/></label><label>Department<input value={form.department} onChange={e=>onForm({...form,department:e.target.value})}/></label><label className="span-2">Course<input value={form.course} onChange={e=>onForm({...form,course:e.target.value})}/></label></div><footer><button className="primary-button" disabled={saving} onClick={onSave}>{saving?'Saving…':'Save profile'}</button></footer></section>}<div className="profile-grid"><section className="membership-panel"><header><span>MEMBERSHIP</span><h2>Organization placement</h2><p>Division and membership changes require approval.</p></header><IdentityMini division={profile.division} access={profile.role_name} membership={profile.membership_level}/><div className="request-card"><div><strong>Division request</strong><p>{profile.pending_division_request?`Pending request: ${profile.pending_division_request.division}`:'Request a move to another AITS division.'}</p></div><CustomSelect value={divisionChoice} options={[{value:0,label:'Choose division'},...divisions.filter(d=>d.id!==profile.division_id).map(d=>({value:d.id,label:d.name}))]} onChange={v=>onDivision(Number(v))} disabled={!!profile.pending_division_request}/><button disabled={saving||!divisionChoice||!!profile.pending_division_request} onClick={onDivisionRequest}>Submit division request</button></div><div className="request-card"><div><strong>Membership request</strong><p>{profile.pending_membership_request?`Pending request: ${profile.pending_membership_request}`:'Request a different membership classification.'}</p></div><CustomSelect value={membershipChoice} options={membershipOptions.map(v=>({value:v,label:v}))} onChange={onMembership} disabled={!!profile.pending_membership_request}/><button disabled={saving||membershipChoice===profile.membership_level||!!profile.pending_membership_request} onClick={onMembershipRequest}>Submit membership request</button></div></section><section className="security-panel"><header><span>SECURITY</span><h2>Email & password</h2><p>Security changes require verification.</p></header><div className="security-block"><div className="security-title"><Icon name="mail"/><div><strong>Change email</strong><small>Current: {profile.email||'No email assigned'}</small></div></div>{emailStage==='request'?<><input type="email" value={emailForm.email} onChange={e=>onEmailForm({...emailForm,email:e.target.value})} placeholder="New email"/><PasswordInput value={emailForm.password} show={showEmailPassword} placeholder="Current password" onChange={v=>onEmailForm({...emailForm,password:v})} onToggle={onToggleEmailPassword}/><button onClick={onEmailRequest} disabled={saving}>Verify new email</button></>:<><p className="security-note">Enter the code sent to <strong>{emailForm.email}</strong>. {emailSeconds>0&&<span className="security-code-timer">Code expires in {formatSecurityTime(emailSeconds)}</span>}</p><input inputMode="numeric" value={emailForm.code} onChange={e=>onEmailForm({...emailForm,code:e.target.value.replace(/\D/g,'').slice(0,6)})} placeholder="6-digit OTP"/><button onClick={onEmailConfirm} disabled={saving||emailForm.code.length<4}>Update email</button></>}</div><div className="security-block"><div className="security-title"><Icon name="key"/><div><strong>Change password</strong><small>{profile.email||'Add an email before changing your password'}</small></div></div>{passwordStage==='request'?<button onClick={onPasswordRequest} disabled={saving||!profile.email}>Send OTP to current email</button>:<><p className="security-note">Enter the OTP sent to your current account email. {passwordSeconds>0&&<span className="security-code-timer">Code expires in {formatSecurityTime(passwordSeconds)}</span>}</p><PasswordInput value={passwordForm.newPassword} show={showNewPassword} placeholder="New password · 10+ characters" onChange={v=>onPasswordForm({...passwordForm,newPassword:v})} onToggle={onToggleNewPassword}/><PasswordInput value={passwordForm.confirmPassword} show={showConfirmPassword} placeholder="Confirm new password" onChange={v=>onPasswordForm({...passwordForm,confirmPassword:v})} onToggle={onToggleConfirmPassword}/><input inputMode="numeric" value={passwordForm.code} onChange={e=>onPasswordForm({...passwordForm,code:e.target.value.replace(/\D/g,'').slice(0,6)})} placeholder="6-digit OTP"/><button onClick={onPasswordConfirm} disabled={saving||!passwordForm.code}>Update password</button></>}</div></section></div><section className="profile-posts"><header><span>YOUR POSTS</span><h2>Recent activity</h2></header>{ownPosts.length?<div className="profile-post-feed">{ownPosts.slice(0,8).map(p=><ProfilePostActivity key={p.id} post={p} profile={profile} onOpen={()=>onOpenPost(p.id)} onEdit={onEditPost} onDelete={onDeletePost} onHistory={onPostHistory}/>)}</div>:<div className="empty-state"><Icon name="spark"/><strong>No recent posts</strong><span>Your posts will appear here.</span></div>}</section></section>
}
function ProfilePostActivity({post,profile,onOpen,onEdit,onDelete,onHistory}:{post:FeedPost;profile:MemberProfile;onOpen:()=>void;onEdit:(post:FeedPost,updates:{title:string;body:string;visibility:Visibility;identity:PostIdentity})=>Promise<void>;onDelete:(post:FeedPost)=>Promise<void>;onHistory:(id:number)=>Promise<{id:number;title:string;body:string;visibility:Visibility;identity:PostIdentity;time:string}[]>}){
  const [editing,setEditing]=useState(false)
  const [historyOpen,setHistoryOpen]=useState(false)
  const [historyLoading,setHistoryLoading]=useState(false)
  const [history,setHistory]=useState<{id:number;title:string;body:string;visibility:Visibility;identity:PostIdentity;time:string}[]>([])
  const [draft,setDraft]=useState({title:post.title||'',body:post.text,visibility:post.visibility,identity:post.identity||'member'} as {title:string;body:string;visibility:Visibility;identity:PostIdentity})
  const [working,setWorking]=useState(false)
  useEffect(()=>setDraft({title:post.title||'',body:post.text,visibility:post.visibility,identity:post.identity||'member'}),[post.id,post.title,post.text,post.visibility,post.identity])
  const elevated=profile.membership_level==='Officer'||profile.role!=='member'
  const identitySrc=post.identity==='organization'?'/logo/aits.png':post.identity==='division'&&post.division_logo?post.division_logo:post.avatar
  async function save(){if(!draft.body.trim()||working)return;setWorking(true);try{await onEdit(post,{...draft,body:draft.body.trim(),title:draft.title.trim()});setEditing(false)}finally{setWorking(false)}}
  async function remove(){if(working||!window.confirm('Delete this post? This removes it from the community feed.'))return;setWorking(true);try{await onDelete(post)}finally{setWorking(false)}}
  async function showHistory(){setHistoryOpen(true);setHistoryLoading(true);try{setHistory(await onHistory(post.id))}finally{setHistoryLoading(false)}}
  return <article className="profile-activity-post">
    <header><span className="feed-avatar"><MemberAvatar src={identitySrc} label={post.author}/></span><div><strong>{post.author}</strong><small>{post.role} · {post.time}{post.edited&&<> · <button type="button" className="edited-inline" onClick={showHistory}>Edited</button></>}</small></div><span className={`visibility-pill ${post.visibility}`}>{post.visibility==='public'?'Public':post.visibility==='division'?post.division||'Division':'Organization'}</span></header>
    {post.title&&<h3>{post.title}</h3>}
    <Markdown text={post.text}/>
    <PostMediaGrid media={post.media||[]} onOpen={onOpen}/>
    <div className="profile-activity-stats"><span>{post.reactions} reactions</span><span>{post.comments} comments</span></div>
    <footer><button type="button" onClick={onOpen}><Icon name="eye"/>View Post</button><button type="button" onClick={()=>setEditing(true)}><Icon name="edit"/>Edit</button><button type="button" className="danger-soft" onClick={()=>void remove()} disabled={working}><Icon name="trash"/>Delete</button></footer>
    {editing&&<div className="profile-post-modal" role="dialog" aria-modal="true"><section><header><div><span>EDIT POST</span><strong>Update your activity</strong></div><button onClick={()=>setEditing(false)}><Icon name="close"/></button></header><label>Title<input value={draft.title} onChange={e=>setDraft({...draft,title:e.target.value})} placeholder="Optional title"/></label><label>Content<textarea value={draft.body} onChange={e=>setDraft({...draft,body:e.target.value})}/></label><div className="profile-post-edit-selects"><CustomSelect value={draft.identity} label="POST AS" options={[{value:'member',label:`Personal · ${profile.membership_level||'Member'}`},{value:'organization',label:'AITS Organization',disabled:!elevated,hint:!elevated?'Officer/elevated access required':undefined},{value:'division',label:`My Division · ${profile.division||'AITS'}`,disabled:!elevated||!profile.division_id,hint:!profile.division_id?'No division assigned':!elevated?'Officer/elevated access required':undefined}]} onChange={v=>setDraft({...draft,identity:v as PostIdentity})}/><CustomSelect value={draft.visibility} label="AUDIENCE" options={[{value:'organization',label:'AITS Organization'},{value:'division',label:`My Division · ${profile.division||'AITS'}`,disabled:!profile.division_id,hint:!profile.division_id?'No division assigned':undefined},{value:'public',label:'Public'}]} onChange={v=>setDraft({...draft,visibility:v as Visibility})}/></div><footer><button onClick={()=>setEditing(false)}>Cancel</button><button className="primary-button" disabled={working||!draft.body.trim()} onClick={()=>void save()}>{working?'Saving…':'Save changes'}</button></footer></section></div>}
    {historyOpen&&<div className="profile-post-modal" role="dialog" aria-modal="true" onMouseDown={e=>{if(e.target===e.currentTarget)setHistoryOpen(false)}}><section><header><div><span>EDIT HISTORY</span><strong>Versions from the last month</strong></div><button onClick={()=>setHistoryOpen(false)}><Icon name="close"/></button></header>{historyLoading?<p>Loading history…</p>:history.length?<div className="profile-post-history">{history.map(item=><article key={item.id}><small>{item.time} · {item.identity} · {item.visibility}</small>{item.title&&<strong>{item.title}</strong>}<Markdown text={item.body}/></article>)}</div>:<div className="empty-state"><strong>No retained versions</strong><span>The Edited badge remains even after one-month history expires.</span></div>}</section></div>}
  </article>
}
function Fact({label,value,wide=false}:{label:string;value:string;wide?:boolean}){return <article className={wide?'wide':''}><small>{label}</small><strong>{value}</strong></article>}
function PasswordInput({value,show,placeholder,onChange,onToggle}:{value:string;show:boolean;placeholder:string;onChange:(v:string)=>void;onToggle:()=>void}){return <div className="password-input"><input type={show?'text':'password'} value={value} autoComplete="new-password" data-form-type="other" onChange={e=>onChange(e.target.value)} placeholder={placeholder}/><button type="button" onClick={onToggle} aria-label={show?'Hide password':'Show password'}><Icon name={show?'eyeOff':'eye'}/></button></div>}

function escapeChatRegExp(value:string){return value.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')}
function chatDisplayText(body:string,participants:ChatParticipant[]){
  let value=body
  for(const person of participants){
    const username=(person.username||'').trim()
    if(!username)continue
    value=value.replace(new RegExp(`(^|\\s)@${escapeChatRegExp(username)}\\b`,'gi'),(_,space:string)=>`${space}@[${person.name}](account:${person.account_id})`)
  }
  return value
}
function stripChatCommands(value:string){
  let body=value.trim()
  for(let i=0;i<3;i++){
    const next=body.replace(/^\/(?:limit(?:view)?-\d+|limittime-\d+)\s+/i,'')
    if(next===body)break
    body=next.trim()
  }
  return body
}
function ChatPage({
  currentAccountID,conversations,active,messages,participants,loading,typing,connection,text,reply,editing,attachments,uploading,recording,filter,search,hasMore,groupCall,
  onFilter,onSearch,onBack,onOpen,onCloseConversation,onText,onKeyDown,onSend,onAttach,onDropFiles,onAudio,onReply,onCancelReply,onEdit,onCancelEdit,onDelete,onPin,onLoadMore,onRevealMessage,onRemoveAttachment,onReact,onAddGroup,onNewPrivate,onAudioCall,onVideoCall,onJoinGroupCall,onProfile
}:{
  currentAccountID:number;conversations:Conversation[];active:Conversation|null;messages:ChatMessage[];participants:ChatParticipant[];loading:boolean;typing:string[];connection:'connecting'|'online'|'offline';text:string;reply:ChatMessage|null;editing:ChatMessage|null;attachments:Attachment[];uploading:boolean;recording:boolean;filter:'all'|'private'|'groups';search:string;hasMore:boolean;groupCall:IncomingCall|null;
  onFilter:(v:'all'|'private'|'groups')=>void;onSearch:(v:string)=>void;onBack:()=>void;onOpen:(c:Conversation)=>void;onCloseConversation:()=>void;onText:(v:string)=>void;onKeyDown:(e:KeyboardEvent<HTMLTextAreaElement>)=>void;onSend:(draft?:string)=>void;onAttach:()=>void;onDropFiles:(files:File[])=>void;onAudio:()=>void;onReply:(m:ChatMessage)=>void;onCancelReply:()=>void;onEdit:(m:ChatMessage)=>void;onCancelEdit:()=>void;onDelete:(m:ChatMessage)=>void;onPin:(m:ChatMessage)=>void;onLoadMore:()=>void;onRevealMessage:(id:number)=>Promise<void>;onRemoveAttachment:(i:number)=>void;onReact:(m:ChatMessage,reaction?:string)=>void;onAddGroup:()=>void;onNewPrivate:()=>void;onAudioCall:()=>void;onVideoCall:()=>void;onJoinGroupCall:()=>void;onProfile:(id:number,event:ReactMouseEvent<HTMLElement>|ReactFocusEvent<HTMLElement>)=>void
}){
  const endRef=useRef<HTMLDivElement|null>(null),streamRef=useRef<HTMLDivElement|null>(null),composerRef=useRef<HTMLTextAreaElement|null>(null)
  const gestureRef=useRef<{messageID:number;pointerID:number;startX:number;startY:number;node:HTMLElement;timer:number|null;longPressed:boolean}|null>(null)
  const [actionMessage,setActionMessage]=useState<ChatMessage|null>(null),[actionMode,setActionMode]=useState<'desktop'|'mobile'>('desktop'),[actionAnchor,setActionAnchor]=useState<{x:number;y:number}|null>(null),[now,setNow]=useState(Date.now()),[dropActive,setDropActive]=useState(false),[expandedMetaID,setExpandedMetaID]=useState<number|null>(null),[draft,setDraft]=useState(text)
  const [reactionPeople,setReactionPeople]=useState<{reaction:string;account_id:number;name:string;avatar:string}[]|null>(null)
  const [history,setHistory]=useState<{id:number;body:string;time:string;editor:string}[]|null>(null)
  const [pinsModal,setPinsModal]=useState<{id:number;body:string;sender:string;pinned_by:number;pinned_at:string}[]|null>(null)
  const [topPin,setTopPin]=useState<{id:number;body:string;sender:string;pinned_by:number;pinned_at:string}|null>(null)
  const [pickerMessage,setPickerMessage]=useState<ChatMessage|null>(null)
  const [customReaction,setCustomReaction]=useState('')
  const [helpModal,setHelpModal]=useState<'commands'|'markdown'|'markup'|'usecommand'|null>(null)
  const [searchModal,setSearchModal]=useState(false),[messageSearch,setMessageSearch]=useState(''),[messageSearchResults,setMessageSearchResults]=useState<ChatSearchResult[]>([]),[messageSearchBusy,setMessageSearchBusy]=useState(false)
  const [showJumpLatest,setShowJumpLatest]=useState(false),[pendingLatest,setPendingLatest]=useState(0)
  const [commandBuilder,setCommandBuilder]=useState<'warning'|'note'|'quote'|'checklist'|'limittime'|'limitview'|'code'|'meeting'|'table'|null>(null)
  const [builderMessage,setBuilderMessage]=useState(''),[builderNumber,setBuilderNumber]=useState('30'),[builderAux,setBuilderAux]=useState('text')
  const [builderItems,setBuilderItems]=useState<Array<{text:string;checked:boolean}>>([{text:'',checked:false},{text:'',checked:false}])
  const [pollModal,setPollModal]=useState(false),[pollQuestion,setPollQuestion]=useState(''),[pollOptions,setPollOptions]=useState(['','']),[pollDuration,setPollDuration]=useState('0'),[pollBusy,setPollBusy]=useState(false),[pollError,setPollError]=useState('')
  const [polls,setPolls]=useState<Record<number,ChatPoll>>({})
  const [composerCaret,setComposerCaret]=useState<number|null>(null)
  const [initialPositioned,setInitialPositioned]=useState(false)
  const emoji=['👍','❤️','😂','😮','😢','😡']
  const commands=[
    {cmd:'/usecommand',purpose:'Open the command builder',sample:'/usecommand',builder:'usecommand'},
    {cmd:'/commands',purpose:'Show the command list',sample:'/commands',builder:'commands'},
    {cmd:'/markdown',purpose:'Open the Markdown syntax guide',sample:'/markdown',builder:'markdown'},
    {cmd:'/markup',purpose:'Open the markup syntax guide',sample:'/markup',builder:'markup'},
    {cmd:'/search',purpose:'Search messages in this conversation',sample:'/search',builder:'search'},
    {cmd:'/poll',purpose:'Create a poll',sample:'/poll',builder:'poll'},
    {cmd:'/warning',purpose:'Build a warning callout',sample:'/warning Important meeting at 3 PM',builder:'warning'},
    {cmd:'/note',purpose:'Build a note callout',sample:'/note Remember the deadline',builder:'note'},
    {cmd:'/quote',purpose:'Build a quoted message',sample:'/quote Quoted text',builder:'quote'},
    {cmd:'/checklist',purpose:'Build a checklist',sample:'/checklist',builder:'checklist'},
    {cmd:'/limittime',purpose:'Set a message expiry time',sample:'/limittime-30 Message',builder:'limittime'},
    {cmd:'/limitview',purpose:'Limit a group message to N viewers',sample:'/limitview-5 Message',builder:'limitview'},
    {cmd:'/code',purpose:'Build a fenced code block',sample:'/code',builder:'code'},
    {cmd:'/meeting',purpose:'Build meeting notes',sample:'/meeting',builder:'meeting'},
    {cmd:'/table',purpose:'Build a Markdown table',sample:'/table',builder:'table'},
    {cmd:'/shrug',purpose:'Insert a shrug',sample:'¯\\_(ツ)_/¯',builder:'shrug'}
  ] as const
  const markdownGuide=`# Heading 1\n## Heading 2\n### Heading 3\n\n**Bold**  __Bold__\n*Italic*  _Italic_\n***Bold + Italic***\n~~Strikethrough~~\n==Highlight==  ++Inserted++\n^Superscript^  ~Subscript~\n\n- Bullet\n  - Nested bullet\n1. Ordered item\n- [ ] Task\n- [x] Done\n\n[Link](https://example.com)\n![Image](image-url.jpg)\n> Blockquote\n\`inline code\`\n\`\`\`python\nprint("Hello AITS")\n\`\`\`\n\n| Left | Center | Right |\n| :--- | :---: | ---: |\n| A | B | C |\n\n> [!NOTE]\n> Note text\n\n$E = mc^2$\n:heart:\n\\*escaped asterisk\\*`
  const markupGuide=`<h1>Heading</h1>\n<p>Paragraph</p>\n<b>Bold</b> <strong>Bold</strong>\n<i>Italic</i> <em>Italic</em>\n<u>Underline</u> <mark>Highlight</mark>\n<del>Deleted</del> <ins>Inserted</ins>\n\n<p style="color:red;">Red text</p>\n<span style="background-color:#fff3a3;">Highlighted span</span>\n<div style="text-align:center;">Centered block</div>\n\n<a href="https://example.com">Link</a>\n<img src="image-url.jpg" alt="Image">\n<blockquote>Quote</blockquote>\n<hr>\n<code>inline_code</code>\n<kbd>Ctrl</kbd> + <kbd>C</kbd>\n<br>\n\n<details>\n  <summary>Open</summary>\n  <p>Hidden content</p>\n</details>\n\n<table>\n  <tr><th>Header</th><th>Header</th></tr>\n  <tr><td>Cell</td><td>Cell</td></tr>\n</table>`
  const request=async(path:string,init?:RequestInit)=>{const r=await fetch(path,{credentials:'include',cache:'no-store',...init});const p=await r.json().catch(()=>({}));if(!r.ok)throw new Error(p.error||'Request failed');return p}
  const resizeComposer=()=>{const input=composerRef.current;if(!input)return;const style=getComputedStyle(input),min=parseFloat(style.minHeight)||42,max=parseFloat(style.maxHeight)||116;input.style.height=`${min}px`;const h=input.scrollHeight;input.style.height=`${Math.min(max,Math.max(min,h))}px`;input.style.overflowY=h>max?'auto':'hidden'}
  useEffect(()=>{const id=window.setInterval(()=>setNow(Date.now()),15000);return()=>clearInterval(id)},[])
  useEffect(()=>setExpandedMetaID(null),[active?.id])
  useEffect(()=>{setDraft(text);setComposerCaret(null)},[text,editing?.id,active?.id])
  useEffect(()=>{const frame=requestAnimationFrame(resizeComposer);return()=>cancelAnimationFrame(frame)},[draft,active?.id])
  const scrollStateRef=useRef({activeID:0,firstID:0,lastID:0,height:0,top:0,nearBottom:true})
  const firstMessageID=messages[0]?.id||0,lastMessageID=messages[messages.length-1]?.id||0
  const scrollToLatest=(behavior:ScrollBehavior='auto')=>{const node=streamRef.current;if(!node)return;node.scrollTo({top:node.scrollHeight,behavior});scrollStateRef.current={...scrollStateRef.current,height:node.scrollHeight,top:node.scrollHeight,nearBottom:true};setShowJumpLatest(false);setPendingLatest(0)}
  useLayoutEffect(()=>{
    const node=streamRef.current;if(!node)return
    const previous=scrollStateRef.current,activeID=active?.id||0,newest=messages[messages.length-1]
    const switched=activeID>0&&previous.activeID!==activeID
    const prepended=previous.activeID===activeID&&previous.firstID>0&&firstMessageID>0&&previous.firstID!==firstMessageID&&previous.lastID===lastMessageID
    const appended=previous.activeID===activeID&&lastMessageID!==0&&previous.lastID!==0&&previous.lastID!==lastMessageID
    if(switched){setInitialPositioned(false);node.scrollTop=node.scrollHeight;setShowJumpLatest(false);setPendingLatest(0)}
    else if(prepended)node.scrollTop=previous.top+(node.scrollHeight-previous.height)
    else if(appended){if(previous.nearBottom||newest?.mine)scrollToLatest('auto');else{setShowJumpLatest(true);if(!newest?.mine)setPendingLatest(count=>count+1)}}
    if(activeID&&!loading&&!initialPositioned){node.scrollTop=node.scrollHeight;scrollStateRef.current={...scrollStateRef.current,height:node.scrollHeight,top:node.scrollTop,nearBottom:true}}
    const distance=node.scrollHeight-node.scrollTop-node.clientHeight
    scrollStateRef.current={activeID,firstID:firstMessageID,lastID:lastMessageID,height:node.scrollHeight,top:node.scrollTop,nearBottom:distance<100}
  },[active?.id,loading,firstMessageID,lastMessageID,messages.length,initialPositioned])
  useEffect(()=>{const node=streamRef.current;if(!node||!typing.length||!scrollStateRef.current.nearBottom)return;scrollToLatest('auto')},[typing.length])
  useEffect(()=>{
    const node=streamRef.current;if(!node||!active?.id)return
    const keepAtLatest=()=>{if(initialPositioned&&!scrollStateRef.current.nearBottom)return;requestAnimationFrame(()=>{if(streamRef.current!==node)return;node.scrollTop=node.scrollHeight;scrollStateRef.current={...scrollStateRef.current,height:node.scrollHeight,top:node.scrollTop,nearBottom:true}})}
    const observer=new ResizeObserver(keepAtLatest)
    node.querySelectorAll('.message-row').forEach(item=>observer.observe(item))
    const timers=!initialPositioned?[0,60,150,300,520,820].map(delay=>window.setTimeout(()=>{if(streamRef.current===node){node.scrollTop=node.scrollHeight;scrollStateRef.current={...scrollStateRef.current,height:node.scrollHeight,top:node.scrollTop,nearBottom:true};if(delay===820)setInitialPositioned(true)}},delay)):[]
    return()=>{observer.disconnect();timers.forEach(window.clearTimeout)}
  },[active?.id,messages.length,loading,initialPositioned])
  const pinSignature=useMemo(()=>messages.filter(m=>m.pinned&&!m.unsent).map(m=>m.id).sort((a,b)=>a-b).join(','),[messages])
  useEffect(()=>{setActionMessage(null);setTopPin(null);setShowJumpLatest(false);setPendingLatest(0);setSearchModal(false);setMessageSearch('');setMessageSearchResults([])},[active?.id])
  useEffect(()=>{if(!active){setTopPin(null);return}void request(`/api/member/chats/${active.id}/pins`).then(p=>setTopPin((p.pins||[])[0]||null)).catch(()=>undefined)},[active?.id,pinSignature])
  useEffect(()=>{if(!searchModal||!active){setMessageSearchResults([]);return}const query=messageSearch.trim();if(!query){setMessageSearchResults([]);setMessageSearchBusy(false);return}setMessageSearchBusy(true);const timer=window.setTimeout(()=>{void request(`/api/member/chats/${active.id}/search?q=${encodeURIComponent(query)}&limit=80`).then(p=>setMessageSearchResults((p.results||[]) as ChatSearchResult[])).catch(()=>setMessageSearchResults([])).finally(()=>setMessageSearchBusy(false))},220);return()=>window.clearTimeout(timer)},[searchModal,messageSearch,active?.id])
  const loadPoll=async(pollID:number)=>{if(!active||pollID<1)return;try{const p=await request(`/api/member/chats/${active.id}/polls/${pollID}`);if(p.poll)setPolls(current=>({...current,[pollID]:p.poll as ChatPoll}))}catch{/* poll may have been removed */}}
  const pollIDs=useMemo(()=>Array.from(new Set(messages.map(m=>Number(m.poll_id||0)).filter(Boolean))),[messages])
  useEffect(()=>{if(!active)return;pollIDs.forEach(id=>void loadPoll(id));const timer=pollIDs.length?window.setInterval(()=>pollIDs.forEach(id=>void loadPoll(id)),20000):0;return()=>{if(timer)window.clearInterval(timer)}},[active?.id,pollIDs.join(',')])
  useEffect(()=>{const handler=(event:Event)=>{const id=Number((event as CustomEvent<{poll_id:number}>).detail?.poll_id||0);if(id&&pollIDs.includes(id))void loadPoll(id)};window.addEventListener('aits-chat-poll-update',handler);return()=>window.removeEventListener('aits-chat-poll-update',handler)},[active?.id,pollIDs.join(',')])
  const shown=conversations.filter(c=>filter==='all'||(filter==='private'?c.scope==='private':c.scope!=='private')).filter(c=>`${c.name} ${c.last}`.toLowerCase().includes(search.trim().toLowerCase()))
  const visibleMessages=messages.filter(m=>!m.expires_at||Date.parse(m.expires_at)>now),replyPreview=(id:number)=>messages.find(m=>m.id===id)
  const jumpToMessage=async(id:number)=>{const find=()=>document.getElementById(`chat-message-${id}`);let hit=find();if(!hit){await onRevealMessage(id);await new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve())));hit=find()}if(!hit&&hasMore){onLoadMore();await new Promise(resolve=>setTimeout(resolve,420));hit=find()}if(hit){hit.scrollIntoView({behavior:'smooth',block:'center'});hit.classList.add('message-focus-flash');window.setTimeout(()=>hit?.classList.remove('message-focus-flash'),1400)}}
  const showReactions=async(m:ChatMessage)=>{if(!active)return;try{const p=await request(`/api/member/chats/${active.id}/messages/${m.id}/reactions`);setReactionPeople(p.people||[])}catch{}}
  const showHistory=async(m:ChatMessage)=>{if(!active)return;try{const p=await request(`/api/member/chats/${active.id}/messages/${m.id}/history`);setHistory(p.history||[])}catch{}}
  const showPins=async()=>{if(!active)return;try{const p=await request(`/api/member/chats/${active.id}/pins`);setPinsModal(p.pins||[])}catch{}}
  const clearGesture=()=>{const g=gestureRef.current;if(!g)return;if(g.timer!==null)clearTimeout(g.timer);g.node.style.transform='';g.node.style.removeProperty('--reply-swipe');gestureRef.current=null}
  const beginGesture=(m:ChatMessage,e:ReactPointerEvent<HTMLElement>)=>{if(e.pointerType==='mouse'||e.button!==0||(e.target as HTMLElement).closest('button,a,input,textarea,audio,video'))return;clearGesture();const row=e.currentTarget,node=(row.querySelector('.message-stack') as HTMLElement|null)||row;const timer=window.setTimeout(()=>{const g=gestureRef.current;if(!g||g.messageID!==m.id)return;g.longPressed=true;node.style.transform='';setActionMode('mobile');setActionMessage(m);navigator.vibrate?.(18)},480);gestureRef.current={messageID:m.id,pointerID:e.pointerId,startX:e.clientX,startY:e.clientY,node,timer,longPressed:false};row.setPointerCapture?.(e.pointerId)}
  const moveGesture=(m:ChatMessage,e:ReactPointerEvent<HTMLElement>)=>{const g=gestureRef.current;if(!g||g.pointerID!==e.pointerId||g.longPressed)return;const dx=e.clientX-g.startX,dy=e.clientY-g.startY;if(Math.abs(dy)>Math.abs(dx)&&Math.abs(dy)>9){clearGesture();return}if(Math.abs(dx)<6)return;if(g.timer!==null){clearTimeout(g.timer);g.timer=null}const directed=m.mine?Math.min(0,dx):Math.max(0,dx),offset=Math.max(-72,Math.min(72,directed));g.node.style.transform=`translateX(${offset}px)`;g.node.style.setProperty('--reply-swipe',String(Math.min(1,Math.abs(offset)/52)))}
  const endGesture=(m:ChatMessage,e:ReactPointerEvent<HTMLElement>)=>{const g=gestureRef.current;if(!g||g.pointerID!==e.pointerId)return;const dx=e.clientX-g.startX,replyNow=!g.longPressed&&(m.mine?dx<=-52:dx>=52);clearGesture();if(replyNow){onReply(m);navigator.vibrate?.(10)}}
  const chooseReaction=(m:ChatMessage,r:string)=>{const value=r.trim();if(!value)return;onReact(m,value);setActionMessage(null);setActionAnchor(null);setPickerMessage(null);setCustomReaction('')}
  const copyMessage=async(m:ChatMessage)=>{const body=stripChatCommands(m.body).trim();if(body)await navigator.clipboard?.writeText(body).catch(()=>undefined);setActionMessage(null);setActionAnchor(null)}
  const openDesktopActionMenu=(m:ChatMessage,button:HTMLButtonElement)=>{
    const rect=button.getBoundingClientRect(),gap=8
    const viewportWidth=document.documentElement.clientWidth||window.innerWidth
    const viewportHeight=document.documentElement.clientHeight||window.innerHeight
    const menuWidth=Math.min(286,Math.max(220,viewportWidth-16))
    const optionCount=(m.id>0?1:0)+(stripChatCommands(m.body).trim()?1:0)+(m.id>0&&(!m.pinned||m.pinned_by===currentAccountID)?1:0)+(m.mine&&m.id>0&&!m.poll_id?1:0)+(m.mine&&m.id>0?1:0)
    const menuHeight=Math.min(330,64+(optionCount*40))
    const preferredX=m.mine?rect.left-menuWidth-gap:rect.right+gap
    const fallbackX=m.mine?rect.right+gap:rect.left-menuWidth-gap
    let x=preferredX
    if(x<8||x+menuWidth>viewportWidth-8)x=fallbackX
    x=Math.max(8,Math.min(viewportWidth-menuWidth-8,x))
    const below=viewportHeight-rect.top-8,above=rect.bottom-8
    let y=below>=menuHeight?rect.top:above>=menuHeight?rect.bottom-menuHeight:rect.top+(rect.height-menuHeight)/2
    y=Math.max(8,Math.min(viewportHeight-menuHeight-8,y))
    setActionAnchor({x,y});setActionMode('desktop');setActionMessage(m)
  }
  const actionMenu=(m:ChatMessage,mobile=false)=><div className={mobile?'message-action-sheet':'message-action-menu desktop-floating'} style={mobile?undefined:{left:actionAnchor?.x??8,top:actionAnchor?.y??8}} onPointerDown={e=>e.stopPropagation()}>
    {!m.unsent&&<div className="message-action-reactions">{emoji.map(r=><button key={r} className={m.my_reaction===r?'active':''} onClick={()=>chooseReaction(m,r)}>{r}</button>)}<button className="reaction-more" aria-label="More emoji reactions" onClick={()=>{setPickerMessage(m);setActionMessage(null);setActionAnchor(null)}}>+</button></div>}
    <div className="message-action-options">{!m.unsent&&m.id>0&&<button onClick={()=>{setActionMessage(null);setActionAnchor(null);onReply(m)}}><Icon name="comment"/><span>Reply</span></button>}{!m.unsent&&!!stripChatCommands(m.body).trim()&&<button onClick={()=>void copyMessage(m)}><Icon name="copy"/><span>Copy</span></button>}{!m.unsent&&m.id>0&&(!m.pinned||m.pinned_by===currentAccountID)&&<button onClick={()=>{setActionMessage(null);setActionAnchor(null);onPin(m)}}><Icon name="pin"/><span>{m.pinned?'Unpin':'Pin'}</span></button>}{!m.unsent&&m.mine&&m.id>0&&!m.poll_id&&<button onClick={()=>{setActionMessage(null);setActionAnchor(null);onEdit(m)}}><Icon name="edit"/><span>Edit</span></button>}{!m.unsent&&m.mine&&m.id>0&&<button className="danger" onClick={()=>{setActionMessage(null);setActionAnchor(null);onDelete(m)}}><Icon name="trash"/><span>Unsend</span></button>}</div>
  </div>
  const resetBuilder=()=>{setBuilderMessage('');setBuilderNumber('30');setBuilderAux('text');setBuilderItems([{text:'',checked:false},{text:'',checked:false}])}
  const openBuilder=(kind:'warning'|'note'|'quote'|'checklist'|'limittime'|'limitview'|'code'|'meeting'|'table')=>{resetBuilder();if(kind==='limitview')setBuilderNumber('5');setHelpModal(null);setCommandBuilder(kind);setDraft('');onText('')}
  const launchCommand=(cmd:string)=>{
    const command=cmd.trim().toLowerCase()
    if(command==='/usecommand'){setHelpModal('usecommand');setDraft('');onText('');return true}
    if(command==='/commands'){setHelpModal('commands');setDraft('');onText('');return true}
    if(command==='/markdown'){setHelpModal('markdown');setDraft('');onText('');return true}
    if(command==='/markup'){setHelpModal('markup');setDraft('');onText('');return true}
    if(command==='/search'){setSearchModal(true);setMessageSearch('');setMessageSearchResults([]);setDraft('');onText('');return true}
    if(command==='/poll'){setPollError('');setPollModal(true);setDraft('');onText('');return true}
    if(command==='/warning'){openBuilder('warning');return true}
    if(command==='/note'){openBuilder('note');return true}
    if(command==='/quote'){openBuilder('quote');return true}
    if(command==='/checklist'){openBuilder('checklist');return true}
    if(command==='/limittime'){openBuilder('limittime');return true}
    if(command==='/limitview'){openBuilder('limitview');return true}
    if(command==='/code'){openBuilder('code');return true}
    if(command==='/meeting'){openBuilder('meeting');return true}
    if(command==='/table'){openBuilder('table');return true}
    if(command==='/shrug'){setDraft('¯\\_(ツ)_/¯');requestAnimationFrame(()=>composerRef.current?.focus());return true}
    return false
  }
  const modalCommand=(value:string)=>launchCommand(value.trim())
  const submitDraft=()=>{const outgoing=draft;if(modalCommand(outgoing))return;setDraft('');setComposerCaret(null);onText('');onSend(outgoing)}
  const handleComposerKeyDown=(event:KeyboardEvent<HTMLTextAreaElement>)=>{if(event.key==='Enter'&&!event.shiftKey){event.preventDefault();submitDraft();return}onKeyDown(event)}
  const handleComposerSend=()=>submitDraft()
  const useCommand=(cmd:string)=>{if(launchCommand(cmd.trim()))return;setDraft(cmd+(cmd.endsWith(' ')?'':' '));requestAnimationFrame(()=>composerRef.current?.focus())}
  const applyCommandBuilder=()=>{
    if(!commandBuilder)return
    const message=builderMessage.trim()
    let value=''
    if(commandBuilder==='warning')value=`> [!WARNING]\n${message.split(/\r?\n/).map(line=>`> ${line}`).join('\n')}`
    if(commandBuilder==='note')value=`> [!NOTE]\n${message.split(/\r?\n/).map(line=>`> ${line}`).join('\n')}`
    if(commandBuilder==='quote')value=message.split(/\r?\n/).map(line=>`> ${line}`).join('\n')
    if(commandBuilder==='checklist')value=builderItems.filter(item=>item.text.trim()).map(item=>`- [${item.checked?'x':' '}] ${item.text.trim()}`).join('\n')
    if(commandBuilder==='limittime'){const seconds=Math.min(86400,Math.max(5,Number(builderNumber)||30));value=`/limittime-${seconds} ${message}`}
    if(commandBuilder==='limitview'){const viewers=Math.min(100,Math.max(1,Number(builderNumber)||5));value=`/limitview-${viewers} ${message}`}
    if(commandBuilder==='code')value=`\`\`\`${builderAux.trim()||'text'}\n${builderMessage.replace(/\s+$/,'')}\n\`\`\``
    if(commandBuilder==='meeting')value=`### Meeting notes\n${message||'- **Topic:** \n- **Decision:** \n- **Owner:** \n- [ ] Follow-up task'}`
    if(commandBuilder==='table'){const rows=builderMessage.split(/\r?\n/).map(row=>row.split('|').map(cell=>cell.trim()).filter(Boolean)).filter(row=>row.length);if(rows.length){const width=Math.max(...rows.map(row=>row.length));const padded=rows.map(row=>[...row,...Array(Math.max(0,width-row.length)).fill('')]);value=`| ${padded[0].join(' | ')} |\n| ${padded[0].map(()=> '---').join(' | ')} |${padded.slice(1).map(row=>`\n| ${row.join(' | ')} |`).join('')}`}else value='| Column 1 | Column 2 |\n| --- | --- |\n| Value 1 | Value 2 |'}
    if(!value.trim())return
    setDraft(value);setCommandBuilder(null);resetBuilder();requestAnimationFrame(()=>composerRef.current?.focus())
  }
  const builderReady=commandBuilder==='checklist'?builderItems.some(item=>item.text.trim()):commandBuilder==='table'?builderMessage.trim().length>0:commandBuilder==='code'?builderMessage.trim().length>0:!!builderMessage.trim()
  const caretPosition=Math.max(0,Math.min(composerCaret??draft.length,draft.length)),mentionHead=draft.slice(0,caretPosition)
  const mentionMatch=!editing?mentionHead.match(/@([\p{L}\p{N}_.-]*)$/u):null
  const insertMention=(p:ChatParticipant)=>{if(!mentionMatch||mentionMatch.index===undefined)return;const token=`@${p.username||p.name.replace(/\s+/g,'')} `,nextCaret=mentionMatch.index+token.length;setDraft(draft.slice(0,mentionMatch.index)+token+draft.slice(caretPosition));setComposerCaret(nextCaret);requestAnimationFrame(()=>{const input=composerRef.current;input?.focus();input?.setSelectionRange(nextCaret,nextCaret)})}
  const createPoll=async()=>{if(!active||pollBusy)return;const question=pollQuestion.trim(),options=pollOptions.map(x=>x.trim()).filter(Boolean);if(!question||options.length<2)return;setPollError('');setPollBusy(true);try{const duration=Number(pollDuration)||0;const p=await request(`/api/member/chats/${active.id}/polls`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({question,options,duration_seconds:duration})});if(p.poll)setPolls(current=>({...current,[Number(p.poll.id)]:p.poll as ChatPoll}));setPollModal(false);setPollQuestion('');setPollOptions(['','']);setPollDuration('0');window.setTimeout(()=>onOpen(active),120)}catch(e){setPollError(e instanceof Error?e.message:'Poll could not be created.')}finally{setPollBusy(false)}}
  const votePoll=async(poll:ChatPoll,optionID:number)=>{if(!active||poll.closed)return;try{const p=await request(`/api/member/chats/${active.id}/polls/${poll.id}/vote`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({option_id:optionID})});if(p.poll)setPolls(current=>({...current,[poll.id]:p.poll as ChatPoll}))}catch{}}
  const pollCard=(m:ChatMessage)=>{if(m.unsent||!m.poll_id)return null;const poll=polls[m.poll_id];if(!poll)return <div className="chat-poll-card loading"><strong>Loading poll…</strong></div>;const closeMs=poll.closes_at?Date.parse(poll.closes_at)-now:0,closed=poll.closed||(poll.closes_at?closeMs<=0:false);return <div className="chat-poll-card"><header><span>Poll</span><strong>{poll.question}</strong><small>{closed?'Poll closed':poll.closes_at?`Closes in ${Math.max(1,Math.ceil(closeMs/60000))} min`:'No time limit'} · {poll.total_votes} vote{poll.total_votes===1?'':'s'}</small></header><div className="poll-options">{poll.options.map(option=>{const pct=poll.total_votes?Math.round(option.votes/poll.total_votes*100):0,selected=poll.my_option_id===option.id;return <button key={option.id} className={selected?'selected':''} disabled={closed} onClick={()=>void votePoll(poll,option.id)}><span className="poll-fill" style={{width:`${pct}%`}}/><span className="poll-label">{option.text}</span><b>{option.votes?`${pct}%`:''}</b></button>})}</div>{poll.my_option_id>0&&!closed&&<small className="poll-change-note">Choose another option to change your vote.</small>}</div>}
  const commandQuery=!editing&&draft.startsWith('/')&&!/\s/.test(draft)?draft.toLowerCase():''
  return <main className={`member-chat-page messenger-layout ${active?'has-active':''}`}>
    <aside className="messenger-sidebar"><header className="messenger-toolbar"><button className="chat-space-close" onClick={onBack}><Icon name="back"/></button><div><span>MEMBER CHAT</span><h1>Chats</h1></div><button onClick={onNewPrivate}><Icon name="edit"/></button></header><div className="chat-tabs"><button className={filter==='all'?'active':''} onClick={()=>onFilter('all')}>All</button><button className={filter==='private'?'active':''} onClick={()=>onFilter('private')}>Private</button><button className={filter==='groups'?'active':''} onClick={()=>onFilter('groups')}>Groups</button><button className="add-group" onClick={onAddGroup}><Icon name="plus"/>Add Group</button></div><label className="chat-search"><Icon name="search"/><input value={search} onChange={e=>onSearch(e.target.value)} placeholder="Search chats"/></label><div className="conversation-list">{shown.map(c=><button key={c.id} className={active?.id===c.id?'active':''} onClick={()=>onOpen(c)}><span className="conversation-avatar"><MemberAvatar src={c.avatar} label={c.name}/></span><span className="conversation-copy"><strong>{c.name}</strong><small>{c.last||'No messages yet'}</small><em className={c.active_call?'call-active':''}>{c.active_call?`${c.active_call.mode==='video'?'Video':'Audio'} call active`:c.scope==='private'?'Private':c.scope==='division'?'Division':c.scope==='organization'?'Organization':'Group'}</em></span><span className="conversation-meta"><small title={c.time}>{relativeActivity(c.time_iso,c.time,now)}</small>{c.unread_count>0&&<b>{c.unread_count}</b>}</span></button>)}{!shown.length&&<div className="chat-empty-list">No chats found.</div>}</div></aside>
    <section className="messenger-room">{!active?<div className="room-empty"><Icon name="chat"/><h2>Select a chat</h2><p>Choose a conversation or start a private chat.</p></div>:<>
      <header className="room-header"><button className="room-mobile-back" onClick={onCloseConversation}><Icon name="back"/></button><span className="room-avatar"><MemberAvatar src={active.avatar} label={active.name}/></span><div className="room-title"><strong>{active.name}</strong><small><span className={`presence-dot ${connection}`}/>{connection==='online'?'Live conversation':connection==='connecting'?'Connecting…':'Offline · messages will queue'}</small></div><div className="room-call-actions"><button onClick={()=>void showPins()} title="Pinned messages"><Icon name="pin"/><span>Pins</span></button>{groupCall?<button className="join-call" onClick={onJoinGroupCall}><Icon name={groupCall.mode==='video'?'video':'phone'}/><span>Join</span></button>:<><button onClick={onAudioCall}><Icon name="phone"/><span>Audio</span></button><button onClick={onVideoCall}><Icon name="video"/><span>Video</span></button></>}</div></header>
      {groupCall&&<div className="group-call-banner"><span className="call-live-dot"/><div><strong>{groupCall.from_name} started a {groupCall.mode} room</strong><small>{Math.max(1,groupCall.participant_count||1)} in the room</small></div><button onClick={onJoinGroupCall}>Join room</button></div>}
      {topPin&&<div className="pinned-message-strip"><Icon name="pin"/><div><button onClick={()=>void jumpToMessage(topPin.id)} title={topPin.body}><strong>{topPin.sender}:</strong> {(stripChatCommands(topPin.body).slice(0,64)+(stripChatCommands(topPin.body).length>64?'…':''))||'Attachment'}</button></div></div>}
      <div ref={streamRef} className={`message-stream ${dropActive?'drop-active':''} ${initialPositioned?'positioned':'positioning'}`} onScroll={e=>{const node=e.currentTarget,distance=node.scrollHeight-node.scrollTop-node.clientHeight,nearBottom=distance<100;scrollStateRef.current={...scrollStateRef.current,height:node.scrollHeight,top:node.scrollTop,nearBottom};if(nearBottom){setShowJumpLatest(false);setPendingLatest(0)}}} onDragEnter={e=>{e.preventDefault();setDropActive(true)}} onDragOver={e=>{e.preventDefault();setDropActive(true)}} onDragLeave={e=>{if(e.currentTarget===e.target)setDropActive(false)}} onDrop={e=>{e.preventDefault();setDropActive(false);onDropFiles(Array.from(e.dataTransfer.files))}} onPointerDown={()=>{if(actionMode==='desktop'){setActionMessage(null);setActionAnchor(null)}}}>
        {dropActive&&<div className="chat-drop-overlay"><Icon name="paperclip"/><strong>Drop files to attach</strong></div>}
        {hasMore&&<button className="load-earlier" disabled={loading} onClick={onLoadMore}>{loading?'Loading…':'Load earlier messages'}</button>}{loading&&!messages.length&&<div className="chat-loading"><Skeleton rows={3}/></div>}
        {visibleMessages.map(m=>{const quoted=m.reply_to?replyPreview(m.reply_to):null,expiry=m.expires_at?Math.max(0,Math.ceil((Date.parse(m.expires_at)-now)/1000)):0,body=stripChatCommands(m.body),entries=Object.entries(m.reaction_summary||{}).sort((a,b)=>b[1]-a[1]),shownReacts=entries.slice(0,3),reactionTotal=entries.reduce((n,[,count])=>n+count,0),menuOpen=actionMode==='desktop'&&actionMessage?.id===m.id;return <article id={`chat-message-${m.id}`} key={m.client_nonce||m.id} className={`message-row ${m.mine?'mine':'theirs'} ${m.pinned?'is-pinned':''} ${m.queued?'queued':''} ${m.unsent?'is-unsent':''} ${m.poll_id?'has-poll':''}`} onPointerDown={e=>beginGesture(m,e)} onPointerMove={e=>moveGesture(m,e)} onPointerUp={e=>endGesture(m,e)} onPointerCancel={clearGesture} onContextMenu={e=>{if(matchMedia('(hover: none), (pointer: coarse)').matches){e.preventDefault();setActionMode('mobile');setActionMessage(m)}}}>{!m.mine&&<button className="message-avatar profile-trigger" onMouseEnter={e=>onProfile(m.sender_account_id,e)} onFocus={e=>onProfile(m.sender_account_id,e)}><MemberAvatar src={m.avatar} label={m.sender}/></button>}<div className="message-stack">{!m.mine&&<button className="message-sender profile-trigger" onMouseEnter={e=>onProfile(m.sender_account_id,e)} onFocus={e=>onProfile(m.sender_account_id,e)}>{m.sender}</button>}<div className="message-content-line"><div className="message-bubble" onClick={e=>{if((e.target as HTMLElement).closest('button,a,input,video,audio'))return;setExpandedMetaID(current=>current===m.id?null:m.id)}}>{quoted&&<button className="quoted-message" onClick={()=>void jumpToMessage(quoted.id)}><strong>{quoted.sender}</strong><span>{stripChatCommands(quoted.body).slice(0,100)||'Attachment'}</span></button>}{body&&!m.poll_id&&<Markdown text={chatDisplayText(body,participants)} onMention={onProfile}/>} {pollCard(m)}<AttachmentPreview attachments={m.attachments||[]}/>{(m.view_limit||0)>0&&<small className="message-rule"><Icon name="eye"/>First {m.view_limit} viewers · {m.view_count||0} viewed</small>}{!m.unsent&&expiry>0&&<small className="message-rule expiring"><Icon name="clock"/>Disappears in {expiry}s</small>}{m.pinned&&<small className="message-rule"><Icon name="pin"/>Pinned</small>}</div>{m.id>0&&!m.unsent&&<button className="message-more-button" aria-expanded={!!menuOpen} onPointerDown={e=>e.stopPropagation()} onClick={e=>{e.stopPropagation();if(menuOpen){setActionMessage(null);setActionAnchor(null);return}openDesktopActionMenu(m,e.currentTarget)}}><Icon name="more"/></button>}</div>{!m.unsent&&m.id>0&&shownReacts.length>0&&<div className="message-reactions has-reactions"><div className="reaction-cluster">{shownReacts.map(([r,count])=><button key={r} className={m.my_reaction===r?'mine':''} onClick={()=>void showReactions(m)}>{r} <b>{count}</b></button>)}{entries.length>3&&<button title={`${reactionTotal} total reactions`} onClick={()=>void showReactions(m)}>+{reactionTotal}</button>}</div></div>}{(expandedMetaID===m.id||m.queued||(m.mine&&!m.seen))&&<div className="message-meta compact-meta"><span>{m.time}{m.edited&&<button className="edited-link" onClick={()=>void showHistory(m)}> · Edited</button>}</span>{m.mine&&<span>{m.queued?'Queued':m.seen?'Seen':'Sent'}</span>}</div>}</div></article>})}
        {typing.length>0&&<div className="typing-row"><span/><span/><span/><small>{typing.join(', ')} typing</small></div>}<div ref={endRef}/>
      </div>
      {actionMode==='desktop'&&actionMessage&&actionAnchor&&createPortal(actionMenu(actionMessage),document.body)}
      {showJumpLatest&&<button type="button" className="jump-latest-button" onClick={()=>scrollToLatest('smooth')}><Icon name="arrow"/><span>{pendingLatest>1?`${pendingLatest} new messages`:pendingLatest===1?'1 new message':'Latest message'}</span></button>}
      <footer className="chat-composer">{editing&&<div className="composer-context"><Icon name="edit"/><span><strong>Editing message</strong><small>{editing.body.slice(0,90)}</small></span><button onClick={()=>{setDraft('');onText('');onCancelEdit()}}><Icon name="close"/></button></div>}{!editing&&reply&&<div className="composer-context"><Icon name="comment"/><span><strong>Replying to {reply.sender}</strong><small>{reply.body.slice(0,90)||'Attachment'}</small></span><button onClick={onCancelReply}><Icon name="close"/></button></div>}{!editing&&attachments.length>0&&<div className="pending-attachments">{attachments.map((a,i)=><div key={`${a.url}-${i}`}><PendingAttachment attachment={a}/><button onClick={()=>onRemoveAttachment(i)}><Icon name="close"/></button></div>)}</div>}<div className="composer-row"><button disabled={!!editing} onClick={onAttach}><Icon name="paperclip"/></button><button disabled={!!editing} className={recording?'recording':''} onClick={onAudio}><Icon name="mic"/></button><div className="composer-editor"><textarea ref={composerRef} value={draft} onChange={e=>{const value=e.currentTarget.value;setComposerCaret(e.currentTarget.selectionStart??value.length);setDraft(value);onText(value)}} onSelect={e=>setComposerCaret(e.currentTarget.selectionStart??e.currentTarget.value.length)} onKeyDown={handleComposerKeyDown} rows={1} placeholder={editing?'Edit message…':'Message…'}/>{commandQuery&&<div className="chat-suggestion-menu command-menu">{commands.filter(c=>c.cmd.toLowerCase().startsWith(commandQuery)).map(c=><button key={c.cmd} type="button" onMouseDown={e=>e.preventDefault()} onClick={()=>useCommand(c.cmd)}><strong>{c.cmd.trim()}</strong><small>{c.purpose}</small></button>)}</div>}{mentionMatch&&<div className="chat-suggestion-menu mention-menu"><small className="mention-directory-label">Conversation members · online or offline</small>{participants.filter(p=>{const q=(mentionMatch[1]||'').toLowerCase();return !q||p.name.toLowerCase().includes(q)||(p.username||'').toLowerCase().includes(q)}).slice(0,20).map(p=><button key={p.account_id} type="button" onMouseDown={e=>e.preventDefault()} onClick={()=>insertMention(p)}><MemberAvatar src={p.avatar} label={p.name}/><span><strong>{p.name}</strong><small>@{p.username||'member'}</small></span></button>)}</div>}</div><button className="send-message" disabled={uploading||(!draft.trim()&&!attachments.length)} onClick={handleComposerSend}><Icon name="send"/></button></div></footer>
      {searchModal&&<div className="chat-mini-modal-layer" onClick={e=>{if(e.target===e.currentTarget)setSearchModal(false)}}><section className="chat-mini-modal chat-search-modal"><button onClick={()=>setSearchModal(false)}><Icon name="close"/></button><span>SEARCH THIS CHAT</span><h3>Find messages</h3><label className="chat-message-search"><Icon name="search"/><input autoFocus value={messageSearch} onChange={e=>setMessageSearch(e.target.value)} placeholder="Search words, names, or attachment titles"/></label><div className="chat-search-results">{messageSearchBusy?<div className="chat-search-state">Searching…</div>:!messageSearch.trim()?<div className="chat-search-state">Type anything to search the full conversation.</div>:messageSearchResults.length?messageSearchResults.map(result=><button key={result.id} type="button" onClick={()=>{setSearchModal(false);void jumpToMessage(result.id)}}><span className="chat-search-result-head"><strong>{result.mine?'You':result.sender}</strong><small>{result.time}</small></span><p>{stripChatCommands(result.body).slice(0,180)|| (result.attachment_match?'Matching attachment':'Message')}</p>{result.attachment_match&&<small className="chat-search-attachment"><Icon name="paperclip" size={12}/>Attachment match</small>}</button>):<div className="chat-search-state">No matches in this conversation.</div>}</div></section></div>}
      {actionMode==='mobile'&&actionMessage&&<div className="message-action-layer" onPointerDown={e=>{if(e.target===e.currentTarget){setActionMessage(null);setActionAnchor(null)}}}>{actionMenu(actionMessage,true)}</div>}
      {reactionPeople&&<div className="chat-mini-modal-layer" onClick={e=>{if(e.target===e.currentTarget)setReactionPeople(null)}}><section className="chat-mini-modal"><button onClick={()=>setReactionPeople(null)}><Icon name="close"/></button><h3>Reactions</h3><div>{reactionPeople.map((p,i)=><article key={`${p.account_id}-${i}`}><MemberAvatar src={p.avatar} label={p.name}/><span><strong>{p.name}</strong><small>{p.reaction}</small></span></article>)}</div></section></div>}
      {history&&<div className="chat-mini-modal-layer" onClick={e=>{if(e.target===e.currentTarget)setHistory(null)}}><section className="chat-mini-modal"><button onClick={()=>setHistory(null)}><Icon name="close"/></button><h3>Edit history <small>last 30 days</small></h3><div>{history.length?history.map(h=><article className="history-entry" key={h.id}><span><Markdown text={h.body}/><small>{h.time} · {h.editor}</small></span></article>):<p>No retained history.</p>}</div></section></div>}
      {pinsModal&&<div className="chat-mini-modal-layer" onClick={e=>{if(e.target===e.currentTarget)setPinsModal(null)}}><section className="chat-mini-modal pins-modal"><button onClick={()=>setPinsModal(null)}><Icon name="close"/></button><h3>Pinned messages</h3><div>{pinsModal.length?pinsModal.map(pin=><article key={pin.id}><button className="pin-jump" onClick={()=>{setPinsModal(null);void jumpToMessage(pin.id)}}><Icon name="pin"/><span><strong>{pin.sender}</strong><small>{stripChatCommands(pin.body).slice(0,120)||'Attachment'} · {pin.pinned_at}</small></span></button>{pin.pinned_by===currentAccountID&&<button className="pin-remove" onClick={()=>{const msg=messages.find(m=>m.id===pin.id)||({id:pin.id,sender_account_id:0,sender:pin.sender,body:pin.body,attachments:[],reply_to:0,time:pin.pinned_at,reactions:0,pinned:true,pinned_by:pin.pinned_by} as ChatMessage);onPin(msg);setPinsModal(null)}}>Unpin</button>}</article>):<p>No pinned messages.</p>}</div></section></div>}
      {pickerMessage&&<div className="chat-mini-modal-layer" onClick={e=>{if(e.target===e.currentTarget)setPickerMessage(null)}}><section className="chat-mini-modal emoji-picker-modal"><button onClick={()=>setPickerMessage(null)}><Icon name="close"/></button><h3>Choose reaction</h3><div className="emoji-grid">{['👍','❤️','😂','😮','😢','😡','👏','🔥','🎉','🤔','👀','✅','💯','🙏','👎','🥳','🤝','💡','🚀','⭐'].map(r=><button key={r} className={pickerMessage.my_reaction===r?'active':''} onClick={()=>chooseReaction(pickerMessage,r)}>{r}</button>)}</div><div className="emoji-native-row"><input autoFocus value={customReaction} onChange={e=>setCustomReaction(e.target.value)} onKeyDown={e=>{if(e.key==='Enter'&&customReaction.trim())chooseReaction(pickerMessage,customReaction)}} maxLength={16} placeholder="Type or paste an emoji"/><button disabled={!customReaction.trim()} onClick={()=>chooseReaction(pickerMessage,customReaction)}>React</button></div></section></div>}
      {commandBuilder&&<div className="chat-mini-modal-layer" onClick={e=>{if(e.target===e.currentTarget)setCommandBuilder(null)}}><section className="chat-mini-modal command-builder-modal"><button onClick={()=>setCommandBuilder(null)}><Icon name="close"/></button><h3>{commandBuilder==='warning'?'Warning':commandBuilder==='note'?'Note':commandBuilder==='quote'?'Quote':commandBuilder==='checklist'?'Checklist':commandBuilder==='limittime'?'Expiring message':commandBuilder==='limitview'?'Viewer-limited message':commandBuilder==='code'?'Code block':commandBuilder==='meeting'?'Meeting notes':'Table'}</h3>{commandBuilder==='checklist'?<div className="checklist-builder"><p>Add the tasks you want to send.</p>{builderItems.map((item,index)=><div key={index}><label className="check-toggle"><input type="checkbox" checked={item.checked} onChange={e=>setBuilderItems(items=>items.map((x,i)=>i===index?{...x,checked:e.target.checked}:x))}/><span>{item.checked?'Checked':'Open'}</span></label><input autoFocus={index===0} value={item.text} onChange={e=>setBuilderItems(items=>items.map((x,i)=>i===index?{...x,text:e.target.value}:x))} placeholder={`Task ${index+1}`}/>{builderItems.length>1&&<button type="button" onClick={()=>setBuilderItems(items=>items.filter((_,i)=>i!==index))}><Icon name="close"/></button>}</div>)}{builderItems.length<12&&<button type="button" className="command-add-row" onClick={()=>setBuilderItems(items=>[...items,{text:'',checked:false}])}><Icon name="plus"/>Add another list</button>}</div>:<div className="command-builder-fields">{commandBuilder==='code'&&<label>Language<input value={builderAux} onChange={e=>setBuilderAux(e.target.value)} placeholder="text, javascript, python…"/></label>}{(commandBuilder==='limittime'||commandBuilder==='limitview')&&<label>{commandBuilder==='limittime'?'Time in seconds':'Maximum viewers'}<input type="number" min={commandBuilder==='limittime'?5:1} max={commandBuilder==='limittime'?86400:100} value={builderNumber} onChange={e=>setBuilderNumber(e.target.value)}/><small>{commandBuilder==='limittime'?'5 seconds to 24 hours':'1 to 100 viewers'}</small></label>}<label>{commandBuilder==='code'?'Code':commandBuilder==='table'?'Rows':commandBuilder==='meeting'?'Notes':'Message'}<textarea autoFocus={commandBuilder!=='code'} value={builderMessage} onChange={e=>setBuilderMessage(e.target.value)} rows={commandBuilder==='table'||commandBuilder==='code'||commandBuilder==='meeting'?6:4} placeholder={commandBuilder==='warning'?'Type the warning message…':commandBuilder==='note'?'Type the note…':commandBuilder==='quote'?'Type the text to quote…':commandBuilder==='limittime'?'Message that will expire…':commandBuilder==='limitview'?'Message for the first viewers…':commandBuilder==='code'?'Paste code here…':commandBuilder==='meeting'?'Write topic, decisions, owners, and follow-ups…':'Header 1 | Header 2\nValue 1 | Value 2'}/>{commandBuilder==='table'&&<small>One row per line. Separate cells with |.</small>}</label></div>}<footer><button className="button-secondary" onClick={()=>setCommandBuilder(null)}>Cancel</button><button className="primary-button" disabled={!builderReady} onClick={applyCommandBuilder}>Put in message</button></footer></section></div>}
      {helpModal&&<div className="chat-mini-modal-layer" onClick={e=>{if(e.target===e.currentTarget)setHelpModal(null)}}><section className="chat-mini-modal chat-reference-modal"><button onClick={()=>setHelpModal(null)}><Icon name="close"/></button><h3>{helpModal==='usecommand'?'Use a command':helpModal==='commands'?'Chat commands':helpModal==='markdown'?'Markdown syntax':'Markup syntax'}</h3>{helpModal==='commands'||helpModal==='usecommand'?<div className={`command-reference-list ${helpModal==='usecommand'?'command-picker-list':''}`}>{commands.filter(c=>helpModal==='commands'||!['/usecommand','/commands'].includes(c.cmd)).map(c=><article key={c.cmd}><span><strong>{c.cmd.trim()}</strong><small>{c.purpose}</small>{helpModal==='commands'&&<code>{c.sample}</code>}</span>{helpModal==='usecommand'&&<button type="button" onClick={()=>{setHelpModal(null);useCommand(c.cmd)}}>Use</button>}</article>)}</div>:<pre>{helpModal==='markdown'?markdownGuide:markupGuide}</pre>}</section></div>}
      {pollModal&&<div className="chat-mini-modal-layer" onClick={e=>{if(e.target===e.currentTarget&&!pollBusy)setPollModal(false)}}><section className="chat-mini-modal poll-builder-modal"><button disabled={pollBusy} onClick={()=>setPollModal(false)}><Icon name="close"/></button><h3>Create poll</h3><label>Question<input autoFocus value={pollQuestion} onChange={e=>setPollQuestion(e.target.value)} maxLength={500} placeholder="Ask a question"/></label><div className="poll-builder-options">{pollOptions.map((option,index)=><label key={index}>Option {index+1}<span><input value={option} onChange={e=>setPollOptions(current=>current.map((value,i)=>i===index?e.target.value:value))} maxLength={240} placeholder={`Choice ${index+1}`}/>{pollOptions.length>2&&<button type="button" onClick={()=>setPollOptions(current=>current.filter((_,i)=>i!==index))}><Icon name="close"/></button>}</span></label>)}</div>{pollOptions.length<8&&<button className="poll-add-option" type="button" onClick={()=>setPollOptions(current=>[...current,''])}><Icon name="plus"/>Add option</button>}<label>Timer<select value={pollDuration} onChange={e=>setPollDuration(e.target.value)}><option value="0">No time limit</option><option value="300">5 minutes</option><option value="900">15 minutes</option><option value="3600">1 hour</option><option value="86400">24 hours</option><option value="259200">3 days</option><option value="604800">7 days</option></select></label>{pollError&&<p className="poll-builder-error">{pollError}</p>}<footer><button className="button-secondary" disabled={pollBusy} onClick={()=>setPollModal(false)}>Cancel</button><button className="primary-button" disabled={pollBusy||!pollQuestion.trim()||pollOptions.filter(x=>x.trim()).length<2} onClick={()=>void createPoll()}>{pollBusy?'Creating…':'Create poll'}</button></footer></section></div>}
    </>}</section>
  </main>
}
function PendingAttachment({attachment}:{attachment:Attachment}){return <div className="pending-attachment-card"><Icon name={attachment.type?.startsWith('image')||attachment.type==='image'?'image':attachment.type?.startsWith('audio')||attachment.type==='audio'?'audio':attachment.type?.startsWith('video')||attachment.type==='video'?'video':'file'}/><span><strong>{attachment.name||'Attachment'}</strong><small>{attachment.size?`${Math.max(1,Math.ceil(attachment.size/1024))} KB`:'Ready to send'}</small></span></div>}
function AttachmentPreview({attachments}:{attachments:Attachment[]}){if(!attachments?.length)return null;return <div className="attachment-preview">{attachments.map((a,i)=>{const url=a.url||a.src||'';const type=a.type||'';if(type==='image'||type.startsWith('image/'))return <a key={i} href={url} target="_blank" rel="noreferrer" className="attachment-image"><img src={url} alt={a.name}/></a>;if(type==='audio'||type.startsWith('audio/'))return <div className="attachment-media" key={i}><audio controls src={url}/><small>{a.name}</small></div>;if(type==='video'||type.startsWith('video/'))return <div className="attachment-media" key={i}><video controls playsInline src={url}/><small>{a.name}</small></div>;return <a key={i} href={url} target="_blank" rel="noreferrer" className="attachment-file"><Icon name="file"/><span>{a.name}<small>{a.size?`${Math.ceil(a.size/1024)} KB`:''}</small></span><Icon name="download"/></a>})}</div>}
function GroupModal({members,name,selected,onName,onToggle,onClose,onSave}:{members:DirectoryMember[];name:string;selected:number[];onName:(v:string)=>void;onToggle:(id:number)=>void;onClose:()=>void;onSave:()=>void}){return <div className="member-modal-layer" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><section className="member-modal group-editor"><button className="modal-x" onClick={onClose}><Icon name="close"/></button><span>PRIVATE GROUP</span><h2>Create a group chat</h2><label>Group name<input value={name} onChange={e=>onName(e.target.value)} placeholder="e.g. Project Team"/></label><div className="group-member-checklist">{members.map(m=><label key={m.account_id}><input type="checkbox" checked={selected.includes(m.account_id)} onChange={()=>onToggle(m.account_id)}/><span className="directory-avatar"><MemberAvatar src={m.avatar} label={m.name}/></span><span><strong>{m.name}</strong><small>@{m.username} · {m.division}</small></span></label>)}</div><footer><button className="button-secondary" onClick={onClose}>Cancel</button><button className="primary-button" disabled={!name.trim()||!selected.length} onClick={onSave}>Create group</button></footer></section></div>}


function PostModal({detail,comments,text,reply,mentionResults,onClose,onReactPost,onText,onSubmit,onReply,onCancelReply,onReactComment,onShare,onMention,onProfile,onEdit,onDelete,onHistory}:{detail:PostDetail;comments:Array<CommentItem&{replies:CommentItem[]}>;text:string;reply:CommentItem|null;mentionResults:MentionResult[];onClose:()=>void;onReactPost:(reaction:string)=>void;onText:(v:string)=>void;onSubmit:()=>void;onReply:(c:CommentItem)=>void;onCancelReply:()=>void;onReactComment:(id:number,reaction:string)=>void;onShare:()=>void;onMention:(m:MentionResult)=>void;onProfile:(id:number,event:ReactMouseEvent<HTMLElement>|ReactFocusEvent<HTMLElement>)=>void;onEdit:(c:CommentItem)=>void;onDelete:(c:CommentItem)=>void;onHistory:(c:CommentItem)=>void}){
  const identitySrc=detail.identity==='organization'?'/logo/aits.png':detail.identity==='division'?(detail.division_logo||'/logo/aits.png'):detail.avatar
  const [postReactionsOpen,setPostReactionsOpen]=useState(false)
  const commentKeyDown=(event:KeyboardEvent<HTMLTextAreaElement>)=>{if(event.key==='Enter'&&!event.shiftKey&&!event.nativeEvent.isComposing){event.preventDefault();if(text.trim())onSubmit()}}
  return <div className="member-post-modal-layer" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><article className="member-post-modal"><button className="post-modal-close" onClick={onClose}><Icon name="close"/></button><header><span className="feed-avatar"><MemberAvatar src={identitySrc} label={detail.author}/></span><div><strong>{detail.author}</strong><small>{detail.role} · {detail.date}{detail.edited&&<span className="edited-post-badge">Edited</span>}</small></div><span className={`visibility-pill ${detail.visibility}`}>{detail.visibility==='public'?'Public':detail.visibility==='division'?detail.division||'Division':'Organization'}</span></header>{detail.title&&<h2>{detail.title}</h2>}<Markdown text={detail.body} onMention={onProfile}/><PostModalMedia media={detail.media||[]}/><div className="post-counts"><span>{detail.reactions} reactions</span><span>{detail.comments} comments</span></div><div className="detail-actions"><div className="social-react-wrap"><button className={detail.reacted_by_me?'active':''} onClick={()=>setPostReactionsOpen(v=>!v)}><Icon name="heart"/>{detail.my_reaction||'React'}</button>{postReactionsOpen&&<SocialReactionPicker selected={detail.my_reaction} onPick={reaction=>{onReactPost(reaction);setPostReactionsOpen(false)}}/>}</div><button onClick={()=>document.getElementById('member-comment-input')?.focus()}><Icon name="comment"/>Comment</button><button onClick={onShare}><Icon name="share"/>Share</button></div>{detail.reaction_summary&&Object.keys(detail.reaction_summary).length>0&&<ReactionSummary summary={detail.reaction_summary}/>}<section className="detail-comments"><h3>Conversation</h3>{comments.map(c=><div className="comment-thread" key={c.id}><CommentRow item={c} onReply={()=>onReply(c)} onReact={reaction=>onReactComment(c.id,reaction)} onProfile={onProfile} onEdit={()=>onEdit(c)} onDelete={()=>onDelete(c)} onHistory={()=>onHistory(c)}/>{c.replies.map(r=><CommentRow key={r.id} item={r} reply onReact={reaction=>onReactComment(r.id,reaction)} onProfile={onProfile} onEdit={()=>onEdit(r)} onDelete={()=>onDelete(r)} onHistory={()=>onHistory(r)}/>)}</div>)}{comments.length===0&&<p className="empty-comments">No comments yet. Start the conversation.</p>}</section><footer className="comment-compose">{reply&&<div className="reply-chip">Replying to {reply.name}<button onClick={onCancelReply}><Icon name="close"/></button></div>}<div className="comment-input-wrap"><textarea id="member-comment-input" value={text} onChange={e=>onText(e.target.value)} onKeyDown={commentKeyDown} placeholder={reply?`Reply to ${reply.name}…`:'Write a comment… Use @ to tag someone.'} rows={2}/>{mentionResults.length>0&&<div className="mention-results">{mentionResults.map(result=><button type="button" key={result.account_id} onClick={()=>onMention(result)}><span className="directory-avatar"><MemberAvatar src={result.avatar} label={result.name}/></span><span><strong>{result.name}</strong><small>@{result.username} · {result.account_kind}{result.division?` · ${result.division}`:''}</small></span></button>)}</div>}<button className="comment-send" disabled={!text.trim()} onClick={onSubmit}><Icon name="send"/></button></div><small className="comment-key-hint">Enter to send · Shift + Enter for a new line</small></footer></article></div>
}
function SocialReactionPicker({selected,onPick}:{selected?:string;onPick:(reaction:string)=>void}){return <div className="social-reaction-picker" role="menu" aria-label="Choose reaction">{SOCIAL_REACTIONS.map(reaction=><button type="button" role="menuitem" aria-label={`React ${reaction}`} className={selected===reaction?'active':''} key={reaction} onClick={()=>onPick(reaction)}>{reaction}</button>)}</div>}
function ReactionSummary({summary}:{summary:Record<string,number>}){const entries=Object.entries(summary).filter(([,count])=>count>0).sort((a,b)=>b[1]-a[1]);if(!entries.length)return null;return <div className="social-reaction-summary">{entries.map(([reaction,count])=><span key={reaction}>{reaction}<b>{count}</b></span>)}</div>}
function CommentRow({item,reply=false,onReply,onReact,onProfile,onEdit,onDelete,onHistory}:{item:CommentItem;reply?:boolean;onReply?:()=>void;onReact:(reaction:string)=>void;onProfile:(id:number,event:ReactMouseEvent<HTMLElement>|ReactFocusEvent<HTMLElement>)=>void;onEdit:()=>void;onDelete:()=>void;onHistory:()=>void}){const [reactionOpen,setReactionOpen]=useState(false);return <div id={`comment-${item.id}`} className={`comment-row ${reply?'reply':''}`}><button type="button" className="comment-avatar profile-trigger" onMouseEnter={e=>onProfile(item.account_id,e)} onFocus={e=>onProfile(item.account_id,e)}><MemberAvatar src={item.avatar} label={item.name}/></button><div><button type="button" className="comment-author profile-trigger" onMouseEnter={e=>onProfile(item.account_id,e)} onFocus={e=>onProfile(item.account_id,e)}>{item.name}</button><Markdown text={item.body} onMention={onProfile}/><small>{item.time}{item.edited?' · Edited':''}</small>{item.reaction_summary&&Object.keys(item.reaction_summary).length>0&&<ReactionSummary summary={item.reaction_summary}/>}<footer><span className="comment-react-wrap"><button className={item.reacted_by_me?'active':''} onClick={()=>setReactionOpen(v=>!v)}>{item.my_reaction||'React'}{item.reactions>0?` · ${item.reactions}`:''}</button>{reactionOpen&&<SocialReactionPicker selected={item.my_reaction} onPick={reaction=>{onReact(reaction);setReactionOpen(false)}}/>}</span>{onReply&&<button onClick={onReply}>Reply</button>}{item.can_edit&&<button onClick={onEdit}>Edit</button>}{item.can_edit&&<button onClick={onDelete}>Delete</button>}{(item.history_count||0)>0&&<button onClick={onHistory}>History · {item.history_count}</button>}</footer></div></div>}

function PostModalMedia({media}:{media:Attachment[]}){const [lightbox,setLightbox]=useState<number|null>(null);const visual=media.filter(item=>{const type=item.type||'';const src=item.url||item.src||'';return type.startsWith('image')||type==='image'||type.startsWith('video')||type==='video'||/\.(png|jpe?g|webp|gif|mp4|webm|mov)(\?|$)/i.test(src)});if(!media.length)return null;return <><div className={`post-modal-media-grid count-${Math.min(visual.length,4)}`}>{visual.slice(0,4).map((item,index)=>{const src=item.url||item.src||'';if(!src)return null;const video=(item.type||'').startsWith('video')||/\.(mp4|webm|mov)(\?|$)/i.test(src);return <button type="button" key={`${src}-${index}`} onClick={()=>{window.dispatchEvent(new Event('aits-close-floating-profile'));setLightbox(index)}}>{video?<video src={src} muted playsInline preload="metadata"/>:<img src={src} alt={item.name||''}/>} {index===3&&visual.length>4&&<span>+{visual.length-4} more</span>}</button>})}</div>{media.filter(item=>!visual.includes(item)).length>0&&<AttachmentPreview attachments={media.filter(item=>!visual.includes(item))}/>} {lightbox!==null&&visual[lightbox]&&<div className="post-lightbox" onMouseDown={e=>{if(e.target===e.currentTarget)setLightbox(null)}}><button className="post-lightbox-close" onClick={()=>setLightbox(null)}><Icon name="close"/></button>{((visual[lightbox].type||'').startsWith('video')||/\.(mp4|webm|mov)(\?|$)/i.test(visual[lightbox].url||visual[lightbox].src||''))?<video controls autoPlay playsInline src={visual[lightbox].url||visual[lightbox].src}/>:<img src={visual[lightbox].url||visual[lightbox].src} alt={visual[lightbox].name||''}/>} {visual.length>1&&<><button className="lightbox-prev" onClick={()=>setLightbox((lightbox-1+visual.length)%visual.length)}><Icon name="back"/></button><button className="lightbox-next" onClick={()=>setLightbox((lightbox+1)%visual.length)}><Icon name="arrow"/></button></>}</div>}</>}

function CommentEditDialog({item,onClose,onSave}:{item:CommentItem;onClose:()=>void;onSave:(body:string)=>void}){const [body,setBody]=useState(item.body);return <div className="member-modal-layer" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><section className="member-modal comment-edit-dialog"><button className="modal-x" onClick={onClose}><Icon name="close"/></button><span>EDIT COMMENT</span><h2>Update your comment</h2><textarea value={body} onChange={e=>setBody(e.target.value)} rows={5}/><footer><button className="button-secondary" onClick={onClose}>Cancel</button><button className="primary-button" disabled={!body.trim()} onClick={()=>onSave(body)}>Save changes</button></footer></section></div>}
function CommentHistoryModal({history,onClose}:{history:{id:number;body:string;time:string;editor:string}[];onClose:()=>void}){return <div className="member-modal-layer" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><section className="member-modal comment-history-dialog"><button className="modal-x" onClick={onClose}><Icon name="close"/></button><span>EDIT HISTORY</span><h2>Previous comment versions</h2><div className="comment-history-list">{history.map(item=><article key={item.id}><Markdown text={item.body}/><small>{item.time} · {item.editor}</small></article>)}</div></section></div>}

function FloatingProfileCard({profile,anchor,onClose,onChat}:{profile:ProfileCardData;anchor:{x:number;y:number};onClose:()=>void;onChat?:()=>void}){return <aside className="floating-profile-card" style={{left:anchor.x,top:anchor.y}} onMouseLeave={onClose}><button className="profile-card-close" onClick={onClose}><Icon name="close"/></button><div className="profile-card-head"><span className="directory-avatar"><MemberAvatar src={profile.avatar} label={profile.name}/></span><div><strong>{profile.name}</strong><small>{profile.username?`@${profile.username}`:profile.account_kind}</small></div></div><div className="profile-card-facts">{profile.email&&<span><small>Email</small><b>{profile.email}</b></span>}<span><small>Type</small><b>{profile.account_kind==='member'?'Member':'Visitor'}</b></span>{profile.division&&<span><small>Division</small><b>{profile.division}</b></span>}{profile.access&&<span><small>Access</small><b>{profile.access}</b></span>}{profile.membership&&<span><small>Membership</small><b>{profile.membership}</b></span>}</div>{onChat&&<button className="profile-card-chat" onClick={onChat}><Icon name="chat"/>Chat</button>}</aside>}
function IncomingCallModal({call,onAccept,onDecline}:{call:IncomingCall;onAccept:()=>void;onDecline:()=>void}){return <div className="incoming-call-layer"><section className="incoming-call-card"><span className="incoming-call-avatar"><MemberAvatar src={call.from_avatar} label={call.from_name}/></span><small>Incoming {call.mode} call</small><h2>{call.from_name}</h2><p>AITS member chat</p><div><button className="decline" onClick={onDecline}><Icon name="close"/>Decline</button><button className="accept" onClick={onAccept}><Icon name={call.mode==='video'?'video':'phone'}/>Accept</button></div></section></div>}
function StreamVideo({stream,muted=false}:{stream:MediaStream;muted?:boolean}){return <video ref={node=>{if(node&&node.srcObject!==stream)node.srcObject=stream}} autoPlay playsInline muted={muted}/>}
function StreamAudio({stream}:{stream:MediaStream}){const ref=useRef<HTMLAudioElement|null>(null);useEffect(()=>{const node=ref.current;if(!node)return;node.srcObject=stream;node.volume=1;node.muted=false;const play=()=>{node.volume=1;node.muted=false;void node.play().catch(()=>undefined)};play();const retry=()=>play();window.addEventListener('pointerdown',retry,{passive:true});window.addEventListener('keydown',retry);stream.addEventListener('addtrack',play);return()=>{window.removeEventListener('pointerdown',retry);window.removeEventListener('keydown',retry);stream.removeEventListener('addtrack',play);if(node.srcObject===stream)node.srcObject=null}},[stream]);return <audio ref={ref} onLoadedMetadata={event=>{event.currentTarget.volume=1;event.currentTarget.muted=false;void event.currentTarget.play().catch(()=>undefined)}} autoPlay/>}
function CallOverlay({call,self,participants,localStream,remoteStreams,remoteStates,muted,cameraOff,onMute,onCamera,onEnd}:{call:ActiveCall;self:MemberProfile;participants:ChatParticipant[];localStream:MediaStream|null;remoteStreams:Record<number,MediaStream>;remoteStates:Record<number,RemoteCallState>;muted:boolean;cameraOff:boolean;onMute:()=>void;onCamera:()=>void;onEnd:()=>void}){
  const remotes=Object.entries(remoteStreams).map(([id,stream])=>({id:Number(id),stream,name:participants.find(p=>p.account_id===Number(id))?.name||'AITS member'}))
  return <div className="member-call-overlay">
    <header><div><small>{call.group?'GROUP ROOM':call.mode==='video'?'VIDEO CALL':'AUDIO CALL'}</small><strong>{call.group?'AITS room':'Private call'} · {remotes.length+1} connected</strong></div></header>
    <div className={`call-stage ${call.mode}`}>
      {localStream&&<article className="call-tile self">
        {call.mode==='video'&&!cameraOff?<StreamVideo stream={localStream} muted/>:<span className="call-avatar">{self.avatar?<MemberAvatar src={self.avatar} label={self.name}/>:initials(self.name)}</span>}
        <b>You</b><small>{muted?'Muted':'Microphone on'}{call.mode==='video'?` · ${cameraOff?'Camera off':'Camera on'}`:''}</small>
      </article>}
      {remotes.map(remote=>{const state=remoteStates[remote.id];return <article className="call-tile" key={remote.id}>
        {call.mode==='video'&&!state?.camera_off?<StreamVideo stream={remote.stream}/>:<><span className="call-avatar">{initials(remote.name)}</span><StreamAudio stream={remote.stream}/></>}
        <b>{remote.name}</b><small>{state?.muted?'Muted':'Microphone on'}{call.mode==='video'?` · ${state?.camera_off?'Camera off':'Camera on'}`:''}</small>
      </article>})}
      {!remotes.length&&<div className="call-waiting"><span/><strong>{call.group?'Waiting for others to join…':'Calling…'}</strong></div>}
    </div>
    <footer>
      <button className={muted?'off':''} onClick={onMute}><Icon name="mic"/><span>{muted?'Unmute':'Mute'}</span></button>
      {call.mode==='video'&&<button className={cameraOff?'off':''} onClick={onCamera}><Icon name="video"/><span>{cameraOff?'Camera on':'Camera off'}</span></button>}
      <button className="hangup" onClick={onEnd}><Icon name="phone"/><span>{call.group&&!call.started_by_me?'Leave':'End'}</span></button>
    </footer>
  </div>
}

