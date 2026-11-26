// The bins: one oak unit per rayon down both sides of the aisle, crates of up to 30 records with a
// kraft divider before each artist who has several. Records are dug like in a shop: those seen lean
// toward you, the one you look at stands up, the rest lean back against the back of the crate.
// A unit draws all its sleeves in one instanced mesh and all its cards (dividers, crate tabs) in
// another; their pictures are layers of the reserves of covers.js, asked for by look().
import * as THREE from "../vendor/three.min.js";
import { fitText, SLEEVE, BACK, SPINES, sleeveGeometry, canvasTexture, wood, phone } from "./world.js";
import { face, faceMaterial, coverUrl, SMALL, BIG, KRAFT } from "./covers.js";
import { crateRuns, PITCH, PAD } from "./aisles.js";
import { REACH } from "./floor.js";

const CRATE = { len: 0.6, wid: 0.36, h: 0.24, wall: 0.014, front: 0.11, gap: 0.025 };
export const BASE_H = 0.78;
const LEAN_BACK = 0.19, STAND = 0.04;
const REC = { thick: 0.0045, h: 0.315 }, CARD = { thick: 0.0025, h: 0.36 };
const TAB = { x: -CRATE.len / 2 + CRATE.wall + 0.03, y: CRATE.wall, z: 0, angle: 0.095, ...CARD, scale: new THREE.Vector3(1, 1.17, 1.1) }; // its top stays in front of the back board
const SEE = phone ? 16 : 22, HAND = 3.2; // metres: a crate's face shown at 64 px, then at 256 px

const recGeo = sleeveGeometry(REC.thick);
const cardGeo = new THREE.BoxGeometry(CARD.thick, CARD.h, 0.3).translate(0, CARD.h / 2, 0);
cardGeo.clearGroups();
cardGeo.addGroup(0, 12, 0); // both faces: a divider shows its name front and back
cardGeo.addGroup(12, 24, 1);
const hashOf = (s) => [...s].reduce((h, c) => (h * 31 + c.codePointAt(0)) >>> 0, 7);
// artistKey files a record as the server does (racks.go, rackKey): « The Beatles » and « Beatles » together.
const artistKey = (r) => (r.sort || r.artist).normalize("NFD").replace(/\p{M}/gu, "").toLowerCase().trim().replace(/^the /, "");
const sameArtist = (a, b) => a?.record && b.record && artistKey(a.record) === artistKey(b.record);

// groupCrates merges the server's crates of 12 into crates of up to 30 records, as a shop packs them.
// The server cuts an artist every 12 records and stands a divider before each run: merged, Miles Davis
// had three in a row, so a run that goes on in the same crate adds its records to the divider before it.
export function groupCrates(rack) {
  return crateRuns(rack.crates).map(([from, to]) => {
    const run = rack.crates.slice(from, to), items = [];
    let div = -1;
    for (const c of run) {
      for (const [i, it] of c.items.entries()) {
        const before = items[div];
        if (it.divider && before?.divider === it.divider) {
          items[div] = { ...before, n: before.n + it.n };
          continue;
        }
        // the 13th record of an artist, alone in its run, opens the next server crate with no divider
        if (i === 0 && before && sameArtist(items.at(-1), it)) items[div] = { ...before, n: before.n + 1 };
        if (it.divider) div = items.length;
        items.push(it);
      }
    }
    const a = run[0].label.split("–")[0], b = run.at(-1).label.split("–").at(-1);
    return { label: a === b ? a : `${a}–${b}`, items, n: run.reduce((n, c) => n + c.n, 0) };
  });
}

// paintCard draws a kraft card with its name at the top: a crate's letters (top), or a divider's artist.
const paintCard = (text, top) => (ctx, s) => {
  const f = s / 512;
  ctx.fillStyle = "#c7a777";
  ctx.fillRect(0, 0, s, s);
  ctx.fillStyle = "rgba(90,60,30,.10)";
  for (let i = 0; i < 700; i++) ctx.fillRect((i * 97) % s, (i * 53) % s, 2 * f, 2 * f);
  ctx.fillStyle = "#2a1c10";
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  fitText(ctx, text, 900, s - 50 * f, (top ? 150 : 64) * f);
  ctx.fillText(text, s / 2, top ? s * 0.17 : s * 0.08);
};

function sideGeo() {
  const { len, h, front, wall } = CRATE;
  const s = new THREE.Shape();
  s.moveTo(len / 2, 0);
  s.lineTo(len / 2, front);
  s.lineTo(-len / 2, h + 0.05);
  s.lineTo(-len / 2, 0);
  return new THREE.ExtrudeGeometry(s, { depth: wall, bevelEnabled: false }).translate(0, 0, -wall / 2);
}

// crateShell is the crate's five boards, in the unit's space, ready to merge with the others.
function crateShell(z) {
  const { len, wid, h, wall, front } = CRATE;
  const at = (g, x, y, zz) => {
    g.translate(x, y + BASE_H, zz + z);
    return g.index ? g.toNonIndexed() : g; // extruded sides come without an index
  };
  return [
    at(new THREE.BoxGeometry(len, wall, wid), 0, wall / 2, 0),
    at(new THREE.BoxGeometry(wall, front, wid), len / 2 - wall / 2, front / 2, 0),
    at(new THREE.BoxGeometry(wall, h + 0.05, wid), -len / 2 + wall / 2, (h + 0.05) / 2, 0),
    at(sideGeo(), 0, 0, wid / 2 - wall / 2),
    at(sideGeo(), 0, 0, -wid / 2 + wall / 2),
  ];
}

// seenAngles is how far each item leans toward you once seen: the first rests on the front board,
// each next one on the one before it, so the pile touches instead of floating like shelves.
function seenAngles(items, step) {
  const { len, wall, front } = CRATE;
  const out = [];
  items.forEach((it, k) => {
    if (k === 0) {
      const dx = len / 2 - wall - it.x, dy = front - wall - (it.thick / 2) * Math.sin(1); // the pivot rises as it tips (place)
      return out.push(Math.acos(it.thick / 2 / Math.hypot(dx, dy)) - Math.atan2(dy, dx));
    }
    const p = items[k - 1], a = out[k - 1];
    const dy = ((p.thick - it.thick) / 2) * Math.sin(a); // how much higher the one before pivots (place)
    const room = step * Math.cos(a) - dy * Math.sin(a) - p.thick / 2; // from this pivot to the back of the one before, across it
    if (it.h <= p.h) { // its top edge comes to rest on the back of the one before
      const r = Math.hypot(it.h, it.thick / 2);
      return out.push(a + Math.max(0, Math.asin(Math.min(1, room / r)) - Math.atan2(it.thick / 2, it.h)));
    }
    const b = p.h + step * Math.sin(a) + dy * Math.cos(a), r = Math.hypot(room, b); // taller: the top of the one before rests on its face
    out.push(a + Math.max(0, Math.acos(Math.min(1, it.thick / 2 / r)) - Math.atan2(b, room)));
  });
  return out;
}

// instanced is one unit's mesh for n sleeves or cards, with the face attribute of each: its two layers
// (-1 until drawn), its colour while it has none, and whether it is owned.
function instanced(geo, mats, n) {
  const g = geo.clone();
  g.setAttribute("face", new THREE.InstancedBufferAttribute(new Float32Array(n * 4), 4));
  const im = new THREE.InstancedMesh(g, mats, n);
  im.castShadow = im.receiveShadow = true;
  im.userData.items = []; // instance -> item, for the pointer
  return im;
}

// dress gives instance i of im its face: src a cover to fetch, or paint a card to draw.
function dress(im, i, ink, owned, src, paint) {
  const attr = im.geometry.attributes.face;
  attr.setXYZW(i, -1, -1, ink, owned ? 1 : 0);
  const f = face(() => {
    attr.setXY(i, f.drawn[SMALL], f.drawn[BIG]);
    attr.needsUpdate = true;
  }, src, paint);
  return f;
}

const m4 = new THREE.Matrix4(), q = new THREE.Quaternion(), p = new THREE.Vector3(), Z = new THREE.Vector3(0, 0, 1), ONE = new THREE.Vector3(1, 1, 1);

// itemMatrix is an item's place in its crate: it tips over its bottom edge, x toward the aisle.
export function itemMatrix(it, out) {
  return out.compose(p.set(it.x, it.y, it.z), q.setFromAxisAngle(Z, it.angle), it.scale ?? ONE);
}

function write(c, it) {
  itemMatrix(it, m4);
  const g = c.group.position, e = m4.elements;
  e[12] += g.x; // the crate's place in the unit
  e[13] += g.y;
  e[14] += g.z;
  it.im.setMatrixAt(it.i, m4);
  it.im.instanceMatrix.needsUpdate = true;
}

// place sets an item at its angle and lift: it tips over its bottom edge, not through the floor.
function place(c, it) {
  it.y = CRATE.wall + it.lift + (it.thick / 2) * Math.abs(Math.sin(it.angle));
  write(c, it);
}

// itemWorld is where an item stands in the shop.
export function itemWorld(c, it, out) { return c.group.localToWorld(out.set(it.x, it.y, it.z)); }

// itemAt is the item of crate c the ray meets first, or null.
export function itemAt(ray, c) {
  for (const h of ray.intersectObjects(c.meshes, false)) {
    const it = h.object.userData.items[h.instanceId];
    if (it?.crate === c) return it;
  }
  return null;
}

// buildCrate fills crate i of a unit with its tab and items; ids counts the instances already used.
function buildCrate(g, z, ids, recs, cards) {
  const group = new THREE.Group();
  group.position.set(0, BASE_H, z);
  const step = Math.min(0.03, 0.35 / Math.max(1, g.items.length - 1)); // the last one clears the tab
  const tab = { ...TAB, im: cards, i: ids.card++ };
  tab.face = dress(cards, tab.i, KRAFT, false, null, paintCard(g.label, true));
  const items = g.items.map((it, k) => {
    const card = !!it.divider;
    const im = card ? cards : recs;
    const o = { ...it, k, im, i: card ? ids.card++ : ids.rec++, ...(card ? CARD : REC), angle: LEAN_BACK, lift: 0, angleT: LEAN_BACK, liftT: 0,
      x: CRATE.len / 2 - CRATE.wall - 0.15 - k * step, z: card ? 0 : ((hashOf(it.record.mbid) % 9) - 4) / 1000 };
    o.face = card ? dress(im, o.i, KRAFT, false, null, paintCard(it.divider, false)) : dress(im, o.i, hashOf(it.record.mbid) % KRAFT, it.owned, coverUrl(it), null);
    im.userData.items[o.i] = o;
    return o;
  });
  const hit = new THREE.Mesh(new THREE.BoxGeometry(CRATE.len, 0.5, CRATE.wid), new THREE.MeshBasicMaterial({ visible: false }));
  hit.position.y = 0.25;
  group.add(hit);
  const front = Math.max(0, items.findIndex((it) => it.record));
  const crate = { group, hit, tab, items, meshes: [recs, cards], seen: seenAngles(items, step), front, label: g.label, n: g.n, k: 0, pulled: -1 };
  hit.userData.crate = crate;
  write(crate, tab);
  for (const it of items) {
    it.crate = crate;
    place(crate, it);
  }
  return crate;
}

// unitMatrix places a unit in its aisle: its first crate toward the door on either side, so the
// rayons read A to Z (or 1 to 100) as you walk in, the records facing the aisle.
const unitMatrix = (u) => new THREE.Matrix4().compose(new THREE.Vector3(u.side * REACH, 0, u.z), new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 1, 0), u.side < 0 ? 0 : Math.PI), ONE);
const unitLength = (k) => k * PITCH + PAD;
const crateZ = (side, length, i) => -side * (length / 2 - PAD / 2 - CRATE.wid / 2 - i * PITCH); // the right side is turned round

// tint gives geometry g a colour per vertex and its wood's repeat: the bases and the crates are one
// mesh, one draw for an aisle's furniture. The colour darkens toward the floor, which keeps the light
// from a unit's foot: occlusion baked in.
function tint(g, hex, [ru, rv]) {
  const c = new THREE.Color(hex), uv = g.attributes.uv, y = g.attributes.position, n = uv.count, col = new Float32Array(n * 3);
  for (let i = 0; i < n; i++) {
    const k = 0.45 + 0.55 * Math.min(1, y.getY(i) / 0.55);
    col.set([c.r * k, c.g * k, c.b * k], i * 3);
    uv.setXY(i, uv.getX(i) * ru, uv.getY(i) * rv);
  }
  g.setAttribute("color", new THREE.BufferAttribute(col, 3));
  return g;
}

const PAINT = 0x6a8e74, RIM = 0x9a7050, KICK = 0x2a2018; // the bases painted green as the counter, an oak rim, a dark plinth

// base is a unit's base length long, its aisle face toward +x: a painted body on a plinth set back,
// an oak rim under the crates and a raised panel every 0.9 m on the aisle face. [[geometry, colour]]
function base(length) {
  const body = BASE_H - 0.1, out = [
    [new THREE.BoxGeometry(0.68, body, length - 0.04).translate(0, 0.1 + body / 2, 0), PAINT],
    [new THREE.BoxGeometry(0.62, 0.1, length - 0.1).translate(0, 0.05, 0), KICK],
    [new THREE.BoxGeometry(0.74, 0.04, length + 0.02).translate(0, BASE_H - 0.02, 0), RIM],
  ];
  const n = Math.max(1, Math.round(length / 0.9)), pw = length / n;
  for (let i = 0; i < n; i++) out.push([new THREE.BoxGeometry(0.018, body - 0.22, pw - 0.14).translate(0.349, 0.1 + body / 2, -length / 2 + pw * (i + 0.5)), PAINT]);
  return out;
}

// frames are an aisle's units with their crates still empty, placed by at (the aisle's place in the
// shop): geos, the oak bases and the crates' boards for one mesh (mats.frame), and blocks, a block of
// spines in each crate standing for its records until the aisle is stocked (buildBins). Only the
// server's crate counts are needed.
export function frames(units, at) {
  const geos = [], blocks = [];
  for (const u of units) {
    const runs = crateRuns(u.rack.crates).slice(u.from, u.to), length = unitLength(runs.length), m = at.clone().multiply(unitMatrix(u));
    for (const [g, hex] of base(length)) geos.push(tint(g.toNonIndexed().applyMatrix4(m), hex, [1, 1.4]));
    runs.forEach(([from, to], i) => {
      const z = crateZ(u.side, length, i), n = u.rack.crates.slice(from, to).reduce((t, c) => t + c.n, 0);
      geos.push(...crateShell(z).map((g) => tint(g.applyMatrix4(m), 0xc49a6c, [0.6, 0.6])));
      const thick = Math.min(0.42, 0.03 + n * 0.0105);
      blocks.push(new THREE.Matrix4().multiplyMatrices(m, new THREE.Matrix4().compose(new THREE.Vector3(CRATE.len / 2 - CRATE.wall - 0.12 - thick / 2, BASE_H + CRATE.wall + 0.15, z),
        q.setFromAxisAngle(Z, LEAN_BACK * 0.6), new THREE.Vector3(thick, 0.3, CRATE.wid - 0.05))));
    });
  }
  return { geos, blocks };
}

// buildUnit fills the unit of a rayon's crates groups with their records and cards, in local space,
// the aisle toward +x; frames stands its base and boards.
function buildUnit(rack, groups, mats, side) {
  const unit = new THREE.Group();
  const length = unitLength(groups.length);
  const nCards = groups.reduce((n, g) => n + 1 + g.items.filter((it) => it.divider).length, 0);
  const recs = instanced(recGeo, [mats.cover, BACK, SLEEVE], groups.reduce((n, g) => n + g.n, 0));
  const cards = instanced(cardGeo, [mats.card, SLEEVE], nCards);
  const ids = { rec: 0, card: 0 };
  const crates = groups.map((g, i) => {
    const c = buildCrate(g, crateZ(side, length, i), ids, recs, cards);
    Object.assign(c, { rack, side });
    unit.add(c.group);
    return c;
  });
  unit.add(recs, cards);
  return { unit, crates, length };
}

const grouped = new WeakMap(); // a rayon's crates of 30, worked out once for all its units

// rackCrates is a rayon's crates of 30 (groupCrates), the same array each time, once its records are
// in (rack.full).
export function rackCrates(rack) {
  if (!grouped.has(rack)) grouped.set(rack, groupCrates(rack.full));
  return grouped.get(rack);
}

// binMaterials are the bins' materials, made once for the shop: every aisle shares them.
export function binMaterials(covers) {
  return { frame: Object.assign(wood([1, 1], 0xffffff, 0.72), { vertexColors: true }), cover: faceMaterial(covers, false), card: faceMaterial(covers, true),
    spines: new THREE.MeshStandardMaterial({ map: spinesTexture(), roughness: 0.7 }) };
}

// spinesTexture: records seen from above or from their edge, side by side, for the crates not stocked.
function spinesTexture() {
  return canvasTexture(512, 64, (ctx, w, h) => {
    let seed = 5;
    const rand = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
    for (let x = 0; x < w;) {
      const t = 2 + Math.floor(rand() * 4);
      ctx.fillStyle = SPINES[Math.floor(rand() * SPINES.length)];
      ctx.fillRect(x, 0, t, h);
      x += t + (rand() < 0.3 ? 1 : 0);
    }
  });
}

// buildBins stocks the units of an aisle (planAisles) under parent, the aisle's group: each a
// rayon's crates [from, to), in the frames stood by frames(). Its racks' records must be in (rack.full).
export function buildBins(parent, units, mats) {
  parent.updateMatrixWorld(true);
  return units.map((u) => {
    const { unit, crates, length } = buildUnit(u.rack, rackCrates(u.rack).slice(u.from, u.to), mats, u.side);
    unit.applyMatrix4(unitMatrix(u));
    parent.add(unit);
    unit.updateMatrixWorld(true);
    for (const c of crates) c.at = c.group.getWorldPosition(new THREE.Vector3());
    return { rack: u.rack, unit, crates, side: u.side, z: u.z, length, from: u.from };
  });
}

// pose sets where each item of a crate should be: k is the one in front of you.
export function pose(crate, digging) {
  crate.items.forEach((it, i) => {
    it.angleT = !digging && crate.k === 0 ? LEAN_BACK : i < crate.k ? -crate.seen[i] : i === crate.k ? STAND : LEAN_BACK;
    it.liftT = digging && i === crate.pulled ? 0.2 : digging && i === crate.k ? 0.035 : 0;
  });
  crate.moving = true;
}

export function animateCrate(crate, dt) {
  const a = dt >= 1 ? 1 : 1 - Math.exp(-dt * 16);
  let still = true;
  for (const it of crate.items) {
    const da = it.angleT - it.angle, dl = it.liftT - it.lift;
    if (Math.abs(da) + Math.abs(dl) < 1e-4) continue;
    still = false;
    it.angle += da * a;
    it.lift += dl * a;
    place(crate, it);
  }
  crate.moving = !still;
}

let order = [], of = null; // the crates by distance, kept sorted from one look to the next

// look asks the reserves for the faces to show from eye, the most needed first: the crate you dig at
// 256 px and it and its neighbours at 64, then the tab and the face in front of each crate around,
// nearest first, at 256 px within reach.
export function look(crates, eye, dug, covers) {
  covers.next();
  if (dug) {
    for (const it of dug.items) covers.want(it.face, BIG);
    const i = crates.indexOf(dug);
    for (const c of [dug, crates[i - 1], crates[i + 1]]) if (c) for (const it of c.items) covers.want(it.face, SMALL);
  }
  if (of !== crates) [order, of] = [[...crates], crates];
  for (const c of order) c.d = c.at.distanceToSquared(eye);
  order.sort((a, b) => a.d - b.d);
  for (const c of order) {
    if (c.d > SEE * SEE) break;
    for (const f of [c.items[c.k].face, c.tab.face]) {
      covers.want(f, SMALL);
      if (c.d < HAND * HAND) covers.want(f, BIG);
    }
  }
}
