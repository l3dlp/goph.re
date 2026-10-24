// goph.re picks: reading progress, table of contents with scroll spy, copy buttons.
(() => {
	const prose = document.getElementById("prose");
	if (!prose) return;

	const bar = document.getElementById("progress");
	const progress = () => {
		const rect = prose.getBoundingClientRect();
		const total = rect.height - innerHeight * 0.6;
		const done = Math.min(1, Math.max(0, -rect.top / Math.max(1, total)));
		bar.style.width = (done * 100).toFixed(2) + "%";
	};
	addEventListener("scroll", progress, { passive: true });
	addEventListener("resize", progress);
	progress();

	const toc = document.getElementById("toc");
	const headings = [...prose.querySelectorAll("h2[id]")];
	if (toc && headings.length > 2) {
		const title = document.createElement("p");
		title.textContent = "contents";
		toc.append(title);
		const links = headings.map(h => {
			const a = document.createElement("a");
			a.href = "#" + h.id;
			a.target = "_self";
			a.textContent = h.textContent;
			toc.append(a);
			return a;
		});
		const spy = new IntersectionObserver(entries => {
			entries.forEach(e => {
				if (!e.isIntersecting) return;
				links.forEach(l => l.classList.toggle("on", l.hash === "#" + e.target.id));
			});
		}, { rootMargin: "0px 0px -70% 0px" });
		headings.forEach(h => spy.observe(h));
	}

	prose.querySelectorAll("pre").forEach(pre => {
		const code = pre.querySelector("code");
		if (!code || !navigator.clipboard) return;
		const button = document.createElement("button");
		button.type = "button";
		button.className = "copy mono";
		button.textContent = "copy";
		button.addEventListener("click", async () => {
			try {
				await navigator.clipboard.writeText(code.innerText);
				button.textContent = "copied ✓";
			} catch (e) {
				button.textContent = "press ctrl+c";
			}
			setTimeout(() => { button.textContent = "copy" }, 1600);
		});
		pre.append(button);
	});
})();
