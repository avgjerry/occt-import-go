// Milestone A feasibility spike: verifies that Emscripten standalone wasm
// built with C++ exceptions (standardized exnref encoding) runs correctly
// under wazero's experimental exception-handling support, in both the
// compiler and interpreter engines.
//
// Build the wasm fixtures first: ./build.sh (requires emsdk).
package spike

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/emscripten"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func newRuntime(ctx context.Context, t *testing.T, interpreter bool) wazero.Runtime {
	t.Helper()
	var rc wazero.RuntimeConfig
	if interpreter {
		rc = wazero.NewRuntimeConfigInterpreter()
	} else {
		rc = wazero.NewRuntimeConfigCompiler()
	}
	rc = rc.WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling)
	r := wazero.NewRuntimeWithConfig(ctx, rc)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	return r
}

func instantiate(ctx context.Context, t *testing.T, r wazero.Runtime, wasmBytes []byte) api.Module {
	t.Helper()
	compiled, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("CompileModule: %v", err)
	}

	// Export any Emscripten-required env functions (e.g.
	// emscripten_notify_memory_growth) only if the module imports them.
	// A runtime allows only one module named "env", so skip if present.
	if r.Module("env") == nil {
		envBuilder := r.NewHostModuleBuilder("env")
		exporter, err := emscripten.NewFunctionExporterForModule(compiled)
		if err != nil {
			t.Fatalf("emscripten exporter: %v", err)
		}
		exporter.ExportFunctions(envBuilder)
		if _, err := envBuilder.Instantiate(ctx); err != nil {
			t.Fatalf("env instantiate: %v", err)
		}
	}

	var stderr bytes.Buffer
	mod, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().
		WithName("").
		WithStartFunctions("_initialize").
		WithStderr(&stderr))
	if err != nil {
		t.Fatalf("InstantiateModule: %v (stderr: %s)", err, stderr.String())
	}
	return mod
}

func readCString(t *testing.T, mod api.Module, ptr uint32) string {
	t.Helper()
	var out []byte
	for i := ptr; ; i++ {
		b, ok := mod.Memory().ReadByte(i)
		if !ok {
			t.Fatalf("out-of-range read at %d", i)
		}
		if b == 0 {
			break
		}
		out = append(out, b)
	}
	return string(out)
}

func loadWasm(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Skipf("wasm fixture %s not built (run ./build.sh): %v", name, err)
	}
	return b
}

func TestExceptionHandling(t *testing.T) {
	fixtures := []string{"eh_exnref.wasm", "eh_legacy_translated.wasm"}
	engines := []struct {
		name        string
		interpreter bool
	}{
		{"compiler", false},
		{"interpreter", true},
	}

	for _, fixture := range fixtures {
		wasmBytes := loadWasm(t, fixture)
		for _, engine := range engines {
			t.Run(fixture+"/"+engine.name, func(t *testing.T) {
				ctx := context.Background()
				r := newRuntime(ctx, t, engine.interpreter)
				defer r.Close(ctx)
				mod := instantiate(ctx, t, r, wasmBytes)

				// Throw + catch inside the guest.
				res, err := mod.ExportedFunction("run_caught").Call(ctx, 7)
				if err != nil {
					t.Fatalf("run_caught: %v", err)
				}
				if int32(res[0]) != 42 {
					t.Fatalf("run_caught = %d, want 42", int32(res[0]))
				}

				// The what() message must round-trip through guest memory.
				res, err = mod.ExportedFunction("last_error").Call(ctx)
				if err != nil {
					t.Fatalf("last_error: %v", err)
				}
				if got := readCString(t, mod, uint32(res[0])); got != "expected failure 7" {
					t.Fatalf("last_error = %q, want %q", got, "expected failure 7")
				}

				// Rethrow across nested frames.
				res, err = mod.ExportedFunction("run_nested").Call(ctx)
				if err != nil {
					t.Fatalf("run_nested: %v", err)
				}
				if int32(res[0]) != 3 {
					t.Fatalf("run_nested = %d, want 3", int32(res[0]))
				}

				// An exception escaping the guest must be a Go error, not a
				// panic or a wedged runtime.
				if _, err = mod.ExportedFunction("run_uncaught").Call(ctx); err == nil {
					t.Fatal("run_uncaught: expected error, got nil")
				} else {
					t.Logf("run_uncaught surfaced as: %v", err)
				}

				// A fresh instance must still work after the trap above.
				mod2 := instantiate(ctx, t, r, wasmBytes)
				res, err = mod2.ExportedFunction("run_caught").Call(ctx, 1)
				if err != nil || int32(res[0]) != 42 {
					t.Fatalf("post-trap instance: res=%v err=%v", res, err)
				}
			})
		}
	}
}
