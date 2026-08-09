// Package occt converts STEP CAD files to GLB (binary glTF) in pure Go.
//
// It embeds OpenCascade (OCCT) compiled to WebAssembly and runs it under
// wazero, so `go get` works on any platform Go supports — no cgo, no system
// dependencies. Names, colors, and assembly hierarchy from the STEP file are
// preserved in the produced GLB.
//
//	glb, err := occt.StepToGLB(ctx, stepBytes)
//
// For servers, create one Converter and reuse it: the wasm module is
// compiled once, and each conversion runs in its own short-lived instance
// (goroutine-safe, memory returned to the OS per call).
package occt

import (
	"context"
	"sync"
)

var (
	defaultOnce      sync.Once
	defaultConverter *Converter
	defaultErr       error
)

// StepToGLB converts a STEP file to GLB using a lazily-initialized
// package-level Converter (wasm compiled on first call).
func StepToGLB(ctx context.Context, step []byte, opts ...Option) ([]byte, error) {
	conv, err := sharedConverter(ctx)
	if err != nil {
		return nil, err
	}
	return conv.StepToGLB(ctx, step, opts...)
}

// IgesToGLB converts an IGES file to GLB using a lazily-initialized
// package-level Converter (wasm compiled on first call).
func IgesToGLB(ctx context.Context, iges []byte, opts ...Option) ([]byte, error) {
	conv, err := sharedConverter(ctx)
	if err != nil {
		return nil, err
	}
	return conv.IgesToGLB(ctx, iges, opts...)
}

func sharedConverter(ctx context.Context) (*Converter, error) {
	defaultOnce.Do(func() {
		defaultConverter, defaultErr = NewConverter(ctx)
	})
	return defaultConverter, defaultErr
}
