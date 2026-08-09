#!/usr/bin/env bash
# Fetches OCCT + RapidJSON at pinned versions and builds OCCT as static
# WebAssembly libraries with Emscripten (standalone, exnref C++ exceptions).
#
# Requires: emsdk (activated or at ~/emsdk), cmake, ninja, curl, shasum.
#
# Outputs: wasm/build/occt-install/{lib,include}
set -euo pipefail
cd "$(dirname "$0")"

OCCT_VERSION="V7_9_1"
OCCT_URL="https://github.com/Open-Cascade-SAS/OCCT/archive/refs/tags/${OCCT_VERSION}.tar.gz"
# SHA-256 of the GitHub source tarball for V7_9_1 (printed on first fetch,
# then pinned here).
OCCT_SHA256="de442298cd8860f5580b01007f67f0ecd0b8900cfa4da467fa3c823c2d1a45df"

RAPIDJSON_COMMIT="ab1842a2dae061284c0a62dca1cc6d5e7e37e346"  # header-only
RAPIDJSON_URL="https://github.com/Tencent/rapidjson/archive/${RAPIDJSON_COMMIT}.tar.gz"
RAPIDJSON_SHA256="39f96f17b40f7201042c9b45d6444cb7eae1b7adfb7455412a86f6140450d32d"

THIRD_PARTY="$PWD/third_party"
BUILD_DIR="$PWD/build"
NPROC="$(sysctl -n hw.ncpu 2>/dev/null || nproc)"

if ! command -v emcmake >/dev/null 2>&1; then
  # shellcheck disable=SC1091
  source "$HOME/emsdk/emsdk_env.sh" >/dev/null 2>&1
fi

fetch() { # url sha256 dest_dir
  local url="$1" sha="$2" dest="$3" tmpdir tarball
  if [ -d "$dest" ]; then
    echo "already fetched: $dest"
    return
  fi
  # mktemp -d with an explicit template is portable (BSD/macOS and GNU).
  tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/occt-fetch.XXXXXX")"
  tarball="$tmpdir/archive.tar.gz"
  echo "fetching $url"
  curl -fsSL "$url" -o "$tarball"
  local got
  got="$(shasum -a 256 "$tarball" | cut -d' ' -f1)"
  if [ "$sha" = "TBD" ]; then
    echo "FIRST FETCH: sha256 of $url is $got — pin it in this script"
  elif [ "$got" != "$sha" ]; then
    echo "checksum mismatch for $url: got $got want $sha" >&2
    exit 1
  fi
  mkdir -p "$dest"
  tar -xzf "$tarball" -C "$dest" --strip-components=1
  rm -rf "$tmpdir"
}

fetch "$OCCT_URL" "$OCCT_SHA256" "$THIRD_PARTY/occt"
fetch "$RAPIDJSON_URL" "$RAPIDJSON_SHA256" "$THIRD_PARTY/rapidjson"

mkdir -p "$BUILD_DIR/occt"
cd "$BUILD_DIR/occt"

# -fwasm-exceptions must be on EVERY object file: OCCT throws Standard_Failure
# pervasively and mixing wasm-EH modes across objects is undefined behavior.
# -sWASM_LEGACY_EXCEPTIONS=0 makes LLVM emit the standardized exnref encoding
# at CODEGEN time (Emscripten's default is still the legacy encoding), which
# is what wazero's exception-handling support accepts (it rejects legacy EH).
# It must match the link-time setting in CMakeLists.txt.
#
# -UOCC_CONVERT_SIGNALS: OCCT's CMake unconditionally enables setjmp-based
# signal-to-exception conversion (OCC_CATCH_SIGNALS), and LLVM's wasm-sjlj
# lowering segfaults when combined with exnref codegen (clang crash in e.g.
# BRepCheck_Analyzer.cxx at every -O level). Wasm has no POSIX signals, so
# the machinery is dead weight here anyway; hard failures surface as wasm
# traps and become Go errors.
EH_FLAGS="-fwasm-exceptions -sWASM_LEGACY_EXCEPTIONS=0 -UOCC_CONVERT_SIGNALS"

emcmake cmake ../../third_party/occt -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DBUILD_LIBRARY_TYPE=Static \
  -DCMAKE_INSTALL_PREFIX="$BUILD_DIR/occt-install" \
  -DBUILD_MODULE_ApplicationFramework=ON \
  -DBUILD_MODULE_DataExchange=ON \
  -DBUILD_MODULE_ModelingAlgorithms=ON \
  -DBUILD_MODULE_ModelingData=ON \
  -DBUILD_MODULE_FoundationClasses=ON \
  -DBUILD_MODULE_Visualization=ON \
  -DBUILD_MODULE_Draw=OFF \
  -DBUILD_MODULE_DETools=OFF \
  -DBUILD_DOC_Overview=OFF \
  -DUSE_TK=OFF \
  -DUSE_TCL=OFF \
  -DUSE_TBB=OFF \
  -DUSE_OPENGL=OFF \
  -DUSE_GLES2=OFF \
  -DUSE_FREETYPE=OFF \
  -DUSE_XLIB=OFF \
  -DUSE_DRACO=OFF \
  -DUSE_FREEIMAGE=OFF \
  -DUSE_OPENVR=OFF \
  -DUSE_FFMPEG=OFF \
  -DUSE_RAPIDJSON=ON \
  -D3RDPARTY_RAPIDJSON_DIR="$THIRD_PARTY/rapidjson" \
  -D3RDPARTY_RAPIDJSON_INCLUDE_DIR="$THIRD_PARTY/rapidjson/include" \
  -DCMAKE_CXX_FLAGS="$EH_FLAGS" \
  -DCMAKE_C_FLAGS="$EH_FLAGS"

ninja -j"$NPROC"
ninja install
echo "OCCT static wasm libraries installed to $BUILD_DIR/occt-install"
