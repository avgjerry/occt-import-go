package wasm

import (
	"context"
	"fmt"
	"strings"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/emscripten"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Runtime holds a wazero runtime with the OCCT module compiled once.
// It is safe for concurrent use: each conversion instantiates its own
// module instance (see abi.go).
type Runtime struct {
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
}

// NewRuntime compiles the embedded OCCT module. interpreter selects wazero's
// interpreter engine instead of the compiler (mainly for tests; much slower).
func NewRuntime(ctx context.Context, interpreter bool) (*Runtime, error) {
	moduleBytes, err := ModuleBytes()
	if err != nil {
		return nil, err
	}

	var config wazero.RuntimeConfig
	if interpreter {
		config = wazero.NewRuntimeConfigInterpreter()
	} else {
		config = wazero.NewRuntimeConfigCompiler()
	}
	// OCCT throws C++ exceptions (compiled to the standardized wasm exnref
	// encoding), which wazero supports behind this experimental feature.
	config = config.
		WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling).
		WithCloseOnContextDone(true)

	runtime := wazero.NewRuntimeWithConfig(ctx, config)

	compiled, err := runtime.CompileModule(ctx, moduleBytes)
	if err != nil {
		_ = runtime.Close(ctx)
		return nil, fmt.Errorf("occt: compile wasm module: %w", err)
	}

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
		_ = runtime.Close(ctx)
		return nil, fmt.Errorf("occt: instantiate WASI: %w", err)
	}
	if err := instantiateEnv(ctx, runtime, compiled); err != nil {
		_ = runtime.Close(ctx)
		return nil, err
	}

	return &Runtime{runtime: runtime, compiled: compiled}, nil
}

// Close releases the runtime and all compiled code.
func (r *Runtime) Close(ctx context.Context) error {
	return r.runtime.Close(ctx)
}

// instantiateEnv builds the "env" host module: wazero's Emscripten helpers
// (emscripten_notify_memory_growth, invoke_* thunks) plus no-op stubs for the
// few raw syscalls Emscripten's standalone mode still imports (the shim's
// mem:// filesystem means none of them do real work — e.g. the glTF writer
// unlinking its mem:// temp file, where "success" without side effects is
// exactly what we want).
func instantiateEnv(ctx context.Context, runtime wazero.Runtime, compiled wazero.CompiledModule) error {
	builder := runtime.NewHostModuleBuilder("env")

	exporter, err := emscripten.NewFunctionExporterForModule(compiled)
	if err != nil {
		return fmt.Errorf("occt: emscripten exporter: %w", err)
	}
	exporter.ExportFunctions(builder)

	for _, def := range compiled.ImportedFunctions() {
		module, name, ok := def.Import()
		if !ok || module != "env" {
			continue
		}
		if name == "emscripten_notify_memory_growth" || strings.HasPrefix(name, "invoke_") {
			continue // provided by the emscripten exporter
		}
		params, results := def.ParamTypes(), def.ResultTypes()
		if strings.HasPrefix(name, "__syscall") {
			// Filesystem syscalls that standalone Emscripten leaves as raw
			// env imports. The mem:// pipeline never needs them to succeed
			// meaningfully (e.g. unlinking a mem:// temp file): report
			// success, do nothing.
			builder.NewFunctionBuilder().
				WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
					for i := range results {
						stack[i] = 0
					}
				}), params, results).
				Export(name)
			continue
		}
		// Unreferenced-at-runtime library stubs (e.g. browser image-decoding
		// helpers pulled in by Emscripten's GL/html5 libraries) that the -O0
		// link keeps alive. They must exist to instantiate the module, but a
		// real call means we mis-assessed — fail loudly rather than corrupt.
		stubName := name
		builder.NewFunctionBuilder().
			WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
				panic(fmt.Sprintf("occt: unexpected runtime call to stubbed env.%s", stubName))
			}), params, results).
			Export(name)
	}

	if _, err := builder.Instantiate(ctx); err != nil {
		return fmt.Errorf("occt: instantiate env module: %w", err)
	}
	return nil
}
