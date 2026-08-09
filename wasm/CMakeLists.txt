cmake_minimum_required(VERSION 3.16)
project(occt_import_go_shim CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

if(NOT EMSCRIPTEN)
  message(FATAL_ERROR "This shim targets Emscripten; configure with emcmake.")
endif()

# Static OCCT built by build_occt.sh (pass -DCMAKE_PREFIX_PATH=.../occt-install).
find_package(OpenCASCADE REQUIRED CONFIG)

add_executable(occt_import_go shim.cpp memfs.cpp)
set_target_properties(occt_import_go PROPERTIES OUTPUT_NAME "occt" SUFFIX ".wasm")

target_include_directories(occt_import_go PRIVATE ${OpenCASCADE_INCLUDE_DIR})

# Linking these four pulls the full transitive OCCT closure via exported targets.
target_link_libraries(occt_import_go PRIVATE TKDEGLTF TKDESTEP TKMesh TKXCAF)

# -fwasm-exceptions + WASM_LEGACY_EXCEPTIONS=0 everywhere (compile AND link,
# here and in build_occt.sh): OCCT throws Standard_Failure pervasively,
# mixing wasm-EH encodings across objects produces a module wazero rejects,
# and wazero only accepts the standardized exnref encoding.
#
# The link runs at -O0 with assertions off: Emscripten's post-link wasm-opt
# (binaryen) fails to parse this module ("popping from empty stack", present
# in both emsdk 6.0.6 and binaryen 131), so binaryen must not touch it —
# LLVM's directly-emitted exnref output is what ships. Objects are still
# compiled -O2.
#
# STACK_SIZE=10MB per occt-import-js precedent: the default 64KB overflows in
# OCCT's recursive geometry code.
# -UOCC_CONVERT_SIGNALS must match build_occt.sh (the imported OpenCASCADE
# targets re-inject the define via INTERFACE_COMPILE_DEFINITIONS; the -U in
# compile options comes later on the command line and wins).
target_compile_options(occt_import_go PRIVATE
  -O2 -fwasm-exceptions "SHELL:-s WASM_LEGACY_EXCEPTIONS=0"
  -UOCC_CONVERT_SIGNALS)
target_link_options(occt_import_go PRIVATE
  -O0
  -fwasm-exceptions
  --no-entry
  "SHELL:-s ASSERTIONS=0"
  "SHELL:-s WASM_LEGACY_EXCEPTIONS=0"
  "SHELL:-s STANDALONE_WASM=1"
  "SHELL:-s ALLOW_MEMORY_GROWTH=1"
  "SHELL:-s INITIAL_MEMORY=64MB"
  "SHELL:-s STACK_SIZE=10MB"
  "SHELL:-s EXPORTED_FUNCTIONS=_oig_version,_oig_step_to_glb,_oig_last_error,_oig_free,_malloc,_free"
)
