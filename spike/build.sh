#!/usr/bin/env bash
# Builds the Milestone A spike wasm binaries.
# Requires an activated emsdk (source ~/emsdk/emsdk_env.sh).
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v em++ >/dev/null 2>&1; then
  if [ -f "$HOME/emsdk/emsdk_env.sh" ]; then
    # shellcheck disable=SC1091
    source "$HOME/emsdk/emsdk_env.sh" >/dev/null 2>&1
  else
    echo "em++ not found; install emsdk first" >&2
    exit 1
  fi
fi

COMMON=(
  -O2
  --no-entry
  -sSTANDALONE_WASM=1
  -sALLOW_MEMORY_GROWTH=1
  -sEXPORTED_FUNCTIONS=_run_caught,_run_nested,_run_uncaught,_last_error,_malloc,_free
)

# Primary path: standardized exnref exception handling, emitted directly.
em++ csrc/hello_eh.cpp -o eh_exnref.wasm "${COMMON[@]}" \
  -fwasm-exceptions -sWASM_LEGACY_EXCEPTIONS=0

# Fallback path: legacy EH opcodes translated to exnref by binaryen.
em++ csrc/hello_eh.cpp -o eh_legacy.wasm "${COMMON[@]}" \
  -fwasm-exceptions -sWASM_LEGACY_EXCEPTIONS=1
wasm-opt --all-features --translate-to-exnref \
  eh_legacy.wasm -o eh_legacy_translated.wasm

echo "--- imports of eh_exnref.wasm ---"
wasm-objdump -x eh_exnref.wasm | sed -n '/Import\[/,/^Function\[/p'
ls -la eh_exnref.wasm eh_legacy_translated.wasm
