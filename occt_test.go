package occt_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/qmuntal/gltf"

	occt "github.com/avgjerry/occt-import-go"
)

var (
	converterOnce sync.Once
	converter     *occt.Converter
	converterErr  error
)

// sharedConverter compiles the wasm module once for the whole test binary.
func sharedConverter(t *testing.T) *occt.Converter {
	t.Helper()
	converterOnce.Do(func() {
		converter, converterErr = occt.NewConverter(context.Background())
	})
	if converterErr != nil {
		t.Fatalf("NewConverter: %v", converterErr)
	}
	return converter
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func decodeGLB(t *testing.T, glb []byte) *gltf.Document {
	t.Helper()
	if len(glb) < 12 || string(glb[0:4]) != "glTF" {
		t.Fatalf("output is not a GLB container (len=%d)", len(glb))
	}
	doc := new(gltf.Document)
	if err := gltf.NewDecoder(bytes.NewReader(glb)).Decode(doc); err != nil {
		t.Fatalf("decode GLB: %v", err)
	}
	return doc
}

func TestStepToGLBScrew(t *testing.T) {
	glb, err := sharedConverter(t).StepToGLB(context.Background(), fixture(t, "screw.step"))
	if err != nil {
		t.Fatalf("StepToGLB: %v", err)
	}
	doc := decodeGLB(t, glb)

	if len(doc.Meshes) == 0 {
		t.Fatal("GLB has no meshes")
	}
	var triangles int
	for _, mesh := range doc.Meshes {
		for _, prim := range mesh.Primitives {
			if prim.Indices != nil {
				triangles += int(doc.Accessors[*prim.Indices].Count) / 3
			}
		}
	}
	if triangles < 100 {
		t.Fatalf("suspiciously few triangles: %d", triangles)
	}
	t.Logf("screw.step -> %d bytes GLB, %d meshes, %d triangles",
		len(glb), len(doc.Meshes), triangles)
}

func TestStepToGLBAssemblyStructure(t *testing.T) {
	glb, err := sharedConverter(t).StepToGLB(context.Background(), fixture(t, "as1-oc-214.stp"))
	if err != nil {
		t.Fatalf("StepToGLB: %v", err)
	}
	doc := decodeGLB(t, glb)

	// AS1 is a multi-part assembly with colors: expect hierarchy, names and
	// materials to survive the conversion.
	if len(doc.Nodes) < 5 {
		t.Errorf("expected an assembly node hierarchy, got %d nodes", len(doc.Nodes))
	}
	var named int
	for _, node := range doc.Nodes {
		if node.Name != "" {
			named++
		}
	}
	if named == 0 {
		t.Error("no node names survived the conversion")
	}
	if len(doc.Materials) == 0 {
		t.Error("no materials (colors) survived the conversion")
	}
	t.Logf("as1 -> %d nodes (%d named), %d meshes, %d materials",
		len(doc.Nodes), named, len(doc.Meshes), len(doc.Materials))
}

func TestIgesToGLB(t *testing.T) {
	// Coarse deflection: bearing.iges is a heavy model and this test asserts
	// structure, not mesh quality; default quality takes minutes on slow CI.
	glb, err := sharedConverter(t).IgesToGLB(context.Background(), fixture(t, "bearing.iges"),
		occt.WithLinearDeflection(0.01))
	if err != nil {
		t.Fatalf("IgesToGLB: %v", err)
	}
	doc := decodeGLB(t, glb)
	if len(doc.Meshes) == 0 {
		t.Fatal("GLB has no meshes")
	}
	t.Logf("bearing.iges -> %d bytes GLB, %d meshes, %d nodes",
		len(glb), len(doc.Meshes), len(doc.Nodes))
}

func TestIgesRejectsStep(t *testing.T) {
	if _, err := sharedConverter(t).IgesToGLB(context.Background(), fixture(t, "screw.step")); !errors.Is(err, occt.ErrParse) {
		t.Errorf("STEP data through IGES reader: got %v, want ErrParse", err)
	}
}

func TestMeshQualityOptions(t *testing.T) {
	ctx := context.Background()
	step := fixture(t, "screw.step")
	conv := sharedConverter(t)

	coarse, err := conv.StepToGLB(ctx, step, occt.WithLinearDeflection(0.01))
	if err != nil {
		t.Fatalf("coarse: %v", err)
	}
	fine, err := conv.StepToGLB(ctx, step, occt.WithLinearDeflection(0.0001))
	if err != nil {
		t.Fatalf("fine: %v", err)
	}
	if len(fine) <= len(coarse) {
		t.Errorf("finer deflection should produce more geometry: fine=%dB coarse=%dB",
			len(fine), len(coarse))
	}
}

func TestErrorPaths(t *testing.T) {
	ctx := context.Background()
	conv := sharedConverter(t)

	if _, err := conv.StepToGLB(ctx, nil); !errors.Is(err, occt.ErrInvalidInput) {
		t.Errorf("nil input: got %v, want ErrInvalidInput", err)
	}
	if _, err := conv.StepToGLB(ctx, []byte("this is not a STEP file")); !errors.Is(err, occt.ErrParse) {
		t.Errorf("garbage input: got %v, want ErrParse", err)
	}
	truncated := fixture(t, "screw.step")[:2000]
	if _, err := conv.StepToGLB(ctx, truncated); err == nil {
		t.Error("truncated STEP: expected an error, got success")
	}
}

func TestConcurrentConversions(t *testing.T) {
	ctx := context.Background()
	conv := sharedConverter(t)
	step := fixture(t, "screw.step")

	const workers = 4
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = conv.StepToGLB(ctx, step)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d: %v", i, err)
		}
	}
}

func TestInterpreterEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("interpreter engine is slow; skipped with -short")
	}
	ctx := context.Background()
	conv, err := occt.NewConverter(ctx, occt.WithInterpreter())
	if err != nil {
		t.Fatalf("NewConverter(interpreter): %v", err)
	}
	defer conv.Close(ctx)
	glb, err := conv.StepToGLB(ctx, fixture(t, "screw.step"))
	if err != nil {
		t.Fatalf("StepToGLB: %v", err)
	}
	decodeGLB(t, glb)
}

func BenchmarkStepToGLBScrew(b *testing.B) {
	ctx := context.Background()
	conv, err := occt.NewConverter(ctx)
	if err != nil {
		b.Fatalf("NewConverter: %v", err)
	}
	defer conv.Close(ctx)
	step, err := os.ReadFile(filepath.Join("testdata", "screw.step"))
	if err != nil {
		b.Fatalf("read fixture: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := conv.StepToGLB(ctx, step); err != nil {
			b.Fatal(err)
		}
	}
}
