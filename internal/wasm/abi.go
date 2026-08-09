package wasm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Error codes mirrored from wasm/oig.h.
const (
	codeOK        = 0
	CodeBadArg    = -1
	CodeParse     = -2
	CodeTransfer  = -3
	CodeEmptyDoc  = -4
	CodeMesh      = -5
	CodeGltfWrite = -6
	CodeException = -7
	CodeOOM       = -8
)

// Input formats mirrored from wasm/oig.h.
type Format int32

const (
	FormatSTEP Format = 0
	FormatIGES Format = 1
)

// ShimError is a failure reported by the wasm shim with its error code and
// the shim's human-readable message.
type ShimError struct {
	Code    int32
	Message string
}

func (e *ShimError) Error() string {
	return fmt.Sprintf("occt shim error %d: %s", e.Code, e.Message)
}

// Params are the raw conversion parameters passed to the shim.
type Params struct {
	Format             Format
	LinearDeflection   float64
	AngularDeflection  float64
	RelativeDeflection bool
}

// ToGLB runs one conversion in a fresh module instance.
//
// Instance-per-call keeps the API goroutine-safe and returns the (possibly
// multi-hundred-MB grown) wasm linear memory to the OS as soon as the call
// finishes — the property a long-running server cares about most.
func (r *Runtime) ToGLB(ctx context.Context, input []byte, params Params) ([]byte, error) {
	if len(input) == 0 {
		return nil, &ShimError{Code: CodeBadArg, Message: "empty input"}
	}
	if int64(len(input)) > math.MaxUint32 {
		return nil, &ShimError{Code: CodeBadArg, Message: "input exceeds 4GiB"}
	}

	var stderr bytes.Buffer
	mod, err := r.runtime.InstantiateModule(ctx, r.compiled, wazero.NewModuleConfig().
		WithName(""). // anonymous: allows concurrent instances
		WithStartFunctions("_initialize").
		WithStderr(&stderr).
		WithStdout(&stderr))
	if err != nil {
		return nil, fmt.Errorf("occt: instantiate module: %w", err)
	}
	defer mod.Close(ctx)

	glb, err := call(ctx, mod, input, params)
	if err != nil {
		if log := bytes.TrimSpace(stderr.Bytes()); len(log) > 0 {
			// Keep the module's own output attached to the shim error so it
			// survives the sentinel mapping in the public API.
			var shimErr *ShimError
			if errors.As(err, &shimErr) {
				shimErr.Message = fmt.Sprintf("%s (module log: %s)", shimErr.Message, log)
				return nil, shimErr
			}
			return nil, fmt.Errorf("%w (module log: %s)", err, log)
		}
		return nil, err
	}
	return glb, nil
}

func call(ctx context.Context, mod api.Module, input []byte, params Params) ([]byte, error) {
	malloc := mod.ExportedFunction("malloc")
	convertFn := mod.ExportedFunction("oig_to_glb")
	lastErrorFn := mod.ExportedFunction("oig_last_error")
	if malloc == nil || convertFn == nil || lastErrorFn == nil {
		return nil, fmt.Errorf("occt: wasm module is missing required exports")
	}

	// Guest allocations: input buffer + two u32 out-params. All guest memory
	// is reclaimed when the instance closes, so nothing is explicitly freed.
	inPtr, err := allocAndWrite(ctx, mod, malloc, input)
	if err != nil {
		return nil, err
	}
	outParamsPtr, err := alloc(ctx, mod, malloc, 8)
	if err != nil {
		return nil, err
	}
	outGlbPtr, outLenPtr := outParamsPtr, outParamsPtr+4

	relative := uint64(0)
	if params.RelativeDeflection {
		relative = 1
	}
	results, err := convertFn.Call(ctx,
		api.EncodeI32(int32(params.Format)),
		uint64(inPtr),
		uint64(uint32(len(input))),
		api.EncodeF64(params.LinearDeflection),
		api.EncodeF64(params.AngularDeflection),
		relative,
		uint64(outGlbPtr),
		uint64(outLenPtr),
	)
	if err != nil {
		// A trap (e.g. uncaught exception or stack exhaustion) inside OCCT.
		return nil, fmt.Errorf("occt: conversion trapped: %w", err)
	}

	if code := int32(uint32(results[0])); code != codeOK {
		return nil, &ShimError{Code: code, Message: readLastError(ctx, mod, lastErrorFn)}
	}

	glbPtr, ok := mod.Memory().ReadUint32Le(outGlbPtr)
	if !ok {
		return nil, fmt.Errorf("occt: out-param read out of range")
	}
	glbLen, ok := mod.Memory().ReadUint32Le(outLenPtr)
	if !ok {
		return nil, fmt.Errorf("occt: out-param read out of range")
	}
	view, ok := mod.Memory().Read(glbPtr, glbLen)
	if !ok {
		return nil, fmt.Errorf("occt: GLB buffer read out of range (ptr=%d len=%d)", glbPtr, glbLen)
	}
	// Copy out: the view aliases wasm linear memory, which dies with the instance.
	glb := make([]byte, glbLen)
	copy(glb, view)
	return glb, nil
}

func alloc(ctx context.Context, mod api.Module, malloc api.Function, size uint32) (uint32, error) {
	results, err := malloc.Call(ctx, uint64(size))
	if err != nil {
		return 0, fmt.Errorf("occt: guest malloc(%d): %w", size, err)
	}
	ptr := uint32(results[0])
	if ptr == 0 {
		return 0, fmt.Errorf("occt: guest malloc(%d) returned NULL", size)
	}
	return ptr, nil
}

func allocAndWrite(ctx context.Context, mod api.Module, malloc api.Function, data []byte) (uint32, error) {
	ptr, err := alloc(ctx, mod, malloc, uint32(len(data)))
	if err != nil {
		return 0, err
	}
	if !mod.Memory().Write(ptr, data) {
		return 0, fmt.Errorf("occt: writing %d bytes into guest memory failed", len(data))
	}
	return ptr, nil
}

func readLastError(ctx context.Context, mod api.Module, lastErrorFn api.Function) string {
	results, err := lastErrorFn.Call(ctx)
	if err != nil {
		return "(oig_last_error unavailable)"
	}
	ptr := uint32(results[0])
	var out []byte
	for i := ptr; ; i++ {
		b, ok := mod.Memory().ReadByte(i)
		if !ok || b == 0 {
			break
		}
		out = append(out, b)
		if len(out) > 4096 {
			break
		}
	}
	return string(out)
}
