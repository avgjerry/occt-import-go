#!/usr/bin/env bash
# Builds the occt-import-go shim wasm module against the static OCCT
# libraries produced by build_occt.sh, then compresses it into the Go
# package's embedded artifact.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v emcmake >/dev/null 2>&1; then
  # shellcheck disable=SC1091
  source "$HOME/emsdk/emsdk_env.sh" >/dev/null 2>&1
fi

OCCT_INSTALL="$PWD/build/occt-install"
if [ ! -d "$OCCT_INSTALL" ]; then
  echo "OCCT not built yet; run ./build_occt.sh first" >&2
  exit 1
fi

mkdir -p build/shim
emcmake cmake -S . -B build/shim -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_PREFIX_PATH="$OCCT_INSTALL" \
  -DCMAKE_FIND_ROOT_PATH="$OCCT_INSTALL"
ninja -C build/shim

# Optimize with binaryen (>= 131; emsdk 6.0.6 bundles it). The link ran at
# -O0 to keep emcc from invoking wasm-opt on its own (see CMakeLists.txt), so
# this pass does the real link-time optimization: ~18.7MB -> ~14.1MB.
#
# Features are enumerated instead of --all-features on purpose: binaryen's
# richer type output (typed function references) crashes wazero's validator,
# so the output must stay MVP + exceptions + the listed post-MVP features.
wasm-opt \
  --enable-exception-handling --enable-reference-types \
  --enable-bulk-memory --enable-bulk-memory-opt \
  --enable-nontrapping-float-to-int --enable-sign-ext \
  --enable-mutable-globals --enable-multivalue \
  --enable-call-indirect-overlong \
  -O2 build/shim/occt.wasm -o build/shim/occt.opt.wasm

ls -la build/shim/occt.wasm build/shim/occt.opt.wasm
zstd -19 -f build/shim/occt.opt.wasm -o ../internal/wasm/occt.wasm.zst
ls -la ../internal/wasm/occt.wasm.zst
