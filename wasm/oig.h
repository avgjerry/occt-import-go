// C ABI of the occt-import-go wasm shim ("oig").
//
// All functions are exported from the standalone wasm module and called from
// Go through wazero. Pointers are 32-bit wasm linear-memory offsets. C++
// exceptions never cross this boundary: every failure is an error code, with
// a human-readable message available from oig_last_error().
#ifndef OIG_H
#define OIG_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

enum {
  OIG_OK = 0,
  OIG_ERR_BADARG = -1,      // invalid arguments (null input, zero length)
  OIG_ERR_STEP_PARSE = -2,  // STEP file could not be parsed
  OIG_ERR_TRANSFER = -3,    // STEP → XCAF document transfer failed
  OIG_ERR_EMPTY_DOC = -4,   // no shapes in the document
  OIG_ERR_MESH = -5,        // meshing produced no triangulation
  OIG_ERR_GLTF_WRITE = -6,  // glTF writer failed
  OIG_ERR_EXCEPTION = -7,   // unexpected C++ exception (see oig_last_error)
  OIG_ERR_OOM = -8,         // allocation failure
};

// Returns a static version string, e.g. "occt-import-go (OCCT 7.9.1)".
const char* oig_version(void);

// Converts a STEP file (in memory) to binary glTF (GLB, in memory).
//
//   step_data / step_len   input STEP file bytes
//   linear_deflection      chordal deflection for meshing (model units, or a
//                          bounding-box fraction when relative_deflection!=0)
//   angular_deflection     angular deflection in radians
//   relative_deflection    non-zero: linear_deflection is relative
//   out_glb / out_len      on OIG_OK, *out_glb is a malloc'd buffer owned by
//                          the caller (free with oig_free) and *out_len its size
//
// Returns OIG_OK or a negative OIG_ERR_* code.
int32_t oig_step_to_glb(const uint8_t* step_data, uint32_t step_len,
                        double linear_deflection, double angular_deflection,
                        int32_t relative_deflection, uint8_t** out_glb,
                        uint32_t* out_len);

// Message for the most recent failure on this instance. Static buffer,
// valid until the next oig_* call. Empty string if no failure occurred.
const char* oig_last_error(void);

// Frees a buffer returned via out_glb.
void oig_free(void* p);

#ifdef __cplusplus
}  // extern "C"
#endif

#endif  // OIG_H
