package occt

import (
	"errors"
	"fmt"

	"github.com/jerrylee1697/occt-import-go/internal/wasm"
)

// Sentinel errors, matchable with errors.Is. The returned error also carries
// the shim's detailed message.
var (
	// ErrInvalidInput reports empty or malformed arguments.
	ErrInvalidInput = errors.New("occt: invalid input")
	// ErrStepParse reports a STEP file that could not be parsed.
	ErrStepParse = errors.New("occt: STEP parse failed")
	// ErrEmptyModel reports a STEP file with no convertible shapes.
	ErrEmptyModel = errors.New("occt: no shapes in STEP file")
	// ErrConversion reports a failure while meshing or writing glTF.
	ErrConversion = errors.New("occt: conversion failed")
)

func mapError(err error) error {
	var shimErr *wasm.ShimError
	if !errors.As(err, &shimErr) {
		return err
	}
	var sentinel error
	switch shimErr.Code {
	case wasm.CodeBadArg:
		sentinel = ErrInvalidInput
	case wasm.CodeStepParse, wasm.CodeTransfer:
		sentinel = ErrStepParse
	case wasm.CodeEmptyDoc:
		sentinel = ErrEmptyModel
	default:
		sentinel = ErrConversion
	}
	return fmt.Errorf("%w: %s", sentinel, shimErr.Message)
}
