import { test } from "node:test";
import assert from "node:assert/strict";

// stock.js loads three.js and world.js, which draw on a canvas when they load: a page's bare minimum
const nothing = new Proxy(function () {}, { get: (_, k) => (k === Symbol.toPrimitive ? () => 0 : nothing), apply: () => nothing });
globalThis.matchMedia = () => ({ matches: false, addEventListener() {} });
globalThis.document = { createElement: () => ({ width: 0, height: 0, style: {}, getContext: () => nothing }) };
const { Stockroom } = await import("../../web/shop3d/stock.js");

let now = 0;
performance.now = () => now;

// room is a stockroom of one aisle holding racks, its methods called on what they read alone
function room(racks, read) {
  const asked = [];
  const load = (rack) => {
    asked.push(rack.id);
    return Promise.resolve(read(rack)).then((full) => (full ? (rack.full = full) : null));
  };
  const crates = [{ label: "A" }, { label: "B" }];
  const r = { plan: [{ units: racks.map((rack) => ({ rack })) }], asked: new Map(), load, aisles: [{ crates }], lists: 0 };
  Object.assign(r, { loaded: Stockroom.prototype.loaded, crateIn: Stockroom.prototype.crateIn, stock: () => true, list: () => r.lists++ });
  return { r, asked };
}

test("an aisle's rayons are asked for once, and again only after a while", () => {
  now = 1000;
  const { r, asked } = room([{ id: "jazz" }, { id: "rock", full: {} }], () => null);
  assert.equal(r.loaded(0), false);
  assert.deepEqual(asked, ["jazz"]); // rock is in already
  now += 5000;
  r.loaded(0);
  assert.deepEqual(asked, ["jazz"]);
  now += 15000;
  r.loaded(0);
  assert.deepEqual(asked, ["jazz", "jazz"]);
});

test("an aisle whose rayons are all in is ready", () => {
  const { r, asked } = room([{ id: "jazz", full: {} }], () => null);
  assert.equal(r.loaded(0), true);
  assert.deepEqual(asked, []);
});

test("a crate is given once its rayon is read, from the end when k < 0", async () => {
  const { r } = room([{ id: "jazz" }], () => ({ crates: [] }));
  assert.equal((await r.crateIn(0, 0)).label, "A");
  assert.equal((await r.crateIn(0, -1)).label, "B");
  assert.equal(r.lists, 2);
});

test("no crate when a rayon could not be read", async () => {
  const { r } = room([{ id: "jazz" }, { id: "soul" }], (rack) => (rack.id === "soul" ? null : { crates: [] }));
  assert.equal(await r.crateIn(0, 0), null);
  assert.equal(r.lists, 0);
});
