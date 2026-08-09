package occt_test

// Host-robustness fuzzing: whatever bytes come in, the library must return
// (GLB or error) without panicking, crashing the wasm runtime, or wedging
// the Converter. The wasm sandbox makes memory-safety bugs in OCCT
// non-exploitable on the host, but traps must still surface as errors.
//
// In normal `go test` runs only the seed corpus executes; use
// `go test -fuzz FuzzStepToGLB` for continuous fuzzing.

import (
	"context"
	"strings"
	"testing"
)

func FuzzStepToGLB(f *testing.F) {
	f.Add([]byte("not a step file at all"))
	f.Add([]byte(""))
	f.Add([]byte("ISO-10303-21;\nHEADER;\nENDSEC;\nEND-ISO-10303-21;"))
	f.Add([]byte("ISO-10303-21;\nHEADER;\n" + strings.Repeat("#1=CARTESIAN_POINT('',(0.,0.,0.));\n", 50)))
	// A truncated real file: valid prefix, missing DATA section end.
	full, err := fixtureBytes("screw.step")
	if err == nil && len(full) > 4000 {
		f.Add(full[:4000])
	}

	conv, err := newFuzzConverter()
	if err != nil {
		f.Fatalf("NewConverter: %v", err)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		glb, err := conv.StepToGLB(context.Background(), data)
		if err == nil {
			if len(glb) < 12 || string(glb[0:4]) != "glTF" {
				t.Fatalf("success without valid GLB output (len=%d)", len(glb))
			}
		}
	})
}
