// The sleeves' pictures. Every face of the shop (a record's cover, a divider's card, a crate's tab) is
// a layer in one of two texture arrays that all the sleeves of a rayon draw in a single call: 64 px for
// the faces you see from the aisle and the crates around the one you dig, 256 px for that crate and the
// faces at hand. The layers go to the faces asked for last (layers.js); a face without one shows its
// colour. A phone kept up to 1 940 separate cover textures before this and iOS closed the tab.
import * as THREE from "../vendor/three.min.js";
import { layerPool } from "./layers.js";

const AT_ONCE = 6; // covers asked to Musaarr at once
const PER_FRAME = 6; // faces drawn into the arrays per frame: a few small uploads each, a frame never waits on many
export const SMALL = 0, BIG = 1; // a face's slots: face.layers[SMALL], face.layers[BIG]
const INKS = [0x1d5ce0, 0xf0b323, 0xef5a3c, 0x34b36b, 0xe9e1cf, 0x2b2622, 0xc7a777]; // printed colours, then kraft
export const KRAFT = INKS.length - 1;

export function coverUrl(it) { return `/api/shop/${it.record.mbid}/cover${it.jp ? "?jp=1" : ""}`; }

// reserve is one texture array of size layers of px², its mipmaps drawn here layer by layer: three's
// own would rebuild every layer's at each upload.
function reserve(renderer, px, size) {
  const levels = Math.log2(px) + 1;
  const tex = new THREE.DataArrayTexture(null, px, px, size);
  Object.assign(tex, { colorSpace: THREE.SRGBColorSpace, minFilter: THREE.LinearMipmapLinearFilter, magFilter: THREE.LinearFilter, generateMipmaps: false, anisotropy: 4 });
  tex.mipmaps = Array.from({ length: levels }, () => ({})); // shortcut: placeholders, so three allocates the levels drawn below
  tex.source.dataReady = false; // allocated empty, then filled one layer at a time
  tex.needsUpdate = true;
  renderer.initTexture(tex);
  const steps = Array.from({ length: levels }, (_, l) => {
    const c = Object.assign(document.createElement("canvas"), { width: px >> l, height: px >> l });
    const ctx = c.getContext("2d");
    ctx.imageSmoothingQuality = "high";
    return { src: new THREE.Texture(c), ctx, s: px >> l };
  });
  const at = new THREE.Vector3();
  // draw paints a face in layer, upside down as WebGL reads rows from the bottom, then halves it level by level.
  function draw(layer, paint) {
    steps.forEach(({ src, ctx, s }, l) => {
      if (l === 0) {
        ctx.setTransform(1, 0, 0, -1, 0, s);
        paint(ctx, s);
      } else ctx.drawImage(steps[l - 1].ctx.canvas, 0, 0, s, s);
      renderer.copyTextureToTexture(src, tex, null, at.set(0, 0, layer), 0, l);
    });
  }
  return { tex, draw, pool: layerPool(size), px };
}

// coverReserves holds both arrays and fills them: covers fetched a few at a time, cards painted, and
// at most PER_FRAME faces drawn per frame (flush). sizes: { small, big } layers.
export function coverReserves(renderer, sizes) {
  const gl = renderer.getContext();
  const most = gl.getParameter(gl.MAX_ARRAY_TEXTURE_LAYERS); // 2 048 on most GPUs, 256 at the least
  const res = [reserve(renderer, 64, Math.min(sizes.small, most)), reserve(renderer, 256, Math.min(sizes.big, most))];
  const fetches = [], ready = [];
  let running = 0;

  const missing = (f) => f.layers.some((l, s) => l >= 0 && f.drawn[s] !== l);
  function lose(f, slot) {
    f.drawn[slot] = -1;
    f.show();
  }
  function pump() {
    while (running < AT_ONCE && fetches.length) {
      const f = fetches.shift();
      f.queued = false;
      if (!missing(f)) continue; // walked past it before its turn
      running++;
      f.loading = true;
      fetch(f.src)
        .then((r) => (r.ok ? r.blob() : Promise.reject(new Error(r.status))))
        .then((b) => createImageBitmap(b))
        .then((bmp) => { f.bitmap = bmp; f.queued = true; ready.push(f); })
        .catch(() => { f.broken = true; }) // no cover: it keeps its colour for this visit
        .finally(() => { f.loading = false; running--; pump(); });
    }
  }
  // want asks for face f in slot (SMALL or BIG) this round; nearest first, as the farthest go without.
  function want(f, slot) {
    if (f.broken) return;
    const got = res[slot].pool.ask(f, slot);
    if (!got) return;
    if (got.lost) lose(got.lost, slot);
    if (f.drawn[slot] === got.layer || f.queued || f.loading) return;
    f.queued = true;
    if (f.paint || f.bitmap) ready.push(f);
    else {
      fetches[slot === BIG ? "unshift" : "push"](f); // the crate you dig comes first
      pump();
    }
  }
  // flush draws the faces whose picture is ready into their layers, then shows them; it returns how many.
  function flush() {
    let n = 0;
    for (; n < PER_FRAME && ready.length; n++) {
      const f = ready.shift();
      f.queued = false;
      f.layers.forEach((layer, slot) => {
        if (layer < 0 || f.drawn[slot] === layer) return;
        res[slot].draw(layer, f.paint ?? ((ctx, s) => ctx.drawImage(f.bitmap, 0, 0, s, s)));
        f.drawn[slot] = layer;
      });
      f.bitmap?.close(); // asked again at 256 px later, it comes from the browser's cache
      f.bitmap = null;
      f.show();
    }
    return n;
  }
  return {
    textures: res.map((r) => r.tex),
    next() { for (const r of res) r.pool.next(); },
    want,
    flush,
    // forget gives back the layers of a face whose crate is gone; a fetch still queued is then skipped.
    forget(f) {
      res.forEach((r, slot) => r.pool.release(f, slot));
      f.broken = true; // never asked for again
    },
    clear() {
      for (const r of res) r.pool.clear();
      for (const f of ready) f.bitmap?.close();
      fetches.length = ready.length = 0;
    },
    held() { return res.map((r) => r.pool.held()); },
  };
}

// face is what a layer shows: a cover (src) or a card painted in code (paint(ctx, size)). show is
// called when its pictures change, to write drawn into the instance that wears it.
export const face = (show, src, paint) => ({ src, paint, show, layers: [-1, -1], drawn: [-1, -1], queued: false, loading: false, broken: false, bitmap: null });

// faceMaterial is a sleeve's or a card's material, its picture taken in the arrays from the instance's
// face attribute: (64 px layer, 256 px layer, colour, owned). The best layer drawn wins, else the
// colour; an owned record wears a green dot in its corner, as in the racks.
export function faceMaterial(covers, card) {
  const m = new THREE.MeshStandardMaterial({ roughness: card ? 0.9 : 0.42 });
  m.defines = card ? { CARD: "" } : {};
  const inks = INKS.map((h) => new THREE.Color(h)), owned = new THREE.Color(0x34b36b);
  m.onBeforeCompile = (sh) => {
    Object.assign(sh.uniforms, { faceSmall: { value: covers.textures[SMALL] }, faceBig: { value: covers.textures[BIG] }, inks: { value: inks }, ownedInk: { value: owned } });
    sh.vertexShader = sh.vertexShader
      .replace("#include <common>", "#include <common>\nattribute vec4 face;\nflat varying vec4 vFace; // flat: an interpolated 2.0 can read 1.9999 and int() picks the wrong colour\nvarying vec2 vFaceUv;")
      .replace("#include <uv_vertex>", "#include <uv_vertex>\nvFace = face;\nvFaceUv = uv;");
    sh.fragmentShader = sh.fragmentShader
      .replace("#include <common>", `#include <common>
uniform sampler2DArray faceSmall;
uniform sampler2DArray faceBig;
uniform vec3 inks[${INKS.length}];
uniform vec3 ownedInk;
flat varying vec4 vFace;
varying vec2 vFaceUv;`)
      .replace("#include <map_fragment>", `{
  vec3 small = texture(faceSmall, vec3(vFaceUv, max(vFace.x, 0.0))).rgb; // both sampled: no branch around a texture read
  vec3 big = texture(faceBig, vec3(vFaceUv, max(vFace.y, 0.0))).rgb;
  vec3 c = vFace.y >= 0.0 ? big : vFace.x >= 0.0 ? small : inks[int(vFace.z)];
  #ifndef CARD
  float dot = (1.0 - smoothstep(0.058, 0.0635, distance(vFaceUv, vec2(0.889)))) * step(0.5, vFace.w);
  c = mix(c, ownedInk, dot);
  #endif
  diffuseColor.rgb *= c;
}`);
  };
  return m;
}

/* ---------- The turntable's label and the sleeves faced out on the walls ---------- */

// Decoded as bitmaps, not <img>: Chrome sizes an <img> by its EXIF density, and a 250 px thumbnail
// tagged as 600 px then fills only a corner of its texture.
const bitmaps = new THREE.ImageBitmapLoader().setOptions({ imageOrientation: "flipY" });

// coverTexture is a texture that shows src once it has arrived.
export function coverTexture(src) {
  const t = new THREE.Texture();
  Object.assign(t, { flipY: false, colorSpace: THREE.SRGBColorSpace }); // already flipped by the decoder
  bitmaps.load(src, (img) => { t.image = img; t.needsUpdate = true; });
  return t;
}

// coverAtlas paints small covers into one texture, for the sleeves faced out on the walls:
// one picture and one draw for all of them. cells is the side of the square grid.
export function coverAtlas(items, cells, onReady) {
  const px = matchMedia("(pointer: coarse)").matches ? 128 : 170; // a phone: 9 MB instead of 34
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = cells * px;
  const ctx = canvas.getContext("2d");
  ctx.fillStyle = "#d8d0c0";
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  const tex = new THREE.CanvasTexture(canvas);
  tex.colorSpace = THREE.SRGBColorSpace;
  let left = items.length;
  items.forEach((it, i) => {
    const img = new Image();
    img.onload = img.onerror = () => {
      if (img.naturalWidth) ctx.drawImage(img, (i % cells) * px, Math.floor(i / cells) * px, px, px);
      if (--left % 12 === 0 || left === 0) tex.needsUpdate = true;
      if (left === 0) onReady?.();
    };
    img.src = coverUrl(it);
  });
  return tex;
}
