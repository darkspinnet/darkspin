package lua51

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/content/lua51/compiler"
)

// Compile produces a native Lua 5.1 chunk using a fixed 32-bit, little-endian
// ABI with single-precision numbers, matching the build-103 game client.
// It never executes the supplied source.
func Compile(source []byte, sourceName string) ([]byte, error) {
	payload, err := CompileContext(context.Background(), source, sourceName)
	if err != nil {
		return nil, fmt.Errorf("compileSource: %w", err)
	}
	return payload, nil
}

// CompileContext compiles source with cancellation using the build-103 ABI.
func CompileContext(ctx context.Context, source []byte, sourceName string) ([]byte, error) {
	header := []byte{0x1b, 'L', 'u', 'a', 0x51, 0, 1, 4, 4, 4, 4, 0}
	payload, err := CompileWithHeaderContext(ctx, source, sourceName, header)
	if err != nil {
		return nil, fmt.Errorf("compileDefault: %w", err)
	}
	return payload, nil
}

// CompileWithHeader compiles Lua source using the ABI described by a native
// Lua 5.1 chunk header. The original chunk's first twelve bytes may be passed
// as header. Debug names, line information, and local scopes are retained.
// Parsing and constant folding use the target's floating-point precision.
func CompileWithHeader(source []byte, sourceName string, header []byte) ([]byte, error) {
	payload, err := CompileWithHeaderContext(context.Background(), source, sourceName, header)
	if err != nil {
		return nil, fmt.Errorf("compileHeader: %w", err)
	}
	return payload, nil
}

// CompileWithHeaderContext compiles source with cancellation and a selected ABI.
func CompileWithHeaderContext(ctx context.Context, source []byte, sourceName string, header []byte) ([]byte, error) {
	target, err := readChunkABI(header)
	if err != nil {
		return nil, fmt.Errorf("targetHeader: %w", err)
	}
	bytecode, err := compiler.Compile(ctx, source, sourceName, target.numberSize)
	if err != nil {
		return nil, fmt.Errorf("sourceCompile: %w", err)
	}
	payload, err := transcodeChunk(bytecode, target)
	if err != nil {
		return nil, fmt.Errorf("chunkEncode: %w", err)
	}
	return payload, nil
}
