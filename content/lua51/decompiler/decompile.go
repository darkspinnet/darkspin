// Package decompiler runs the embedded Lua decompiler in an isolated WASI module.
package decompiler

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const (
	maximumInputSize      = 16 << 20
	maximumSourceSize     = 32 << 20
	maximumDiagnosticSize = 64 << 10
	maximumMemoryPages    = 4096 // 256 MiB; no host filesystem or process access.
)

//go:embed decompiler.wasm
var module []byte

// Compilation is shared across independent guest instances during package
// exports. The in-memory cache intentionally lives for the process lifetime.
var compilationCache = wazero.NewCompilationCache()

// Attribution contains the root attribution file for binary distributions.
// See its section 2 for unluac-rs and Rust/WASI dependencies. Mage maps this
// logical embed path to the single root ATTRIBUTION.txt through its overlay.
//
//go:embed ATTRIBUTION.txt
var Attribution string

// Decompile recovers Lua 5.1 source without executing the supplied chunk.
// Each invocation has independent memory and a thirty-second execution limit.
func Decompile(ctx context.Context, payload []byte) (source []byte, resultErr error) {
	if len(payload) > maximumInputSize {
		return nil, errors.New("bytecode exceeds 16 MiB")
	}
	if len(payload) < 12 || string(payload[:5]) != "\x1bLua\x51" {
		return nil, errors.New("expected native Lua 5.1 bytecode")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	config := wazero.NewRuntimeConfig().
		WithCompilationCache(compilationCache).
		WithMemoryLimitPages(maximumMemoryPages).
		WithCloseOnContextDone(true)
	runtime := wazero.NewRuntimeWithConfig(ctx, config)
	defer func() {
		err := runtime.Close(ctx)
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("runtimeClose: %w", err))
			source = nil
		}
	}()
	host, err := wasi_snapshot_preview1.Instantiate(ctx, runtime)
	if err != nil {
		return nil, fmt.Errorf("wasiCreate: %w", err)
	}
	// The runtime owns and closes the host module alongside the guest.
	if host == nil {
		return nil, errors.New("WASI host module unavailable")
	}
	stdout := boundedOutput{limit: maximumSourceSize}
	stderr := boundedOutput{limit: maximumDiagnosticSize}
	moduleConfig := wazero.NewModuleConfig().
		WithStdin(bytes.NewReader(payload)).
		WithStdout(&stdout).
		WithStderr(&stderr)
	// InstantiateWithConfig closes its compiled code when the WASI command exits,
	// evicting the shared cache. Compile explicitly so later chunks reuse it.
	compiled, err := runtime.CompileModule(ctx, module)
	if err != nil {
		return nil, fmt.Errorf("moduleCompile: %w", err)
	}
	guest, err := runtime.InstantiateModule(ctx, compiled, moduleConfig)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("decompileContext: %w", ctx.Err())
		}
		if stdout.isExceeded || stderr.isExceeded {
			return nil, fmt.Errorf("decompileLimit: %w", err)
		}
		diagnostic := strings.TrimSpace(stderr.String())
		if diagnostic != "" {
			return nil, fmt.Errorf("decompileRun: %w: %s", err, diagnostic)
		}
		return nil, fmt.Errorf("decompileRun: %w", err)
	}
	if guest == nil {
		return nil, errors.New("decompiler module unavailable")
	}
	if stdout.isExceeded || stderr.isExceeded {
		return nil, errors.New("decompiler output limit exceeded")
	}
	return stdout.Bytes(), nil
}

type boundedOutput struct {
	bytes.Buffer
	limit      int
	isExceeded bool
}

func (e *boundedOutput) Write(payload []byte) (int, error) {
	if len(payload) > e.limit-e.Len() {
		e.isExceeded = true
		return 0, errors.New("decompiler output limit exceeded")
	}
	n, err := e.Buffer.Write(payload)
	if err != nil {
		return n, fmt.Errorf("outputWrite: %w", err)
	}
	return n, nil
}
