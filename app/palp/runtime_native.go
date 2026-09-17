//go:build !js || !wasm

package main

import (
	"fmt"
	"os"
)

func run() {
	_, _ = fmt.Fprintln(os.Stderr, "palp is a WebAssembly tool; build with GOOS=js GOARCH=wasm")
}
