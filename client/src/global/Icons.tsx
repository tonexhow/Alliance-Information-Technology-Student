import type { ReactNode } from 'react'

export type IconName =
  | 'arrow' | 'back' | 'bell' | 'calendar' | 'chat' | 'chevronLeft' | 'chevronRight'
  | 'close' | 'code' | 'comment' | 'group' | 'heart' | 'info' | 'login' | 'logout'
  | 'moon' | 'palette' | 'pin' | 'send' | 'share' | 'spark' | 'sun' | 'users' | 'user'
  | 'clock' | 'terminal' | 'network' | 'shield' | 'globe' | 'menu' | 'bolt' | 'external'
  | 'file' | 'play' | 'music' | 'facebook' | 'instagram' | 'github' | 'mail' | 'phone'
  | 'edit' | 'key' | 'lock' | 'search' | 'plus' | 'trash' | 'refresh' | 'library'
  | 'paperclip' | 'audio' | 'video' | 'image' | 'download' | 'copy' | 'qr' | 'check'
  | 'database' | 'system' | 'archive' | 'more' | 'eye' | 'eyeOff' | 'filter' | 'mic' | 'stop' | 'pause'

export function Icon({ name, size = 20, className }: { name: IconName | string; size?: number; className?: string }) {
  const common = {
    width: size, height: size, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor',
    strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const,
    'aria-hidden': true, className,
  }
  const paths: Record<string, ReactNode> = {
    arrow:<path d="M5 12h14M13 6l6 6-6 6"/>,
    back:<path d="m15 18-6-6 6-6"/>,
    bell:<><path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9"/><path d="M10 21h4"/></>,
    calendar:<><path d="M6 2v4M18 2v4M3 9h18"/><rect x="3" y="4" width="18" height="17" rx="3"/></>,
    chat:<><path d="M21 15a4 4 0 0 1-4 4H8l-5 3V7a4 4 0 0 1 4-4h10a4 4 0 0 1 4 4z"/><path d="M8 10h.01M12 10h.01M16 10h.01"/></>,
    chevronLeft:<path d="m15 18-6-6 6-6"/>, chevronRight:<path d="m9 18 6-6-6-6"/>,
    close:<path d="M18 6 6 18M6 6l12 12"/>,
    code:<><path d="m8 9-4 3 4 3M16 9l4 3-4 3M14 5l-4 14"/></>,
    comment:<path d="M21 15a4 4 0 0 1-4 4H8l-5 3V7a4 4 0 0 1 4-4h10a4 4 0 0 1 4 4z"/>,
    group:<><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75"/></>,
    heart:<path d="M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.6l-1-1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z"/>,
    info:<><circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/></>,
    login:<><path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4M10 17l5-5-5-5M15 12H3"/></>,
    logout:<><path d="M14 3h5a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-5"/><path d="m10 17-5-5 5-5M5 12h11"/></>,
    moon:<path d="M20.7 13.1A8.5 8.5 0 1 1 10.9 3.3a7 7 0 0 0 9.8 9.8Z"/>,
    sun:<><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4 12H2M22 12h-2M5 5l1.4 1.4M17.6 17.6 19 19M19 5l-1.4 1.4M6.4 17.6 5 19"/></>,
    palette:<><path d="M12 3a9 9 0 0 0 0 18h1.4a2 2 0 0 0 1.4-3.4l-.4-.4a2 2 0 0 1 1.4-3.4H18a3 3 0 0 0 3-3A9 9 0 0 0 12 3Z"/><path d="M7.5 10h.01M9.5 6.5h.01M14.5 6.5h.01M17 10h.01"/></>,
    pin:<path d="m12 17-4 4M15 3l6 6-4 1-4 4-1 4-6-6 4-1 4-4z"/>,
    send:<><path d="m22 2-7 20-4-9-9-4Z"/><path d="M22 2 11 13"/></>,
    share:<><circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><path d="m8.6 10.5 6.8-4M8.6 13.5l6.8 4"/></>,
    spark:<><path d="m12 3 1.3 4.2L17.5 9l-4.2 1.8L12 15l-1.3-4.2L6.5 9l4.2-1.8z"/><path d="M18.5 15l.7 2.3 2.3.7-2.3.7-.7 2.3-.7-2.3-2.3-.7 2.3-.7z"/></>,
    users:<><path d="M16 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="8.5" cy="7" r="4"/><path d="M20 8v6M23 11h-6"/></>,
    user:<><circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/></>,
    clock:<><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></>,
    terminal:<><rect x="3" y="4" width="18" height="16" rx="3"/><path d="m7 9 3 3-3 3M13 15h4"/></>,
    network:<><circle cx="12" cy="5" r="2.4"/><circle cx="5" cy="18" r="2.4"/><circle cx="19" cy="18" r="2.4"/><path d="m10.8 7-4.6 8.8M13.2 7l4.6 8.8M7.4 18h9.2"/></>,
    shield:<><path d="M12 3 4.5 6v5.2c0 4.7 3.2 8.1 7.5 9.8 4.3-1.7 7.5-5.1 7.5-9.8V6z"/><path d="m9.5 12 1.8 1.8 3.7-4"/></>,
    globe:<><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.3 2.5 3.5 5.5 3.5 9S14.3 18.5 12 21C9.7 18.5 8.5 15.5 8.5 12S9.7 5.5 12 3Z"/></>,
    menu:<path d="M4 7h16M4 12h16M4 17h16"/>,
    bolt:<path d="m13 2-7 11h6l-1 9 7-12h-6z"/>,
    external:<><path d="M14 4h6v6M20 4l-9 9"/><path d="M18 13v5a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h5"/></>,
    file:<><path d="M6 2h8l4 4v16H6z"/><path d="M14 2v5h5"/></>,
    play:<><circle cx="12" cy="12" r="9"/><path d="m10 8 6 4-6 4z"/></>,
    pause:<><path d="M9 5v14M15 5v14"/></>,
    music:<><path d="M9 18V5l10-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="16" cy="16" r="3"/></>,
    audio:<><path d="M9 18V5l10-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="16" cy="16" r="3"/></>,
    facebook:<><path d="M14 8h3V4h-3c-3 0-5 2-5 5v3H6v4h3v6h4v-6h3l1-4h-4V9c0-.7.3-1 1-1Z"/></>,
    instagram:<><rect x="3" y="3" width="18" height="18" rx="5"/><circle cx="12" cy="12" r="4"/><circle cx="17.5" cy="6.5" r=".8" fill="currentColor" stroke="none"/></>,
    github:<><path d="M12 2a10 10 0 0 0-3.2 19.5c.5.1.7-.2.7-.5v-2c-2.8.6-3.4-1.2-3.4-1.2-.5-1.2-1.1-1.5-1.1-1.5-.9-.6.1-.6.1-.6 1 .1 1.6 1.1 1.6 1.1.9 1.6 2.4 1.1 3 .9.1-.7.4-1.1.7-1.4-2.2-.3-4.6-1.1-4.6-5 0-1.1.4-2 1-2.7-.1-.3-.4-1.3.1-2.7 0 0 .8-.3 2.8 1a9.7 9.7 0 0 1 5.1 0c2-1.3 2.8-1 2.8-1 .5 1.4.2 2.4.1 2.7.7.7 1 1.6 1 2.7 0 3.9-2.4 4.7-4.6 5 .4.3.7.9.7 1.9V21c0 .3.2.6.7.5A10 10 0 0 0 12 2Z"/></>,
    mail:<><rect x="3" y="5" width="18" height="14" rx="2"/><path d="m3 7 9 6 9-6"/></>,
    phone:<><path d="M22 16.9v3a2 2 0 0 1-2.2 2 19.8 19.8 0 0 1-8.6-3.1 19.5 19.5 0 0 1-6-6A19.8 19.8 0 0 1 2.1 4.2 2 2 0 0 1 4.1 2h3a2 2 0 0 1 2 1.7c.1 1 .4 2 .7 2.9a2 2 0 0 1-.4 2.1L8.1 10a16 16 0 0 0 6 6l1.3-1.3a2 2 0 0 1 2.1-.4 13 13 0 0 0 2.9.7A2 2 0 0 1 22 16.9Z"/></>,
    edit:<><path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L8 18l-4 1 1-4Z"/></>,
    key:<><circle cx="8" cy="15" r="4"/><path d="m11 12 9-9M17 6l2 2M14 9l2 2"/></>,
    lock:<><rect x="5" y="10" width="14" height="10" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/></>,
    search:<><circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/></>,
    plus:<><path d="M12 5v14M5 12h14"/></>,
    trash:<><path d="M4 7h16M9 7V4h6v3M7 7l1 14h8l1-14"/><path d="M10 11v6M14 11v6"/></>,
    refresh:<><path d="M20 6v5h-5"/><path d="M4 18v-5h5"/><path d="M18.4 9A7 7 0 0 0 6 6.6L4 11M6 15a7 7 0 0 0 12 2.4L20 13"/></>,
    library:<><path d="M4 4h4v16H4zM10 4h4v16h-4zM16 4h4v16h-4z"/></>,
    paperclip:<path d="m21.4 11.6-8.9 8.9a6 6 0 0 1-8.5-8.5l9.2-9.2a4 4 0 0 1 5.7 5.7l-9.2 9.2a2 2 0 1 1-2.8-2.8l8.5-8.5"/>,
    video:<><rect x="3" y="5" width="13" height="14" rx="2"/><path d="m16 10 5-3v10l-5-3z"/></>,
    image:<><rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9" r="1.5"/><path d="m21 15-5-5L5 20"/></>,
    download:<><path d="M12 3v11M7.5 10.5 12 15l4.5-4.5"/><path d="M4 16.5v2.5a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2.5"/></>,
    copy:<><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/></>,
    qr:<><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><path d="M14 14h3v3h-3zM18 14h3v7h-3M14 19h3v2h-3"/></>,
    check:<path d="m5 12 4 4L19 6"/>,
    database:<><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v6c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 11v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6"/></>,
    system:<><rect x="3" y="4" width="18" height="14" rx="2"/><path d="M8 22h8M12 18v4"/></>,
    archive:<><path d="M4 7h16v14H4zM3 3h18v4H3zM9 11h6"/></>,
    more:<><circle cx="5" cy="12" r="1" fill="currentColor"/><circle cx="12" cy="12" r="1" fill="currentColor"/><circle cx="19" cy="12" r="1" fill="currentColor"/></>,
    eye:<><path d="M2 12s3.5-6 10-6 10 6 10 6-3.5 6-10 6S2 12 2 12Z"/><circle cx="12" cy="12" r="2.5"/></>,
    eyeOff:<><path d="m3 3 18 18M10.6 6.2A11 11 0 0 1 12 6c6.5 0 10 6 10 6a17 17 0 0 1-3.1 3.7M6.6 6.6C3.6 8.4 2 12 2 12s3.5 6 10 6c1.1 0 2.1-.2 3-.4"/><path d="M10.7 10.7a2 2 0 0 0 2.6 2.6"/></>,
    filter:<><path d="M4 6h16M7 12h10M10 18h4"/></>,
    mic:<><rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3M9 21h6"/></>,
    stop:<rect x="6" y="6" width="12" height="12" rx="2"/>,
  }
  return <svg {...common}>{paths[name] ?? paths.info}</svg>
}
