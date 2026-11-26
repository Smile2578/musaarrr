// The shop seen from above in a corner of the screen: its units, the counter, you and where you look,
// the door at the bottom. A click on it walks you there. It is aria-hidden: the rayons' buttons take a
// keyboard or a screen reader anywhere in the shop.

// floorMap draws floor f (floor.js) on canvas, with the obstacles besides the bins (the counter, the
// shelves). at turns a point clicked on it into one on the floor; draw shows you at x, z facing yaw.
export function floorMap(canvas, f, obstacles) {
  const box = matchMedia("(pointer: coarse)").matches ? 116 : 176; // CSS pixels, the longest side
  const depth = f.door + 1.5 - f.back;
  const s = box / Math.max(f.width, depth);
  const w = Math.round(f.width * s), h = Math.round(depth * s), r = Math.min(devicePixelRatio, 2);
  Object.assign(canvas, { width: w * r, height: h * r });
  Object.assign(canvas.style, { width: `${w}px`, height: `${h}px` });
  const px = (x) => (x + f.width / 2) * s, pz = (z) => (z - f.back) * s;
  const base = document.createElement("canvas");
  Object.assign(base, { width: w * r, height: h * r });
  const b = base.getContext("2d");
  b.scale(r, r);
  const rect = (o) => b.fillRect(px(o.x0), pz(o.z0), (o.x1 - o.x0) * s, (o.z1 - o.z0) * s);
  b.fillStyle = "#c49a6c";
  f.blocks.forEach(rect);
  b.fillStyle = "#5d4a3a";
  obstacles.forEach(rect);
  const ctx = canvas.getContext("2d");
  return {
    at: (x, y) => ({ x: x / s - f.width / 2, z: y / s + f.back }),
    draw(x, z, yaw) {
      ctx.setTransform(r, 0, 0, r, 0, 0);
      ctx.clearRect(0, 0, w, h);
      ctx.drawImage(base, 0, 0, w, h);
      const cx = px(x), cy = pz(z), a = Math.atan2(-Math.cos(yaw), -Math.sin(yaw)); // yaw 0 looks up the map, toward -z
      ctx.fillStyle = "rgb(239 230 210 / 30%)";
      ctx.beginPath();
      ctx.moveTo(cx, cy);
      ctx.arc(cx, cy, 22, a - 0.5, a + 0.5);
      ctx.fill();
      ctx.fillStyle = "#c9a25b";
      ctx.beginPath();
      ctx.arc(cx, cy, 4, 0, Math.PI * 2);
      ctx.fill();
    },
  };
}
