import { test } from "node:test";
import assert from "node:assert/strict";
import { irradiance, shade } from "../../web/shop3d/bake.js";

// a floor of 10 × 10 m in cells of 0.5 m, its first cell at x = 0, z = 0, rows toward +z
const floor = { o: [0.25, 0, 0.25], u: [0.5, 0, 0], v: [0, 0, 0.5], n: [0, 1, 0], nu: 20, nv: 20 };
const at = (g, x, z) => g[Math.floor(z / 0.5) * 20 + Math.floor(x / 0.5)];

test("a lamp lights the floor under it most, and less farther off", () => {
  const g = irradiance(floor, [{ x: 5, y: 2.5, z: 5, strength: 10 }]);
  assert.ok(at(g, 5, 5) > at(g, 7, 5) && at(g, 7, 5) > at(g, 9.9, 5) && at(g, 9.9, 5) > 0);
  assert.ok(Math.abs(at(g, 2.9, 5) - at(g, 7.1, 5)) < 1e-6); // the same both sides: cells at 2.75 and 7.25
});

test("two lamps add up; a lamp behind a wall lights nothing on it", () => {
  const one = irradiance(floor, [{ x: 5, y: 2.5, z: 5, strength: 10 }]);
  const two = irradiance(floor, [{ x: 5, y: 2.5, z: 5, strength: 10 }, { x: 5, y: 2.5, z: 5, strength: 10 }]);
  assert.ok(Math.abs(at(two, 6, 6) - 2 * at(one, 6, 6)) < 1e-6);
  const under = irradiance(floor, [{ x: 5, y: -1, z: 5, strength: 10 }]);
  assert.ok(under.every((e) => e === 0));
});

test("the floor darkens under a bin and close to it, not far away", () => {
  const g = new Float32Array(400).fill(1);
  shade(g, floor, [{ x0: 4, x1: 6, z0: 4, z1: 6 }], 0.5);
  assert.ok(at(g, 5, 5) < 0.5); // under it
  assert.ok(at(g, 6.3, 5) < 1 && at(g, 6.3, 5) > at(g, 5, 5)); // at its foot
  assert.equal(at(g, 9, 9), 1); // far off
});
