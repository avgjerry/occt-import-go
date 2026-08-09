// In-memory OSD_FileSystem for the "mem://" scheme.
//
// RWGltf_CafWriter performs all of its output through
// OSD_FileSystem::DefaultFileSystem(), so registering this protocol lets the
// shim run the whole STEP -> GLB pipeline without touching any real
// filesystem (Emscripten standalone WASI file support is incomplete, and the
// Go host would otherwise need to mount directories).
//
// Single-threaded by design: the wasm module runs one conversion at a time.
#ifndef OIG_MEMFS_H
#define OIG_MEMFS_H

#include <OSD_FileSystem.hxx>

#include <memory>
#include <sstream>
#include <string>

class OigMemFileSystem : public OSD_FileSystem
{
  DEFINE_STANDARD_RTTIEXT(OigMemFileSystem, OSD_FileSystem)
public:
  // Registers the mem:// protocol as the preferred default protocol.
  // Safe to call repeatedly; only the first call registers.
  static void RegisterOnce();

  // Returns the buffer for a mem:// url, or nullptr if absent.
  static std::shared_ptr<std::stringbuf> Find(const std::string& theUrl);

  // Drops all in-memory files (called between conversions).
  static void Clear();

public:
  Standard_Boolean IsSupportedPath(const TCollection_AsciiString& theUrl) const override;

  Standard_Boolean IsOpenIStream(const std::shared_ptr<std::istream>& theStream) const override;

  Standard_Boolean IsOpenOStream(const std::shared_ptr<std::ostream>& theStream) const override;

  std::shared_ptr<std::streambuf> OpenStreamBuffer(const TCollection_AsciiString& theUrl,
                                                   const std::ios_base::openmode  theMode,
                                                   const int64_t                  theOffset = 0,
                                                   int64_t* theOutBufSize = NULL) override;
};

#endif  // OIG_MEMFS_H
