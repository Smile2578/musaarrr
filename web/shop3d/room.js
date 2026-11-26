// The room around the floor plan (floor.js): its walls, floor and shop front (decor.js), the lamps
// (lamps.js) and their light, shelves of records and a ledge of covers faced out along the walls,
// posters and neons, a sign over each rayon, the counter and its hi-fi at the back. It stands once for
// the shop; the aisles' bins come and go (stock.js).
import * as THREE from "../vendor/three.min.js";
import { hang, lightPool, signs, wallShelves, coverLedge, faceOutCovers, counter, phone } from "./world.js";
import { buildRoom, street, arcades, piers, posters, neons } from "./decor.js";
import { fixtures } from "./lamps.js";
import { coverAtlas, coverTexture } from "./covers.js";
import { REACH } from "./floor.js";
import { rangeOf } from "./aisles.js";

// furnish puts up the room of floor f for the aisles of plan, its walls showing records of the shop.
// It returns the spots the real lights follow you to (setLights), the turning platter and what one
// cannot walk through besides the bins.
export function furnish(scene, models, f, plan, records) {
  const front = f.door + 1.5, half = f.width / 2, till = f.back + 2.4;
  const [shelf, register, plant, chair, stool, hifi] = models;
  const lamps = fixtures(f, till);
  const box = (x, z, r) => ({ x0: x - r, x1: x + r, z0: z - r, z1: z + r });
  const shelves = [[front - 0.7, 0.5], [f.end - 0.8, f.back + 0.6]]; // by the door, and behind the bins
  const obstacles = [{ x0: -1.9, x1: 1.9, z0: till - 0.4, z1: till + 0.4 }, box(3.2, till + 1.5, 0.45), box(-0.2, till + 0.9, 0.22), box(-3.45, f.back + 0.9, 0.35),
    box(-2.4, till - 0.05, 0.2), box(2.4, till - 0.05, 0.2), box(3.9, till + 2.1, 0.16), ...piers(f),
    ...shelves.flatMap(([from, to]) => [-1, 1].map((s) => ({ x0: s < 0 ? -half : half - 0.42, x1: s < 0 ? -half + 0.42 : half, z0: to - 0.56, z1: from + 0.56 })))];
  buildRoom(scene, f, front, lamps.map((l) => l.light), [...f.blocks, ...obstacles]);
  street(scene, f.width, front);
  arcades(scene, f);
  hang(scene, hifi, lamps);
  const setLights = lightPool(scene, phone ? 3 : 4, phone ? 1 : 2);
  scene.add(new THREE.HemisphereLight(0xffe2c4, 0x24170e, 0.2));
  let seed = 11;
  const rand = () => (seed = (seed * 16807) % 2147483647) / 2147483647; // the same shop on every visit
  const slots = shelves.flatMap(([from, to]) => wallShelves(scene, shelf, rand, f.width, from, to, phone ? 26 : 40));
  for (const side of [-1, 1]) slots.push(...coverLedge(scene, side, side * (half - 0.045), front - 0.1, f.back + 0.1));
  for (let i = 0; i < 20; i++) slots.push({ x: -2.85 + (i % 10) * 0.63, y: i < 10 ? 1.32 : 1.72, z: f.back + 0.08, ry: -Math.PI / 2, lean: 0.14 });
  // 100 covers at most, the walls repeat them: 19 000 records made a 140 Mo picture of them
  const pool = [...records].sort((a, b) => (a.owned - b.owned) || (rand() - 0.5)).slice(0, Math.min(slots.length, 100));
  const cells = Math.ceil(Math.sqrt(pool.length));
  if (pool.length) faceOutCovers(scene, slots, coverAtlas(pool, cells), cells, pool.length);
  const labelTex = coverTexture(`/api/shop/${(pool[0] ?? records[0])?.record.mbid}/cover`);
  const platter = counter(scene, { register, plant, chair, stool, hifi }, till, f.back, labelTex);
  signs(scene, signsOver(f, plan));
  const walled = lamps.filter((l) => l.kind === "sconce" && Math.abs(Math.abs(l.x) - half) < 0.01); // along the walls, not on the piers
  posters(scene, walled.map((l) => ({ x: l.x - Math.sign(l.x) * 0.005, y: 2.72, z: l.z - 1.8, ry: l.ry })).filter((p) => p.z > f.back + 0.6));
  neons(scene, [
    { text: t("shop3d.neon"), x: -2.9, y: 2.55, z: f.back + 0.06, ry: 0, color: "#ff4f7a" }, // either side of the shop's name
    { text: "Hi-Fi", x: 2.9, y: 2.55, z: f.back + 0.06, ry: 0, color: "#4fd7ff" },
  ]);
  return { spots: lamps.filter((l) => l.light.pool).map((l) => l.light), setLights, platter, obstacles };
}

// signsOver hangs a rayon's name over the aisle side of its first unit in each aisle, seen from the
// door down the aisle; over the middle of the unit, it met the sign of the unit back to back with it.
// A rayon in several aisles says under its name what each holds (« A–F »): the same name over three
// aisles side by side told nothing.
function signsOver(f, plan) {
  const aisles = new Map(); // rack → the aisles it is in
  for (const a of plan) for (const r of new Set(a.units.map((u) => u.rack))) aisles.set(r, (aisles.get(r) ?? 0) + 1);
  return plan.flatMap((a, i) => a.units.filter((u, k) => a.units.findIndex((w) => w.rack === u.rack) === k).map((u) => {
    const own = a.units.filter((w) => w.rack === u.rack);
    const range = aisles.get(u.rack) > 1 ? rangeOf(u.rack, own[0].from, own.at(-1).to) : "";
    return { text: u.rack.name, range, x: f.aisles[i].x + u.side * (REACH - 0.45), z: f.aisles[i].z + u.z + u.length / 2 + 0.25 };
  }));
}
