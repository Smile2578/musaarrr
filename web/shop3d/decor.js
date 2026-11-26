// The room of a Paris record shop round the floor plan: a parquet and plaster walls over a panelled
// dado, lit by every lamp of the shop as baked once (bake.js, lightmaps); the shop front seen from
// inside, its windows on a street at night; framed posters and neon signs. The bins, the furniture and
// the lamps themselves stand elsewhere (stock.js, world.js).
import * as THREE from "../vendor/three.min.js";
import { mergeGeometries } from "../vendor/three.min.js";
import { HEIGHT, pbr, canvasTexture, mesh, phone, fitText } from "./world.js";
import { irradiance, shade, texels } from "./bake.js";

const TUNGSTEN = [1, 0.74, 0.47]; // the colour of the light the lamps bake: about 2 700 K
const PAINT = 0x1f3a2e, DADO = 1.02; // the green of the woodwork; the height of the panelling
const CELL = phone ? 0.16 : 0.1; // metres per texel of the lightmaps

// lightmap is the light lamps cast on a rectangle of the room, size metres from corner p0 (u = v = 0)
// along across (u) and up (v), facing n, darkened under rects (the floor only): a texture for a
// material's lightMap, which lights it on top of the shop's few real lights.
function lightmap(p0, across, up, size, n, lamps, rects = []) {
  const nu = Math.max(2, Math.round(size[0] / CELL)), nv = Math.max(2, Math.round(size[1] / CELL));
  const cu = size[0] / nu, cv = size[1] / nv;
  const s = { o: p0.map((c, i) => c + across[i] * cu / 2 + up[i] * cv / 2), u: across.map((c) => c * cu), v: up.map((c) => c * cv), n, nu, nv };
  const g = irradiance(s, lamps);
  if (rects.length) shade(g, s, rects, 0.55);
  else for (let j = 0; j < nv; j++) { // a wall: darker at its foot and under the ceiling
    const y = (j + 0.5) * cv, k = (1 - 0.45 * Math.exp(-y / 0.25)) * (1 - 0.35 * Math.exp(-(HEIGHT - y) / 0.35));
    for (let i = 0; i < nu; i++) g[j * nu + i] *= k;
  }
  const t = new THREE.DataTexture(texels(g, TUNGSTEN, 0.55), nu, nv);
  Object.assign(t, { magFilter: THREE.LinearFilter, minFilter: THREE.LinearFilter, needsUpdate: true });
  return t;
}

// buildRoom puts up the floor, walls, ceiling and shop front of floor f between the door (front) and
// the back wall, lit by lamps; rects are what stands on the floor (the bins' bases, the furniture).
export function buildRoom(scene, f, front, lamps, rects) {
  const w = f.width, back = f.back, d = front - back, half = w / 2;
  const light = lightmap([-half, 0, front], [1, 0, 0], [0, 0, -1], [w, d], [0, 1, 0], lamps,
    [...rects, { x0: -half - 1, x1: -half, z0: back, z1: front }, { x0: half, x1: half + 1, z0: back, z1: front }, { x0: -half, x1: half, z0: back - 1, z1: back }]);
  const floor = mesh(new THREE.PlaneGeometry(w, d), pbr("herringbone_parquet", [w / 1.8, d / 1.8], { lightMap: light, color: 0xc4c4c4, roughness: 0.72 }), false);
  floor.rotation.x = -Math.PI / 2;
  floor.position.z = (front + back) / 2;
  scene.add(floor);
  // each wall: [corner at u = 0, the way along it, its length, the way it faces]
  const sides = [[[-half, 0, front], [0, 0, -1], d, [1, 0, 0]], [[half, 0, back], [0, 0, 1], d, [-1, 0, 0]], [[-half, 0, back], [1, 0, 0], w, [0, 0, 1]], [[half, 0, front], [-1, 0, 0], w, [0, 0, -1]]];
  const plaster = (len) => pbr("painted_plaster_wall", [len / 2.6, HEIGHT / 2.6], { color: 0xcfae86 });
  sides.forEach(([p0, along, len, n], k) => {
    const light = lightmap(p0, along, [0, 1, 0], [len, HEIGHT], n, lamps);
    scene.add(wall(p0, along, len, n, k === 3 ? shopFront(len) : [], plaster(len), light));
  });
  ceiling(scene, w, front, back);
}

// wall is a wall from p0 along a for len metres, facing n: plaster over a panelled dado painted green,
// both lit by light (their uvs are where they stand on the wall), with holes left where cut says
// ([[u0, u1, y0, y1]], metres: the shop front's windows).
function wall(p0, a, len, n, cut, plaster, light) {
  const parts = [], paint = [];
  const at = (u, y, depth, w, h, t) => new THREE.BoxGeometry(w, h, t).translate(u - len / 2, y - HEIGHT / 2, depth + t / 2);
  const spans = holes(len, cut);
  for (const [u0, u1] of spans) parts.push(new THREE.PlaneGeometry(u1 - u0, HEIGHT).translate((u0 + u1) / 2 - len / 2, 0, 0));
  for (const [u0, u1, y0, y1] of cut) {
    parts.push(new THREE.PlaneGeometry(u1 - u0, y0).translate((u0 + u1) / 2 - len / 2, y0 / 2 - HEIGHT / 2, 0));
    parts.push(new THREE.PlaneGeometry(u1 - u0, HEIGHT - y1).translate((u0 + u1) / 2 - len / 2, (y1 + HEIGHT) / 2 - HEIGHT / 2, 0));
  }
  for (const [u0, u1] of spans) { // the dado: a skirting, a rail on top, a panel every 0.9 m
    paint.push(at((u0 + u1) / 2, 0.08, 0, u1 - u0, 0.16, 0.025), at((u0 + u1) / 2, DADO, 0, u1 - u0, 0.06, 0.035));
    const n0 = Math.max(1, Math.round((u1 - u0) / 0.9)), pw = (u1 - u0) / n0;
    for (let i = 0; i < n0; i++) paint.push(at(u0 + pw * (i + 0.5), (DADO + 0.16) / 2, 0, pw - 0.14, DADO - 0.36, 0.014));
    paint.push(at((u0 + u1) / 2, DADO / 2, -0.005, u1 - u0, DADO, 0.008));
  }
  paint.push(at(len / 2, HEIGHT - 0.07, 0, len, 0.14, 0.06)); // the cornice
  const geo = mergeGeometries([mergeGeometries(parts), mergeGeometries(paint)], true);
  wallUvs(geo, len);
  const m = mesh(geo, [Object.assign(plaster, { lightMap: light }), new THREE.MeshStandardMaterial({ color: PAINT, roughness: 0.45, lightMap: light })], false);
  m.position.set(p0[0] + (a[0] * len) / 2, HEIGHT / 2, p0[2] + (a[2] * len) / 2);
  m.rotation.y = Math.atan2(n[0], n[2]);
  return m;
}

// holes are the spans of a wall len long left whole between the cuts: [[u0, u1]].
function holes(len, cut) {
  const out = [];
  let u = 0;
  for (const [u0, u1] of [...cut].sort((a, b) => a[0] - b[0])) {
    if (u0 > u) out.push([u, u0]);
    u = u1;
  }
  if (u < len) out.push([u, len]);
  return out;
}

// wallUvs makes the uvs of a wall's geometry, centred on it, where each point stands on the wall:
// the plaster's picture repeats over them, the lightmap covers them once.
function wallUvs(geo, len) {
  const p = geo.attributes.position, uv = geo.attributes.uv;
  for (let i = 0; i < p.count; i++) uv.setXY(i, p.getX(i) / len + 0.5, p.getY(i) / HEIGHT + 0.5);
}

function ceiling(scene, w, front, back) {
  const d = front - back;
  const c = mesh(new THREE.PlaneGeometry(w, d), new THREE.MeshStandardMaterial({ color: 0x241a13, roughness: 0.9 }), false);
  c.rotation.x = Math.PI / 2;
  c.position.set(0, HEIGHT, (front + back) / 2);
  const beams = [];
  for (let z = back + 1.5; z < front; z += 2.5) beams.push(new THREE.BoxGeometry(w, 0.24, 0.2).translate(0, HEIGHT - 0.12, z));
  scene.add(c, mesh(mergeGeometries(beams), new THREE.MeshStandardMaterial({ color: 0x33241a, roughness: 0.8 }), false));
}

const PIER = 0.4, SPRING = 2.35, CROWN = 2.85, DEEP = 0.36; // metres: an arcade's piers, where its arches spring and rise to

// arcades part the shop in rooms between two rows of aisles (f.arches): a plastered wall with a basket
// arch over each aisle, on piers between the aisles and against the walls. One mesh for all.
export function arcades(scene, f) {
  if (!f.arches.length) return;
  const half = f.width / 2;
  const geos = f.arches.map(({ z, xs }) => {
    const edges = [-half + 0.3, ...xs.flatMap((x) => [x - PIER / 2, x + PIER / 2]), half - 0.3]; // the arches' sides, in pairs
    const s = new THREE.Shape().moveTo(-half, 0).lineTo(-half, HEIGHT).lineTo(half, HEIGHT).lineTo(half, 0);
    for (let i = edges.length - 2; i >= 0; i -= 2) { // back along the floor, up and over each arch
      const [x0, x1] = [edges[i], edges[i + 1]];
      s.lineTo(x1, 0).lineTo(x1, SPRING).absellipse((x0 + x1) / 2, SPRING, (x1 - x0) / 2, CROWN - SPRING, 0, Math.PI, false);
      s.lineTo(x0, 0);
    }
    return new THREE.ExtrudeGeometry(s, { depth: DEEP, bevelEnabled: false, curveSegments: 20 }).translate(0, 0, z - DEEP / 2);
  });
  scene.add(mesh(mergeGeometries(geos), pbr("painted_plaster_wall", [1 / 2.6, 1 / 2.6], { color: 0xcfae86 })));
}

// piers are what an arcade's piers take of the floor: obstacles.
export const piers = (f) => f.arches.flatMap(({ z, xs }) => [...xs, -f.width / 2 + 0.15, f.width / 2 - 0.15]
  .map((x) => ({ x0: x - PIER / 2, x1: x + PIER / 2, z0: z - DEEP / 2, z1: z + DEEP / 2 })));

// shopFront is where the front wall, len long, opens on the street: a bay every 3 m, the door in the
// middle one, the ends left for the shelves. [[u0, u1, y0, y1]]
function shopFront(len) {
  const n = Math.max(1, Math.floor((len - 2.4) / 3)), u0 = (len - n * 3) / 2;
  return Array.from({ length: n }, (_, i) => [u0 + i * 3 + 0.35, u0 + i * 3 + 2.65, i === (n - 1) >> 1 ? 0.02 : 0.62, 2.72]);
}

// street fills the shop front's bays (on the wall at z = front, len long) with their frames and the
// street outside at night behind the glass.
export function street(scene, w, front) {
  const bays = shopFront(w), frames = [], glass = [];
  const night = canvasTexture(phone ? 1024 : 2048, phone ? 256 : 512, paintNight);
  for (const [u0, u1, y0, y1] of bays) {
    const x0 = w / 2 - u1, x1 = w / 2 - u0, cx = (x0 + x1) / 2, bw = x1 - x0, door = y0 < 0.1;
    const g = new THREE.PlaneGeometry(bw, y1 - y0).translate(cx, (y0 + y1) / 2, front + 0.04);
    const uv = g.attributes.uv;
    for (let i = 0; i < uv.count; i++) uv.setXY(i, (cx + (uv.getX(i) - 0.5) * bw) / w + 0.5, uv.getY(i));
    glass.push(g);
    const bar = (x, y, sx, sy) => frames.push(new THREE.BoxGeometry(sx, sy, 0.09).translate(x, y, front - 0.02));
    for (const x of [x0, x1, ...(door ? [cx] : [x0 + bw / 3, x0 + (2 * bw) / 3])]) bar(x, (y0 + y1) / 2, 0.07, y1 - y0);
    bar(cx, y1, bw + 0.07, 0.09);
    bar(cx, y0, bw + 0.07, door ? 0.04 : 0.1);
    if (!door) bar(cx, 2.15, bw, 0.05); // a transom
  }
  const pane = mesh(mergeGeometries(glass), new THREE.MeshBasicMaterial({ map: night, side: THREE.BackSide }), false);
  scene.add(pane, mesh(mergeGeometries(frames), new THREE.MeshStandardMaterial({ color: PAINT, roughness: 0.4 })));
}

// paintNight is a street at night through the glass: the buildings across it, their lit windows and
// the lamps, a little blurred.
function paintNight(ctx, w, h) {
  const sky = ctx.createLinearGradient(0, 0, 0, h);
  sky.addColorStop(0, "#0b1424");
  sky.addColorStop(0.62, "#1b2638");
  sky.addColorStop(1, "#3a2c22");
  ctx.fillStyle = sky;
  ctx.fillRect(0, 0, w, h);
  let seed = 7;
  const rand = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
  ctx.filter = `blur(${w / 700}px)`;
  for (let x = 0; x < w; x += w / 9) { // the buildings across the street, their windows
    ctx.fillStyle = "#121a27";
    ctx.fillRect(x, h * 0.04, w / 9 - w / 160, h * 0.62);
    for (let i = 0; i < 4; i++) for (let k = 0; k < 4; k++) {
      ctx.fillStyle = rand() < 0.4 ? `rgba(255,${180 + rand() * 40},${110 + rand() * 40},0.75)` : "#1a2232";
      ctx.fillRect(x + w / 60 + k * w / 42, h * (0.08 + i * 0.13), w / 70, h * 0.08);
    }
  }
  for (let x = w / 14; x < w; x += w / 7) { // the street lamps
    const g = ctx.createRadialGradient(x, h * 0.5, 0, x, h * 0.5, h * 0.3);
    g.addColorStop(0, "rgba(255,200,130,0.9)");
    g.addColorStop(1, "rgba(255,200,130,0)");
    ctx.fillStyle = g;
    ctx.fillRect(x - h * 0.3, h * 0.2, h * 0.6, h * 0.6);
  }
  ctx.filter = "none";
}

// POSTERS are the shop's posters: a genre, a year, a few shapes, no words of a language.
const POSTERS = [
  ["JAZZ", "1959", "#e9dcc0", "#1d1d1b", "#c8432c"], ["SOUL", "1967", "#f0b323", "#2a1d14", "#1d5ce0"],
  ["DUB", "1976", "#34b36b", "#101510", "#f0e6cf"], ["DISCO", "1978", "#ef5a3c", "#fff3df", "#2b2622"],
  ["BOSSA", "1963", "#f3e7c9", "#1f3a2e", "#e0a040"], ["FUNK", "1972", "#2b2622", "#f0b323", "#ef5a3c"],
  ["TECHNO", "1991", "#121212", "#e9e1cf", "#34b36b"], ["SALSA", "1975", "#c8432c", "#fff0d8", "#f0b323"],
];

// posters hangs framed posters at slots ({ x, y, z, ry }), all painted in one picture: two draws.
export function posters(scene, slots) {
  if (!slots.length) return;
  const pw = phone ? 256 : 384, ph = pw * 1.4;
  const atlas = canvasTexture(pw * 4, ph * 2, (ctx) => POSTERS.forEach((p, i) => {
    ctx.save();
    ctx.translate((i % 4) * pw, Math.floor(i / 4) * ph);
    paintPoster(ctx, pw, ph, ...p);
    ctx.restore();
  }));
  const faces = [], frames = [], m = new THREE.Matrix4();
  slots.forEach((s, k) => {
    const i = k % POSTERS.length, g = new THREE.PlaneGeometry(0.5, 0.7), uv = g.attributes.uv;
    for (let j = 0; j < uv.count; j++) uv.setXY(j, ((i % 4) + uv.getX(j)) / 4, 1 - (Math.floor(i / 4) + 1 - uv.getY(j)) / 2);
    m.makeRotationY(s.ry).setPosition(s.x, s.y, s.z);
    faces.push(g.translate(0, 0, 0.022).applyMatrix4(m));
    for (const [x, y, w, h] of [[0, 0.37, 0.58, 0.04], [0, -0.37, 0.58, 0.04], [-0.27, 0, 0.04, 0.78], [0.27, 0, 0.04, 0.78]]) {
      frames.push(new THREE.BoxGeometry(w, h, 0.035).translate(x, y, 0.0175).applyMatrix4(m));
    }
  });
  scene.add(mesh(mergeGeometries(faces), new THREE.MeshStandardMaterial({ map: atlas, roughness: 0.35 }), false),
    mesh(mergeGeometries(frames), new THREE.MeshStandardMaterial({ color: 0x17120e, roughness: 0.5 }), false));
}

function paintPoster(ctx, w, h, word, year, bg, ink, accent) {
  ctx.fillStyle = bg;
  ctx.fillRect(0, 0, w, h);
  ctx.fillStyle = accent;
  ctx.beginPath();
  ctx.arc(w * 0.5, h * 0.4, w * 0.3, 0, Math.PI * 2);
  ctx.fill();
  ctx.fillStyle = ink;
  ctx.fillRect(w * 0.08, h * 0.08, w * 0.84, h * 0.012);
  for (let i = 0; i < 5; i++) ctx.fillRect(w * 0.2 + i * w * 0.13, h * 0.32, w * 0.025, h * 0.18); // a few bars of a score
  ctx.textAlign = "center";
  fitText(ctx, word, 900, w * 0.84, w * 0.3);
  ctx.fillText(word, w / 2, h * 0.82);
  ctx.font = `500 ${Math.round(w * 0.07)}px "Libre Franklin", system-ui, sans-serif`;
  ctx.fillText(year, w / 2, h * 0.92);
}

// neons hangs signs written in light at list ({ text, x, y, z, ry, color }): one picture, one draw;
// the bloom of a computer makes them glow, a phone shows the glow painted round them.
export function neons(scene, list) {
  const cw = phone ? 512 : 1024, ch = cw / 4;
  const atlas = canvasTexture(cw, ch * list.length, (ctx) => list.forEach((n, i) => paintNeon(ctx, n.text, n.color, i * ch, cw, ch)));
  const geos = list.map((n, i) => {
    const g = new THREE.PlaneGeometry(1.6, 0.4), uv = g.attributes.uv;
    for (let j = 0; j < uv.count; j++) uv.setXY(j, uv.getX(j), 1 - (i + 1 - uv.getY(j)) / list.length);
    return g.applyMatrix4(new THREE.Matrix4().makeRotationY(n.ry).setPosition(n.x, n.y, n.z));
  });
  const mat = new THREE.MeshBasicMaterial({ map: atlas, transparent: true, blending: THREE.AdditiveBlending, depthWrite: false });
  mat.color.setScalar(phone ? 1.2 : 3.2); // over the bloom's threshold on a computer
  scene.add(new THREE.Mesh(mergeGeometries(geos), mat));
}

function paintNeon(ctx, text, color, top, w, h) {
  ctx.save();
  ctx.translate(0, top);
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  fitText(ctx, text, 300, w * 0.86, h * 0.62);
  ctx.lineJoin = ctx.lineCap = "round";
  ctx.shadowColor = color;
  for (const [blur, width, style] of [[h * 0.22, h * 0.05, color], [h * 0.08, h * 0.035, color], [0, h * 0.014, "#fff8ec"]]) {
    ctx.shadowBlur = blur;
    ctx.lineWidth = width;
    ctx.strokeStyle = style;
    ctx.strokeText(text, w / 2, h / 2);
  }
  ctx.restore();
}
