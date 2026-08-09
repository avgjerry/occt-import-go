# occt-import-go

Convert **STEP** CAD files to **GLB** (binary glTF) in pure Go.

This package embeds [Open CASCADE Technology (OCCT)](https://dev.opencascade.org/)
— the same CAD kernel behind [occt-import-js](https://github.com/kovacsv/occt-import-js) —
compiled to WebAssembly, and runs it in-process under
[wazero](https://wazero.io), a zero-dependency pure-Go WebAssembly runtime.

- **No cgo, no system dependencies.** `go get` and it works: Linux
  (glibc *and* musl/Alpine), macOS, amd64/arm64 — anywhere Go runs.
- **Names, colors, and assembly hierarchy** from the STEP file are preserved
  in the GLB output (via OCCT's XCAF document + `RWGltf_CafWriter` pipeline).
- **Goroutine-safe.** The wasm module is compiled once; each conversion runs
  in an isolated, short-lived instance whose memory is reclaimed immediately.

## Usage

```go
import occt "github.com/avgjerry/occt-import-go"

// One-shot:
glb, err := occt.StepToGLB(ctx, stepBytes)

// Server-style (compile the wasm once, convert many times, concurrently):
conv, err := occt.NewConverter(ctx)
defer conv.Close(ctx)

glb, err = conv.StepToGLB(ctx, stepBytes,
    occt.WithLinearDeflection(0.001),  // mesh quality: chordal deflection
    occt.WithAngularDeflection(0.5),   // mesh quality: radians
)
```

Mesh quality defaults to a bounding-box-relative linear deflection of `0.001`
and an angular deflection of `0.5` rad (the same defaults as occt-import-js),
so results are scale-independent. Use `WithRelativeDeflection(false)` to
switch the linear deflection to absolute model units (millimeters for most
STEP files).

Errors are matchable with `errors.Is`: `ErrInvalidInput`, `ErrStepParse`,
`ErrEmptyModel`, `ErrConversion`. Conversions honor context cancellation and
deadlines.

## How it works

```
STEP bytes ─▶ STEPCAFControl_Reader ─▶ XCAF document ─▶ BRepMesh_IncrementalMesh
                                                             │
GLB bytes  ◀─ in-memory mem:// filesystem ◀─ RWGltf_CafWriter┘
        (all inside a WebAssembly sandbox hosted by wazero — no file I/O)
```

The C++ side is a ~400-line shim (`wasm/shim.cpp`) over unmodified OCCT
7.9.1, built as a standalone wasm module with Emscripten using the
standardized WebAssembly exception-handling (exnref) encoding, which wazero
supports as an experimental feature. The build is reproducible: pinned,
checksummed sources (`wasm/build_occt.sh`) and a pinned Emscripten version.

Because execution is single-threaded wasm, one conversion uses one core and
runs a few times slower than native OCCT — a fine trade for a dependency-free
`go get` on a server that can run conversions concurrently. Ballpark: the
88KB `screw.step` test fixture converts in ~7s on an Apple M1 Pro at default
mesh quality (plus a one-time ~13s wasm compilation at startup). If you need
maximum single-conversion throughput, a cgo backend is a possible future
addition.

## Rebuilding the wasm module

```sh
./wasm/build_occt.sh   # fetch + build OCCT 7.9.1 static wasm libs (once, ~30 min)
./wasm/build_shim.sh   # build shim, link, compress into internal/wasm/occt.wasm.zst
```

Requires cmake, ninja, zstd, and [emsdk](https://github.com/emscripten-core/emsdk).
CI (`.github/workflows/build-wasm.yml`) rebuilds the artifact and checks it
against the committed one.

## Scope & roadmap

v1 converts STEP → GLB. Candidates for later: IGES input, Draco compression,
a mesh-data API (geometry access without GLB), OCCT 8.x.

## License

MIT for this package. The embedded wasm module contains OCCT, licensed
LGPL-2.1 with the Open CASCADE Exception (which permits this form of
distribution), and RapidJSON (MIT) — see
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
