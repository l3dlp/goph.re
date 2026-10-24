// Shared bits of the goph.re chrome: status-line clock, help popup, number-key navigation.
(() => {
	const clock = document.getElementById("clock");
	if (clock) {
		const tick = () => {
			const d = new Date();
			clock.textContent = d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" });
		};
		tick();
		setInterval(tick, 10000);
	}

	const help = document.getElementById("help");
	const toggleHelp = show => { if (help) help.hidden = show === undefined ? !help.hidden : !show };
	document.querySelectorAll("[data-help]").forEach(b => b.addEventListener("click", () => toggleHelp(true)));
	help?.addEventListener("click", () => toggleHelp(false));

	const routes = { 1: "/", 2: "/pulse", 3: "/golang", 4: "/experts", 5: "/blogs", 6: "/podcasts", 7: "/community", 8: "/forums", 9: "/all" };
	document.addEventListener("keydown", e => {
		if (e.ctrlKey || e.metaKey || e.altKey || e.target.closest?.("input, textarea")) return;
		if (help && !help.hidden) { toggleHelp(false); e.preventDefault(); return }
		if (e.key === "?") { toggleHelp(true); e.preventDefault() }
		else if (routes[e.key]) location.href = routes[e.key];
		else if (e.key === "m") location.href = "/me";
	});
})();
