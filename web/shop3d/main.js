// Le disquaire en 3D: walk the shop, lean over a bin, flip through the records and pull one out to
// open its album page. rack.js opens it on #disquaire and passes the shop's index of racks and callbacks
// (rackOf reads a rack's records).
// The aisles stand side by side (floor.js); you walk among them freely, and the records are stocked
// in the aisles near you only: the others show their crates filled with spines.
// three.js r187, the bundle the stage uses too (web/vendor/README.md lists what it carries)
import * as THREE from "../vendor/three.min.js";
import { RoomEnvironment, EffectComposer, RenderPass, UnrealBloomPass, OutputPass } from "../vendor/three.min.js";
import { loadModels } from "./world.js";
import { binMaterials, rackCrates, pose, animateCrate, look, itemWorld, itemAt } from "./bins.js";
import { Stockroom } from "./stock.js";
import { coverReserves } from "./covers.js";
import { planAisles } from "./aisles.js";
import { layout, walls } from "./floor.js";
import { furnish } from "./room.js";
import { floorMap } from "./map.js";
import { pace } from "./pace.js";

const touch = matchMedia("(pointer: coarse)").matches;
const still = matchMedia("(prefers-reduced-motion: reduce)");
const EYE = 1.62, PACE = 4.2, STRIDE = 2.6, TURN = 1.9; // m/s along a way, m/s with a key held, rad/s
const JUMP = 16; // metres: a longer way is jumped
const OPEN = "musaarr.shop3d.open"; // when the shop went on screen; still there at the next visit, the tab died in it

let ui, host, renderer, camera, composer, envTex, models, covers;
let scene = null, built = null, spots = [], setLights = null, platter = null, platterAt = null, mats = null, map = null;
let plan = [], floor = null, ways = null, store = null, here = 0, inside = -1; // the plan, where it stands, its bins (stock.js), the aisle you are in
let running = false, frameId = 0, opening = 0, visit = 0; // visit: a leave() during the loading cancels that enter()
const st = { mode: "walk", x: 0, z: 0, xT: 0, zT: 0, yaw: 0, yawT: 0, pitch: -0.1, pitchT: -0.1, focus: null, hover: null, crate: null, back: null,
  path: [], straight: false, look: null, goal: null, then: null, wheel: 0, wheelAt: 0, wheelNew: true, via: 0 }; // path: the way you walk, goal the crate at its end
const camPos = new THREE.Vector3(), camLook = new THREE.Vector3(), posT = new THREE.Vector3(), lookT = new THREE.Vector3();
const v = new THREE.Vector3(), aisleDir = new THREE.Vector3(), q = new THREE.Quaternion(), ray = new THREE.Raycaster(), ndc = new THREE.Vector2();
const keys = new Set(); // the walking keys held down
const clamp = (x, a, b) => Math.max(a, Math.min(b, x));
const $ = (s) => ui.querySelector(s);

// enter shows the shop; it builds the room the first time and again when the stock changed.
// A tab that dies in the shop (iOS closes it when memory runs out) reopens on it, and would die again:
// that visit throws instead, and rack.js shows the racks.
export async function enter(opts) {
  if (!armed && diedInside()) {
    mark(false); // « Voir la boutique en 3D » tries again
    throw new Error("the tab closed in the 3D shop last time");
  }
  armed = true;
  mark(true);
  const mine = ++visit;
  ui = opts.root;
  host = opts;
  if (!renderer) await setup();
  $("#shop3d-hide").checked = opts.hide;
  if (built !== opts.racks) await build(opts.racks, mine);
  if (mine !== visit) return;
  if (st.crate) { // back from an album page: the sleeve goes back in its crate
    st.crate.pulled = -1;
    pose(st.crate, true);
    panel();
  }
  hint();
  ui.focus({ preventScroll: true });
  if (!running) {
    running = true;
    last = performance.now();
    quality = pace(1, sharpest()); // a new visit starts sharp again
    sharpen(quality.ratio);
    frame();
  }
}

let armed = false; // this page put the mark: it is not a leftover

function mark(on) {
  try {
    if (on) localStorage.setItem(OPEN, String(Date.now()));
    else localStorage.removeItem(OPEN);
  } catch { /* private mode: no guard */ }
}

function diedInside() {
  try {
    const at = Number(localStorage.getItem(OPEN));
    return at > 0 && Date.now() - at < 120000; // older, it is a browser closed by hand
  } catch { return false; }
}

export function leave() {
  visit++;
  mark(false);
  clearTimeout(opening);
  keys.clear();
  running = false;
  cancelAnimationFrame(frameId);
}

async function setup() {
  renderer = new THREE.WebGLRenderer({ canvas: $("#shop3d-scene"), antialias: true, powerPreference: "high-performance" });
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.05;
  renderer.shadowMap.enabled = true;
  renderer.shadowMap.type = THREE.PCFShadowMap;
  camera = new THREE.PerspectiveCamera(54, 1, 0.03, 70);
  envTex = new THREE.PMREMGenerator(renderer).fromScene(new RoomEnvironment(), 0.04).texture;
  covers = coverReserves(renderer, touch ? { small: 512, big: 64 } : { small: 1024, big: 96 }); // 11 and 22 Mo on a phone, 22 and 34 Mo here
  resize();
  addEventListener("resize", resize);
  // hidden, the tab may be closed to free memory: not the shop's doing
  document.addEventListener("visibilitychange", () => { if (running) mark(!document.hidden); });
  addEventListener("pagehide", () => mark(false));
  addEventListener("pageshow", () => { if (running) mark(true); }); // back from the cache
  addEventListener("blur", () => keys.clear()); // a key let go in another window
  bindInput();
  await document.fonts.load('900 100px "Libre Franklin"');
}

const sharpest = () => Math.min(devicePixelRatio, touch ? 1.25 : 2);

// sharpen draws the shop at ratio canvas pixels per CSS pixel.
function sharpen(ratio) {
  renderer.setPixelRatio(ratio);
  composer?.setPixelRatio(ratio);
  resize();
}

function resize() {
  dirty = true;
  const w = innerWidth, h = innerHeight;
  camera.aspect = w / h;
  camera.fov = w / h < 0.8 ? 74 : 54; // a phone held upright sees the bins on both sides
  camera.updateProjectionMatrix();
  renderer.setSize(w, h, false);
  composer?.setSize(w, h);
}

// dispose frees what root holds on the GPU, but the materials in keep.
function dispose(root, keep = []) {
  root.traverse((o) => {
    if (o.isInstancedMesh) o.dispose(); // its instance buffers
    o.shadow?.dispose(); // a light's shadow map
    o.geometry?.dispose();
    for (const m of [o.material].flat()) {
      if (!m || keep.includes(m)) continue;
      for (const v of Object.values(m)) if (v?.isTexture) v.dispose(); // three uploads a shared one again when it is next drawn
      m.dispose();
    }
  });
}

// build puts up the shop for this index of racks: the room, and every aisle's units with their crates,
// the records left out until you come near (stockNear). mine is the visit it builds for: a later
// enter() or a leave() wins, and this one stops.
async function build(racks, mine) {
  const loading = $("#shop3d-loading");
  loading.hidden = false;
  models = await loadModels(renderer);
  const p = planAisles(racks), f = layout(p);
  // the aisle at the door is stocked first: its records also go on the walls
  const first = p.length ? await Promise.all(p[doorAisle(f, p)].units.map(({ rack }) => rack.full ?? host.rackOf(rack))) : [];
  if (mine !== visit) return;
  [plan, floor] = [p, f];
  highlight(null);
  if (scene) dispose(scene);
  scene = new THREE.Scene();
  scene.background = new THREE.Color(0x0d0a08);
  scene.fog = new THREE.FogExp2(0x1b130d, 0.035);
  scene.environment = envTex;
  scene.environmentIntensity = 0.22;
  const records = first.filter(Boolean).flatMap((r) => r.crates.flatMap((c) => c.items.filter((it) => it.record)));
  const room = furnish(scene, models, floor, plan, records);
  ({ spots, setLights, platter } = room);
  platterAt = platter.getWorldPosition(new THREE.Vector3());
  ways = walls(floor, room.obstacles);
  map = floorMap($("#shop3d-map"), floor, room.obstacles);
  mats = binMaterials(covers);
  covers.clear();
  store = new Stockroom(scene, plan, floor, mats, covers, dispose, (rack) => host.rackOf(rack));
  for (const p of composer?.passes ?? []) p.dispose?.();
  composer?.dispose();
  composer = null;
  if (!touch) {
    composer = new EffectComposer(renderer);
    // the scene is drawn into the composer's targets, not the canvas: its antialias does not reach them
    for (const rt of [composer.renderTarget1, composer.renderTarget2]) rt.samples = 4;
    composer.addPass(new RenderPass(scene, camera));
    // only the bulbs glow: at 0.9 the sleeves under a spot did too, and a haze covered the crate you dig in
    composer.addPass(new UnrealBloomPass(new THREE.Vector2(innerWidth, innerHeight), 0.3, 0.55, 2.5));
    composer.addPass(new OutputPass());
  }
  rayons(racks);
  const stay = built && ways.free(st.xT, st.zT); // the stock changed: you stay where you stood
  Object.assign(st, { mode: "walk", crate: null, focus: null, hover: null, path: [], goal: null, then: null });
  if (!stay) atDoor();
  $("#shop3d-dig").hidden = true;
  $("#shop3d-map").hidden = false;
  inside = -1;
  built = racks;
  loading.hidden = true;
  const n = racks.reduce((sum, r) => sum + (r.id === "mois" ? 0 : r.n), 0); // this month's records are also in their genre
  say(n ? t("shop3d.enter", { racks: tn("shop.racks", racks.filter((r) => r.n > 0).length), records: tn("records.count", n) }) : t("shop3d.empty"));
}

// doorAisle is the aisle of plan p in front of the door of floor f: the middle one of the first row.
const doorAisle = (f, p) => Math.floor((Math.min(f.cols, p.length) - 1) / 2);
// atDoor stands you inside the door, in front of the middle aisle, looking down it.
function atDoor() {
  const a = floor.aisles[doorAisle(floor, plan)];
  const x = a?.x ?? 0, z = floor.door + 0.6;
  Object.assign(st, { x, z, xT: x, zT: z, yaw: 0, yawT: 0, pitch: -0.1, pitchT: -0.1 });
  camPos.set(x, EYE, z + 0.5);
  camLook.set(x, 1.3, z - 5);
  looked = 0;
  dirty = true;
}

/* ---------- The aisles near you ---------- */

// stockNear stocks the aisles near you and gives back those left behind; never the one you dig in or walk to.
function stockNear() {
  if (!store.near(st.x, st.z, [st.crate, st.goal, st.focus].map((c) => c?.aisle))) return;
  if (st.hover?.gone) highlight(null);
  dirty = true;
}

// whereAmI follows the aisle you walk in: the rayons' buttons show its rayons, a screen reader hears them.
function whereAmI() {
  const i = store.aisleAt(st.x, st.z);
  if (i === inside) return;
  inside = i;
  if (i < 0) return;
  here = i;
  const racks = plan[i].units.map((u) => u.rack);
  for (const b of $("#shop3d-aisles").children) b.toggleAttribute("aria-current", racks.includes(b.rack));
  $("#shop3d-aisles [aria-current]")?.scrollIntoView({ block: "nearest", inline: "nearest" }); // a phone shows a few rayons at a time
  if (!st.goal) say(aisleSays(i)); // on the way to a crate, its own words come at the end
}

// aisleSays names aisle i and its rayons, for the screen reader.
function aisleSays(i) {
  const names = [...new Set(plan[i].units.map((u) => u.rack.name))];
  return t("shop3d.aisleEnter", { n: num(i + 1), total: num(plan.length), racks: new Intl.ListFormat(document.documentElement.lang).format(names) });
}

// rayons lists every rayon of the shop; one walks you to its first crate and leans you over it: from
// there the panel's buttons go on, a screen reader's way through the shop.
function rayons(racks) {
  $("#shop3d-aisles").replaceChildren(...racks.flatMap((rack) => {
    const a = plan.findIndex((x) => x.units.some((u) => u.rack === rack));
    if (a < 0) return []; // an empty rayon has no bins
    const b = document.createElement("button");
    b.type = "button";
    b.textContent = rack.name;
    b.rack = rack;
    b.addEventListener("click", async () => {
      const s = store;
      if ((await s.crateIn(a, 0)) && s === store) go(s.aisles[a].crates.find((c) => c.rack === rack));
    });
    return [b];
  }));
}

/* ---------- Walking ---------- */

function say(text) { $("#shop3d-live").textContent = text; }

function hint() {
  $("#shop3d-hint").textContent = st.mode === "dig"
    ? t(touch ? "shop3d.hintDigTouch" : "shop3d.hintDig")
    : t(touch ? "shop3d.hintWalkTouch" : "shop3d.hintWalk");
}

const glow = new THREE.Mesh(new THREE.BoxGeometry(0.62, 0.3, 0.38), new THREE.MeshBasicMaterial({ color: 0xffb060, transparent: true, opacity: 0.1, depthWrite: false, blending: THREE.AdditiveBlending }));
function highlight(crate) {
  if (st.hover === crate) return;
  st.hover = crate;
  glow.removeFromParent();
  if (crate) {
    glow.position.set(0, 0.15, 0);
    crate.group.add(glow);
  }
  if (renderer) renderer.domElement.style.cursor = crate ? "pointer" : "";
  dirty = true;
}

// near is yaw a as the shortest turn from where you look; toward the yaw that looks from x, z to tx, tz.
const near = (a) => st.yaw + Math.atan2(Math.sin(a - st.yaw), Math.cos(a - st.yaw));
const toward = (x, z, tx, tz) => near(Math.atan2(-(tx - x), -(tz - z)));

// standAt is where you stand to dig crate c, in its aisle level with it, and how you look at it there.
function standAt(c) {
  const p = { x: floor.aisles[c.aisle].x, z: c.at.z + 0.95 };
  return { p, look: { yawT: toward(p.x, p.z, c.at.x, c.at.z), pitchT: -0.42 } };
}

// halt stops a walk on its way: you take over.
function halt() {
  Object.assign(st, { path: [], goal: null, then: null, focus: null });
}

// step moves you by d metres the way you look (back when d < 0), along whatever is in the way.
function step(d) {
  const to = ways.slide({ x: st.xT, z: st.zT }, { x: st.xT - Math.sin(st.yawT) * d, z: st.zT - Math.cos(st.yawT) * d });
  st.xT = to.x;
  st.zT = to.z;
}

// walkTo walks you round the bins to p, then looks as look says and does then. A long way is jumped:
// the room dims and you are there.
function walkTo(p, look = null, then = null, goal = null) {
  const way = ways.path({ x: st.xT, z: st.zT }, p);
  if (!way.length) return;
  let len = 0;
  way.reduce((a, b) => ((len += Math.hypot(b.x - a.x, b.z - a.z)), b), { x: st.xT, z: st.zT });
  Object.assign(st, { path: way, look, then, goal, straight: way.length === 1 && len < 3 }); // a few steps: you keep your eyes on the crate
  if (st.straight && look) Object.assign(st, look);
  if (still.matches || len > JUMP) jump(way.at(-1));
}

let jumping = 0; // the jump on its way: a later one wins

async function jump(p) {
  const mine = ++jumping;
  const canvas = renderer.domElement;
  if (!still.matches) {
    canvas.style.opacity = "0";
    await new Promise((done) => setTimeout(done, 180)); // the fade of shop3d.css
    if (mine !== jumping) return;
    if (!st.path.length) { // you took over during the fade
      canvas.style.opacity = "";
      return;
    }
  }
  Object.assign(st, { x: p.x, z: p.z, xT: p.x, zT: p.z, path: [] }, st.look && { yaw: st.look.yawT, pitch: st.look.pitchT });
  aim(0);
  camPos.copy(posT); // no glide across the shop
  camLook.copy(lookT);
  canvas.style.opacity = "";
  arrive();
}

function arrive() {
  const then = st.then;
  if (st.look) Object.assign(st, st.look, { yawT: near(st.look.yawT) }); // you may have turned round on the way
  Object.assign(st, { look: null, then: null, goal: null });
  then?.();
}

// walk moves you along your way, or as the keys held say.
function walk(dt) {
  const ahead = (keys.has("f") ? 1 : 0) - (keys.has("b") ? 1 : 0), turn = (keys.has("l") ? 1 : 0) - (keys.has("r") ? 1 : 0);
  if (ahead || turn) {
    if (st.path.length) halt();
    st.yawT += turn * TURN * dt;
    step(ahead * STRIDE * dt);
    return;
  }
  const p = st.path[0];
  if (!p) return;
  const dx = p.x - st.xT, dz = p.z - st.zT, d = Math.hypot(dx, dz), reach = PACE * dt;
  if (!st.straight && d > 0.05) st.yawT = toward(st.xT, st.zT, p.x, p.z);
  if (d > reach) {
    st.xT += (dx / d) * reach;
    st.zT += (dz / d) * reach;
    return;
  }
  Object.assign(st, { xT: p.x, zT: p.z });
  st.path.shift();
  if (!st.path.length) arrive();
}

/* ---------- Digging ---------- */

// go leans you over crate c: from where you stand in its aisle, else after walking there.
function go(c) {
  if (!c) return;
  if (st.mode === "dig") {
    if (c.aisle === st.crate.aisle) return switchTo(c);
    standUp();
  }
  const { p, look } = standAt(c);
  halt();
  st.focus = c;
  if (store.aisleAt(st.xT, st.zT) === c.aisle && ways.clear({ x: st.xT, z: st.zT }, p)) {
    Object.assign(st, { xT: p.x, zT: p.z }, look);
    return dig(c);
  }
  walkTo(p, look, () => dig(c), c);
}

// focusCrate walks up to the crate dir away from the one you looked at last, else to the nearest.
async function focusCrate(dir) {
  if (!plan.length) return;
  const s = store, at = st.focus?.gone ? null : st.focus;
  let c = at ? await s.neighbour(at, dir) : null;
  if (!at) {
    const gap = (i) => s.gap(i, st.x, st.z), far = (x) => Math.hypot(x.at.x - st.x, x.at.z - st.z);
    if (!s.all.length) await s.crateIn(plan.reduce((b, _, i) => (gap(i) < gap(b) ? i : b), 0), 0);
    c = s.all.reduce((b, x) => (far(x) < far(b) ? x : b), s.all[0]);
  }
  if (!c || s !== store) return;
  const { p, look } = standAt(c);
  highlight(c);
  walkTo(p, look, null, c);
  st.focus = c;
  say(t("shop3d.atCrate", { rack: c.rack.name, label: c.label, records: tn("records.count", c.n) }));
}

function panel() {
  const c = st.crate;
  const it = c.items[c.k];
  const recs = c.items.slice(0, c.k + 1).filter((x) => x.record).length;
  const where = it.record ? t("crate.pos", { i: num(recs), n: num(c.n) }) : t("shop3d.divider");
  $("#shop3d-pos").textContent = `${c.rack.name} · ${t("shop3d.crate", { label: c.label })} · ${where}`;
  $("#shop3d-title").textContent = it.record ? it.record.title : it.divider;
  $("#shop3d-meta").textContent = it.record ? [it.record.artist, it.record.year || "", it.record.style].filter(Boolean).join(" · ")
    : t("shop3d.dividerBehind", { records: tn("records.count", it.n) });
  const tags = $("#shop3d-tags");
  tags.replaceChildren();
  const tag = (cls, text) => tags.append(Object.assign(document.createElement("span"), { className: `tag ${cls}`, textContent: text }));
  if (it.jp) tag("tag--jp", t("shop3d.jp"));
  if (it.owned) tag("tag--own", t("shop.owned"));
  $("#shop3d-open").hidden = !it.record;
  if (listed !== c) mirror(c);
  for (const [k, li] of [...$("#shop3d-items").children].entries()) li.firstChild.toggleAttribute("aria-current", k === c.k);
  const list = store.aisles[c.aisle].crates;
  const has = (dir) => !!list[list.indexOf(c) + dir] || !!plan[c.aisle + dir]; // the crate next door, maybe in the next aisle
  $("#shop3d-prev").disabled = !has(onScreen(-1));
  $("#shop3d-next").disabled = !has(onScreen(1));
}

let listed = null; // the crate mirror() lists

// mirror lists the crate you dig for a screen reader, which has no wheel nor arrows on a phone: a
// button per record or divider, its press brings it to the front, a second opens its album page.
function mirror(c) {
  listed = c;
  $("#shop3d-items").replaceChildren(...c.items.map((it, k) => {
    const li = document.createElement("li"), b = document.createElement("button");
    b.type = "button";
    b.tabIndex = -1; // the keys flip already: Tab does not walk 30 buttons nobody sees
    b.textContent = it.record ? `${it.record.title}, ${it.record.artist}` : t("shop3d.dividerNamed", { name: it.divider });
    b.addEventListener("click", () => (k === c.k ? open() : setK(k, false)));
    li.append(b);
    return li;
  }));
}

function dig(crate) {
  st.back = { xT: st.xT, zT: st.zT, yawT: st.yawT, pitchT: st.pitchT };
  highlight(null);
  st.mode = "dig";
  st.crate = crate;
  crate.pulled = -1;
  if (crate.k === 0 && !crate.items[0].record) crate.k = crate.front; // past the divider, onto the first record
  looked = 0; // its covers at 256 px on the next frame
  pose(crate, true);
  $("#shop3d-dig").hidden = false;
  $("#shop3d-tip").hidden = $("#shop3d-map").hidden = true;
  panel();
  hint();
  const it = crate.items[crate.k];
  say(t("shop3d.digging", { label: crate.label, rack: crate.rack.name }) + (it.record ? ` ${it.record.title}, ${it.record.artist}.` : ""));
}

// switchTo leans over another crate of the aisle without stepping back first: the one next door with
// ← →, a swipe or the panel's arrows (across the aisle at the end of a unit), or any crate clicked.
function switchTo(c) {
  if (!c || c === st.crate) return;
  clearTimeout(opening);
  st.crate.pulled = -1;
  pose(st.crate, false);
  if (c.rack !== st.crate.rack && !still.matches) {
    // across the aisle: stand up and turn to it first (aim), as you would, rather than sweep the camera over the floor
    v.subVectors(camLook, camPos).normalize();
    Object.assign(st, { x: camPos.x, z: camPos.z, yaw: Math.atan2(-v.x, -v.z), pitch: Math.asin(v.y), via: performance.now() + 450 });
  }
  st.mode = "walk";
  const { p, look } = standAt(c);
  Object.assign(st, { xT: p.x, zT: p.z, focus: c }, look);
  dig(c);
}

// onScreen turns a move to the left or right of the screen into one along the crates: leaning over
// the right side of the aisle, the crates after the one in front are on your left.
const onScreen = (dir) => (st.crate?.side > 0 ? -dir : dir);

// nextCrate leans over the crate dir away, in the next aisle past the end of this one; at says where
// in it: its first or last item, else where you left it.
async function nextCrate(dir, at) {
  const s = store, c = await s.neighbour(st.crate, dir);
  if (c && at) c.k = at === "first" ? 0 : c.items.length - 1; // dig() steps past a divider in front
  if (s === store) go(c);
}

// standUp puts the crate you dig back as it was and stands you up where you were.
function standUp() {
  clearTimeout(opening);
  const c = st.crate;
  c.pulled = -1;
  pose(c, false);
  st.mode = "walk";
  st.crate = null;
  Object.assign(st, st.back);
  $("#shop3d-dig").hidden = true;
  $("#shop3d-map").hidden = false;
  hint();
}

function back() {
  standUp();
  say(t("shop3d.back"));
}

// setK moves to item k. Past either end of the crate it goes on in the crate next door when cross
// lets it: a wheel spin or a held key stops at the end, the next notch or press carries on.
function setK(k, cross = true) {
  const c = st.crate;
  const end = k < 0 ? 0 : c.items.length - 1;
  if ((k < 0 || k > end) && cross && c.k === end) return nextCrate(k < 0 ? -1 : 1, k < 0 ? "last" : "first");
  const to = clamp(k, 0, c.items.length - 1);
  if (to === c.k) return;
  clearTimeout(opening); // flipping away cancels a sleeve on its way out
  c.k = to;
  c.pulled = -1;
  pose(c, true);
  panel();
  const it = c.items[to];
  clearTimeout(setK.t);
  setK.t = setTimeout(() => say(it.record ? `${it.record.title}, ${it.record.artist}` : t("shop3d.dividerNamed", { name: it.divider })), 350);
}

function nextDivider(dir) {
  const c = st.crate;
  for (let k = c.k + dir; k >= 0 && k < c.items.length; k += dir) if (c.items[k].divider) return k;
  return dir > 0 ? c.items.length - 1 : 0;
}

// open pulls the sleeve out of the crate, then opens its album page.
function open() {
  const c = st.crate;
  const it = c.items[c.k];
  if (!it.record) return;
  c.pulled = c.k;
  pose(c, true);
  say(t("shop3d.opening", { title: it.record.title }));
  clearTimeout(opening);
  opening = setTimeout(() => host.openAlbum(it.record), still.matches ? 0 : 380); // cancelled by back() or leave()
}

// random leans over a record you don't have: in a rayon drawn by its size, then anywhere in it.
async function random() {
  const s = store, racks = [...new Set(plan.flatMap((a) => a.units.map((u) => u.rack)))].filter((r) => r.id !== "mois");
  let at = Math.random() * racks.reduce((n, r) => n + r.n, 0);
  const rack = racks.find((r) => (at -= r.n) < 0) ?? racks.at(-1);
  if (!rack || !(rack.full ?? (await host.rackOf(rack))) || s !== store) return;
  const pool = []; // [aisle, unit, crate, item] of each record of it you don't have
  plan.forEach((a, ai) => a.units.forEach((u, ui) => u.rack === rack && rackCrates(rack).slice(u.from, u.to).forEach((g, ci) => g.items.forEach((it, k) => {
    if (it.record && !it.owned) pool.push([ai, ui, ci, k]);
  }))));
  if (!pool.length) return;
  const [ai, ui, ci, k] = pool[Math.floor(Math.random() * pool.length)];
  if (!(await s.crateIn(ai, 0)) || s !== store) return;
  const c = s.aisles[ai].units[ui].crates[ci];
  if (c === st.crate) return setK(k);
  c.k = k;
  go(c);
}

/* ---------- Input ---------- */

function aimAt(x, y) {
  ndc.set((x / innerWidth) * 2 - 1, -(y / innerHeight) * 2 + 1);
  ray.setFromCamera(ndc, camera);
  return ray;
}

function pick(x, y, targets) { return aimAt(x, y).intersectObjects(targets, false)[0]; }

// floorAt is the point of the floor under the pointer, or of the unit's base it meets first.
function floorAt(x, y) {
  const hit = pick(x, y, store.solids), { origin: o, direction: d } = ray.ray;
  const far = d.y < 0 ? -o.y / d.y : Infinity; // to the floor, y = 0
  if (hit && hit.distance < far) return hit.point;
  return far < Infinity ? v.copy(o).addScaledVector(d, far) : null;
}

const WALK_KEYS = { arrowup: "f", w: "f", z: "f", arrowdown: "b", s: "b", a: "l", q: "l", d: "r" };

const drag = { on: false, x: 0, y: 0, moved: 0, acc: 0, side: 0, flips: 0 }; // the pointer held down

function bindInput() {
  const canvas = $("#shop3d-scene");
  canvas.addEventListener("pointerdown", (e) => Object.assign(drag, { on: true, x: e.clientX, y: e.clientY, moved: 0, acc: 0, side: 0, flips: 0 }));
  addEventListener("pointerup", pointerUp);
  canvas.addEventListener("pointermove", (e) => (drag.on ? dragged(e) : hovered(e)));
  canvas.addEventListener("wheel", wheel, { passive: false });
  $("#shop3d-map").addEventListener("click", (e) => {
    const r = e.currentTarget.getBoundingClientRect();
    if (st.mode === "dig") standUp();
    halt();
    walkTo(map.at(e.clientX - r.left, e.clientY - r.top));
  });
  ui.addEventListener("keyup", (e) => keys.delete(WALK_KEYS[e.key.toLowerCase()]));
  ui.addEventListener("keydown", (e) => {
    // a button keeps Enter and Space, a box its keys; the arrows still flip after a click on ‹ ›
    if (e.key !== "Escape" && (e.target.closest("input") || (e.target.closest("button") && (e.key === "Enter" || e.key === " ")))) return;
    if (!(st.mode === "dig" ? digKey(e) : walkKey(e))) return; // Tab and the others keep their use
    e.preventDefault();
  });
  $("#shop3d-open").addEventListener("click", open);
  $("#shop3d-back").addEventListener("click", back);
  for (const [id, dir] of [["#shop3d-prev", -1], ["#shop3d-next", 1]]) {
    $(id).addEventListener("click", (e) => {
      nextCrate(onScreen(dir));
      if (e.currentTarget.disabled) ui.focus({ preventScroll: true }); // a disabled button drops the focus, and the keys with it
    });
  }
  $("#shop3d-exit").addEventListener("click", () => host.exit());
  $("#shop3d-random").addEventListener("click", random);
  $("#shop3d-hide").addEventListener("change", (e) => host.setHide(e.target.checked));
}

// pointerUp is a click or a tap: on a crate you lean over it, on the floor you walk there; over a
// crate, on a sleeve it comes to the front, a second time its album page opens.
function pointerUp(e) {
  if (!drag.on || !running) return;
  drag.on = false;
  if (drag.moved > 8) return;
  if (st.mode === "walk") {
    const c = pick(e.clientX, e.clientY, store.targets)?.object.userData.crate;
    if (c) return go(c);
    const p = floorAt(e.clientX, e.clientY);
    if (p) {
      halt();
      walkTo({ x: p.x, z: p.z });
    }
    return;
  }
  aimAt(e.clientX, e.clientY);
  const it = itemAt(ray, st.crate);
  if (it) it.k === st.crate.k ? open() : setK(it.k);
  else go(pick(e.clientX, e.clientY, store.targets)?.object.userData.crate);
}

// dragged: in the aisles you grab the room, it turns as the pointer goes and a finger up walks on;
// over a crate, a swipe down flips, sideways it goes to the crate next door.
function dragged(e) {
  const dx = e.clientX - drag.x, dy = e.clientY - drag.y;
  drag.moved += Math.abs(dx) + Math.abs(dy);
  drag.x = e.clientX;
  drag.y = e.clientY;
  if (st.mode === "walk" && drag.moved > 8) {
    halt();
    st.yawT += dx * 0.0045;
    if (e.pointerType === "mouse") st.pitchT = clamp(st.pitchT + dy * 0.003, -0.75, 0.35);
    else step(-dy * 0.016);
  } else if (st.mode === "dig") {
    drag.acc += dy;
    drag.side += dx;
    if (Math.abs(drag.side) > 70 && Math.abs(drag.side) > Math.abs(drag.acc) * 2) { // a swipe sideways: the crate next door
      nextCrate(onScreen(drag.side < 0 ? 1 : -1));
      drag.side = drag.acc = 0;
      drag.on = false;
    } else if (Math.abs(drag.acc) > 38) {
      setK(st.crate.k + Math.sign(drag.acc), !drag.flips++); // only a new swipe leaves the crate
      drag.acc = 0;
    }
  }
}

// hovered lights the crate under the mouse in the aisles and names it.
function hovered(e) {
  if (st.mode !== "walk" || e.pointerType !== "mouse") return;
  const c = pick(e.clientX, e.clientY, store.targets)?.object.userData.crate ?? null;
  highlight(c);
  const tip = $("#shop3d-tip");
  tip.hidden = !c;
  if (c) {
    tip.textContent = `${c.rack.name} · ${t("shop3d.crate", { label: c.label })} · ${tn("records.count", c.n)}`;
    tip.style.transform = `translate(${e.clientX + 16}px, ${e.clientY + 18}px)`;
  }
}

// wheel walks in the aisles; over a crate a notch flips a record.
function wheel(e) {
  e.preventDefault();
  const dy = e.deltaMode === 1 ? e.deltaY * 40 : e.deltaY;
  if (st.mode === "walk") {
    halt();
    step(clamp(-dy * 0.006, -0.8, 0.8));
    highlight(null);
    $("#shop3d-tip").hidden = true;
    return;
  }
  const now = performance.now();
  if (now - st.wheelAt > 220) Object.assign(st, { wheel: 0, wheelNew: true }); // a new turn of the wheel
  st.wheelAt = now;
  st.wheel += dy;
  if (Math.abs(st.wheel) >= 50) { // one record per notch, as in the racks
    setK(st.crate.k + Math.sign(st.wheel) * Math.max(1, Math.round(Math.abs(st.wheel) / 100)), st.wheelNew);
    st.wheel = 0;
    st.wheelNew = false;
  }
}

// walkKey handles a key in the aisles; false when it is not one of the shop's.
function walkKey(e) {
  const held = WALK_KEYS[e.key.toLowerCase()];
  if (held && !e.ctrlKey && !e.metaKey && !e.altKey) keys.add(held);
  else if (e.key === "ArrowRight") focusCrate(1);
  else if (e.key === "ArrowLeft") focusCrate(-1);
  else if (e.key === "PageDown" || e.key === "PageUp") { // the next aisle's entrance
    const a = floor.aisles[here + (e.key === "PageDown" ? 1 : -1)];
    if (a) walkTo({ x: a.x, z: a.z + 0.7 }, { yawT: toward(a.x, a.z + 0.7, a.x, a.z - 1), pitchT: -0.1 });
  } else if ((e.key === "Enter" || e.key === " ") && (st.hover || st.focus)) go(st.hover ?? st.focus);
  else if (e.key === "Escape") host.exit();
  else return false;
  return true;
}

// digKey handles a key over a crate; false when it is not one of the shop's.
function digKey(e) {
  const k = { ArrowDown: 1, ArrowUp: -1 }[e.key];
  if (k) setK(st.crate.k + k, !e.repeat);
  else if (e.key === "ArrowRight" || e.key === "ArrowLeft") nextCrate(onScreen(e.key === "ArrowRight" ? 1 : -1));
  else if (e.key === "PageDown" || e.key === "PageUp") setK(nextDivider(e.key === "PageDown" ? 1 : -1));
  else if (e.key === "Home") setK(0);
  else if (e.key === "End") setK(st.crate.items.length - 1);
  else if (e.key === "Enter" || e.key === " ") open();
  else if (e.key === "Escape" || e.key === "Backspace") back();
  else return false;
  return true;
}

/* ---------- Frame ---------- */

function aim(dt) {
  if (st.mode === "walk" || performance.now() < st.via) {
    const ease = (rate) => (still.matches ? 1 : 1 - Math.exp(-dt * rate)); // per second: the same walk at 60 Hz and 144 Hz
    st.x += (st.xT - st.x) * ease(7.7);
    st.z += (st.zT - st.z) * ease(7.7);
    st.yaw += (st.yawT - st.yaw) * ease(5);
    st.pitch += (st.pitchT - st.pitch) * ease(5);
    posT.set(st.x, EYE, st.z);
    lookT.set(-Math.sin(st.yaw) * Math.cos(st.pitch), Math.sin(st.pitch), -Math.cos(st.yaw) * Math.cos(st.pitch)).add(posT);
    return;
  }
  const c = st.crate;
  itemWorld(c, c.items[c.k], v);
  c.group.getWorldQuaternion(q);
  aisleDir.set(1, 0, 0).applyQuaternion(q);
  const out = c.pulled === c.k; // the panel covers the bottom of the screen: aim below the cover
  posT.copy(v).addScaledVector(aisleDir, out ? 0.78 : 0.64).add({ x: 0, y: out ? 0.26 : 0.6, z: 0 });
  lookT.copy(v).addScaledVector(aisleDir, 0.03).add({ x: 0, y: out ? 0.07 : 0.12, z: 0 });
}

let last = 0, looked = 0, quality = null, dirty = true, drawnAt = 0, drew = false;

// frame moves what moves, then draws the shop only when something changed: the camera, a crate,
// a cover come in, the record turning nearby. Still, it draws twice a second, for the room's
// pictures that arrive late.
function frame() {
  if (!running) return;
  const now = performance.now();
  if (drew) { // how long the frame drawn last took, to the next one: on a slow device, fewer pixels
    const r = quality.frame(Math.min(now - last, 100), now); // a tab hidden for a while is not a slow frame
    if (r) sharpen(r);
  }
  const dt = Math.min((now - last) / 1000, 0.05);
  last = now;
  if (st.mode === "walk") walk(dt);
  aim(dt);
  stockNear();
  whereAmI();
  const moved = camPos.distanceToSquared(posT) + camLook.distanceToSquared(lookT) > 1e-8;
  const a = still.matches ? 1 : 1 - Math.exp(-dt * 5.5);
  camPos.lerp(posT, a);
  if (now < st.via) camLook.copy(camPos).add(lookT).sub(posT); // crossing the aisle, the head turns by its angles: its gaze never sweeps the floor
  else camLook.lerp(lookT, a);
  camera.position.copy(camPos);
  camera.lookAt(camLook);
  if (looked-- <= 0) { // every few frames: which faces to show, and how sharp
    look(store.all, camPos, st.mode === "dig" ? st.crate : null, covers);
    looked = 4;
  }
  let busy = covers.flush() > 0 || moved || dirty;
  for (const c of store.all) if (c.moving) { animateCrate(c, still.matches ? 1 : dt); busy = true; }
  if (!still.matches) { // the record on the counter turns, unless motion is reduced
    platter.rotation.y -= dt * 3.49; // 33⅓ tours
    busy ||= camPos.distanceToSquared(platterAt) < 36; // seen from 6 m or less
  }
  drew = busy || now - drawnAt > 500;
  if (drew) {
    setLights(camPos, spots);
    composer ? composer.render() : renderer.render(scene, camera);
    if (st.mode === "walk") map.draw(camPos.x, camPos.z, st.yaw);
    drawnAt = now;
    dirty = false;
  }
  frameId = requestAnimationFrame(frame);
}
