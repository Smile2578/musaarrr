// The light baked once, when the shop is built: what all its lamps (lamps.js) cast on the floor and the
// walls, and how the floor darkens under the bins and at their feet. Grids of numbers, which room.js
// makes into lightmaps; the real lights of the shop then only follow you. No three.js here: node tests it.

const FAR = 8; // metres: a lamp farther off adds next to nothing to a cell, and is skipped
const BULB = 0.35 ** 2; // a lamp is no point: its light right next to it stays finite

const dot = (a, b) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];

// irradiance is the light on each cell of surface s from lamps ({ x, y, z, strength }): a grid of
// s.nu × s.nv cells, the first centred at s.o, the next across at + s.u and the next row at + s.v
// (s.u and s.v at right angles), facing s.n. A lamp shines alike all round; on a cell its light
// falls off with the square of the distance, and with the slant it arrives at.
export function irradiance(s, lamps) {
  const out = new Float32Array(s.nu * s.nv);
  const du = Math.hypot(...s.u), dv = Math.hypot(...s.v);
  const U = s.u.map((c) => c / du), V = s.v.map((c) => c / dv);
  for (const l of lamps) {
    const p = [l.x - s.o[0], l.y - s.o[1], l.z - s.o[2]];
    const h = dot(p, s.n);
    if (h <= 0) continue; // behind the surface
    const cu = dot(p, U) / du, cv = dot(p, V) / dv;
    const i0 = Math.max(0, Math.floor(cu - FAR / du)), i1 = Math.min(s.nu - 1, Math.ceil(cu + FAR / du));
    const j0 = Math.max(0, Math.floor(cv - FAR / dv)), j1 = Math.min(s.nv - 1, Math.ceil(cv + FAR / dv));
    for (let j = j0; j <= j1; j++) {
      const b = (j - cv) * dv;
      for (let i = i0; i <= i1; i++) {
        const a = (i - cu) * du, d2 = a * a + b * b + h * h + BULB;
        out[j * s.nu + i] += (l.strength * h) / (d2 * Math.sqrt(d2));
      }
    }
  }
  return out;
}

// shade darkens grid g of floor s (s.u along x, s.v along z) under each of rects ({ x0, x1, z0, z1 })
// and within reach metres of it: the light that the bins and the walls keep from their feet.
export function shade(g, s, rects, reach) {
  const ix = (x) => (x - s.o[0]) / s.u[0], jz = (z) => (z - s.o[2]) / s.v[2];
  for (const r of rects) {
    const [i0, i1] = [ix(r.x0 - reach), ix(r.x1 + reach)].sort((a, b) => a - b);
    const [j0, j1] = [jz(r.z0 - reach), jz(r.z1 + reach)].sort((a, b) => a - b);
    for (let j = Math.max(0, Math.ceil(j0)); j <= Math.min(s.nv - 1, Math.floor(j1)); j++) {
      const z = s.o[2] + j * s.v[2], dz = Math.max(r.z0 - z, 0, z - r.z1);
      for (let i = Math.max(0, Math.ceil(i0)); i <= Math.min(s.nu - 1, Math.floor(i1)); i++) {
        const x = s.o[0] + i * s.u[0], d = Math.hypot(Math.max(r.x0 - x, 0, x - r.x1), dz);
        g[j * s.nu + i] *= d === 0 ? 0.3 : 1 - 0.6 * (1 - Math.min(1, d / reach)) ** 2;
      }
    }
  }
  return g;
}

// texels are grid g as RGBA bytes tinted by tint ([r, g, b], 0 to 1), the light e shown as
// 1 - exp(-e * exposure): a soft limit, so a wall right by a lamp is bright, not blown out.
export function texels(g, tint, exposure) {
  const out = new Uint8Array(g.length * 4);
  g.forEach((e, k) => {
    const v = 1 - Math.exp(-e * exposure);
    for (let c = 0; c < 3; c++) out[k * 4 + c] = Math.round(255 * v * tint[c]);
    out[k * 4 + 3] = 255;
  });
  return out;
}
