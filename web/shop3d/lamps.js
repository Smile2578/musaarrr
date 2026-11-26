// Where the shop's lamps hang, each a model of assets/hifi.glb: a brass bar over each line of bins, two
// to a row, of opal globes in one room and enamel shades in the next; prismatic pendants over the
// counter; sconces along the side walls and on the arcades' piers; a floor lamp in the listening
// corner. Each
// gives a light, baked into the floor and walls (bake.js) and, for the hanging ones, followed by the few
// real lights of the shop (pool). No three.js here: node tests it.
import { SIDE } from "./aisles.js";
import { LANE, REACH } from "./floor.js";

const BAR = 2.6, PENDANT = 2.62, SCONCE = 2.3, EVERY = 3.6; // metres: heights, and between two sconces

// fixtures are floor f's lamps, the counter at z = till: { kind, x, y, z, ry, light: { x, y, z, strength, pool } }.
export function fixtures(f, till) {
  const out = [];
  const add = (kind, x, y, z, ry, ly, strength, pool = true) => out.push({ kind, x, y, z, ry, light: { x, y: ly, z, strength, pool } });
  for (let r = 0; r < f.rows; r++) {
    const row = f.aisles.slice(r * f.cols, (r + 1) * f.cols);
    if (!row.length) continue;
    const lines = [row[0].x - REACH, ...row.slice(0, -1).map((a) => a.x + LANE / 2), row.at(-1).x + REACH];
    for (const x of lines) for (const k of [0.25, 0.75]) add(r % 2 ? "billard" : "rampe", x, BAR, row[0].z - SIDE * k, Math.PI / 2, BAR - 0.14, 20);
  }
  for (const { z, xs } of f.arches ?? []) for (const x of xs) for (const s of [-1, 1]) add("sconce", x, SCONCE - 0.2, z + s * 0.18, s < 0 ? Math.PI : 0, SCONCE - 0.05, 4, false);
  for (const x of [-1.55, 1.55]) add("holophane", x, PENDANT, till, 0, PENDANT - 0.22, 16);
  const half = f.width / 2;
  for (let z = f.door - 0.6; z > f.back + 1; z -= EVERY) {
    for (const s of [-1, 1]) add("sconce", s * half, SCONCE, z, -s * Math.PI / 2, SCONCE + 0.15, 5, false);
  }
  add("floorlamp", 3.9, 0, till + 2.1, 0, 1.5, 7, false);
  for (const l of out) if (l.kind === "sconce") [l.light.x, l.light.z] = [l.light.x + Math.sin(l.ry) * 0.17, l.light.z + Math.cos(l.ry) * 0.17]; // the tulip stands off the wall
  return out;
}
