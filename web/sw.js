"use strict";
// Network first: a deploy shows at the next open. The cached shell only answers when Musaarr
// cannot be reached (Tailscale off, NAS down), so the home-screen app opens and says so.
const CACHE = "musaarr-shell";
const SHELL = ["/", "/i18n.js", "/i18n/en.json", "/i18n/fr.json", "/app.js", "/session.js", "/app.css", "/rack.js", "/rack.css", "/shop3d.css", "/fonts/libre-franklin.woff2", "/icon.svg", "/manifest.webmanifest"];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));

self.addEventListener("fetch", (e) => {
  const req = e.request;
  const url = new URL(req.url);
  if (req.method !== "GET" || url.origin !== self.location.origin || url.pathname.startsWith("/api/")) return;
  const key = req.mode === "navigate" && url.pathname !== "/login.html" ? "/" : url.pathname;
  e.respondWith(fetch(req).then((res) => {
    if (res.ok && !res.redirected && SHELL.includes(key)) { // signed out, "/" leads to the sign-in page: not the shell
      const copy = res.clone();
      e.waitUntil(caches.open(CACHE).then((c) => c.put(key, copy)));
    }
    return res;
  }).catch(async () => (await caches.match(key)) || Response.error()));
});
