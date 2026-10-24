// Backdrop: the crowd illustration as a slowly flowing, gradient-mapped watermark.
// The five ramp colours come from the CSS variables --bd0..--bd4, so every theme
// re-tints the image (like a Photoshop gradient-map layer in a blend mode).
// Options on <body>: data-backdrop="0.6" (intensity), data-backdrop-focus="0.5 0.42" (image centre).
(() => {
	const body = document.body;
	const canvas = document.createElement("canvas");
	canvas.id = "backdrop";
	canvas.setAttribute("aria-hidden", "true");
	body.prepend(canvas);

	const gl = canvas.getContext("webgl", { antialias: false, alpha: false, preserveDrawingBuffer: false });
	if (!gl) { document.documentElement.classList.add("no-webgl"); return }

	const vert = `
		attribute vec2 pos;
		varying vec2 uv;
		void main() { uv = pos * 0.5 + 0.5; gl_Position = vec4(pos, 0.0, 1.0); }`;
	const frag = `
		precision mediump float;
		varying vec2 uv;
		uniform sampler2D img;
		uniform vec2 res;
		uniform vec2 focus;
		uniform float t;
		uniform float scroll;
		uniform float intensity;
		uniform vec3 c0, c1, c2, c3, c4;

		float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
		float noise(vec2 p) {
			vec2 i = floor(p), f = fract(p);
			f = f * f * (3.0 - 2.0 * f);
			return mix(mix(hash(i), hash(i + vec2(1, 0)), f.x), mix(hash(i + vec2(0, 1)), hash(i + vec2(1, 1)), f.x), f.y);
		}
		float fbm(vec2 p) {
			float v = 0.0, a = 0.5;
			for (int i = 0; i < 4; i++) { v += a * noise(p); p *= 2.03; a *= 0.5; }
			return v;
		}
		vec3 ramp(float l) {
			if (l < 0.30) return mix(c0, c1, l / 0.30);
			if (l < 0.55) return mix(c1, c2, (l - 0.30) / 0.25);
			if (l < 0.78) return mix(c2, c3, (l - 0.55) / 0.23);
			return mix(c3, c4, clamp((l - 0.78) / 0.22, 0.0, 1.0));
		}
		void main() {
			// Cover-fit the square image, slight parallax with scroll.
			vec2 q = uv - 0.5;
			float ar = res.x / res.y;
			if (ar > 1.0) q.y /= ar; else q.x *= ar;
			q *= 0.92;
			q.y -= scroll * 0.035;
			vec2 flow = vec2(fbm(q * 3.0 + t * 0.035), fbm(q * 3.0 + 7.3 - t * 0.03)) - 0.5;
			vec2 st = q + focus + flow * 0.03;
			vec3 src = texture2D(img, vec2(st.x, 1.0 - st.y)).rgb;

			float l = dot(src, vec3(0.2126, 0.7152, 0.0722));
			float n = fbm(q * 1.6 + vec2(t * 0.02, -t * 0.016));
			vec3 mapped = ramp(smoothstep(0.02, 0.85, l));
			// Soft-light-ish fusion of the source and its gradient map.
			vec3 col = mix(src, mapped, 0.62 + 0.25 * n);
			col = mix(col, col * col * 1.6 + 0.04 * mapped, 0.25);
			// Drifting aurora in the ramp's highlight colours.
			col += ramp(0.62 + 0.38 * n) * 0.10 * smoothstep(0.45, 0.85, n);

			float vig = smoothstep(0.95, 0.1, length(q * vec2(1.0, 1.3)));
			col = mix(c0, col, intensity * (0.35 + 0.65 * vig));
			col += (hash(gl_FragCoord.xy + fract(t) * 91.0) - 0.5) * 0.018;
			gl_FragColor = vec4(col, 1.0);
		}`;

	const compile = (type, src) => {
		const s = gl.createShader(type);
		gl.shaderSource(s, src);
		gl.compileShader(s);
		if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s));
		return s;
	};
	let prog;
	try {
		prog = gl.createProgram();
		gl.attachShader(prog, compile(gl.VERTEX_SHADER, vert));
		gl.attachShader(prog, compile(gl.FRAGMENT_SHADER, frag));
		gl.linkProgram(prog);
		gl.useProgram(prog);
	} catch (e) {
		console.warn("backdrop:", e);
		document.documentElement.classList.add("no-webgl");
		canvas.remove();
		return;
	}

	const buf = gl.createBuffer();
	gl.bindBuffer(gl.ARRAY_BUFFER, buf);
	gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
	const posLoc = gl.getAttribLocation(prog, "pos");
	gl.enableVertexAttribArray(posLoc);
	gl.vertexAttribPointer(posLoc, 2, gl.FLOAT, false, 0, 0);

	const u = name => gl.getUniformLocation(prog, name);
	const U = Object.fromEntries(["res", "focus", "t", "scroll", "intensity", "c0", "c1", "c2", "c3", "c4", "img"].map(n => [n, u(n)]));

	const hexToRgb = hex => {
		const m = hex.trim().replace("#", "");
		const v = parseInt(m.length === 3 ? m.replace(/./g, "$&$&") : m, 16);
		return [(v >> 16 & 255) / 255, (v >> 8 & 255) / 255, (v & 255) / 255];
	};
	function readColors() {
		const style = getComputedStyle(document.documentElement);
		["c0", "c1", "c2", "c3", "c4"].forEach((c, i) => {
			const value = style.getPropertyValue(`--bd${i}`) || "#000";
			gl.uniform3fv(U[c], hexToRgb(value));
		});
	}
	const intensity = parseFloat(body.dataset.backdrop || "0.6");
	const focus = (body.dataset.backdropFocus || "0.5 0.45").split(" ").map(Number);
	gl.uniform1f(U.intensity, intensity);
	gl.uniform2f(U.focus, focus[0], focus[1]);
	gl.uniform1i(U.img, 0);
	readColors();
	document.addEventListener("themechange", () => { readColors(); draw(last) });

	// Render at reduced resolution: the image is soft anyway, and it keeps the GPU idle.
	const scale = 0.5;
	function resize() {
		const w = Math.max(1, Math.round(innerWidth * scale)), h = Math.max(1, Math.round(innerHeight * scale));
		if (canvas.width !== w || canvas.height !== h) {
			canvas.width = w; canvas.height = h;
			gl.viewport(0, 0, w, h);
			gl.uniform2f(U.res, w, h);
		}
	}

	let ready = false, last = 0;
	function draw(time) {
		if (!ready) return;
		resize();
		const max = Math.max(1, document.documentElement.scrollHeight - innerHeight);
		gl.uniform1f(U.t, time / 1000);
		gl.uniform1f(U.scroll, scrollY / max);
		gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
	}

	const img = new Image();
	img.onload = () => {
		const tex = gl.createTexture();
		gl.bindTexture(gl.TEXTURE_2D, tex);
		gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGB, gl.RGB, gl.UNSIGNED_BYTE, img);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
		ready = true;
		canvas.classList.add("on");
		draw(performance.now());
		if (!still) requestAnimationFrame(loop);
	};
	img.src = body.dataset.backdropImage || "/gfx/crowd.png";

	// ~30 fps animation, paused when hidden; a single frame for reduced motion.
	const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
	function loop(time) {
		if (!document.hidden && time - last > 33) { last = time; draw(time) }
		requestAnimationFrame(loop);
	}
	addEventListener("resize", () => draw(last || performance.now()));
	if (still) addEventListener("scroll", () => draw(last), { passive: true });
})();
