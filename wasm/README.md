# wasm build

This directory produces `internal/wasm/occt.wasm.zst`, the embedded module:
unmodified OCCT 7.9.1 + `shim.cpp`/`memfs.cpp`, compiled to standalone
WebAssembly with Emscripten.

```sh
./build_occt.sh   # 1. fetch pinned OCCT + RapidJSON, build static libs (~30 min)
./build_shim.sh   # 2. compile + link shim, compress artifact into internal/wasm/
```

Prerequisites: [emsdk](https://github.com/emscripten-core/emsdk) (tested with
6.0.6) at `~/emsdk` or on PATH, plus `cmake`, `ninja`, `zstd`.

## Design notes

- **Exception handling.** OCCT relies on C++ exceptions. Every object is
  compiled with `-fwasm-exceptions -sWASM_LEGACY_EXCEPTIONS=0` so LLVM emits
  the standardized **exnref** encoding directly — the only encoding wazero
  accepts. The compile-time flag matters: Emscripten's default is still the
  legacy encoding, and Emscripten links exnref system libraries when the
  setting is 0, so a default-compiled OCCT produces a mixed-encoding module
  that wazero rejects.
- **binaryen runs once, carefully.** Emscripten's own post-link `wasm-opt`
  invocation cannot parse the module while any object still carries the
  legacy EH encoding ("popping from empty stack"), so the shim links at
  `-O0 -sASSERTIONS=0` to keep emcc from invoking it. `build_shim.sh` then
  runs `wasm-opt -O2` itself on the pure-exnref output (18.7MB → 14.1MB)
  with an explicitly enumerated feature list — `--all-features` output uses
  typed-reference constructs that crash wazero's validator. This pass also
  matters functionally: without it, OCCT's machine-generated STEP dispatch
  functions (RWStepAP214_*) exceed wazero's 16MB per-function machine-code
  limit in the compiler engine.
- **Signal conversion is disabled** (`-UOCC_CONVERT_SIGNALS`). OCCT's CMake
  unconditionally enables setjmp-based signal-to-exception conversion, and
  LLVM's wasm-sjlj lowering segfaults when combined with exnref codegen
  (clang crash at every -O level). Wasm has no POSIX signals, so nothing is
  lost: hard faults surface as wasm traps, which wazero returns as Go errors.
- **No filesystem.** `memfs.cpp` registers a `mem://` protocol with OCCT's
  `OSD_FileSystem`, so STEP input and GLB output stay in wasm memory. The
  few `__syscall_*` imports Emscripten's standalone mode leaves in `env` are
  stubbed on the Go side (internal/wasm/runtime.go) and never do real work.
- **Reproducibility.** Sources are fetched at pinned versions with SHA-256
  verification (`build_occt.sh`). CI rebuilds the artifact and compares it
  against the committed one (`.github/workflows/build-wasm.yml`).
