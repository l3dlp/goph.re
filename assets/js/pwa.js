// goph.re PWA glue: service worker, install button, app badge and push opt-in.
(() => {
	if (!("serviceWorker" in navigator)) return;
	const store = {
		get(k) { try { return localStorage.getItem(k) } catch (e) { return null } },
		set(k, v) { try { localStorage.setItem(k, v) } catch (e) {} },
	};

	const ready = navigator.serviceWorker.register("/sw.js", { scope: "/" }).then(() => navigator.serviceWorker.ready);
	ready.catch(e => console.warn("service worker:", e));

	// Reading the site clears the "N new" badge on the app icon.
	const clearBadge = () => { if (document.visibilityState === "visible" && navigator.clearAppBadge) navigator.clearAppBadge().catch(() => {}) };
	clearBadge();
	document.addEventListener("visibilitychange", clearBadge);

	// Install ------------------------------------------------------------------
	let deferred = null;
	const installButtons = document.querySelectorAll("[data-install]");
	addEventListener("beforeinstallprompt", e => {
		e.preventDefault();
		deferred = e;
		installButtons.forEach(b => { b.hidden = false });
	});
	installButtons.forEach(b => b.addEventListener("click", async () => {
		if (!deferred) return;
		deferred.prompt();
		await deferred.userChoice;
		deferred = null;
		installButtons.forEach(x => { x.hidden = true });
	}));
	addEventListener("appinstalled", () => installButtons.forEach(x => { x.hidden = true }));

	// Push ---------------------------------------------------------------------
	const panel = document.getElementById("notify");
	if (!panel) return;
	const boxes = [...panel.querySelectorAll("input[type=checkbox]")];
	const hint = document.getElementById("notifyHint");
	const say = text => { hint.textContent = text || ""; hint.hidden = !text };

	const standalone = matchMedia("(display-mode: standalone)").matches || navigator.standalone;
	const ios = /iPhone|iPad|iPod/.test(navigator.userAgent);
	if (!("PushManager" in window) || !("Notification" in window)) {
		if (ios && !standalone) {
			panel.hidden = false;
			boxes.forEach(b => { b.disabled = true });
			say("On iPhone: Share → Add to Home Screen, then open goph.re from there to turn notifications on.");
		}
		return;
	}

	const keyToBytes = base64 => {
		const padded = (base64 + "=".repeat((4 - base64.length % 4) % 4)).replace(/-/g, "+").replace(/_/g, "/");
		return Uint8Array.from(atob(padded), c => c.charCodeAt(0));
	};
	const post = (url, body) => fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
	const rememberTopics = async topics => {
		store.set("push-topics", JSON.stringify(topics));
		try { await (await caches.open("gophre-prefs")).put("/__push-topics", new Response(JSON.stringify(topics))) } catch (e) {}
	};

	let key = null;
	(async () => {
		const res = await fetch("/push/key").catch(() => null);
		const info = res && res.ok ? await res.json() : {};
		if (!info.enabled) return;
		key = info.key;
		panel.hidden = false;
		const reg = await ready;
		const sub = await reg.pushManager.getSubscription();
		let topics = [];
		try { topics = JSON.parse(store.get("push-topics") || "[]") } catch (e) {}
		if (sub && Notification.permission === "granted") {
			boxes.forEach(b => { b.checked = topics.includes(b.value) });
			if (!topics.length) boxes.forEach(b => { b.checked = b.value === "official" });
		}
		if (Notification.permission === "denied") say("Notifications are blocked for goph.re in your browser settings.");
	})();

	let busy = false;
	boxes.forEach(box => box.addEventListener("change", async () => {
		if (busy) return;
		busy = true;
		boxes.forEach(b => { b.disabled = true });
		const topics = boxes.filter(b => b.checked).map(b => b.value);
		try {
			const reg = await ready;
			let sub = await reg.pushManager.getSubscription();
			if (topics.length === 0) {
				if (sub) {
					await post("/push/unsubscribe", { endpoint: sub.endpoint });
					await sub.unsubscribe();
				}
				await rememberTopics([]);
				say("Notifications off.");
			} else {
				if (Notification.permission !== "granted") {
					const permission = await Notification.requestPermission();
					if (permission !== "granted") throw new Error(permission === "denied" ? "Notifications are blocked for goph.re in your browser settings." : "Notifications were not allowed.");
				}
				const first = !sub;
				if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyToBytes(key) });
				const res = await post("/push/subscribe" + (first ? "?welcome=1" : ""), { ...sub.toJSON(), topics });
				if (!res.ok) throw new Error("The server refused the subscription.");
				await rememberTopics(topics);
				say(first ? "Done. A welcome notification is on its way." : "Saved.");
			}
		} catch (e) {
			box.checked = !box.checked;
			say(e.message || String(e));
		}
		boxes.forEach(b => { b.disabled = false });
		busy = false;
	}));
})();
