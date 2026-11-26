// layerPool hands a fixed number of texture layers to the faces that ask for one. Asked again, a face
// keeps its layer; when none is free, the layer asked for longest ago changes hands, unless it was
// asked for in this same round: then there are more faces to show than layers, and the face asked
// last (the farthest, as the shop asks nearest first) goes without. No three.js here: node tests it.
export function layerPool(size) {
  const fresh = () => Array.from({ length: size }, (_, i) => size - 1 - i); // pop() hands out layer 0 first
  const free = fresh();
  const owner = new Array(size).fill(null);
  const asked = new Map(); // layer -> round it was last asked for, oldest first
  let round = 0;
  return {
    next() { round++; },
    // ask returns face's layer in this pool, slot being where the face keeps it (face.layers[slot]),
    // and the face that lost it to make room, or null when it gets none.
    ask(face, slot) {
      const had = face.layers[slot];
      if (had >= 0) {
        asked.delete(had);
        asked.set(had, round);
        return { layer: had, lost: null };
      }
      let layer = free.pop(), lost = null;
      if (layer === undefined) {
        const [old, when] = asked.entries().next().value;
        if (when === round) return null;
        layer = old;
        lost = owner[old];
        lost.layers[slot] = -1;
        asked.delete(old);
      }
      owner[layer] = face;
      face.layers[slot] = layer;
      asked.set(layer, round);
      return { layer, lost };
    },
    // release gives face's layer back, for a face whose crate is gone.
    release(face, slot) {
      const layer = face.layers[slot];
      if (layer < 0) return;
      face.layers[slot] = -1;
      owner[layer] = null;
      asked.delete(layer);
      free.push(layer);
    },
    // clear gives every layer back, for a shop built again.
    clear() {
      for (const f of owner) if (f) f.layers.fill(-1);
      owner.fill(null);
      asked.clear();
      free.splice(0, free.length, ...fresh());
    },
    held() { return asked.size; },
  };
}
