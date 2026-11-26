// Musaarr's 3D stage (lot 3): the record, printed as a halftone. app.js loads it after
// the first paint, never with reduced motion or Save-Data; without WebGL 2 it throws and the app
// stays whole. It plays the opening once, then turns behind « Quel artiste ? », speeds up with a
// blue wave while a search runs, and sweeps a green glint when an album reaches Plex.
import {
  WebGLRenderer, Scene, PerspectiveCamera, OrthographicCamera, Group, Mesh, CircleGeometry, CylinderGeometry,
  PlaneGeometry, BufferGeometry, Float32BufferAttribute, ShaderMaterial, CanvasTexture, WebGLRenderTarget, Vector2,
} from "./vendor/three.min.js";

const $ = (s) => document.querySelector(s);
const lightScheme = matchMedia("(prefers-color-scheme: light)");
const reduce = matchMedia("(prefers-reduced-motion: reduce)");
const RPM33 = (33.333 / 60) * Math.PI * 2;
const IDLE = 0.32; // rad/s: the record turns slowly behind the prompt

const canvas = document.createElement("canvas");
canvas.className = "stage";
canvas.setAttribute("aria-hidden", "true");
const renderer = new WebGLRenderer({ canvas, alpha: true, antialias: true, powerPreference: "low-power" });
renderer.setPixelRatio(Math.min(devicePixelRatio || 1, 1.5));
renderer.setClearColor(0x000000, 0);

/* ---------- The record ---------- */

// The blue label: the wordmark, the spindle hole, a thin printed ring; drawn again once the font is in.
const labelCanvas = document.createElement("canvas");
labelCanvas.width = labelCanvas.height = 512;
function drawLabel() {
  const g = labelCanvas.getContext("2d");
  g.clearRect(0, 0, 512, 512);
  g.fillStyle = "#1d5ce0";
  g.beginPath(); g.arc(256, 256, 256, 0, Math.PI * 2); g.fill();
  g.strokeStyle = "rgba(255,255,255,.55)";
  g.lineWidth = 3;
  g.beginPath(); g.arc(256, 256, 228, 0, Math.PI * 2); g.stroke();
  g.fillStyle = "#fff";
  g.textAlign = "center";
  g.font = "900 92px 'Libre Franklin', system-ui, sans-serif";
  g.fillText("musaarr", 256, 190);
  g.font = "700 30px 'Libre Franklin', system-ui, sans-serif";
  g.fillText(t("stage.label").toUpperCase(), 256, 372);
  g.fillStyle = "#0b0b0c";
  g.beginPath(); g.arc(256, 256, 20, 0, Math.PI * 2); g.fill();
}
drawLabel();

const vinyl = new ShaderMaterial({
  uniforms: { uSpin: { value: 0 }, uTime: { value: 0 }, uWave: { value: 0 }, uGlint: { value: 0 }, uLabel: { value: new CanvasTexture(labelCanvas) } },
  side: 2, // DoubleSide
  vertexShader: `varying vec2 vUv; void main() { vUv = uv; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
  fragmentShader: `
    precision highp float;
    varying vec2 vUv;
    uniform float uSpin, uTime, uWave, uGlint;
    uniform sampler2D uLabel;
    void main() {
      vec2 p = vUv * 2.0 - 1.0;
      float r = length(p);
      if (r > 1.0) discard;
      float a = atan(p.y, p.x);
      if (r < 0.345) { // the label turns with the record
        float c = cos(uSpin), s = sin(uSpin);
        vec2 q = mat2(c, s, -s, c) * p;
        gl_FragColor = vec4(texture2D(uLabel, q / 0.69 + 0.5).rgb, 1.0);
        return;
      }
      float g = 0.5 + 0.5 * sin(r * 540.0);
      float gap = 0.0; // the flat rings between tracks
      for (int i = 0; i < 5; i++) gap = max(gap, 1.0 - smoothstep(0.003, 0.009, abs(r - (0.43 + float(i) * 0.105))));
      float rim = smoothstep(0.955, 0.985, r);
      // the sheen stays facing the light while the grooves run under it
      float sheen = pow(abs(cos(a - 0.6)), 16.0) + 0.35 * pow(abs(sin(a - 0.6)), 80.0);
      float dust = 0.82 + 0.18 * sin(a * 9.0 - uSpin * 9.0 + r * 37.0);
      float lum = sheen * (0.22 + 0.78 * g) * (1.0 - 0.55 * gap) * dust * 0.85 + gap * 0.1 + rim * 0.06;
      vec3 col = vec3(0.028, 0.028, 0.034) + vec3(lum);
      float wr = 0.36 + fract(uTime * 0.5) * 0.64; // a search: a blue wave runs out through the grooves
      float w = exp(-pow((r - wr) / 0.04, 2.0)) * uWave;
      col = mix(col, vec3(0.114, 0.361, 0.878), clamp(w * (0.55 + 0.45 * g), 0.0, 1.0));
      float ga = abs(mod(a - uGlint * 6.2832 + 3.1416, 6.2832) - 3.1416); // an arrival: a green glint goes round once
      float gl = exp(-pow(ga / 0.3, 2.0)) * sin(uGlint * 3.1416) * (0.4 + 0.6 * g);
      col = mix(col, vec3(0.204, 0.702, 0.420), clamp(gl, 0.0, 1.0));
      gl_FragColor = vec4(col, 1.0);
    }`,
});
const edge = new ShaderMaterial({
  vertexShader: `void main() { gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
  fragmentShader: `void main() { gl_FragColor = vec4(0.07, 0.07, 0.08, 1.0); }`,
});

const record = new Group();
record.add(new Mesh(new CircleGeometry(1, 180), vinyl));
const rim = new Mesh(new CylinderGeometry(1, 1, 0.016, 180, 1, true), edge);
rim.rotation.x = Math.PI / 2;
record.add(rim);

// sleeve is the opening's paper sleeve: a block of blue, a black rule, Reid Miles style.
function sleeveTexture() {
  const c = document.createElement("canvas");
  c.width = c.height = 512;
  const g = c.getContext("2d");
  g.fillStyle = "#f7f7f5"; g.fillRect(0, 0, 512, 512);
  g.fillStyle = "#1d5ce0"; g.fillRect(36, 36, 440, 300);
  g.fillStyle = "#0b0b0c"; g.fillRect(36, 368, 260, 26); g.fillRect(36, 412, 170, 26);
  return new CanvasTexture(c);
}
const sleeve = new Mesh(new PlaneGeometry(2.1, 2.1), new ShaderMaterial({
  uniforms: { uMap: { value: sleeveTexture() }, uFade: { value: 1 } },
  transparent: true,
  vertexShader: `varying vec2 vUv; void main() { vUv = uv; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
  fragmentShader: `precision highp float; varying vec2 vUv; uniform sampler2D uMap; uniform float uFade;
    void main() { gl_FragColor = vec4(texture2D(uMap, vUv).rgb, uFade); }`,
}));

const scene = new Scene();
scene.add(record, sleeve);
const camera = new PerspectiveCamera(32, 1, 0.1, 50);
camera.position.set(0, 0, 6);

/* ---------- The print: a halftone screen over the whole scene ---------- */

const target = new WebGLRenderTarget(1, 1);
const halftone = new ShaderMaterial({
  uniforms: { tScene: { value: target.texture }, uRes: { value: new Vector2(1, 1) }, uCell: { value: 6 }, uLight: { value: 0 } },
  transparent: true, depthTest: false, depthWrite: false,
  vertexShader: `void main() { gl_Position = vec4(position.xy, 0.0, 1.0); }`,
  fragmentShader: `
    precision highp float;
    uniform sampler2D tScene;
    uniform vec2 uRes;
    uniform float uCell, uLight;
    void main() {
      mat2 rot = mat2(0.7071, 0.7071, -0.7071, 0.7071); // a 45° screen, like a printed sleeve
      vec2 q = rot * gl_FragCoord.xy;
      vec2 cell = (floor(q / uCell) + 0.5) * uCell;
      vec4 c = texture2D(tScene, (transpose(rot) * cell) / uRes);
      float lum = dot(c.rgb, vec3(0.299, 0.587, 0.114));
      // dark ground: light ink, with a floor so the black vinyl still shows its shape; paper: dark ink
      float cover = c.a * mix(max(lum, 0.1), 1.0 - lum * 0.9, uLight);
      float rad = sqrt(cover) * uCell * 0.64;
      float m = (1.0 - smoothstep(rad - 0.8, rad + 0.8, length(q - cell))) * smoothstep(0.0, 0.8, rad); // no dot where nothing is
      vec3 ink = mix(c.rgb / max(max(c.r, max(c.g, c.b)), 0.12), c.rgb, uLight);
      gl_FragColor = vec4(ink * m, m);
    }`,
});
const tri = new BufferGeometry();
tri.setAttribute("position", new Float32BufferAttribute([-1, -1, 0, 3, -1, 0, -1, 3, 0], 3));
const print = new Mesh(tri, halftone);
print.frustumCulled = false;
const printScene = new Scene();
printScene.add(print);
const flat = new OrthographicCamera(-1, 1, 1, -1, 0, 1);

/* ---------- Where it shows ---------- */

const view = $("#search-view");
const prompt = $("#prompt");
const opening = $("#opening");
const s = { mode: "home", spin: 0, speed: IDLE, time: 0, wave: 0, busy: false, glint: -1, glints: 0, tilt: new Vector2(), aim: new Vector2(), seen: false, raf: 0, last: 0, calm: 0, t0: 0, done: null };

function size() {
  const w = canvas.clientWidth, h = canvas.clientHeight;
  if (!w || !h) return;
  renderer.setSize(w, h, false);
  target.setSize(canvas.width, canvas.height);
  halftone.uniforms.uRes.value.set(canvas.width, canvas.height);
  halftone.uniforms.uCell.value = (s.mode === "opening" ? 5 : 4.5) * renderer.getPixelRatio();
  camera.aspect = w / h;
  camera.updateProjectionMatrix();
  if (s.mode === "opening") { // the mark prints just under the record
    const r = openingR() * (h / (2 * HALF_H));
    opening.style.setProperty("--mark-top", `${Math.round(h / 2 - r * 0.3 + r * 0.86 + 28)}px`);
  }
}

const HALF_H = Math.tan((camera.fov * Math.PI) / 360) * camera.position.z; // half the view's height, in world units

// place frames the record at home: big and leaning back at the top right, behind « Quel artiste ? »,
// half off the edge on a phone. Sizes are in CSS pixels, u turns them into world units.
function place(dt) {
  if (s.mode !== "home") return;
  const w = canvas.clientWidth, h = canvas.clientHeight, u = (2 * HALF_H) / h;
  const r = w < 700 ? w * 0.5 : Math.min(w * 0.22, h * 0.45);
  // a phone's edge is the screen's: the record bleeds off it; a wide stage can end inside the window
  const x = w < 700 ? w * 1.04 : w - r * 1.12, y = r * (w < 700 ? 0.55 : 0.62) + 8;
  record.scale.setScalar(r * u);
  record.position.set((x - w / 2) * u, (h / 2 - y) * u, 0);
  s.tilt.lerp(s.aim, 1 - Math.exp(-dt * 3));
  record.rotation.set(-0.98 + s.tilt.y * 0.16, 0.18 + s.tilt.x * 0.2, 0);
  sleeve.visible = false;
}

function frame(now) {
  s.raf = 0;
  const idle = s.mode === "home" && !s.busy && s.glint < 0;
  if (idle && prompt.hidden) { // results cover it: dim, then rest until the prompt or a search comes back
    s.calm ||= now;
    if (now - s.calm > 800) {
      s.last = 0;
      return;
    }
  } else s.calm = 0;
  if (idle && s.last && now - s.last < 30) { // turning slowly: 30 frames a second are enough
    wake();
    return;
  }
  const dt = Math.min(0.05, s.last ? (now - s.last) / 1000 : 0.016);
  s.last = now;
  s.time += dt;
  if (s.mode === "opening") playOpening(now);
  else {
    const want = s.busy ? RPM33 : IDLE;
    s.speed += (want - s.speed) * (1 - Math.exp(-dt * (s.busy ? 2.2 : 1.1)));
    s.wave += ((s.busy ? 1 : 0) - s.wave) * (1 - Math.exp(-dt * 3));
    canvas.classList.toggle("is-dim", prompt.hidden && !s.busy);
  }
  s.spin += s.speed * dt;
  if (s.glint >= 0) {
    s.glint += dt / 1.6;
    if (s.glint >= 1) s.glint = s.glints-- > 1 ? 0 : -1;
  }
  vinyl.uniforms.uSpin.value = s.spin;
  vinyl.uniforms.uTime.value = s.time;
  vinyl.uniforms.uWave.value = s.wave;
  vinyl.uniforms.uGlint.value = Math.max(0, s.glint);
  halftone.uniforms.uLight.value = lightScheme.matches ? 1 : 0;
  place(dt);
  renderer.setRenderTarget(target);
  renderer.render(scene, camera);
  renderer.setRenderTarget(null);
  renderer.render(printScene, flat);
  wake();
}

// wake keeps the loop running only while something shows: the opening, or the prompt on screen.
function wake() {
  const on = !document.hidden && (s.mode === "opening" || s.seen);
  if (on && !s.raf) s.raf = requestAnimationFrame(frame);
  if (!on && s.raf) {
    cancelAnimationFrame(s.raf);
    s.raf = 0;
    s.last = 0;
  }
}

function toHome() {
  s.mode = "home";
  view.prepend(canvas);
  size();
  wake();
}

/* ---------- The opening: the record slides out of its sleeve, turns, the mark prints ---------- */

const ease = (t) => (t <= 0 ? 0 : t >= 1 ? 1 : t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2);
const span = (t, a, b) => ease((t - a) / (b - a));

// openingR is the record's radius in the opening: as big as the screen allows with the sleeve beside it.
const openingR = () => Math.min(HALF_H * 0.46, HALF_H * camera.aspect * 0.62);

function playOpening(now) {
  const t = (now - s.t0) / 1000;
  const R = openingR();
  record.scale.setScalar(R);
  sleeve.scale.setScalar(R);
  sleeve.visible = t < 2.3;
  // the record slides out to the right as the pair stays centred, then the sleeve leaves to the left
  const out = span(t, 0.3, 1.4), centre = span(t, 1.4, 2.3);
  sleeve.position.set(-R * 0.525 * out - R * 2.6 * centre, R * 0.3, 0.02);
  sleeve.material.uniforms.uFade.value = 1 - centre;
  record.position.set(R * 0.525 * out * (1 - centre), R * 0.3, 0);
  record.rotation.set(-0.62 * centre, 0, 0);
  s.speed = IDLE + (RPM33 - IDLE) * span(t, 1.2, 2.4);
  s.wave = Math.sin(Math.PI * Math.min(1, Math.max(0, (t - 2.2) / 1.4)));
  if (t > 2.4) opening.classList.add("is-printed");
  if (t > 3.7) finishOpening();
}

function playOpeningNow() {
  s.mode = "opening";
  opening.dataset.playing = "1";
  s.t0 = performance.now();
  s.spin = 0;
  opening.hidden = false;
  opening.classList.remove("is-printed", "is-leaving");
  opening.prepend(canvas);
  for (const n of document.body.children) if (n !== opening && n.tagName !== "SCRIPT") n.inert = true;
  s.done = document.activeElement;
  $("#opening-skip").focus();
  size();
  wake();
}

function finishOpening() {
  if (s.mode !== "opening") return;
  try { localStorage.setItem("musaarr.opened", "1"); } catch { /* private mode: it plays again next time */ }
  for (const n of document.body.children) n.inert = false;
  opening.classList.add("is-printed", "is-leaving");
  delete opening.dataset.playing;
  s.mode = "leaving";
  setTimeout(() => {
    opening.hidden = true;
    opening.classList.remove("is-leaving");
    toHome();
    if (s.done?.isConnected && s.done !== document.body) s.done.focus({ preventScroll: true });
  }, 260);
}

/* ---------- Wiring ---------- */

new ResizeObserver(size).observe(canvas);
new IntersectionObserver(([e]) => {
  s.seen = e.isIntersecting;
  wake();
}).observe(canvas);
document.addEventListener("visibilitychange", wake);
document.addEventListener("musaarr:search", (e) => {
  s.busy = Boolean(e.detail);
  wake();
});
document.addEventListener("musaarr:arrived", (e) => {
  s.glints = Math.min(3, Number(e.detail) || 1);
  s.glint = 0;
  wake();
});
addEventListener("pointermove", (e) => {
  if (e.pointerType !== "mouse") return;
  s.aim.set((e.clientX / innerWidth) * 2 - 1, (e.clientY / innerHeight) * 2 - 1);
}, { passive: true });
if (typeof DeviceOrientationEvent !== "undefined" && typeof DeviceOrientationEvent.requestPermission !== "function") {
  addEventListener("deviceorientation", (e) => { // Android: the phone's tilt; iOS would ask, so it is left out
    if (e.gamma == null) return;
    s.aim.set(Math.max(-1, Math.min(1, e.gamma / 30)), Math.max(-1, Math.min(1, (e.beta - 45) / 30)));
  }, { passive: true });
}
canvas.addEventListener("webglcontextlost", () => {
  cancelAnimationFrame(s.raf);
  canvas.remove();
  finishOpening();
});
reduce.addEventListener("change", () => {
  if (!reduce.matches) return;
  finishOpening();
  canvas.remove(); // reduced motion turned on: no 3D at all
});
opening.addEventListener("pointerdown", finishOpening);
opening.addEventListener("keydown", (e) => {
  if (e.key !== "Tab") finishOpening();
});
$("#opening-skip").addEventListener("click", finishOpening);
const replay = $("#replay");
replay.hidden = false;
replay.addEventListener("click", playOpeningNow);

await document.fonts.ready; // the label's wordmark is drawn in Libre Franklin
drawLabel();
vinyl.uniforms.uLabel.value.needsUpdate = true;
if (!opening.hidden) playOpeningNow();
else toHome();
