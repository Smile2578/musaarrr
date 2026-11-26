// The shop floor: where each aisle of the plan (planAisles) stands, side by side and in rows one
// behind the other, and how you walk among them: on free floor only, sliding along the bins you bump
// into, and round them to go further. No three.js here: node tests it.
import { SIDE } from "./aisles.js";

export const LANE = 3.82; // metres from an aisle's centre to the next: two units back to back between them
export const REACH = 1.55; // from an aisle's centre to its units' centres
const BASE = 0.72, CROSS = 2.8, DOOR = 4.5, BACK = 5.5; // a unit's depth, a cross aisle, the floor before the bins and behind them
const BODY = 0.3, CELL = 0.2; // metres: how close you come to a bin or a wall; the grid a way is looked for on

// layout places the aisles: a few side by side, more in rows of the same number. An aisle's x is its
// centre, z where its bins start (they run toward -z, as planAisles lays them); blocks are the units'
// bases on the floor; end is where the last row of bins ends, back and door the walls. Between two
// rows, an arcade parts the shop in rooms: its piers stand at xs, between the aisles, in the middle
// of the cross aisle at z.
export function layout(plan) {
  const n = Math.max(1, plan.length);
  const rows = Math.max(1, Math.min(4, Math.round(Math.sqrt(n / 2.5))));
  const cols = Math.ceil(n / rows);
  const aisles = plan.map((_, a) => ({ x: ((a % cols) - (cols - 1) / 2) * LANE, z: -Math.floor(a / cols) * (SIDE + CROSS) || 0 })); // || 0: not -0
  const blocks = plan.flatMap((a, i) => a.units.map((u) => {
    const x = aisles[i].x + u.side * REACH, z = aisles[i].z + u.z;
    return { x0: x - BASE / 2, x1: x + BASE / 2, z0: z - u.length / 2, z1: z + u.length / 2 };
  }));
  const width = Math.max(8.4, (cols - 1) * LANE + 2 * (REACH + BASE / 2 + 0.05)); // the outer bins against the walls
  const end = -rows * SIDE - (rows - 1) * CROSS;
  const arches = Array.from({ length: rows - 1 }, (_, r) => ({ z: -(r + 1) * SIDE - r * CROSS - CROSS / 2, xs: aisles.slice(r * cols + 1, (r + 1) * cols).map((a) => a.x - LANE / 2) }));
  return { rows, cols, aisles, blocks, width, door: DOOR, end, back: end - BACK, arches };
}

// walls is where one may stand on floor f, with more obstacles (the counter): free, slide and clear
// for a step, path for a walk.
export function walls(f, more = []) {
  const rects = [...f.blocks, ...more].map((b) => ({ x0: b.x0 - BODY, x1: b.x1 + BODY, z0: b.z0 - BODY, z1: b.z1 + BODY }));
  const half = f.width / 2 - BODY;
  const free = (x, z) => Math.abs(x) <= half && z >= f.back + BODY && z <= f.door + 1.5 - BODY
    && !rects.some((r) => x > r.x0 && x < r.x1 && z > r.z0 && z < r.z1);
  const clear = (a, b) => {
    const n = Math.ceil(Math.hypot(b.x - a.x, b.z - a.z) / (CELL / 2));
    for (let i = 1; i <= n; i++) if (!free(a.x + ((b.x - a.x) * i) / n, a.z + ((b.z - a.z) * i) / n)) return false;
    return true;
  };
  // slide takes a step from a toward b, along an obstacle when it is in the way
  const slide = (a, b) => (free(b.x, b.z) ? b : free(b.x, a.z) ? { x: b.x, z: a.z } : free(a.x, b.z) ? { x: a.x, z: b.z } : a);
  return { free, clear, slide, path: (a, b) => findPath(f, free, clear, a, b) };
}

// findPath is the way from a to b as straight legs over free floor (A* on a grid, then shortcuts),
// ending at b, or at the free floor nearest to it.
function findPath(f, free, clear, a, b) {
  if (free(b.x, b.z) && clear(a, b)) return [b];
  const x0 = -f.width / 2, z0 = f.back, nx = Math.ceil(f.width / CELL), nz = Math.ceil((f.door + 1.5 - f.back) / CELL);
  const at = (i) => ({ x: x0 + ((i % nx) + 0.5) * CELL, z: z0 + (Math.floor(i / nx) + 0.5) * CELL });
  const index = (p) => Math.min(nz - 1, Math.max(0, Math.floor((p.z - z0) / CELL))) * nx + Math.min(nx - 1, Math.max(0, Math.floor((p.x - x0) / CELL)));
  const open = (i) => { const p = at(i); return free(p.x, p.z); };
  const goal = nearestOpen(index(b), nx, nz, open);
  const cells = aStar(index(a), goal, nx, nz, open, at);
  if (!cells) return [];
  const pts = cells.map(at);
  pts[pts.length - 1] = free(b.x, b.z) ? b : toward(pts.at(-1), b, free);
  const way = [];
  for (let from = a, i = 0; i < pts.length;) {
    let j = pts.length - 1;
    while (j > i && !clear(from, pts[j])) j--;
    way.push((from = pts[j]));
    i = j + 1;
  }
  return way;
}

// toward is the free point nearest to b on the way from a (free) to b, closer than a grid cell's centre.
function toward(a, b, free) {
  let lo = 0, hi = 1;
  for (let k = 0; k < 12; k++) {
    const m = (lo + hi) / 2;
    if (free(a.x + (b.x - a.x) * m, a.z + (b.z - a.z) * m)) lo = m;
    else hi = m;
  }
  return { x: a.x + (b.x - a.x) * lo, z: a.z + (b.z - a.z) * lo };
}

// nearestOpen is the open cell nearest to i, in rings around it.
function nearestOpen(i, nx, nz, open) {
  if (open(i)) return i;
  const cx = i % nx, cz = Math.floor(i / nx);
  for (let r = 1; r < Math.max(nx, nz); r++) {
    let best = -1, bd = Infinity;
    for (let dz = -r; dz <= r; dz++) for (let dx = -r; dx <= r; dx++) {
      if (Math.max(Math.abs(dx), Math.abs(dz)) !== r) continue;
      const x = cx + dx, z = cz + dz, d = dx * dx + dz * dz;
      if (x >= 0 && x < nx && z >= 0 && z < nz && d < bd && open(z * nx + x)) [best, bd] = [z * nx + x, d];
    }
    if (best >= 0) return best;
  }
  return i;
}

// aStar is the cells from start to goal, 8 ways round, or null when none leads there.
function aStar(start, goal, nx, nz, open, at) {
  const g = new Float64Array(nx * nz).fill(Infinity), from = new Int32Array(nx * nz).fill(-1), done = new Uint8Array(nx * nz);
  const end = at(goal), h = (i) => { const p = at(i); return Math.hypot(p.x - end.x, p.z - end.z) / CELL; };
  const heap = [[h(start), start]];
  g[start] = 0;
  while (heap.length) {
    const [, i] = pop(heap);
    if (i === goal) break;
    if (done[i]) continue;
    done[i] = 1;
    const x = i % nx, z = Math.floor(i / nx);
    for (const [dx, dz] of [[1, 0], [-1, 0], [0, 1], [0, -1], [1, 1], [1, -1], [-1, 1], [-1, -1]]) {
      const j = (z + dz) * nx + x + dx;
      if (x + dx < 0 || x + dx >= nx || z + dz < 0 || z + dz >= nz || done[j] || !open(j)) continue;
      if (dx && dz && !(open(z * nx + x + dx) && open((z + dz) * nx + x))) continue; // no corner cut
      const c = g[i] + (dx && dz ? Math.SQRT2 : 1);
      if (c < g[j]) {
        g[j] = c;
        from[j] = i;
        push(heap, [c + h(j), j]);
      }
    }
  }
  if (from[goal] < 0 && goal !== start) return null;
  const out = [];
  for (let i = goal; i !== start && i >= 0; i = from[i]) out.unshift(i);
  return out.length ? out : [goal];
}

function push(heap, e) {
  heap.push(e);
  for (let i = heap.length - 1; i > 0;) {
    const p = (i - 1) >> 1;
    if (heap[p][0] <= heap[i][0]) break;
    [heap[p], heap[i]] = [heap[i], heap[p]];
    i = p;
  }
}

function pop(heap) {
  const top = heap[0], last = heap.pop();
  if (heap.length) {
    heap[0] = last;
    for (let i = 0; ;) {
      const l = 2 * i + 1, r = l + 1;
      let m = i;
      if (l < heap.length && heap[l][0] < heap[m][0]) m = l;
      if (r < heap.length && heap[r][0] < heap[m][0]) m = r;
      if (m === i) break;
      [heap[m], heap[i]] = [heap[i], heap[m]];
      i = m;
    }
  }
  return top;
}
