// Package wasm embeds the OCCT conversion module and hosts it under wazero.
package wasm

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// The OCCT + shim standalone wasm module, built reproducibly by
// wasm/build_occt.sh + wasm/build_shim.sh and compressed with zstd.
//
//go:embed occt.wasm.zst
var compressed []byte

var (
	decompressOnce sync.Once
	wasmBytes      []byte
	decompressErr  error
)

// ModuleBytes returns the decompressed wasm module, decoding it on first use.
func ModuleBytes() ([]byte, error) {
	decompressOnce.Do(func() {
		reader, err := zstd.NewReader(nil)
		if err != nil {
			decompressErr = fmt.Errorf("occt: init zstd: %w", err)
			return
		}
		defer reader.Close()
		wasmBytes, decompressErr = reader.DecodeAll(compressed, nil)
		if decompressErr != nil {
			decompressErr = fmt.Errorf("occt: decompress embedded wasm: %w", decompressErr)
		}
	})
	return wasmBytes, decompressErr
}
