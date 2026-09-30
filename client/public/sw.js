const CACHE = 'aits-shell-v19'
const CORE = ['/', '/manifest.webmanifest', '/icon-192.png', '/icon-512.png', '/application.json', '/logo/aits.png', '/logo/cite.png', '/logo/bcc.png']

async function cacheResponse(cache, url, requestInit = {}) {
  try {
    const response = await fetch(url, requestInit)
    if (response && response.ok) await cache.put(url, response.clone())
    return response
  } catch {
    return undefined
  }
}

async function precacheShell() {
  const cache = await caches.open(CACHE)
  await Promise.all(CORE.map((url) => cacheResponse(cache, url, { cache: 'reload' })))

  const root = await cacheResponse(cache, '/', { cache: 'reload' })
  if (!root || !root.ok) return

  try {
    const html = await root.clone().text()
    const urls = new Set()
    for (const match of html.matchAll(/(?:src|href)=["']([^"']+)["']/gi)) {
      try {
        const url = new URL(match[1], self.location.origin)
        if (url.origin !== self.location.origin) continue
        if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/ws/')) continue
        if (url.pathname === '/') continue
        urls.add(url.pathname + url.search)
      } catch {
        // Ignore malformed/unsupported URLs in markup.
      }
    }
    await Promise.all([...urls].map((url) => cacheResponse(cache, url, { cache: 'reload' })))
  } catch {
    // The root document is already cached, so navigation still has a fallback.
  }
}

self.addEventListener('install', (event) => {
  event.waitUntil(precacheShell())
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key))))
  )
  self.clients.claim()
})

async function networkFirst(request) {
  const cache = await caches.open(CACHE)
  try {
    const response = await fetch(request)
    if (response.ok) await cache.put(request, response.clone())
    return response
  } catch {
    return (await cache.match(request)) || (request.mode === 'navigate' ? cache.match('/') : undefined) || Response.error()
  }
}

async function staleWhileRevalidate(request) {
  const cache = await caches.open(CACHE)
  const cached = await cache.match(request)
  const update = fetch(request)
    .then(async (response) => {
      if (response.ok) await cache.put(request, response.clone())
      return response
    })
    .catch(() => undefined)
  return cached || (await update) || Response.error()
}

self.addEventListener('fetch', (event) => {
  const request = event.request
  if (request.method !== 'GET') return

  const url = new URL(request.url)
  if (url.origin !== self.location.origin || url.pathname.startsWith('/api/') || url.pathname.startsWith('/ws/')) return

  if (request.mode === 'navigate' || request.destination === 'script' || request.destination === 'style' || request.destination === 'worker') {
    event.respondWith(networkFirst(request))
    return
  }

  if (request.destination === 'image' || request.destination === 'font') {
    event.respondWith(staleWhileRevalidate(request))
    return
  }

  event.respondWith(networkFirst(request))
})
