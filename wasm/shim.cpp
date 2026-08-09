// occt-import-go wasm shim: STEP/IGES bytes in -> GLB bytes out.
//
// Pipeline (mirrors what cascadio does for Python, written from scratch):
//   STEPCAFControl_Reader::ReadStream   (STEP, from memory)
//   IGESCAFControl_Reader::ReadFile     (IGES, via the fopen interposer
//                                        below: OCCT's IGES reader has no
//                                        stream API, and the standalone
//                                        Emscripten libc cannot open real
//                                        files, so a magic path serves the
//                                        in-memory buffer through fmemopen)
//   -> XCAF TDocStd_Document
//   BRepMesh_IncrementalMesh            -> triangulation (single-threaded)
//   RWGltf_CafWriter                    -> binary glTF written to mem://
//
// C++ exceptions never escape the extern "C" boundary; see oig.h.

#include "memfs.h"
#include "oig.h"

#include <BRepMesh_IncrementalMesh.hxx>
#include <BRep_Tool.hxx>
#include <IFSelect_ReturnStatus.hxx>
#include <IGESCAFControl_Reader.hxx>
#include <IMeshTools_Parameters.hxx>
#include <Message_ProgressRange.hxx>
#include <Poly_Triangulation.hxx>
#include <RWGltf_CafWriter.hxx>
#include <STEPCAFControl_Reader.hxx>
#include <Standard_Failure.hxx>
#include <Standard_Version.hxx>
#include <TColStd_IndexedDataMapOfStringString.hxx>
#include <TDF_LabelSequence.hxx>
#include <TDocStd_Document.hxx>
#include <TopAbs_ShapeEnum.hxx>
#include <TopExp_Explorer.hxx>
#include <TopLoc_Location.hxx>
#include <TopoDS.hxx>
#include <TopoDS_Shape.hxx>
#include <XCAFApp_Application.hxx>
#include <XCAFDoc_DocumentTool.hxx>
#include <XCAFDoc_ShapeTool.hxx>

#include <cerrno>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <exception>
#include <functional>
#include <sstream>
#include <string>

namespace
{
std::string g_last_error;

const char THE_GLB_URL[] = "mem://out.glb";

// Magic path served by the fopen interposer from g_fopen_input.
const char THE_MEM_INPUT_PATH[] = "/oig/input";
std::string g_fopen_input;

void setError(const std::string& theMessage)
{
  g_last_error = theMessage;
}

// Configures a CAF reader (STEP or IGES; they share the interface).
template <typename ReaderT>
ReaderT makeReader()
{
  ReaderT aReader;
  aReader.SetColorMode(true);
  aReader.SetNameMode(true);
  aReader.SetLayerMode(true);
  return aReader;
}

// Parses + transfers with a reader-specific load step.
template <typename ReaderT>
int32_t readInto(const char* theFormatName,
                 const std::function<IFSelect_ReturnStatus(ReaderT&)>& theLoad,
                 const Handle(TDocStd_Document)& theDoc)
{
  ReaderT aReader = makeReader<ReaderT>();
  if (theLoad(aReader) != IFSelect_RetDone)
  {
    setError(std::string(theFormatName) + " file could not be parsed");
    return OIG_ERR_PARSE;
  }
  if (!aReader.Transfer(theDoc))
  {
    setError(std::string(theFormatName) + " to XCAF document transfer failed");
    return OIG_ERR_TRANSFER;
  }
  return OIG_OK;
}

// Meshes the document and writes it as GLB into the mem:// filesystem,
// returning the bytes through the malloc'd out-params.
int32_t meshAndWrite(const Handle(TDocStd_Document)& theDoc, double theLinearDeflection,
                     double theAngularDeflection, bool theRelativeDeflection,
                     uint8_t** theOutGlb, uint32_t* theOutLen)
{
  Handle(XCAFDoc_ShapeTool) aShapeTool = XCAFDoc_DocumentTool::ShapeTool(theDoc->Main());
  TDF_LabelSequence         aRootLabels;
  aShapeTool->GetFreeShapes(aRootLabels);
  if (aRootLabels.IsEmpty())
  {
    setError("document contains no shapes");
    return OIG_ERR_EMPTY_DOC;
  }

  IMeshTools_Parameters aMeshParams;
  aMeshParams.Deflection = theLinearDeflection;
  aMeshParams.Angle      = theAngularDeflection;
  aMeshParams.Relative   = theRelativeDeflection;
  aMeshParams.InParallel = false;  // wasm build is single-threaded

  bool hasTriangulation = false;
  for (TDF_LabelSequence::Iterator aRootIter(aRootLabels); aRootIter.More(); aRootIter.Next())
  {
    TopoDS_Shape aShape;
    if (!XCAFDoc_ShapeTool::GetShape(aRootIter.Value(), aShape) || aShape.IsNull())
    {
      continue;
    }
    BRepMesh_IncrementalMesh aMesher(aShape, aMeshParams);
    (void)aMesher;
    for (TopExp_Explorer aFaceIter(aShape, TopAbs_FACE); !hasTriangulation && aFaceIter.More();
         aFaceIter.Next())
    {
      TopLoc_Location aLoc;
      hasTriangulation =
        !BRep_Tool::Triangulation(TopoDS::Face(aFaceIter.Current()), aLoc).IsNull();
    }
  }
  if (!hasTriangulation)
  {
    setError("meshing produced no triangulation");
    return OIG_ERR_MESH;
  }

  {
    RWGltf_CafWriter aWriter(THE_GLB_URL, /*theIsBinary*/ true);
    aWriter.SetMergeFaces(true);
    TColStd_IndexedDataMapOfStringString aFileInfo;
    if (!aWriter.Perform(theDoc, aFileInfo, Message_ProgressRange()))
    {
      setError("glTF writer failed");
      return OIG_ERR_GLTF_WRITE;
    }
  }

  std::shared_ptr<std::stringbuf> aGlbBuf = OigMemFileSystem::Find(THE_GLB_URL);
  if (aGlbBuf == nullptr)
  {
    setError("glTF writer produced no output");
    return OIG_ERR_GLTF_WRITE;
  }
  const std::string aGlb = aGlbBuf->str();
  OigMemFileSystem::Clear();
  if (aGlb.empty())
  {
    setError("glTF writer produced empty output");
    return OIG_ERR_GLTF_WRITE;
  }

  uint8_t* anOut = static_cast<uint8_t*>(std::malloc(aGlb.size()));
  if (anOut == NULL)
  {
    setError("out of memory allocating GLB buffer");
    return OIG_ERR_OOM;
  }
  std::memcpy(anOut, aGlb.data(), aGlb.size());
  *theOutGlb = anOut;
  *theOutLen = (uint32_t)aGlb.size();
  return OIG_OK;
}

// Shared conversion driver: reads a document via theRead, then meshes/writes.
int32_t convert(const std::function<int32_t(const Handle(TDocStd_Document)&)>& theRead,
                double theLinearDeflection, double theAngularDeflection,
                bool theRelativeDeflection, uint8_t** theOutGlb, uint32_t* theOutLen)
{
  OigMemFileSystem::RegisterOnce();
  OigMemFileSystem::Clear();

  Handle(TDocStd_Document) aDoc;
  XCAFApp_Application::GetApplication()->NewDocument("BinXCAF", aDoc);

  const int32_t aReadStatus = theRead(aDoc);
  if (aReadStatus != OIG_OK)
  {
    return aReadStatus;
  }
  return meshAndWrite(aDoc, theLinearDeflection, theAngularDeflection, theRelativeDeflection,
                      theOutGlb, theOutLen);
}

// Wraps a conversion body in the catch-everything boundary.
int32_t runGuarded(const std::function<int32_t()>& theBody)
{
  try
  {
    return theBody();
  }
  catch (const Standard_Failure& aFailure)
  {
    setError(std::string("OCCT exception: ")
             + (aFailure.GetMessageString() != NULL ? aFailure.GetMessageString() : "unknown"));
    return OIG_ERR_EXCEPTION;
  }
  catch (const std::exception& anException)
  {
    setError(std::string("C++ exception: ") + anException.what());
    return OIG_ERR_EXCEPTION;
  }
  catch (...)
  {
    setError("unknown C++ exception");
    return OIG_ERR_EXCEPTION;
  }
}

bool validParams(double theLinearDeflection, double theAngularDeflection, uint8_t** theOutGlb,
                 uint32_t* theOutLen)
{
  return theOutGlb != NULL && theOutLen != NULL && theLinearDeflection > 0.0
         && theAngularDeflection > 0.0;
}
}  // namespace

extern "C" {

const char* oig_version(void)
{
  return "occt-import-go (OCCT " OCC_VERSION_COMPLETE ")";
}

// fopen interposer. With static linking this definition shadows the libc
// one everywhere in the module. That is safe here: the standalone
// Emscripten libc cannot open real files anyway (its open() stub always
// fails — the wasm imports no path_open/openat), and the only fopen caller
// left in this build is OCCT's IGES reader (fonts, images, and signal
// handling are compiled out). The magic path serves the current in-memory
// input through fmemopen, which is pure userspace in musl.
FILE* fopen(const char* pathname, const char* mode)
{
  if (pathname != NULL && strcmp(pathname, THE_MEM_INPUT_PATH) == 0 && !g_fopen_input.empty())
  {
    return fmemopen((void*)g_fopen_input.data(), g_fopen_input.size(), mode);
  }
  errno = ENOENT;
  return NULL;
}

int32_t oig_to_glb(int32_t format, const uint8_t* in_data, uint32_t in_len,
                   double linear_deflection, double angular_deflection,
                   int32_t relative_deflection, uint8_t** out_glb, uint32_t* out_len)
{
  g_last_error.clear();
  if (in_data == NULL || in_len == 0
      || !validParams(linear_deflection, angular_deflection, out_glb, out_len))
  {
    setError("invalid arguments");
    return OIG_ERR_BADARG;
  }
  *out_glb = NULL;
  *out_len = 0;
  return runGuarded([&]() {
    std::function<int32_t(const Handle(TDocStd_Document)&)> aRead;
    switch (format)
    {
      case OIG_FORMAT_STEP:
        aRead = [&](const Handle(TDocStd_Document)& theDoc) {
          return readInto<STEPCAFControl_Reader>(
            "STEP",
            [&](STEPCAFControl_Reader& theReader) {
              // ReadStream keeps no reference to the stream after returning,
              // and the parser needs seek support, so feed it an
              // istringstream copy.
              std::istringstream aStream(
                std::string(reinterpret_cast<const char*>(in_data), in_len));
              return theReader.ReadStream("occt-import-go", aStream);
            },
            theDoc);
        };
        break;
      case OIG_FORMAT_IGES:
        aRead = [&](const Handle(TDocStd_Document)& theDoc) {
          // No stream API for IGES: route through the fopen interposer.
          g_fopen_input.assign(reinterpret_cast<const char*>(in_data), in_len);
          const int32_t aStatus = readInto<IGESCAFControl_Reader>(
            "IGES",
            [&](IGESCAFControl_Reader& theReader) {
              return theReader.ReadFile(THE_MEM_INPUT_PATH);
            },
            theDoc);
          g_fopen_input.clear();
          return aStatus;
        };
        break;
      default:
        setError("unknown input format");
        return (int32_t)OIG_ERR_BADARG;
    }
    return convert(aRead, linear_deflection, angular_deflection, relative_deflection != 0,
                   out_glb, out_len);
  });
}

const char* oig_last_error(void)
{
  return g_last_error.c_str();
}

void oig_free(void* p)
{
  std::free(p);
}

}  // extern "C"
