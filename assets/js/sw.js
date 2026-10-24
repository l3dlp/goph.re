// goph.re service worker: offline shell, cached feed for offline reading, Web Push.
const VERSION = "gophre-v1";
const SHELL = `${VERSION}-shell`;
const RUNTIME = `${VERSION}-runtime`;
const DATA = `${VERSION}-data`;
// Survives version bumps: the topics the user picked, for pushsubscriptionchange.
const PREFS = "gophre-prefs";

const PRECACHE = [
	"/offline",
	"/css/site.css", "/css/feed.css", "/css/pulse.css", "/css/me.css",
	"/js/site.js", "/js/wall.js", "/js/backdrop.js", "/js/me.js", "/js/pwa.js",
	"/gfx/crowd.jpg",
	"/gfx/icons/icon.svg", "/gfx/icons/icon-192.png", "/gfx/icons/badge-96.png",
	"/manifest.webmanifest",
];

self.addEventListener("install", event => {
	event.waitUntil(caches.open(SHELL).then(c => c.addAll(PRECACHE)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", event => {
	event.waitUntil((async () => {
		for (const key of await caches.keys()) {
			if (!key.startsWith(VERSION) && key !== PREFS) await caches.delete(key);
		}
		if (self.registration.navigationPreload) await self.registration.navigationPreload.enable();
		await self.clients.claim();
	})());
});

// Network first, falling back to the cache (and to `fallback` for pages).
async function networkFirst(event, cacheName, fallback) {
	const cache = await caches.open(cacheName);
	try {
		const response = (await event.preloadResponse) || await fetch(event.request);
		if (response.ok) cache.put(event.request, response.clone());
		return response;
	} catch (e) {
		const cached = await cache.match(event.request, { ignoreVary: true });
		if (cached) return cached;
		if (fallback) return (await caches.match(fallback)) || Response.error();
		throw e;
	}
}

// Serve from cache at once, refresh in the background.
async function staleWhileRevalidate(event, cacheName) {
	const cache = await caches.open(cacheName);
	const cached = await cache.match(event.request);
	const refresh = fetch(event.request).then(response => {
		if (response.ok || response.type === "opaque") cache.put(event.request, response.clone());
		return response;
	}).catch(() => cached);
	if (cached) { event.waitUntil(refresh); return cached }
	return refresh;
}

self.addEventListener("fetch", event => {
	const { request } = event;
	if (request.method !== "GET") return;
	const url = new URL(request.url);

	if (url.origin === location.origin) {
		if (url.pathname.startsWith("/auth/") || url.pathname.startsWith("/admin") || url.pathname.startsWith("/push/")) return;
		if (request.mode === "navigate") { event.respondWith(networkFirst(event, RUNTIME, "/offline")); return }
		if (url.pathname === "/feed" || url.pathname === "/search" || url.pathname.startsWith("/article/")) {
			event.respondWith(networkFirst(event, DATA)); return;
		}
		if (/^\/(css|js|gfx)\//.test(url.pathname) || url.pathname === "/manifest.webmanifest") {
			event.respondWith(staleWhileRevalidate(event, SHELL)); return;
		}
		return;
	}
	if (url.hostname === "fonts.googleapis.com" || url.hostname === "fonts.gstatic.com") {
		event.respondWith(staleWhileRevalidate(event, RUNTIME));
	}
});

// Web Push -------------------------------------------------------------------
self.addEventListener("push", event => {
	let data = {};
	try { data = event.data ? event.data.json() : {} } catch (e) { data = { body: event.data && event.data.text() } }
	const title = data.title || "goph.re";
	const options = {
		body: data.body || "Something new landed in Go.",
		icon: "/gfx/icons/icon-192.png",
		badge: "/gfx/icons/badge-96.png",
		tag: data.tag || "gophre",
		renotify: true,
		timestamp: Date.now(),
		data: { url: data.url || "/" },
		actions: data.url && data.url !== "/" ? [{ action: "open", title: "Read" }, { action: "feed", title: "Open goph.re" }] : [],
	};
	event.waitUntil((async () => {
		await self.registration.showNotification(title, options);
		if (data.count && "setAppBadge" in self.navigator) {
			try { await self.navigator.setAppBadge(data.count) } catch (e) {}
		}
	})());
});

self.addEventListener("notificationclick", event => {
	event.notification.close();
	const target = new URL(event.action === "feed" ? "/" : event.notification.data.url || "/", location.origin);
	event.waitUntil((async () => {
		if ("clearAppBadge" in self.navigator) { try { await self.navigator.clearAppBadge() } catch (e) {} }
		if (target.origin !== location.origin) return self.clients.openWindow(target.href);
		const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
		for (const client of windows) {
			if (new URL(client.url).origin === location.origin && "focus" in client) {
				await client.focus();
				return client.navigate(target.href);
			}
		}
		return self.clients.openWindow(target.href);
	})());
});

// The browser rotated the subscription: re-register it with the same topics.
self.addEventListener("pushsubscriptionchange", event => {
	event.waitUntil((async () => {
		const old = event.oldSubscription;
		const res = await fetch("/push/key");
		const { key } = await res.json();
		const sub = event.newSubscription || await self.registration.pushManager.subscribe({
			userVisibleOnly: true,
			applicationServerKey: key,
		});
		const stored = await (await caches.open(PREFS)).match("/__push-topics");
		const topics = stored ? await stored.json() : ["official"];
		await fetch("/push/subscribe", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ ...sub.toJSON(), topics }),
		});
		if (old) {
			await fetch("/push/unsubscribe", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ endpoint: old.endpoint }),
			});
		}
	})());
});
