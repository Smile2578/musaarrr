// The aisles' bins as you walk among them: every aisle's units stand from the start, their crates
// filled with spines (frames); the aisles near you are stocked with their records (buildBins) and
// given back as you walk away, with the covers' layers they held. 50 000 records stocked at once
// would hold 50 000 instances and faces; a few aisles hold a few thousand. The empty units are a mesh
// a row of aisles and the spines one instanced mesh: a few draws for the whole shop. The shop comes as
// an index (its rayons and their crates); a rayon's records are read as you come near its aisle.
import * as THREE from "../vendor/three.min.js";
import { mergeGeometries } from "../vendor/three.min.js";
import { frames, buildBins, BASE_H } from "./bins.js";
import { SLEEVE, BACK } from "./world.js";
import { LANE, REACH } from "./floor.js";
import { SIDE } from "./aisles.js";

const NEAR = 5.5, AWAY = 8.5; // metres from an aisle's units: closer it is stocked, farther given back
const AGAIN = 15000; // ms before a rayon whose records did not come is asked for again
const HIDDEN = new THREE.Matrix4().makeScale(0, 0, 0);

// Stockroom stands plan's aisles in scene where floor f puts them, with the bins' materials mats.
// dispose(root, keep) frees what root holds on the GPU but the materials in keep; load(rack) reads a
// rayon's records into rack.full (a promise of them, null when they could not be read). all: the
// crates stocked, in the plan's order; targets: what the pointer meets; solids: the units' bases.
export class Stockroom {
  constructor(scene, plan, f, mats, covers, dispose, load) {
    Object.assign(this, { plan, f, mats, covers, dispose, load, all: [], asked: new Map() });
    const rows = Array.from({ length: f.rows }, () => []), blocks = [];
    this.aisles = plan.map((a, i) => {
      const group = new THREE.Group();
      group.position.set(f.aisles[i].x, 0, f.aisles[i].z);
      group.updateMatrix();
      scene.add(group);
      const fr = frames(a.units, group.matrix), from = blocks.length;
      rows[Math.floor(i / f.cols)].push(...fr.geos);
      blocks.push(...fr.blocks);
      return { group, blocks: [from, blocks.length], units: null, crates: [] };
    });
    for (const geos of rows.filter((r) => r.length)) {
      const m = new THREE.Mesh(mergeGeometries(geos), mats.frame);
      m.castShadow = m.receiveShadow = true;
      scene.add(m);
    }
    this.blocks = blocks;
    this.filler = new THREE.InstancedMesh(new THREE.BoxGeometry(1, 1, 1), mats.spines, Math.max(1, blocks.length));
    this.filler.count = blocks.length;
    blocks.forEach((b, i) => this.filler.setMatrixAt(i, b));
    this.filler.receiveShadow = true;
    scene.add(this.filler);
    this.solids = this.targets = bases(scene, f);
  }

  // gap is how far x, z is from aisle i's units.
  gap(i, x, z) {
    const a = this.f.aisles[i];
    return Math.hypot(Math.max(0, Math.abs(x - a.x) - REACH - 0.36), Math.max(0, z - a.z, a.z - SIDE - z));
  }

  // aisleAt is the aisle whose walk x, z is in, or -1 in front of the bins, behind them or between rows.
  aisleAt(x, z) { return this.f.aisles.findIndex((a) => Math.abs(x - a.x) < LANE / 2 && z < a.z + 0.2 && z > a.z - SIDE); }

  // show shows aisle a's spines, or hides them while its records are in.
  show(a, on) {
    for (let k = a.blocks[0]; k < a.blocks[1]; k++) this.filler.setMatrixAt(k, on ? this.blocks[k] : HIDDEN);
    this.filler.instanceMatrix.needsUpdate = true;
  }

  // loaded asks for the records of aisle i's rayons not in yet, once in a while; true when all are in.
  loaded(i) {
    const now = performance.now();
    let all = true;
    for (const { rack } of this.plan[i].units) {
      if (rack.full) continue;
      all = false;
      if (now - (this.asked.get(rack) ?? -AGAIN) < AGAIN) continue;
      this.asked.set(rack, now);
      this.load(rack);
    }
    return all;
  }

  // stock puts aisle i's records in its crates, in place of the spines; true if it was not stocked
  // yet and its records are in.
  stock(i) {
    const a = this.aisles[i];
    if (a.units || !this.loaded(i)) return false;
    a.units = buildBins(a.group, this.plan[i].units, this.mats);
    a.crates = a.units.flatMap((u) => u.crates);
    for (const c of a.crates) c.aisle = i;
    this.show(a, false);
    return true;
  }

  // unstock gives aisle i's records back: their instances, and the covers' layers they held.
  unstock(i) {
    const a = this.aisles[i];
    for (const c of a.crates) {
      c.gone = true;
      for (const it of [c.tab, ...c.items]) this.covers.forget(it.face);
    }
    for (const u of a.units) {
      this.dispose(u.unit, [...Object.values(this.mats), SLEEVE, BACK]); // shared by every aisle
      u.unit.removeFromParent();
    }
    Object.assign(a, { units: null, crates: [] });
    this.show(a, true);
  }

  list() {
    this.all = this.aisles.flatMap((a) => a.crates);
    this.targets = [...this.all.map((c) => c.hit), ...this.solids];
  }

  // near stocks the nearest aisle within reach of x, z, one a call, and gives back those left behind
  // but the aisles in keep; true when the stock changed.
  near(x, z, keep) {
    let best = -1, bd = NEAR, gone = false;
    this.aisles.forEach((a, i) => {
      const d = this.gap(i, x, z);
      if (!a.units && d < bd && this.loaded(i)) [best, bd] = [i, d];
      else if (a.units && d > AWAY && !keep.includes(i)) {
        this.unstock(i);
        gone = true;
      }
    });
    const changed = (best >= 0 && this.stock(best)) || gone;
    if (changed) this.list();
    return changed;
  }

  // crateIn is crate k of aisle i (from the end when k < 0), stocked once its records are in; null
  // when they could not be read.
  async crateIn(i, k) {
    const full = await Promise.all(this.plan[i].units.map(({ rack }) => rack.full ?? this.load(rack)));
    if (!full.every(Boolean)) return null;
    if (this.stock(i)) this.list();
    return this.aisles[i].crates.at(k);
  }

  // neighbour is the crate dir away from c, in the next aisle past the end of its own; null at the shop's ends.
  async neighbour(c, dir) {
    const own = this.aisles[c.aisle].crates;
    return own[own.indexOf(c) + dir] ?? (this.plan[c.aisle + dir] ? this.crateIn(c.aisle + dir, dir > 0 ? 0 : -1) : null);
  }
}

// bases are what the pointer meets on its way to a crate or the floor: the units' bases, an
// invisible box each.
function bases(scene, f) {
  const hidden = new THREE.MeshBasicMaterial({ visible: false }), box = new THREE.BoxGeometry(1, 1, 1);
  return f.blocks.map((b) => {
    const m = new THREE.Mesh(box, hidden);
    m.position.set((b.x0 + b.x1) / 2, BASE_H / 2, (b.z0 + b.z1) / 2);
    m.scale.set(b.x1 - b.x0, BASE_H, b.z1 - b.z0);
    scene.add(m);
    return m;
  });
}
