package occt

import (
	"context"

	"github.com/avgjerry/occt-import-go/internal/wasm"
)

// Converter holds the compiled OCCT wasm module. It is safe for concurrent
// use: every conversion runs in a fresh, short-lived module instance.
type Converter struct {
	runtime  *wasm.Runtime
	defaults options
}

// ConverterOption configures a Converter.
type ConverterOption func(*converterConfig)

type converterConfig struct {
	interpreter bool
	defaults    options
}

// WithInterpreter selects wazero's interpreter engine instead of its
// optimizing compiler. Much slower; intended for tests and debugging.
func WithInterpreter() ConverterOption {
	return func(c *converterConfig) { c.interpreter = true }
}

// WithDefaultOptions sets conversion options applied to every StepToGLB call
// on this Converter (still overridable per call).
func WithDefaultOptions(opts ...Option) ConverterOption {
	return func(c *converterConfig) {
		for _, opt := range opts {
			opt(&c.defaults)
		}
	}
}

// NewConverter compiles the embedded OCCT wasm module (the expensive step,
// done once) and returns a reusable, goroutine-safe Converter.
func NewConverter(ctx context.Context, opts ...ConverterOption) (*Converter, error) {
	config := converterConfig{defaults: defaultOptions()}
	for _, opt := range opts {
		opt(&config)
	}
	runtime, err := wasm.NewRuntime(ctx, config.interpreter)
	if err != nil {
		return nil, err
	}
	return &Converter{runtime: runtime, defaults: config.defaults}, nil
}

// StepToGLB converts one STEP file to GLB.
func (c *Converter) StepToGLB(ctx context.Context, step []byte, opts ...Option) ([]byte, error) {
	o := c.defaults
	for _, opt := range opts {
		opt(&o)
	}
	glb, err := c.runtime.StepToGLB(ctx, step, wasm.Params{
		LinearDeflection:   o.linearDeflection,
		AngularDeflection:  o.angularDeflection,
		RelativeDeflection: o.relativeDeflection,
	})
	if err != nil {
		return nil, mapError(err)
	}
	return glb, nil
}

// Close releases the compiled module and runtime.
func (c *Converter) Close(ctx context.Context) error {
	return c.runtime.Close(ctx)
}
