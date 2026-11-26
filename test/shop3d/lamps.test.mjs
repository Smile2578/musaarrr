import { test } from "node:test";
import assert from "node:assert/strict";
import { fixtures } from "../../web/shop3d/lamps.js";
import { layout } from "../../web/shop3d/floor.js";
import { SIDE } from "../../web/shop3d/aisles.js";

const unit = (side) => ({ side, z: -SIDE / 2, length: SIDE - 0.2 });
const aisles = (n) => Array.from({ length: n }, () => ({ units: [unit(-1), unit(1)] }));
const kinds = (list) => list.reduce((o, l) => ({ ...o, [l.kind]: (o[l.kind] ?? 0) + 1 }), {});

test("bars of lamps hang over every line of bins, two to a row; pendants over the counter", () => {
  const f = layout(aisles(3));
  const list = fixtures(f, f.back + 2.4);
  const k = kinds(list);
  assert.equal(k.rampe, 4 * 2); // 4 lines of bins (2 against the walls, 2 back to back), one row
  assert.equal(k.holophane, 2); // over the counter
  assert.ok(k.sconce >= 4); // along both walls
  for (const l of list) assert.ok(Math.abs(l.x) <= f.width / 2 && l.z <= f.door + 1.5 && l.z >= f.back, `${l.kind} inside the room`);
});

test("rooms one behind the other change their lamps; the arcades' piers wear sconces", () => {
  const f = layout(aisles(16));
  const list = fixtures(f, f.back + 2.4), k = kinds(list);
  assert.equal(k.rampe, (7 + 5) * 2); // rows of 6, 6 and 4 aisles: 7, 7 and 5 lines of bins; the first and the last row
  assert.equal(k.billard, 7 * 2); // the middle one
  const piers = list.filter((l) => l.kind === "sconce" && Math.abs(l.x) < f.width / 2 - 0.1);
  assert.equal(piers.length, 2 * 5 * 2); // two arcades of five piers, a sconce on each face
});

test("each lamp gives a light for the shop to bake and to follow you with", () => {
  for (const l of fixtures(layout(aisles(3)), -14)) {
    assert.ok(l.light.y > 0.5 && l.light.y < 3.4 && l.light.strength > 0, l.kind);
  }
});
