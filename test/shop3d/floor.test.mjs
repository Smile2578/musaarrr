import { test } from "node:test";
import assert from "node:assert/strict";
import { layout, walls, LANE } from "../../web/shop3d/floor.js";
import { SIDE } from "../../web/shop3d/aisles.js";

const unit = (side) => ({ side, z: -SIDE / 2, length: SIDE - 0.2 }); // a side full of bins
const aisles = (n) => Array.from({ length: n }, () => ({ units: [unit(-1), unit(1)] }));

test("a few aisles stand side by side in one row, centred on the door", () => {
  const f = layout(aisles(3));
  assert.equal(f.rows, 1);
  assert.deepEqual(f.aisles.map((a) => a.x), [-LANE, 0, LANE]);
  assert.deepEqual(f.aisles.map((a) => a.z), [0, 0, 0]);
  assert.ok(f.door > 0 && f.end === -SIDE && f.back < f.end - 3); // room for the counter behind the bins
});

test("many aisles make rows, one behind the other, with a cross aisle between", () => {
  const f = layout(aisles(16));
  assert.equal(f.rows, 3);
  assert.equal(f.cols, 6);
  assert.equal(f.aisles[6].x, f.aisles[0].x); // the second row starts again on the left
  assert.ok(f.aisles[6].z < -SIDE - 2); // behind the first, past a cross aisle
  assert.ok(f.width >= 6 * LANE);
  assert.equal(f.arches.length, 2); // an arcade between two rows
  for (const [r, a] of f.arches.entries()) {
    assert.ok(a.z < f.aisles[r * 6].z - SIDE && a.z > f.aisles[(r + 1) * 6].z); // in the cross aisle
    const between = f.aisles.slice(r * 6, r * 6 + 5).map((x) => x.x + LANE / 2); // a pier between two aisles
    assert.equal(a.xs.length, 5);
    a.xs.forEach((x, i) => assert.ok(Math.abs(x - between[i]) < 1e-9));
  }
});

test("each unit is an obstacle where its base stands", () => {
  const f = layout(aisles(1));
  const [left, right] = f.blocks;
  assert.ok(left.x1 < 0 && right.x0 > 0);
  assert.ok(left.z0 < left.z1 && Math.abs(left.z1 - left.z0 - (SIDE - 0.2)) < 1e-9);
});

const plan = layout(aisles(3));
const w = walls(plan, [{ x0: -1, x1: 1, z0: plan.back + 1, z1: plan.back + 2 }]);

test("you walk in the aisles and the cross aisles, not through the bins nor the walls", () => {
  assert.ok(w.free(0, -5)); // the middle aisle
  assert.ok(w.free(LANE / 2 - 1.9, plan.door - 1)); // in front of the bins
  assert.ok(!w.free(1.55, -2)); // on a unit
  assert.ok(!w.free(0.95, -2)); // too close to it
  assert.ok(!w.free(plan.width, 0)); // past the wall
  assert.ok(!w.free(0, plan.back + 1.5)); // the counter
});

test("a step into the bins slides along them", () => {
  const to = w.slide({ x: 0.5, z: -3 }, { x: 1.4, z: -3.4 });
  assert.ok(to.x < 1.2 && Math.abs(to.z + 3.4) < 1e-9);
});

test("the way to the next aisle goes round the bins, every leg clear", () => {
  const from = { x: 0, z: -5 }, to = { x: LANE, z: -5 };
  const way = w.path(from, to);
  assert.ok(way.length >= 2);
  assert.deepEqual(way.at(-1), to);
  let at = from;
  for (const p of way) {
    assert.ok(w.clear(at, p), `${JSON.stringify(at)} → ${JSON.stringify(p)}`);
    at = p;
  }
  assert.ok(Math.min(...way.map((p) => p.z)) < -SIDE + 1.5 || Math.max(...way.map((p) => p.z)) > -1); // by an end of the bins
});

test("in the same aisle it is a straight line", () => {
  assert.deepEqual(w.path({ x: 0, z: -1 }, { x: 0, z: -8 }), [{ x: 0, z: -8 }]);
});

test("a spot on the bins is reached from the nearest free floor", () => {
  const way = w.path({ x: 0, z: -5 }, { x: 1.55, z: -5 });
  const end = way.at(-1);
  assert.ok(w.free(end.x, end.z));
  assert.ok(Math.hypot(end.x - 1.55, end.z + 5) < 0.8);
});
