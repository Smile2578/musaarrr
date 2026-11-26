import { test } from "node:test";
import assert from "node:assert/strict";
import { crateRuns, planAisles, rangeOf, SIDE, PITCH, PAD, GAP } from "../../web/shop3d/aisles.js";

// rack builds a summary rayon: crates of the server with only their counts, as ?summary=1 gives them.
const rack = (id, ...ns) => ({ id, name: id, n: ns.reduce((a, b) => a + b, 0), crates: ns.map((n, i) => ({ label: `${id}${i}`, n })) });
const big = (id, crates30) => rack(id, ...Array(crates30 * 5).fill(6)); // five crates of 6 make one crate of 30

test("crateRuns packs the server's crates of 12 into crates of 30 by their counts", () => {
  assert.deepEqual(crateRuns([{ n: 12 }, { n: 12 }, { n: 12 }, { n: 5 }]), [[0, 2], [2, 4]]);
  assert.deepEqual(crateRuns([]), []);
});

test("small rayons alternate sides in one aisle, both from the door", () => {
  const a = planAisles([rack("mois", 12, 12), rack("jazz", 12), rack("soul", 12)]);
  assert.equal(a.length, 1);
  assert.deepEqual(a[0].units.map((u) => [u.rack.id, u.side]), [["mois", -1], ["jazz", 1], ["soul", -1]]);
  const [mois, jazz, soul] = a[0].units;
  assert.equal(mois.z, -(PITCH + PAD) / 2);
  assert.equal(jazz.z, mois.z);
  assert.ok(Math.abs(soul.z - (mois.z - (PITCH + PAD) - GAP)) < 1e-9, "soul comes after mois, toward the back");
});

test("a long rayon is cut where a side ends and carried on, then a new aisle opens", () => {
  const perSide = Math.floor((SIDE - PAD) / PITCH);
  const a = planAisles([big("rock", perSide * 3)]);
  assert.equal(a.length, 2);
  assert.deepEqual(a.flatMap((x) => x.units.map((u) => [u.from, u.to])), [[0, perSide], [perSide, 2 * perSide], [2 * perSide, 3 * perSide]]);
});

test("every crate is placed once, in order, and no side runs past its length", () => {
  const racks = [rack("mois", 12, 12, 12, 12, 12, 6), big("jazz", 40), rack("blues", 3), big("rock", 70), rack("vide"), big("pop", 9)];
  const aisles = planAisles(racks);
  const seen = new Map();
  for (const { units } of aisles) {
    for (const side of [-1, 1]) {
      const us = units.filter((u) => u.side === side);
      const end = us.reduce((m, u) => Math.min(m, u.z - ((u.to - u.from) * PITCH + PAD) / 2), 0);
      assert.ok(end >= -SIDE - 1e-9, `a side ends at ${end}`);
      us.forEach((u, i) => i && assert.ok(u.z < us[i - 1].z, "toward the back"));
    }
    for (const u of units) {
      assert.equal(u.from, seen.get(u.rack.id) ?? 0, `${u.rack.id} carries on where it stopped`);
      seen.set(u.rack.id, u.to);
    }
  }
  for (const r of racks) assert.equal(seen.get(r.id) ?? 0, crateRuns(r.crates).length, r.id);
  assert.ok(!aisles.some((a) => !a.units.length), "no empty aisle");
  assert.ok(GAP > 0);
});

test("rangeOf says what crates of 30 file, from the first label to the last", () => {
  const soul = { crates: [{ label: "A", n: 12 }, { label: "A–C", n: 12 }, { label: "D", n: 12 }, { label: "Ho", n: 12 }, { label: "Ho–Hu", n: 12 }, { label: "#", n: 5 }] };
  assert.equal(rangeOf(soul, 0, 1), "A–C"); // crateRuns: [0, 2), [2, 4), [4, 6)
  assert.equal(rangeOf(soul, 1, 2), "D–Ho");
  assert.equal(rangeOf(soul, 0, 3), "A–#");
  assert.equal(rangeOf({ crates: [{ label: "B", n: 12 }, { label: "B", n: 12 }] }, 0, 1), "B");
  assert.equal(rangeOf({ crates: [{ label: "1–12", n: 12 }, { label: "13–24", n: 12 }] }, 0, 1), "1–24");
});
