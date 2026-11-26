// node --test test/shop3d/*.test.mjs (web/ is embedded in the binary: its tests live here)
import { test } from "node:test";
import assert from "node:assert/strict";
import { layerPool } from "../../web/shop3d/layers.js";

const face = () => ({ layers: [-1, -1] });

test("a face keeps its layer when asked again", () => {
  const p = layerPool(2), a = face();
  assert.equal(p.ask(a, 0).layer, 0);
  p.next();
  assert.deepEqual(p.ask(a, 0), { layer: 0, lost: null });
  assert.deepEqual(a.layers, [0, -1]);
});

test("the layer asked for longest ago changes hands", () => {
  const p = layerPool(2), [a, b, c] = [face(), face(), face()];
  p.ask(a, 1);
  p.ask(b, 1);
  p.next();
  p.ask(a, 1); // a asked again: b is now the oldest
  const r = p.ask(c, 1);
  assert.equal(r.lost, b);
  assert.equal(r.layer, 1);
  assert.deepEqual([a.layers[1], b.layers[1], c.layers[1]], [0, -1, 1]);
});

test("more faces than layers in one round: the last asked goes without", () => {
  const p = layerPool(2), [a, b, c] = [face(), face(), face()];
  p.ask(a, 0);
  p.ask(b, 0);
  assert.equal(p.ask(c, 0), null);
  assert.equal(c.layers[0], -1);
  assert.equal(p.held(), 2);
});

test("clear gives every layer back", () => {
  const p = layerPool(3), [a, b] = [face(), face()];
  p.ask(a, 0);
  p.ask(b, 0);
  p.clear();
  assert.deepEqual([a.layers, b.layers], [[-1, -1], [-1, -1]]);
  assert.equal(p.held(), 0);
  assert.equal(p.ask(face(), 0).layer, 0);
});

test("a face given back frees its layer for the next one", () => {
  const p = layerPool(1), [a, b] = [face(), face()];
  p.ask(a, 1);
  p.release(a, 1);
  assert.equal(a.layers[1], -1);
  assert.equal(p.held(), 0);
  assert.deepEqual(p.ask(b, 1), { layer: 0, lost: null }); // in the same round: the layer was free
  p.release(face(), 1); // a face without a layer: nothing to give back
  assert.equal(p.held(), 1);
});
