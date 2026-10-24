// goph.re infinite wall.
(() => {
	const feed = document.getElementById("feed");
	const sentinel = document.getElementById("sentinel");
	const moreButton = document.getElementById("more");
	const search = document.getElementById("searchInput");
	const newCount = document.getElementById("newCount");

	const PAGE_SIZE = 40;
	const QUALITY = new Set(["golang", "official", "experts", "blogs", "podcasts", "community", "tools", "courses", "examples"]);
	const store = {
		get(k) { try { return localStorage.getItem(k) } catch (e) { return null } },
		set(k, v) { try { localStorage.setItem(k, v) } catch (e) {} },
	};

	// Votes: the server's copy wins, localStorage covers anonymous visitors.
	const voteOf = id => (FEED.votes && FEED.votes[id]) || store.get(`vote_${id}`);

	// "Since your last visit" only makes sense on the main feed.
	const trackVisits = !FEED.path && !FEED.all;
	const lastSeen = trackVisits ? store.get("feed-last-seen") : null;
	let newest = null, fresh = 0, dividerPlaced = !lastSeen;

	let mode = "feed", query = "", page = 0, loading = false, done = false, lastDay = "";

	// Rendering -------------------------------------------------------------
	const dayLabel = d => {
		const today = new Date(); today.setHours(0, 0, 0, 0);
		const that = new Date(d); that.setHours(0, 0, 0, 0);
		const diff = Math.round((today - that) / 864e5);
		if (diff === 0) return "today";
		if (diff === 1) return "yesterday";
		return d.toLocaleDateString("en-GB", { weekday: "short", day: "numeric", month: "short", year: diff > 300 ? "numeric" : undefined });
	};
	const age = d => {
		const s = (Date.now() - d) / 1000;
		if (s < 3600) return Math.max(1, Math.round(s / 60)) + "m";
		if (s < 86400) return Math.round(s / 3600) + "h";
		if (s < 86400 * 30) return Math.round(s / 86400) + "d";
		return d.toLocaleDateString("en-GB", { day: "numeric", month: "short" });
	};
	const host = url => { try { return new URL(url).hostname.replace(/^www\./, "") } catch (e) { return "" } };
	const el = (tag, cls, text) => {
		const e = document.createElement(tag);
		if (cls) e.className = cls;
		if (text !== undefined) e.textContent = text;
		return e;
	};

	function render(article) {
		const date = new Date(article.date);
		const path = article.source?.path || "";
		const vote = voteOf(article.id);
		if (vote === "BAD" && !FEED.all && mode === "feed") return;

		const isNew = lastSeen && article.date > lastSeen;
		if (!dividerPlaced && !isNew && mode === "feed" && page === 1) {
			const since = el("div", "since mono");
			since.id = "since";
			since.append(el("span", "", fresh ? `↑ ${fresh} new since your last visit · you're caught up` : "nothing new since your last visit"));
			feed.append(since);
			dividerPlaced = true;
		}

		const day = dayLabel(date);
		if (day !== lastDay) {
			const h = el("h2", "day mono");
			h.append(el("span", "", day));
			feed.append(h);
			lastDay = day;
		}

		const card = QUALITY.has(path) || (article.resume && path !== "forums");
		const item = el("article", `item ${card ? "card glass" : "row"} ${vote || ""}`);
		item.dataset.id = article.id;
		if (isNew) { item.classList.add("new"); fresh++ }

		const meta = el("div", "meta mono");
		const src = el("a", `src p-${path}`, article.source?.name || "?");
		src.href = "/" + path; src.target = "_self";
		meta.append(src, el("span", "age", age(date)));
		if (card) meta.append(el("span", "host", host(article.url)));
		meta.append(el("span", "badge kept", "kept"));

		const title = el("h3");
		const link = el("a", "", article.name);
		link.href = article.url; link.target = "_blank"; link.rel = "noopener";
		title.append(link);

		const acts = el("div", "acts mono");
		const keep = el("button", "keep"); keep.type = "button"; keep.innerHTML = "keep <kbd>s</kbd>";
		const skip = el("button", "skip"); skip.type = "button"; skip.innerHTML = "skip <kbd>x</kbd>";
		keep.addEventListener("click", () => vote(item, "GOOD"));
		skip.addEventListener("click", () => vote(item, "BAD"));
		acts.append(keep, skip);

		item.append(meta, title);
		if (article.resume && card) {
			const resume = el("div", "resume");
			resume.innerHTML = article.resume; // sanitized server-side with bluemonday
			resume.querySelectorAll("a").forEach(a => { a.target = "_blank"; a.rel = "noopener" });
			item.append(resume);
			requestAnimationFrame(() => {
				if (resume.scrollHeight > resume.clientHeight + 8) {
					const more = el("button", "expand mono", "more ↓");
					more.type = "button";
					more.addEventListener("click", () => toggle(item));
					item.insertBefore(more, acts.parentNode === item ? acts : null);
					item.classList.add("clamped");
				}
			});
		}
		item.append(acts);
		item.addEventListener("mouseenter", () => { if (!keyboardMode) select(item, false) });
		feed.append(item);
	}

	function toggle(item) {
		if (!item.classList.contains("clamped") && !item.classList.contains("open")) return;
		const open = item.classList.toggle("open");
		item.classList.toggle("clamped", !open);
		const b = item.querySelector(".expand");
		if (b) b.textContent = open ? "less ↑" : "more ↓";
	}

	// Loading ---------------------------------------------------------------
	async function load() {
		if (loading || done) return;
		loading = true;
		sentinel.classList.add("busy");
		page++;
		const url = mode === "search"
			? `/search?q=${encodeURIComponent(query)}&page=${page}&size=30`
			: `/feed?path=${encodeURIComponent(FEED.path)}&page=${page}&size=${PAGE_SIZE}`;
		try {
			const res = await fetch(url);
			const articles = await res.json();
			if (!articles || articles.length === 0) {
				done = true;
				if (page === 1) feed.append(el("p", "empty mono", mode === "search" ? `grep: no match for "${query}"` : "nothing here yet."));
			} else {
				if (page === 1 && mode === "feed") newest = articles[0].date;
				articles.forEach(render);
				if (page === 1 && mode === "feed" && trackVisits) announce();
			}
		} catch (e) {
			page--;
			console.error("Error loading articles:", e);
		}
		loading = false;
		sentinel.classList.remove("busy");
		sentinel.classList.toggle("done", done);
	}

	function announce() {
		if (!lastSeen) { store.set("feed-last-seen", newest); return }
		if (fresh > 0) {
			newCount.textContent = `${fresh} new`;
			document.title = `(${fresh}) ${document.title}`;
		}
		// The visit counts once the reader has actually been here a little while.
		const save = () => newest && store.set("feed-last-seen", newest);
		setTimeout(save, 15000);
		addEventListener("pagehide", save);
	}

	function reset(newMode, q) {
		mode = newMode; query = q || ""; page = 0; done = false; lastDay = "";
		feed.replaceChildren();
		selected = null;
		load();
	}

	new IntersectionObserver(entries => {
		if (entries.some(e => e.isIntersecting)) load();
	}, { rootMargin: "1200px 0px" }).observe(sentinel);
	moreButton.addEventListener("click", load);

	// Votes -----------------------------------------------------------------
	async function vote(item, value) {
		const id = item.dataset.id;
		item.classList.remove("GOOD", "BAD");
		item.classList.add(value, "voted");
		store.set(`vote_${id}`, value);
		if (FEED.votes) FEED.votes[id] = value;
		if (value === "BAD" && !FEED.all) {
			if (selected === item) move(1);
			item.style.height = item.offsetHeight + "px";
			requestAnimationFrame(() => item.classList.add("leaving"));
			setTimeout(() => item.remove(), 450);
		}
		try {
			const res = await fetch(`/vote/${id}/${value}`, { method: "POST" });
			if (res.status === 401) location.href = "/auth/github";
		} catch (e) {
			console.error("Error submitting vote:", e);
		}
	}

	// Keyboard --------------------------------------------------------------
	let selected = null, keyboardMode = false;
	addEventListener("mousemove", () => { keyboardMode = false }, { passive: true });
	function select(item, scroll = true) {
		if (selected) selected.classList.remove("sel");
		selected = item;
		if (!item) return;
		item.classList.add("sel");
		if (scroll) item.scrollIntoView({ block: "center", behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
	}
	function move(delta) {
		const items = [...feed.querySelectorAll(".item:not(.leaving)")];
		if (!items.length) return;
		let i = items.indexOf(selected);
		if (i === -1) {
			// Start from the first item visible on screen.
			i = items.findIndex(x => x.getBoundingClientRect().bottom > 80) - (delta > 0 ? 1 : 0);
		}
		const next = items[Math.max(0, Math.min(items.length - 1, i + delta))];
		select(next);
		if (items.length - items.indexOf(next) < 8) load();
	}

	document.addEventListener("keydown", e => {
		if (e.ctrlKey || e.metaKey || e.altKey) return;
		if (e.target === search) {
			if (e.key === "Escape") { search.value = ""; search.blur(); runSearch("") }
			if (e.key === "Enter" || e.key === "ArrowDown") { search.blur(); keyboardMode = true; move(1) }
			return;
		}
		if (!document.getElementById("help").hidden) return;
		const act = {
			j: () => move(1), k: () => move(-1),
			o: () => selected?.querySelector("h3 a").click(),
			Enter: () => selected?.querySelector("h3 a").click(),
			s: () => selected && vote(selected, "GOOD"),
			x: () => selected && vote(selected, "BAD"),
			e: () => selected && toggle(selected),
			n: () => { const s = document.getElementById("since"); if (s) { s.scrollIntoView({ block: "center", behavior: "smooth" }); select(s.nextElementSibling?.classList.contains("item") ? s.nextElementSibling : s.nextElementSibling?.nextElementSibling, false) } },
			g: () => { scrollTo({ top: 0, behavior: "smooth" }); select(null) },
			"/": () => search?.focus(),
			Escape: () => select(null),
		}[e.key];
		if (act) { e.preventDefault(); keyboardMode = true; act() }
	});

	// Search ----------------------------------------------------------------
	let searchTimer;
	function runSearch(q) {
		q = q.trim();
		const url = new URL(location);
		if (q) url.searchParams.set("q", q); else url.searchParams.delete("q");
		history.replaceState(null, "", url);
		reset(q ? "search" : "feed", q);
	}
	search?.addEventListener("input", () => {
		clearTimeout(searchTimer);
		searchTimer = setTimeout(() => runSearch(search.value), 300);
	});

	const initial = new URLSearchParams(location.search).get("q");
	if (initial && search) {
		search.value = initial;
		reset("search", initial);
	} else {
		load();
	}
})();
