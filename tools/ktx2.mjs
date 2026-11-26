// Turns the 3D shop's Poly Haven JPEG textures into KTX2 (Basis Universal), which the GPU keeps
// compressed: a picture takes 0.35 to 1.4 MB of graphics memory instead of 5.6, and downloads in half
// the bytes. Colour maps are ETC1S at 1k; relief, roughness and ARM maps are UASTC at 512 px, as ETC1S
// turned the parquet's reflections into blotches. The .glb files are pointed at the .ktx2
// (KHR_texture_basisu) instead of the .jpg.
// Run it once after adding or changing a texture (the sources are the 1k JPEGs from polyhaven.com):
//   npm i ktx2-encoder@0.6.0 jpeg-js@0.4.4
//   node tools/ktx2.mjs web/shop3d/assets   # then delete the .jpg it encoded
import { encodeToKTX2 } from "ktx2-encoder";
import jpeg from "jpeg-js";
import fs from "node:fs";
import path from "node:path";

const root = process.argv[2];
const files = fs.readdirSync(root, { recursive: true }).map((f) => path.join(root, f));

// half averages each 2×2 block of RGBA pixels.
const half = ({ data, width, height }) => {
  const w = width / 2, h = height / 2, out = new Uint8Array(w * h * 4);
  for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) for (let c = 0; c < 4; c++) {
    const i = (2 * y * width + 2 * x) * 4 + c;
    out[(y * w + x) * 4 + c] = (data[i] + data[i + 4] + data[i + width * 4] + data[i + width * 4 + 4] + 2) >> 2;
  }
  return { data: out, width: w, height: h };
};
const decode = (most) => async (buf) => {
  const { data, width, height } = jpeg.decode(buf, { useTArray: true, formatAsRGBA: true });
  let im = { data: new Uint8Array(data), width, height };
  while (im.width > most) im = half(im);
  return im;
};

// The room's own textures (tex/) were loaded with TextureLoader, which flips them; KTX2 is never
// flipped, so they are flipped here once. glTF textures are not flipped at all.
for (const f of files.filter((f) => f.endsWith(".jpg"))) {
  const colour = /_diff/.test(f), flip = f.includes(`${path.sep}tex${path.sep}`);
  const ktx = await encodeToKTX2(new Uint8Array(fs.readFileSync(f)), colour
    ? { imageDecoder: decode(1024), generateMipmap: true, isYFlip: flip, isUASTC: false, qualityLevel: 160, compressionLevel: 2, isPerceptual: true, isSetKTX2SRGBTransferFunc: true }
    : { imageDecoder: decode(512), generateMipmap: true, isYFlip: flip, isUASTC: true, uastcLDRQualityLevel: 2, enableRDO: true, rdoQualityLevel: 1, needSupercompression: true,
        isNormalMap: /_nor_/.test(f), isPerceptual: false, isSetKTX2SRGBTransferFunc: false });
  fs.writeFileSync(f.replace(/\.jpg$/, ".ktx2"), ktx);
  console.log(f, fs.statSync(f).size, "→", ktx.length);
}

// A .glb is a 12-byte header, a JSON chunk, then the binary chunk; only the JSON changes.
for (const f of files.filter((f) => f.endsWith(".glb"))) {
  const glb = fs.readFileSync(f), size = glb.readUInt32LE(12);
  const gltf = JSON.parse(glb.subarray(20, 20 + size));
  if (gltf.extensionsRequired?.includes("KHR_texture_basisu")) continue;
  for (const im of gltf.images ?? []) Object.assign(im, { uri: im.uri.replace(/\.jpg$/, ".ktx2"), mimeType: "image/ktx2" });
  for (const tx of gltf.textures ?? []) {
    tx.extensions = { ...tx.extensions, KHR_texture_basisu: { source: tx.source } };
    delete tx.source;
  }
  for (const k of ["extensionsUsed", "extensionsRequired"]) gltf[k] = [...(gltf[k] ?? []), "KHR_texture_basisu"];
  let json = Buffer.from(JSON.stringify(gltf));
  json = Buffer.concat([json, Buffer.alloc((4 - (json.length % 4)) % 4, " ")]);
  const head = Buffer.alloc(20);
  head.write("glTF", 0);
  head.writeUInt32LE(2, 4);
  head.writeUInt32LE(20 + json.length + glb.length - 20 - size, 8);
  head.writeUInt32LE(json.length, 12);
  head.write("JSON", 16);
  fs.writeFileSync(f, Buffer.concat([head, json, glb.subarray(20 + size)]));
  console.log(f, "→ KHR_texture_basisu");
}
