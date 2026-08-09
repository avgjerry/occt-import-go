package occt_test

// Correctness-proving tests: these go beyond "output is structurally valid
// glTF" and pin down geometric properties of the conversion.
//
// Because the converter is a single-threaded wasm module (deterministic IEEE
// floating point, no host randomness), identical input + options produce
// byte-identical GLB on every platform. The golden values below were
// computed from the fixtures and hold across architectures; they are
// intentionally tolerance-banded so they survive artifact rebuilds with
// newer OCCT/Emscripten versions unless the geometry meaningfully changes —
// which is exactly the class of regression (e.g. OCCT meshing bugs) they
// are meant to catch.

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"

	occt "github.com/avgjerry/occt-import-go"
)

type geomStats struct {
	min, max  [3]float64
	area      float64
	triangles int
}

func measureGLB(t *testing.T, glb []byte) geomStats {
	t.Helper()
	doc := new(gltf.Document)
	if err := gltf.NewDecoder(bytes.NewReader(glb)).Decode(doc); err != nil {
		t.Fatalf("decode GLB: %v", err)
	}
	s := geomStats{
		min: [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)},
		max: [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)},
	}
	for _, mesh := range doc.Meshes {
		for _, prim := range mesh.Primitives {
			posIdx, ok := prim.Attributes[gltf.POSITION]
			if !ok {
				continue
			}
			pos, err := modeler.ReadPosition(doc, doc.Accessors[posIdx], nil)
			if err != nil {
				t.Fatalf("read positions: %v", err)
			}
			for _, p := range pos {
				for i := 0; i < 3; i++ {
					s.min[i] = math.Min(s.min[i], float64(p[i]))
					s.max[i] = math.Max(s.max[i], float64(p[i]))
				}
			}
			if prim.Indices == nil {
				continue
			}
			idx, err := modeler.ReadIndices(doc, doc.Accessors[*prim.Indices], nil)
			if err != nil {
				t.Fatalf("read indices: %v", err)
			}
			s.triangles += len(idx) / 3
			for i := 0; i+2 < len(idx); i += 3 {
				a, b, c := pos[idx[i]], pos[idx[i+1]], pos[idx[i+2]]
				ab := [3]float64{float64(b[0] - a[0]), float64(b[1] - a[1]), float64(b[2] - a[2])}
				ac := [3]float64{float64(c[0] - a[0]), float64(c[1] - a[1]), float64(c[2] - a[2])}
				cx := ab[1]*ac[2] - ab[2]*ac[1]
				cy := ab[2]*ac[0] - ab[0]*ac[2]
				cz := ab[0]*ac[1] - ab[1]*ac[0]
				s.area += 0.5 * math.Sqrt(cx*cx+cy*cy+cz*cz)
			}
		}
	}
	return s
}

func assertNear(t *testing.T, what string, got, want, relTol float64) {
	t.Helper()
	if math.Abs(got-want) > relTol*math.Abs(want) {
		t.Errorf("%s = %v, want %v (±%v%%)", what, got, want, relTol*100)
	}
}

// Golden geometric properties of the fixtures at default options. glTF
// output is in meters (OCCT converts from millimeter model units), so the
// screw is a ~20x20x42mm part and AS1 a ~180x150x200mm assembly.
func TestGeometryGoldens(t *testing.T) {
	cases := []struct {
		fixture   string
		min, max  [3]float64
		area      float64
		triangles int
	}{
		{
			fixture: "screw.step",
			min:     [3]float64{-0.0278, -0.0108, -0.0346},
			max:     [3]float64{-0.0080, 0.0092, 0.0077},
			area:    0.001928, triangles: 2938,
		},
		{
			fixture: "as1-oc-214.stp",
			min:     [3]float64{-0.0075, -0.0075, 0.0},
			max:     [3]float64{0.1800, 0.1500, 0.2000},
			area:    0.141070, triangles: 7768,
		},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			s := measureGLB(t, convertDefault(t, tc.fixture))

			// Bounding box: 1mm absolute tolerance — model scale and
			// placement must be exactly right, whatever the mesh density.
			const bboxTol = 0.001
			for i := 0; i < 3; i++ {
				if math.Abs(s.min[i]-tc.min[i]) > bboxTol || math.Abs(s.max[i]-tc.max[i]) > bboxTol {
					t.Errorf("bbox axis %d = [%v, %v], want [%v, %v] (±%vm)",
						i, s.min[i], s.max[i], tc.min[i], tc.max[i], bboxTol)
				}
			}
			// Total surface area: 2% — catches gross tessellation errors
			// (missing faces, degenerate triangles, wrong deflection).
			assertNear(t, "surface area", s.area, tc.area, 0.02)
			// Triangle count: 25% band — sensitive to mesher version, but a
			// collapse or explosion signals a real regression (e.g. the
			// conical-surface meshing bug that hit older OCCT releases).
			assertNear(t, "triangle count", float64(s.triangles), float64(tc.triangles), 0.25)
		})
	}
}

// Same input + options must produce byte-identical output: the wasm module
// is single-threaded and deterministic, and conversions run in fresh
// instances, so any divergence means hidden state is leaking between calls.
func TestDeterministicOutput(t *testing.T) {
	first := convertDefault(t, "screw.step")
	second, err := sharedConverter(t).StepToGLB(context.Background(), fixture(t, "screw.step"))
	if err != nil {
		t.Fatalf("second conversion: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("conversions differ: %d vs %d bytes", len(first), len(second))
	}
}

// A cancelled context must abort a running conversion promptly and must not
// wedge the converter for later calls.
func TestContextCancellation(t *testing.T) {
	conv := sharedConverter(t)
	// linkrods.step is heavy enough that it cannot finish in 300ms.
	step := fixture(t, "linkrods.step")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := conv.StepToGLB(ctx, step)
	if err == nil {
		t.Fatal("expected cancellation error, got success")
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("cancellation took %v, expected prompt abort", elapsed)
	}

	// Converter must still work afterwards.
	glb, err := conv.StepToGLB(context.Background(), fixture(t, "screw.step"))
	if err != nil {
		t.Fatalf("conversion after cancellation: %v", err)
	}
	decodeGLB(t, glb)
}

// Repeated conversions on one Converter must keep working (each call's wasm
// instance is torn down; nothing may accumulate or wedge).
func TestSequentialConversions(t *testing.T) {
	ctx := context.Background()
	conv := sharedConverter(t)
	step := fixture(t, "screw.step")
	for i := 0; i < 8; i++ {
		if _, err := conv.StepToGLB(ctx, step, occt.WithLinearDeflection(0.01)); err != nil {
			t.Fatalf("conversion %d: %v", i, err)
		}
	}
}

func TestOptionValidation(t *testing.T) {
	ctx := context.Background()
	conv := sharedConverter(t)
	step := fixture(t, "screw.step")

	for name, opt := range map[string]occt.Option{
		"zero linear deflection":      occt.WithLinearDeflection(0),
		"negative linear deflection":  occt.WithLinearDeflection(-1),
		"zero angular deflection":     occt.WithAngularDeflection(0),
		"negative angular deflection": occt.WithAngularDeflection(-0.5),
	} {
		if _, err := conv.StepToGLB(ctx, step, opt); !errors.Is(err, occt.ErrInvalidInput) {
			t.Errorf("%s: got %v, want ErrInvalidInput", name, err)
		}
	}
}
