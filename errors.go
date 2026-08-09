package occt

import (
	"errors"
	"fmt"

	"github.com/avgjerry/occt-import-go/internal/wasm"
)

// Sentinel errors, matchable with errors.Is. The returned error also carries
// the shim's detailed message.
var (
	// ErrInvalidInput reports empty or malformed arguments.
	ErrInvalidInput = errors.New("occt: invalid input")
	// ErrParse reports an input file that could not be parsed.
	ErrParse = errors.New("occt: file parse failed")
	// ErrEmptyModel reports an input file with no convertible shapes.
	ErrEmptyModel = errors.New("occt: no shapes in file")
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
	case wasm.CodeParse, wasm.CodeTransfer:
		sentinel = ErrParse
	case wasm.CodeEmptyDoc:
		sentinel = ErrEmptyModel
	default:
		sentinel = ErrConversion
	}
	return fmt.Errorf("%w: %s", sentinel, shimErr.Message)
}
