// Package compiler compiles Lua 5.1 source in an isolated, embedded WASI module.
package compiler

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

//go:embed compiler32.wasm
var module32 []byte

//go:embed compiler64.wasm
var module64 []byte

// Compiled code is shared across disposable guests for the process lifetime.
var compilationCache = wazero.NewCompilationCache()

// Attribution: root ATTRIBUTION.txt, section 1 (Lua compiler and WASI runtime).

// Compile parses and serializes source without executing its loaded function.
// numberSize selects IEEE single (4) or double (8) precision for parsing and
// constant folding, as well as the serialized number representation.
func Compile(ctx context.Context, source []byte, sourceName string, numberSize int) (payload []byte, resultErr error) {
	if len(source) > 16<<20 {
		return nil, errors.New("source exceeds 16 MiB")
	}
	if len(sourceName) > 4096 || strings.ContainsRune(sourceName, '\x00') {
		return nil, errors.New("invalid source name")
	}
	module := module32
	if numberSize == 8 {
		module = module64
	} else if numberSize != 4 {
		return nil, errors.New("unsupported Lua number size")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	config := wazero.NewRuntimeConfig().
		WithCompilationCache(compilationCache).
		WithMemoryLimitPages(4096).
		WithCloseOnContextDone(true)
	runtime := wazero.NewRuntimeWithConfig(ctx, config)
	defer func() {
		err := runtime.Close(ctx)
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("runtimeClose: %w", err))
			payload = nil
		}
	}()
	host, err := wasi_snapshot_preview1.Instantiate(ctx, runtime)
	if err != nil {
		return nil, fmt.Errorf("wasiCreate: %w", err)
	}
	if host == nil {
		return nil, errors.New("WASI host module unavailable")
	}
	stdout := boundedOutput{limit: 32 << 20}
	stderr := boundedOutput{limit: 64 << 10}
	moduleConfig := wazero.NewModuleConfig().
		WithArgs("lua-compile", sourceName).
		WithStdin(bytes.NewReader(source)).
		WithStdout(&stdout).
		WithStderr(&stderr)
	compiled, err := runtime.CompileModule(ctx, module)
	if err != nil {
		return nil, fmt.Errorf("compilerLoad: %w", err)
	}
	guest, err := runtime.InstantiateModule(ctx, compiled, moduleConfig)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("compileContext: %w", ctx.Err())
		}
		if stdout.isExceeded || stderr.isExceeded {
			return nil, fmt.Errorf("compileLimit: %w", err)
		}
		diagnostic := strings.TrimSpace(stderr.String())
		if diagnostic != "" {
			return nil, fmt.Errorf("compileRun: %w: %s", err, diagnostic)
		}
		return nil, fmt.Errorf("compileRun: %w", err)
	}
	if guest == nil {
		return nil, errors.New("compiler module unavailable")
	}
	if stdout.isExceeded || stderr.isExceeded {
		return nil, errors.New("compiler output limit exceeded")
	}
	if stdout.Len() < 12 {
		return nil, errors.New("compiler produced no bytecode")
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
		return 0, errors.New("compiler output limit exceeded")
	}
	n, err := e.Buffer.Write(payload)
	if err != nil {
		return n, fmt.Errorf("outputWrite: %w", err)
	}
	return n, nil
}
