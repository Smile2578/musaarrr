// What stands in the room round the bins: the lamps, the signs over the rayons, cube shelves packed
// with records, a ledge of covers, the counter with its turntable, amplifier and speakers. Furniture
// and textures: Poly Haven (CC0); the counter, the hi-fi and the lamps: assets/hifi (tools/blender/hifi.py).
// The walls, floor and ceiling: decor.js.
import * as THREE from "../vendor/three.min.js";
import { mergeGeometries, GLTFLoader, KTX2Loader } from "../vendor/three.min.js";

export const HEIGHT = 3.4;
const CREAM = "#efe6d2", BRASS = "#c9a25b", BOARD = "#16241d";
const A = "/shop3d/assets";

export const phone = matchMedia("(pointer: coarse)").matches;
const ALL = ["diffuse", "nor_gl", "rough"];
const SETS = { herringbone_parquet: ["diffuse", "nor_gl"], painted_plaster_wall: ALL, oak_veneer_01: ALL }; // the parquet is satin all over: no roughness map
const pictures = new Map(); // each picture loaded once: copies with their own repeat share its upload to the GPU

// loadPictures loads the room's textures with ktx (a KTX2Loader): the GPU keeps them compressed. A
// phone keeps the colour only: the relief and roughness maps would double its graphics memory, and
// iOS closes a tab that takes too much.
function loadPictures(ktx) {
  return Promise.all(Object.entries(SETS).flatMap(([id, maps]) => (phone ? ["diffuse"] : maps).map(async (map) => {
    const t = await ktx.loadAsync(`${A}/tex/${id}_${map}.ktx2`);
    t.wrapS = t.wrapT = THREE.RepeatWrapping;
    t.anisotropy = 8;
    pictures.set(`${id}_${map}`, t);
  })));
}

const MODELS = ["wooden_display_shelves_01", "CashRegister_01", "potted_plant_02", "mid_century_lounge_chair", "wooden_stool_01", "hifi"];
let modelsLoading = null;

// loadModels loads the furniture and the room's pictures once; a failure is tried again on the next
// visit. Their KTX2 textures are decoded in workers, stopped once everything is in.
export function loadModels(renderer) {
  if (modelsLoading) return modelsLoading;
  const ktx = new KTX2Loader().setTranscoderPath("/vendor/basis/").setWorkerLimit(2).detectSupport(renderer);
  const gltf = new GLTFLoader().setKTX2Loader(ktx);
  modelsLoading = Promise.all([loadPictures(ktx), ...MODELS.map((id) => gltf.loadAsync(`${A}/${id}/${id}.glb`).then((g) => {
    g.scene.traverse((o) => {
      if (!o.isMesh) return;
      o.castShadow = o.receiveShadow = true;
      if (phone) Object.assign(o.material, { normalMap: null, roughnessMap: null, metalnessMap: null, aoMap: null }); // colour only on a phone, as the room
    });
    return g.scene;
  }))]).then(([, ...scenes]) => (veneer(scenes.at(-1)), scenes), (err) => { modelsLoading = null; throw err; }).finally(() => ktx.dispose());
  return modelsLoading;
}

const GLOW = { opal: 0.9, glass: 1.1, shade: 0.8, vu: 1.6, bulb: 4 }; // what the glass and the meters give off

// veneer puts the oak's picture, darkened to walnut, on the hi-fi's wood: its uvs are in metres. Its
// colours stay: the occlusion Blender baked. The lamps glow softer than Blender's, under the bloom.
function veneer(hifi) {
  const walnut = Object.assign(wood([0.7, 0.7], 0x8a6248, 0.5), { vertexColors: true });
  hifi.traverse((o) => {
    if (!o.isMesh) return;
    if (o.material.name === "walnut") o.material = walnut;
    if (o.material.name in GLOW) o.material.emissiveIntensity = GLOW[o.material.name];
  });
}

// pbr: a material from one of the Poly Haven texture sets loadPictures loaded.
export function pbr(id, repeat, extra = {}) {
  const tex = (map) => {
    const t = pictures.get(`${id}_${map}`)?.clone() ?? null;
    t?.repeat.set(repeat[0], repeat[1]);
    return t;
  };
  return new THREE.MeshStandardMaterial({ map: tex("diffuse"), ...(phone ? {} : { normalMap: tex("nor_gl"), roughnessMap: tex("rough") }), ...extra });
}

export const wood = (repeat, color, roughness) => pbr("oak_veneer_01", repeat, { color, roughness });

export function canvasTexture(w, h, paint) {
  const c = document.createElement("canvas");
  c.width = w;
  c.height = h;
  paint(c.getContext("2d"), w, h);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  t.anisotropy = 8;
  return t;
}

export function fitText(ctx, text, weight, maxW, size) {
  let s = size;
  do ctx.font = `${weight} ${s--}px "Libre Franklin", system-ui, sans-serif`;
  while (ctx.measureText(text).width > maxW && s > 10);
}

export const SLEEVE = new THREE.MeshStandardMaterial({ color: 0xd8d0c0, roughness: 0.8 });

// BACK is the back of a sleeve: two columns of track titles, liner notes and a barcode.
export const BACK = new THREE.MeshStandardMaterial({ roughness: 0.75, map: canvasTexture(256, 256, (ctx, s) => {
  ctx.fillStyle = "#e4dccb";
  ctx.fillRect(0, 0, s, s);
  ctx.fillStyle = "#2b2622";
  ctx.fillRect(18, 18, 120, 9);
  ctx.fillStyle = "rgba(43,38,34,.55)";
  for (let col = 0; col < 2; col++) for (let i = 0; i < 6; i++) ctx.fillRect(18 + col * 116, 44 + i * 14, 50 + ((i * 37 + col * 19) % 50), 5);
  ctx.fillStyle = "rgba(43,38,34,.3)";
  for (let i = 0; i < 9; i++) ctx.fillRect(18, 140 + i * 9, s - 36 - ((i * 23) % 60), 3);
  ctx.fillStyle = "#fff";
  ctx.fillRect(s - 74, s - 46, 56, 30);
  ctx.fillStyle = "#111";
  for (let x = s - 70; x < s - 22; x += 3) if ((x * 7) % 5 < 3) ctx.fillRect(x, s - 42, 2, 22);
}) });

// sleeveGeometry: a 12-inch sleeve standing on its bottom edge; +x is the cover, -x the back.
export function sleeveGeometry(thick) {
  const g = new THREE.BoxGeometry(thick, 0.315, 0.315).translate(0, 0.1575, 0);
  g.clearGroups();
  g.addGroup(0, 6, 0);
  g.addGroup(6, 6, 1);
  g.addGroup(12, 24, 2);
  return g;
}

export function mesh(geo, mat, cast = true) {
  const m = new THREE.Mesh(geo, mat);
  m.castShadow = cast;
  m.receiveShadow = true;
  return m;
}

// hang hangs the lamps of list (lamps.js) from the ceiling, each a model of hifi drawn in one call
// per part for all of its kind, and their rods: a big shop hangs a few hundred.
export function hang(scene, hifi, list) {
  const m = new THREE.Matrix4(), q = new THREE.Quaternion(), e = new THREE.Euler(), one = new THREE.Vector3(1, 1, 1);
  const rods = [];
  for (const kind of new Set(list.map((l) => l.kind))) {
    const mine = list.filter((l) => l.kind === kind);
    instanced(scene, hifi.getObjectByName(kind), mine.map((l) => new THREE.Matrix4().compose(new THREE.Vector3(l.x, l.y, l.z), q.setFromEuler(e.set(0, l.ry, 0)).clone(), one)));
    if (kind === "sconce" || kind === "floorlamp") continue;
    for (const l of mine) for (const k of kind === "rampe" || kind === "billard" ? [-0.6, 0.6] : [0]) {
      rods.push(m.compose(new THREE.Vector3(l.x + k * Math.cos(l.ry), l.y, l.z - k * Math.sin(l.ry)), q.identity(), new THREE.Vector3(1, HEIGHT - l.y, 1)).clone()); // along the bar
    }
  }
  const rod = new THREE.InstancedMesh(new THREE.CylinderGeometry(0.004, 0.004, 1, 6).translate(0, 0.5, 0), new THREE.MeshStandardMaterial({ color: 0x8c7a55, metalness: 0.8, roughness: 0.4 }), rods.length);
  rods.forEach((r, i) => rod.setMatrixAt(i, r));
  scene.add(rod);
}

// lightPool lights the spots nearest the eye with a few spot lights that follow you down the
// aisle: the shaders keep a fixed number of lights, and only the closest cast shadows.
export function lightPool(scene, count, shadows) {
  const lights = Array.from({ length: count }, (_, i) => {
    const l = new THREE.SpotLight(0xffc489, 34, 0, 1.0, 0.85, 2);
    if (i < shadows) {
      l.castShadow = true;
      l.shadow.mapSize.set(1024, 1024);
      l.shadow.bias = -0.0004;
      l.shadow.normalBias = 0.025;
      l.shadow.camera.near = 0.2;
      l.shadow.camera.far = 4;
    }
    scene.add(l, l.target);
    return l;
  });
  return (eye, spots) => {
    const d = (s) => (s.x - eye.x) ** 2 + (s.z - eye.z) ** 2;
    const near = [...spots].sort((a, b) => d(a) - d(b)).slice(0, count);
    near.sort((a, b) => a.z - b.z || a.x - b.x); // a stable order: a light moves only when its spot changes
    lights.forEach((l, i) => {
      const s = near[i];
      if (!s) { l.intensity = 0; return; }
      l.intensity = s.strength ?? 34;
      l.position.set(s.x, s.y - 0.12, s.z);
      l.target.position.set(s.x, 0, s.z);
    });
  };
}

// signs hangs a painted board on two chains for each of list ({ text, range, x, z }), the name on both
// faces, and under it what the aisle holds when range is set. Each board is painted once in one picture, and the boards, their faces and the chains are three
// meshes: a board per rayon of every aisle made 800 draws in a big shop.
export function signs(scene, list) {
  if (!list.length) return;
  const key = (s) => `${s.text.toUpperCase()}\n${s.range ?? ""}`, names = [...new Set(list.map(key))];
  const cw = phone ? 384 : 512, ch = cw * 0.3125, per = Math.floor(2048 / cw), W = per * cw, H = Math.ceil(names.length / per) * ch;
  const atlas = canvasTexture(W, H, (ctx) => names.forEach((name, i) => {
    ctx.save();
    ctx.translate((i % per) * cw, Math.floor(i / per) * ch);
    paintSign(ctx, ...name.split("\n"), cw, ch);
    ctx.restore();
  }));
  const boards = [], faces = [], chains = [], len = HEIGHT - 2.79;
  for (const s of list) {
    const i = names.indexOf(key(s)), col = i % per, row = Math.floor(i / per);
    boards.push(new THREE.BoxGeometry(1.08, 0.34, 0.035).translate(s.x, 2.62, s.z));
    for (const ry of [0, Math.PI]) { // front and back
      const g = new THREE.PlaneGeometry(1.06, 0.32), uv = g.attributes.uv;
      for (let j = 0; j < uv.count; j++) uv.setXY(j, ((col + uv.getX(j)) * cw) / W, 1 - ((row + 1 - uv.getY(j)) * ch) / H);
      faces.push(g.translate(0, 0, 0.0178).rotateY(ry).translate(s.x, 2.62, s.z));
    }
    for (const k of [-1, 1]) chains.push(new THREE.CylinderGeometry(0.006, 0.006, len, 6).translate(s.x + k * 0.45, 2.79 + len / 2, s.z));
  }
  scene.add(mesh(mergeGeometries(boards), new THREE.MeshStandardMaterial({ color: 0x3a2a1c, roughness: 0.7 })),
    mesh(mergeGeometries(faces), new THREE.MeshStandardMaterial({ map: atlas, roughness: 0.6 }), false),
    new THREE.Mesh(mergeGeometries(chains), new THREE.MeshStandardMaterial({ color: 0x8c7a55, metalness: 0.8, roughness: 0.35 })));
}

function paintSign(ctx, text, range, cw, ch) {
  const f = cw / 1024;
  ctx.fillStyle = BOARD;
  ctx.fillRect(0, 0, cw, ch);
  ctx.strokeStyle = BRASS;
  ctx.lineWidth = 8 * f;
  ctx.strokeRect(18 * f, 18 * f, cw - 36 * f, ch - 36 * f);
  ctx.fillStyle = CREAM;
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  fitText(ctx, text, 900, cw - 120 * f, range ? 112 * f : 150 * f);
  ctx.fillText(text, cw / 2, range ? ch * 0.38 : ch / 2 + 6 * f);
  if (!range) return;
  ctx.fillStyle = BRASS;
  fitText(ctx, range, 700, cw - 160 * f, 84 * f);
  ctx.fillText(range, cw / 2, ch * 0.73);
}

// instanced draws every copy of a model in one call per part.
function instanced(scene, model, matrices) {
  model.updateMatrixWorld(true);
  model.traverse((o) => {
    if (!o.isMesh) return;
    const inst = new THREE.InstancedMesh(o.geometry, o.material, matrices.length);
    const m = new THREE.Matrix4();
    matrices.forEach((mat, i) => inst.setMatrixAt(i, m.multiplyMatrices(mat, o.matrixWorld)));
    inst.castShadow = false;
    inst.receiveShadow = true;
    scene.add(inst);
  });
}

export const SPINES = ["#e9e1cf", "#1b1b1d", "#7a2d22", "#203a5c", "#c49a3a", "#3b5a40", "#a14a2f", "#d9d2c0", "#2e2a26", "#5d4a3a", "#8c8478", "#b8462f", "#f2efe8", "#30302c"];

// wallShelves lines both walls from z = from to to with cube shelves packed with records, and
// returns where covers stand faced out in a few cubbies.
export function wallShelves(scene, shelf, rand, width, from, to, perCell) {
  const zs = [];
  for (let z = from; z > to; z -= 1.12) zs.push(z);
  const rows = [0.405, 0.78, 1.155];
  const spineGeo = new THREE.BoxGeometry(0.3, 0.312, 0.0055);
  const spines = new THREE.InstancedMesh(spineGeo, new THREE.MeshStandardMaterial({ roughness: 0.75 }), zs.length * 2 * 9 * perCell);
  spines.receiveShadow = true;
  const m = new THREE.Matrix4(), q = new THREE.Quaternion(), e = new THREE.Euler(), p = new THREE.Vector3(), one = new THREE.Vector3(1, 1, 1), col = new THREE.Color();
  const shelves = [], faceOut = [];
  let n = 0;
  for (const side of [-1, 1]) {
    const x = side * (width / 2 - 0.2), front = -side;
    for (const z of zs) {
      shelves.push(new THREE.Matrix4().compose(new THREE.Vector3(x, 0, z), q.setFromEuler(e.set(0, side < 0 ? 0 : Math.PI, 0)).clone(), one));
      for (const y0 of rows) {
        for (let k = -1; k <= 1; k++) {
          const cz = z + k * 0.36;
          if (rand() < 0.12) {
            faceOut.push({ x: x + front * 0.05, y: y0, z: cz, ry: front > 0 ? 0 : Math.PI, lean: 0.12 });
            continue;
          }
          const fill = Math.floor(perCell * (0.65 + rand() * 0.35));
          for (let i = 0; i < fill; i++) {
            q.setFromEuler(e.set(i === fill - 1 ? 0.25 + rand() * 0.2 : (rand() - 0.5) * 0.04, 0, 0));
            p.set(x + front * (0.02 + rand() * 0.012), y0 + 0.157, cz - 0.159 + i * (0.318 / perCell));
            spines.setMatrixAt(n, m.compose(p, q, one));
            spines.setColorAt(n++, col.set(SPINES[Math.floor(rand() * SPINES.length)]).offsetHSL(0, 0, (rand() - 0.5) * 0.08));
          }
        }
      }
    }
  }
  instanced(scene, shelf, shelves);
  spines.count = n;
  scene.add(spines);
  return faceOut;
}

// coverLedge runs a narrow shelf along the side wall at x from z = from to to, over the bins and the
// cube shelves, and returns where sleeves stand on it faced out, every 35 cm.
export function coverLedge(scene, side, x, from, to) {
  const ledge = mesh(new THREE.BoxGeometry(0.09, 0.03, from - to), ledgeMat);
  ledge.position.set(x, 1.845, (from + to) / 2);
  scene.add(ledge);
  const slots = [];
  for (let z = from - 0.2; z > to + 0.15; z -= 0.35) slots.push({ x: x - side * 0.015, y: 1.86, z, ry: side < 0 ? 0 : Math.PI, lean: 0.16 });
  return slots;
}

const ledgeMat = new THREE.MeshStandardMaterial({ color: 0x3b2818, roughness: 0.6 });

// faceOutCovers stands sleeves up, leaning back, all painted from one atlas of n covers: one draw call.
export function faceOutCovers(scene, slots, atlas, cells, n) {
  const geo = new THREE.PlaneGeometry(0.315, 0.315).rotateY(Math.PI / 2).translate(0.0025, 0.1575, 0);
  const geos = slots.map((s, slot) => {
    const g = geo.clone(), i = slot % n;
    const uv = g.attributes.uv;
    const u0 = (i % cells) / cells, v0 = 1 - (Math.floor(i / cells) + 1) / cells;
    for (let j = 0; j < uv.count; j++) uv.setXY(j, u0 + uv.getX(j) / cells, v0 + uv.getY(j) / cells);
    return g.applyMatrix4(new THREE.Matrix4().makeRotationFromEuler(new THREE.Euler(0, s.ry, s.lean, "YXZ")).setPosition(s.x, s.y, s.z));
  });
  const all = new THREE.BufferGeometry();
  const join = (name, size) => {
    const arr = new Float32Array(geos.reduce((t, g) => t + g.attributes[name].array.length, 0));
    let o = 0;
    for (const g of geos) { arr.set(g.attributes[name].array, o); o += g.attributes[name].array.length; }
    all.setAttribute(name, new THREE.BufferAttribute(arr, size));
  };
  join("position", 3);
  join("normal", 3);
  join("uv", 2);
  const idx = [];
  geos.forEach((g, i) => { for (const j of g.index.array) idx.push(j + i * 4); });
  all.setIndex(idx);
  scene.add(mesh(all, new THREE.MeshStandardMaterial({ map: atlas, roughness: 0.45 }), false));
}

// counter: the till at the back, its turntable turning between the amplifier and the register, two
// speakers on stands either side, and the listening corner before it: the armchair on a rug by a floor
// lamp. It returns the platter, which turns.
export function counter(scene, models, z, back, labelTex) {
  const { register, plant, chair, stool, hifi } = models;
  const hi = new THREE.Group(); // the hi-fi's models, merged once placed
  const place = (model, x, y, zz, ry = 0, s = 1, into = scene) => {
    const o = model.clone();
    o.position.set(x, y, zz);
    o.rotation.y = ry;
    o.scale.setScalar(s);
    o.traverse((m) => { if (m.isMesh) m.castShadow = m.receiveShadow = true; });
    into.add(o);
    return o;
  };
  const top = 1.03, part = (name) => hifi.getObjectByName(name);
  place(part("counter"), 0, 0, z, 0, 1, hi);
  place(part("amplifier"), -1.2, top, z + 0.05, 0.12, 1, hi);
  const deck = place(part("turntable"), -0.45, top, z, -0.08, 1, hi);
  for (const x of [-2.4, 2.4]) {
    place(part("stand"), x, 0, z - 0.05, -Math.sign(x) * 0.25, 1, hi);
    place(part("speaker"), x, 0.6, z - 0.05, -Math.sign(x) * 0.25, 1, hi);
  }
  place(part("floorlamp"), 3.9, 0, z + 2.1, 0, 1, hi);
  hi.updateMatrixWorld(true);
  const platter = deck.getObjectByName("platter");
  scene.attach(platter); // it turns: kept apart
  scene.add(still(hi));
  place(register, 1.15, top, z, Math.PI * 0.94);
  place(plant, -3.45, 0, back + 0.9, 0, 1.6);
  place(chair, 3.2, 0, z + 1.5, -Math.PI * 0.72);
  place(stool, -0.2, 0, z + 0.9);
  rug(scene, 3.1, z + 1.6);
  for (const y of [1.3, 1.7]) {
    const ledge = mesh(new THREE.BoxGeometry(6.6, 0.03, 0.1), ledgeMat);
    ledge.position.set(0, y - 0.015, back + 0.05);
    scene.add(ledge);
  }
  shopName(scene, back);
  return record(platter, labelTex);
}

// still merges the meshes of group, which never move, into one per material: the counter, the hi-fi
// and the floor lamp cost a dozen draws instead of forty.
function still(group) {
  const by = new Map(), out = new THREE.Group();
  group.traverse((o) => {
    if (!o.isMesh) return;
    if (!by.has(o.material)) by.set(o.material, []);
    by.get(o.material).push(o.geometry.clone().applyMatrix4(o.matrixWorld));
  });
  for (const [mat, geos] of by) out.add(mesh(mergeGeometries(geos), mat));
  return out;
}

// record lays a record on platter, its label the cover's.
function record(platter, labelTex) {
  const grooves = canvasTexture(512, 512, (ctx, s) => {
    ctx.fillStyle = "#0c0c0d";
    ctx.fillRect(0, 0, s, s);
    for (let r = s * 0.17; r < s / 2; r += 2.2) {
      ctx.strokeStyle = `rgba(255,255,255,${0.02 + ((r * 13) % 7) / 200})`;
      ctx.beginPath();
      ctx.arc(s / 2, s / 2, r, 0, Math.PI * 2);
      ctx.stroke();
    }
  });
  const black = new THREE.MeshStandardMaterial({ color: 0x0d0d0e, roughness: 0.3 });
  const vinyl = mesh(new THREE.CylinderGeometry(0.149, 0.149, 0.003, 64), [black, new THREE.MeshStandardMaterial({ map: grooves, roughness: 0.28, metalness: 0.1 }), black]);
  vinyl.position.y = 0.0265; // on the mat
  const label = new THREE.Mesh(new THREE.CylinderGeometry(0.048, 0.048, 0.0035, 48), new THREE.MeshStandardMaterial({ map: labelTex, roughness: 0.6 }));
  label.position.y = 0.0268;
  platter.add(vinyl, label);
  return platter;
}

// rug lays a woven rug at x, z, its pattern painted.
function rug(scene, x, z) {
  const tex = canvasTexture(512, 384, (ctx, w, h) => {
    ctx.fillStyle = "#7a2d22";
    ctx.fillRect(0, 0, w, h);
    ctx.strokeStyle = "#e0b870";
    ctx.lineWidth = 10;
    ctx.strokeRect(22, 22, w - 44, h - 44);
    ctx.fillStyle = "#1f3a2e";
    for (let i = 0; i < 5; i++) for (let j = 0; j < 3; j++) {
      ctx.save();
      ctx.translate(72 + i * 92, 92 + j * 100);
      ctx.rotate(Math.PI / 4);
      ctx.fillRect(-26, -26, 52, 52);
      ctx.restore();
    }
  });
  const r = mesh(new THREE.PlaneGeometry(2.2, 1.6), new THREE.MeshStandardMaterial({ map: tex, roughness: 0.95 }), false);
  r.rotation.set(-Math.PI / 2, 0, 0.1);
  r.position.set(x, 0.004, z);
  scene.add(r);
}

function shopName(scene, back) {
  const tex = canvasTexture(2048, 640, (ctx, cw, ch) => {
    ctx.fillStyle = BOARD;
    ctx.fillRect(0, 0, cw, ch);
    ctx.fillStyle = BRASS;
    ctx.fillRect(0, ch - 40, cw, 14);
    ctx.fillRect(0, 26, cw, 14);
    ctx.fillStyle = CREAM;
    ctx.textAlign = "center";
    const name = t("shop3d.sign").toUpperCase();
    fitText(ctx, name, 900, cw - 200, 300);
    ctx.fillText(name, cw / 2, ch * 0.66);
    ctx.font = '500 70px "Libre Franklin", system-ui, sans-serif';
    ctx.fillStyle = BRASS;
    ctx.fillText(`MUSAARR  ·  ${t("shop3d.signSub").toUpperCase()}`, cw / 2, ch * 0.86);
  });
  const board = mesh(new THREE.PlaneGeometry(3.2, 1.0), new THREE.MeshStandardMaterial({ map: tex, roughness: 0.55 }), false);
  board.position.set(0, 2.55, back + 0.02);
  scene.add(board);
}
