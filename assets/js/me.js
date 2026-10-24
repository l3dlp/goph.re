// goph.re/me — keyboard-driven terminal page.
(() => {
	const $ = (s, el = document) => el.querySelector(s);
	const $$ = (s, el = document) => [...el.querySelectorAll(s)];
	const panes = $$(".pane");
	const themes = ["gruvbox", "tokyonight", "nord"];
	const store = {
		get(k) { try { return localStorage.getItem(k) } catch (e) { return null } },
		set(k, v) { try { localStorage.setItem(k, v) } catch (e) {} },
	};

	// Panes ----------------------------------------------------------------
	let current = 0;
	function focusPane(i) {
		current = (i + panes.length) % panes.length;
		panes.forEach((p, j) => p.classList.toggle("active", j === current));
		const list = $(".list", panes[current]);
		if (list && !$("li.sel", list)) select(list, visible(list)[0]);
	}
	panes.forEach((p, i) => p.addEventListener("mousedown", () => focusPane(i)));

	// Lists ----------------------------------------------------------------
	const visible = list => $$("li[data-id]", list).filter(li => !li.classList.contains("hidden"));
	function select(list, li) {
		$$("li.sel", list).forEach(x => x.classList.remove("sel"));
		if (!li) return;
		li.classList.add("sel");
		li.scrollIntoView({ block: "nearest" });
	}
	function move(delta) {
		const list = $(".list", panes[current]);
		if (!list) return;
		const items = visible(list);
		if (!items.length) return;
		const at = items.indexOf($("li.sel", list));
		const next = delta === Infinity ? items.length - 1 : delta === -Infinity ? 0 : Math.max(0, Math.min(items.length - 1, at + delta));
		select(list, items[next]);
	}
	const selected = () => $("li.sel", panes[current]);
	$$(".list li[data-id]").forEach(li => li.addEventListener("mouseenter", () => {
		const pane = li.closest(".pane");
		if (pane.classList.contains("active")) select(li.closest(".list"), li);
	}));

	// Votes ----------------------------------------------------------------
	async function vote(id, value) {
		const res = await fetch(`/vote/${id}/${value}`, { method: "POST" });
		if (res.status === 401) { location.href = "/auth/github"; return false }
		if (!res.ok) return false;
		store.set(`vote_${id}`, value);
		return true;
	}

	async function drop() {
		const li = selected();
		if (!li || !li.closest("#bookmarks") || li.classList.contains("gone")) return;
		if (await vote(li.dataset.id, "BAD")) {
			li.classList.add("gone");
			move(1);
			setTimeout(() => { li.remove(); bump(-1) }, 600);
		}
	}

	async function save() {
		const li = selected();
		if (!li || !li.closest("#fresh") || li.classList.contains("saved")) return;
		if (!(await vote(li.dataset.id, "GOOD"))) return;
		li.classList.add("saved");
		const d = li.dataset;
		const row = document.createElement("li");
		row.dataset.id = d.id;
		row.dataset.text = `${d.name} ${d.source} ${d.path}`;
		row.className = "new";
		const now = new Date();
		const stamp = now.toLocaleString("en-US", { month: "short" }) + " " + String(now.getDate()).padStart(2, " ") + " " +
			String(now.getHours()).padStart(2, "0") + ":" + String(now.getMinutes()).padStart(2, "0");
		const cells = [["perm", "-rw-r--r--"], ["owner", $(".ls .owner")?.textContent || "you"], [`group p-${d.path}`, d.path]];
		cells.forEach(([cls, text]) => { const s = document.createElement("span"); s.className = cls; s.textContent = text; row.append(s) });
		const t = document.createElement("time"); t.textContent = stamp; row.append(t);
		const a = document.createElement("a");
		a.className = `name p-${d.path}`; a.href = d.url; a.target = "_blank"; a.rel = "noopener"; a.textContent = d.name;
		row.append(a);
		const list = $("#bookmarks");
		$("li:not([data-id])", list)?.remove();
		list.prepend(row);
		bump(1);
	}

	function bump(n) {
		const total = $("#total");
		total.textContent = parseInt(total.textContent, 10) + n;
	}

	// Filter (less-style "/") ------------------------------------------------
	const cmdline = $("#cmdline"), filter = $("#filter");
	function openFilter() {
		focusPane(0);
		cmdline.hidden = false;
		filter.focus();
	}
	function applyFilter() {
		const words = filter.value.toLowerCase().split(/\s+/).filter(Boolean);
		$$("#bookmarks li[data-id]").forEach(li => {
			const text = li.dataset.text.toLowerCase();
			li.classList.toggle("hidden", !words.every(w => text.includes(w)));
		});
		select($("#bookmarks"), visible($("#bookmarks"))[0]);
	}
	function closeFilter(clear) {
		if (clear) { filter.value = ""; applyFilter() }
		if (!filter.value) cmdline.hidden = true;
		filter.blur();
	}
	filter.addEventListener("input", applyFilter);
	filter.addEventListener("keydown", e => {
		if (e.key === "Escape") { e.preventDefault(); closeFilter(true) }
		if (e.key === "Enter") { e.preventDefault(); closeFilter(false) }
		e.stopPropagation();
	});

	// Theme ----------------------------------------------------------------
	const themeName = $("#themeName");
	function setTheme(t) {
		document.documentElement.dataset.theme = t;
		themeName.textContent = t;
		store.set("me-theme", t);
		const meta = $('meta[name="theme-color"]');
		if (meta) meta.content = getComputedStyle(document.body).backgroundColor;
	}
	const cycleTheme = () => setTheme(themes[(themes.indexOf(document.documentElement.dataset.theme) + 1) % themes.length]);
	themeName.textContent = document.documentElement.dataset.theme;
	themeName.addEventListener("click", cycleTheme);

	// Help -----------------------------------------------------------------
	const help = $("#help");
	help.addEventListener("click", () => { help.hidden = true });
	$(".status .keys").addEventListener("click", () => { help.hidden = false });

	// Keys -----------------------------------------------------------------
	document.addEventListener("keydown", e => {
		if (e.ctrlKey || e.metaKey || e.altKey) return;
		if (!help.hidden) { help.hidden = true; e.preventDefault(); return }
		const actions = {
			j: () => move(1), ArrowDown: () => move(1),
			k: () => move(-1), ArrowUp: () => move(-1),
			g: () => move(-Infinity), G: () => move(Infinity),
			o: () => selected()?.querySelector("a")?.click(),
			Enter: () => selected()?.querySelector("a")?.click(),
			x: drop, d: drop, s: save,
			"/": openFilter,
			Escape: () => closeFilter(true),
			Tab: () => focusPane(current + (e.shiftKey ? -1 : 1)),
			t: cycleTheme,
			"?": () => { help.hidden = false },
			q: () => { location.href = "/" },
		};
		const action = actions[e.key];
		if (action) { e.preventDefault(); action() }
	});

	// Clock ----------------------------------------------------------------
	const clock = $("#clock");
	const tick = () => {
		const d = new Date();
		clock.textContent = d.toLocaleDateString("en-GB", { day: "2-digit", month: "short" }) + " " +
			d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
	};
	tick();
	setInterval(tick, 1000);

	focusPane(0);
})();
