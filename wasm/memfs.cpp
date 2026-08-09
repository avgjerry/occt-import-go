#include "memfs.h"

#include <OSD_StreamBuffer.hxx>

#include <cstring>
#include <map>

IMPLEMENT_STANDARD_RTTIEXT(OigMemFileSystem, OSD_FileSystem)

namespace
{
static const char THE_MEM_SCHEME[] = "mem://";

// url -> bidirectional string buffer. A buffer opened for writing stays
// registered so a later read-open (e.g. RWGltf reading back its .bin.tmp)
// sees the written content through the same stringbuf.
static std::map<std::string, std::shared_ptr<std::stringbuf>>& files()
{
  static std::map<std::string, std::shared_ptr<std::stringbuf>> theFiles;
  return theFiles;
}

static bool isMemUrl(const char* theUrl)
{
  return strncmp(theUrl, THE_MEM_SCHEME, sizeof(THE_MEM_SCHEME) - 1) == 0;
}
}  // namespace

void OigMemFileSystem::RegisterOnce()
{
  static const bool isRegistered = []() {
    OSD_FileSystem::AddDefaultProtocol(new OigMemFileSystem(), /*theIsPreferred*/ true);
    return true;
  }();
  (void)isRegistered;
}

std::shared_ptr<std::stringbuf> OigMemFileSystem::Find(const std::string& theUrl)
{
  auto anIt = files().find(theUrl);
  return anIt != files().end() ? anIt->second : nullptr;
}

void OigMemFileSystem::Clear()
{
  files().clear();
}

Standard_Boolean OigMemFileSystem::IsSupportedPath(const TCollection_AsciiString& theUrl) const
{
  return isMemUrl(theUrl.ToCString());
}

Standard_Boolean OigMemFileSystem::IsOpenIStream(
  const std::shared_ptr<std::istream>& theStream) const
{
  std::shared_ptr<OSD_IStreamBuffer> aStream =
    std::dynamic_pointer_cast<OSD_IStreamBuffer>(theStream);
  return aStream.get() != NULL && isMemUrl(aStream->Url().c_str());
}

Standard_Boolean OigMemFileSystem::IsOpenOStream(
  const std::shared_ptr<std::ostream>& theStream) const
{
  std::shared_ptr<OSD_OStreamBuffer> aStream =
    std::dynamic_pointer_cast<OSD_OStreamBuffer>(theStream);
  return aStream.get() != NULL && isMemUrl(aStream->Url().c_str());
}

std::shared_ptr<std::streambuf> OigMemFileSystem::OpenStreamBuffer(
  const TCollection_AsciiString& theUrl,
  const std::ios_base::openmode  theMode,
  const int64_t                  theOffset,
  int64_t*                       theOutBufSize)
{
  (void)theOffset;
  if (!IsSupportedPath(theUrl))
  {
    return nullptr;
  }
  const std::string anUrl(theUrl.ToCString());

  if ((theMode & std::ios_base::out) != 0)
  {
    // Open-for-write truncates: a fresh buffer replaces any previous file.
    // in|out so the same buffer can later serve the read-back path.
    auto aBuf = std::make_shared<std::stringbuf>(std::ios_base::in | std::ios_base::out
                                                 | std::ios_base::binary);
    files()[anUrl] = aBuf;
    return aBuf;
  }

  auto aBuf = Find(anUrl);
  if (aBuf == nullptr)
  {
    return nullptr;
  }
  if (theOutBufSize != NULL)
  {
    *theOutBufSize = (int64_t)aBuf->str().size();
  }
  // Rewind the get area: the buffer may have been read before.
  aBuf->pubseekoff(0, std::ios_base::beg, std::ios_base::in);
  return aBuf;
}
