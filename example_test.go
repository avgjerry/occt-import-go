package occt_test

import (
	"context"
	"log"
	"os"

	occt "github.com/avgjerry/occt-import-go"
)

// Convert a STEP file to GLB with default mesh quality.
func Example() {
	step, err := os.ReadFile("model.step")
	if err != nil {
		log.Fatal(err)
	}

	glb, err := occt.StepToGLB(context.Background(), step,
		occt.WithLinearDeflection(0.001),
		occt.WithAngularDeflection(0.5))
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile("model.glb", glb, 0o644); err != nil {
		log.Fatal(err)
	}
}
