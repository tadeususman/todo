// Service worker — network-first for everything so updates propagate instantly.
// Only public static assets are cached (offline fallback); pages with user data never are.

const CACHE = 'todo-v47';

self.addEventListener('install', (e) => {
  self.skipWaiting();
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))
    ).then(() => self.registration.navigationPreload && self.registration.navigationPreload.enable())
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (e) => {
  if (e.request.method !== 'GET') return;

  const url = new URL(e.request.url);

  // Static files: left to the browser's HTTP cache (the server marks ?v=N URLs immutable).
  // Pages are never stored; offline shows a small notice.
  if (url.pathname.startsWith('/static/')) return;
  const isNav = e.request.mode === 'navigate';

  // Pages are never stored: they hold private data, and a cached copy would stay readable after logout.
  if (isNav) {
    // preloadResponse is the page request the browser already started while this worker woke up.
    e.respondWith(
      (async () => (await e.preloadResponse) || fetch(e.request))()
        .catch(() => new Response(OFFLINE_HTML, { status: 503, headers: { 'Content-Type': 'text/html; charset=utf-8' } }))
    );
    return;
  }
});

const OFFLINE_HTML = '<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">' +
  '<title>Offline</title><body style="font-family:system-ui,sans-serif;display:grid;place-items:center;min-height:100vh;margin:0;color:#64748b;text-align:center">' +
  '<div><p style="font-size:1.1rem;margin:0 0 .5rem">Tidak ada koneksi</p><p style="margin:0;font-size:.9rem">Sambungkan ke internet lalu muat ulang.</p></div>';

// ---- Web Push: deadline reminders ----
self.addEventListener('push', (e) => {
  let d = {};
  try { d = e.data ? e.data.json() : {}; } catch (_) {}
  e.waitUntil(self.registration.showNotification(d.title || 'todo', {
    body: d.body || '',
    icon: '/static/icons/icon-192.png',
    badge: '/static/icons/favicon-48.png',
    data: { url: typeof d.url === 'string' && d.url.startsWith('/') ? d.url : '/home' },
  }));
});

self.addEventListener('notificationclick', (e) => {
  e.notification.close();
  const url = (e.notification.data && e.notification.data.url) || '/home';
  e.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((list) => {
    for (const c of list) {
      if ('focus' in c) { c.navigate(url); return c.focus(); }
    }
    return self.clients.openWindow(url);
  }));
});
