package lua51

import (
	"bytes"
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/content/lua51/decompiler"
	"github.com/yuin/gopher-lua/parse"
)

// Decompile recovers Lua 5.1 source from native bytecode without executing it.
// Unsupported control flow and invalid output are reported instead of returning
// disassembly or diagnostic pseudocode as editable source.
func Decompile(ctx context.Context, payload []byte) ([]byte, error) {
	source, err := decompiler.Decompile(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("sourceDecompile: %w", err)
	}
	statements, err := parse.Parse(bytes.NewReader(source), "@decompiled.lua")
	if err != nil {
		return nil, fmt.Errorf("sourceValidate: %w", err)
	}
	// Empty chunks are valid Lua 5.1 source too.
	if statements == nil {
		return source, nil
	}
	return source, nil
}
