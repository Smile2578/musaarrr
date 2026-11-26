// The shop in aisles of the same length, so a room is built once and only its bins change: at 19 000
// records a single aisle ran 185 m, and its walls and shelves grew with it. planAisles works from the
// rayons' crate counts alone (/api/shop/racks?summary=1 is enough), so where everything stands is
// known before any record is loaded. No three.js here: node tests it.

export const PER_CRATE = 30; // records in a crate of the 3D shop, which packs the server's crates of 12
export const PITCH = 0.385, PAD = 0.12, GAP = 1.1; // metres: a crate and the space to the next, a unit's ends, between units
export const SIDE = 10.5; // metres of bins along each side of an aisle
const MIN = 3; // crates: a side with less room than this, for a rayon that has more, is full

// crateRuns packs the server's crates into crates of up to PER_CRATE records, by their counts only,
// as a shop packs them: [from, to) ranges of the server's crates.
export function crateRuns(crates) {
  const out = [];
  let n = PER_CRATE + 1;
  crates.forEach((c, i) => {
    if (n + c.n > PER_CRATE) {
      out.push([i, i]);
      n = 0;
    }
    n += c.n;
    out.at(-1)[1] = i + 1;
  });
  return out;
}

// planAisles lays the rayons in aisles: each on the side with the most room left, from the door
// toward the back, a rayon too long for it cut where the side ends and carried on, and a new aisle
// opened when neither side has room. A unit is { rack, from, to, side, z, length }: crates [from, to)
// of the rayon (crateRuns), on the left (-1) or right (1), centred at z (the first crates start at z = 0).
export function planAisles(racks) {
  const aisles = [];
  let cur = null;
  const open = () => aisles.push((cur = { units: [], used: [0, 0] }));
  open();
  for (const rack of racks) {
    const total = crateRuns(rack.crates).length;
    for (let from = 0; from < total;) {
      const room = cur.used.map((u) => Math.floor((SIDE - u - PAD) / PITCH + 1e-9));
      const s = room[0] >= room[1] ? 0 : 1;
      if (room[s] < Math.min(total - from, MIN) && cur.units.length) {
        open();
        continue;
      }
      const k = Math.max(1, Math.min(total - from, room[s]));
      const length = k * PITCH + PAD;
      cur.units.push({ rack, from, to: from + k, side: s ? 1 : -1, z: -cur.used[s] - length / 2, length });
      cur.used[s] += length + GAP;
      from += k;
    }
  }
  return aisles.filter((a) => a.units.length).map(({ units }) => ({ units }));
}

// rangeOf is what crates [from, to) of a rayon (crateRuns) file, for its sign over an aisle: from the
// first crate's label to the last's, « A–F », « Ho–Hu », « 1–24 ».
export function rangeOf(rack, from, to) {
  const runs = crateRuns(rack.crates);
  const a = rack.crates[runs[from][0]].label.split("–")[0], b = rack.crates[runs[to - 1][1] - 1].label.split("–").at(-1);
  return a === b ? a : `${a}–${b}`;
}
