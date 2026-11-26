import { test } from "node:test";
import assert from "node:assert/strict";
import { pace } from "../../web/shop3d/pace.js";

// run feeds n frames of ms each, from time t, and returns the ratios pace asked for.
const run = (p, n, ms, t = 0) => {
  const out = [];
  for (let i = 0; i < n; i++) {
    const r = p.frame(ms, t + i * ms);
    if (r !== null) out.push(r);
  }
  return out;
};

test("frames on time leave the ratio alone", () => {
  const p = pace(1, 2);
  assert.deepEqual(run(p, 2000, 16.7), []);
  assert.equal(p.ratio, 2);
});

test("late frames lower the ratio a step at a time, never under the floor", () => {
  const p = pace(1, 2);
  const got = run(p, 3000, 40);
  assert.deepEqual(got, [1.75, 1.5, 1.25, 1]);
  assert.equal(p.ratio, 1);
});

test("nothing is measured in the first seconds, while the shop loads", () => {
  const p = pace(1, 2);
  assert.deepEqual(run(p, 60, 50), []); // 3 s of slow frames: still loading
});

test("it waits after a step before judging again, and does not climb back", () => {
  const p = pace(1, 2);
  assert.deepEqual(run(p, 230, 40), [1.75]); // the step comes at 6.6 s; the frames until 9.6 s are not counted
  assert.deepEqual(run(p, 3000, 8, 230 * 40), []); // fast again: stays where it went
  assert.equal(p.ratio, 1.75);
});

test("a single long frame among many on time changes nothing", () => {
  const p = pace(1, 2);
  let t = 0;
  for (let i = 0; i < 600; i++) {
    const ms = i === 400 ? 300 : 16.7;
    assert.equal(p.frame(ms, (t += ms)), null);
  }
});
