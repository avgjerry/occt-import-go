// occt-import-go wasm shim: STEP bytes in -> GLB bytes out.
//
// Pipeline (mirrors what cascadio does for Python, written from scratch):
//   STEPCAFControl_Reader::ReadStream  -> XCAF TDocStd_Document
//   BRepMesh_IncrementalMesh           -> triangulation (single-threaded)
//   RWGltf_CafWriter                   -> binary glTF written to mem://
//
// C++ exceptions never escape the extern "C" boundary; see oig.h.

#include "memfs.h"
#include "oig.h"

#include <BRepMesh_IncrementalMesh.hxx>
#include <BRep_Tool.hxx>
#include <IFSelect_ReturnStatus.hxx>
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

#include <cstdlib>
#include <cstring>
#include <exception>
#include <sstream>
#include <string>

namespace
{
std::string g_last_error;

const char THE_GLB_URL[] = "mem://out.glb";

void setError(const std::string& theMessage)
{
  g_last_error = theMessage;
}

int32_t convert(const uint8_t* theStepData, uint32_t theStepLen, double theLinearDeflection,
                double theAngularDeflection, bool theRelativeDeflection, uint8_t** theOutGlb,
                uint32_t* theOutLen)
{
  OigMemFileSystem::RegisterOnce();
  OigMemFileSystem::Clear();

  // --- STEP -> XCAF document ---
  Handle(TDocStd_Document) aDoc;
  XCAFApp_Application::GetApplication()->NewDocument("BinXCAF", aDoc);

  STEPCAFControl_Reader aReader;
  aReader.SetColorMode(true);
  aReader.SetNameMode(true);
  aReader.SetLayerMode(true);

  {
    // ReadStream keeps no reference to the stream after returning, and the
    // STEP parser needs seek support, so feed it an istringstream copy.
    std::istringstream aStream(
      std::string(reinterpret_cast<const char*>(theStepData), theStepLen));
    if (aReader.ReadStream("occt-import-go.step", aStream) != IFSelect_RetDone)
    {
      setError("STEP file could not be parsed");
      return OIG_ERR_STEP_PARSE;
    }
  }

  if (!aReader.Transfer(aDoc))
  {
    setError("STEP to XCAF document transfer failed");
    return OIG_ERR_TRANSFER;
  }

  Handle(XCAFDoc_ShapeTool) aShapeTool = XCAFDoc_DocumentTool::ShapeTool(aDoc->Main());
  TDF_LabelSequence         aRootLabels;
  aShapeTool->GetFreeShapes(aRootLabels);
  if (aRootLabels.IsEmpty())
  {
    setError("document contains no shapes");
    return OIG_ERR_EMPTY_DOC;
  }

  // --- triangulate ---
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

  // --- XCAF document -> GLB (into the mem:// filesystem) ---
  {
    RWGltf_CafWriter aWriter(THE_GLB_URL, /*theIsBinary*/ true);
    aWriter.SetMergeFaces(true);
    TColStd_IndexedDataMapOfStringString aFileInfo;
    if (!aWriter.Perform(aDoc, aFileInfo, Message_ProgressRange()))
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
}  // namespace

extern "C" {

const char* oig_version(void)
{
  return "occt-import-go (OCCT " OCC_VERSION_COMPLETE ")";
}

int32_t oig_step_to_glb(const uint8_t* step_data, uint32_t step_len, double linear_deflection,
                        double angular_deflection, int32_t relative_deflection, uint8_t** out_glb,
                        uint32_t* out_len)
{
  g_last_error.clear();
  if (step_data == NULL || step_len == 0 || out_glb == NULL || out_len == NULL
      || !(linear_deflection > 0.0) || !(angular_deflection > 0.0))
  {
    setError("invalid arguments");
    return OIG_ERR_BADARG;
  }
  *out_glb = NULL;
  *out_len = 0;
  try
  {
    return convert(step_data, step_len, linear_deflection, angular_deflection,
                   relative_deflection != 0, out_glb, out_len);
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

const char* oig_last_error(void)
{
  return g_last_error.c_str();
}

void oig_free(void* p)
{
  std::free(p);
}

}  // extern "C"
