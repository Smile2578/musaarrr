# web/vendor

`three.min.js` is three.js r187 (npm `three@0.187.0`, MIT, see `LICENSE`), cut down to the exports
`web/stage.js` and the 3D shop (`web/shop3d/*.js`) import, with the few addons the shop needs, and
minified. One bundle for both, so the browser loads three.js once. three ships no minified build on
npm, so it is rebuilt with:

```sh
npm i three@0.187.0 esbuild@0.24.2
cat > entry.js <<'EOF'
export { WebGLRenderer, Scene, PerspectiveCamera, OrthographicCamera, Group, Mesh, CircleGeometry, CylinderGeometry, PlaneGeometry, BufferGeometry, Float32BufferAttribute, ShaderMaterial, CanvasTexture, WebGLRenderTarget, Vector2,
  DataArrayTexture, DataTexture, InstancedBufferAttribute, LinearFilter, LinearMipmapLinearFilter,
  ACESFilmicToneMapping, AdditiveBlending, BackSide, BoxGeometry, BufferAttribute, Color, Euler, ExtrudeGeometry, FogExp2, HemisphereLight, ImageBitmapLoader, InstancedMesh, LatheGeometry, LoadingManager, Matrix4, MeshBasicMaterial, MeshStandardMaterial, PCFShadowMap, PMREMGenerator, Quaternion, Raycaster, RepeatWrapping, SRGBColorSpace, Shape, SphereGeometry, SpotLight, Texture, TextureLoader, Vector3 } from "three";
export { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
export { RoomEnvironment } from "three/examples/jsm/environments/RoomEnvironment.js";
export { mergeGeometries } from "three/examples/jsm/utils/BufferGeometryUtils.js";
export { EffectComposer } from "three/examples/jsm/postprocessing/EffectComposer.js";
export { RenderPass } from "three/examples/jsm/postprocessing/RenderPass.js";
export { UnrealBloomPass } from "three/examples/jsm/postprocessing/UnrealBloomPass.js";
export { OutputPass } from "three/examples/jsm/postprocessing/OutputPass.js";
export { KTX2Loader } from "three/examples/jsm/loaders/KTX2Loader.js";
EOF
npx esbuild entry.js --bundle --format=esm --minify --legal-comments=inline --target=es2020 --outfile=three.min.js
```

Add an export to that list when `stage.js` or the shop imports a new one (`static_test.go` checks
the shop's addons are there). It is served from Musaarr itself, so the CSP stays `script-src 'self'`.

## basis/

The Basis Universal transcoder (Apache 2.0, see `basis/LICENSE` and `basis/NOTICE`) that `KTX2Loader`
runs in its workers to decode the shop's KTX2 textures, copied from the same npm package and patched:
embind compiles its call wrappers with `new Function`, which the CSP refuses, so they are rewritten as
plain closures (the CSP only adds `'wasm-unsafe-eval'` and `blob:` workers):

```sh
cp node_modules/three/examples/jsm/libs/basis/basis_transcoder.{js,wasm} basis/
node ../../tools/basis-no-eval.mjs basis/basis_transcoder.js
```
