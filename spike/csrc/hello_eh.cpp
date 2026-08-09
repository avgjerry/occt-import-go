// Feasibility spike for occt-import-go (Milestone A).
//
// OCCT throws C++ exceptions (Standard_Failure) pervasively, so before
// building OCCT for wasm we verify the full chain in miniature:
//   C++ exceptions -> Emscripten standalone wasm (exnref) -> wazero EH.
//
// Exercised paths:
//   - throw + catch fully inside the guest (the common OCCT case)
//   - rethrow across nested stack frames
//   - an exception escaping to the host (must surface as a Go error, not
//     crash the runtime)
//   - a C string returned to the host (last_error pattern the real shim uses)

#include <cstdint>
#include <stdexcept>
#include <string>

static std::string g_last_error;

namespace {

int32_t helper(int32_t depth) {
  if (depth == 0) {
    throw std::runtime_error("deep");
  }
  try {
    return helper(depth - 1);
  } catch (...) {
    if (depth == 3) {
      return depth;
    }
    throw;
  }
}

}  // namespace

extern "C" {

// Throws and catches internally; returns 42 on the catch path.
int32_t run_caught(int32_t x) {
  try {
    if (x > 0) {
      throw std::runtime_error("expected failure " + std::to_string(x));
    }
    return -1;
  } catch (const std::runtime_error& e) {
    g_last_error = e.what();
    return 42;
  }
}

// Rethrows through 8 frames, caught at depth 3.
int32_t run_nested(void) {
  return helper(8);
}

// Escapes to the host on purpose.
int32_t run_uncaught(void) {
  throw std::logic_error("uncaught on purpose");
}

const char* last_error(void) {
  return g_last_error.c_str();
}

}  // extern "C"
