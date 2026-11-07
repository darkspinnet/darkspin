//go:build mage

package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// BuildLuaCompiler rebuilds both embedded Lua 5.1 compiler modules.
// Supply extracted WASI SDK and original Lua 5.1.5 distribution directories.
func (Darkrun) BuildLuaCompiler(wasiSDK, luaSource string) error {
	sdkPath, err := filepath.Abs(wasiSDK)
	if err != nil {
		return fmt.Errorf("sdkPath: %w", err)
	}
	sourcePath, err := filepath.Abs(filepath.Join(luaSource, "src"))
	if err != nil {
		return fmt.Errorf("sourcePath: %w", err)
	}
	workPath, err := filepath.Abs(filepath.Join("bin", "cache", "lua-compiler"))
	if err != nil {
		return fmt.Errorf("workPath: %w", err)
	}
	temporaryPath := filepath.Join(workPath, "temporary")
	err = os.MkdirAll(temporaryPath, 0o755)
	if err != nil {
		return fmt.Errorf("temporaryMkdir: %w", err)
	}
	clangName := "clang"
	if runtime.GOOS == "windows" {
		clangName += ".exe"
	}
	clangPath := filepath.Join(sdkPath, "bin", clangName)
	for _, width := range []int{32, 64} {
		err = buildLuaCompilerWidth(clangPath, sdkPath, sourcePath, workPath, temporaryPath, width)
		if err != nil {
			return fmt.Errorf("compilerBuild[%d]: %w", width, err)
		}
	}
	return nil
}

var luaCompilerSourceNames = []string{
	"lapi.c", "lcode.c", "ldebug.c", "ldo.c", "ldump.c", "lfunc.c", "lgc.c",
	"llex.c", "lmem.c", "lobject.c", "lopcodes.c", "lparser.c", "lstate.c",
	"lstring.c", "ltable.c", "ltm.c", "lundump.c", "lvm.c", "lzio.c", "lauxlib.c",
}

func buildLuaCompilerWidth(clangPath, sdkPath, sourcePath, workPath, temporaryPath string, width int) error {
	buildPath := filepath.Join(workPath, fmt.Sprintf("float%d", width))
	if strings.EqualFold(filepath.Clean(sourcePath), filepath.Clean(buildPath)) {
		return errors.New("Lua source must be the original distribution, outside the build directory")
	}
	err := os.MkdirAll(buildPath, 0o755)
	if err != nil {
		return fmt.Errorf("buildMkdir: %w", err)
	}
	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return fmt.Errorf("sourceRead: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".c" && filepath.Ext(entry.Name()) != ".h" {
			continue
		}
		err = copyFile(filepath.Join(sourcePath, entry.Name()), filepath.Join(buildPath, entry.Name()))
		if err != nil {
			return fmt.Errorf("sourceCopy: %w", err)
		}
	}
	err = configureLuaCompiler(buildPath, width)
	if err != nil {
		return fmt.Errorf("sourceConfigure: %w", err)
	}
	compilerPath, err := filepath.Abs(filepath.Join("content", "lua51", "compiler"))
	if err != nil {
		return fmt.Errorf("compilerPath: %w", err)
	}
	outputName := fmt.Sprintf("compiler%d.wasm", width)
	outputPath := filepath.Join(workPath, outputName)
	args := []string{
		"--sysroot=" + filepath.Join(sdkPath, "share", "wasi-sysroot"),
		"--target=wasm32-wasi", "-Os", "-DLUA_ANSI", "-ffp-contract=off",
		"-Wl,--strip-all", "-Wl,--max-memory=268435456", "-Wl,-z,stack-size=1048576",
		"-I" + buildPath, filepath.Join(compilerPath, "wasm", "compile.c"),
	}
	for _, name := range luaCompilerSourceNames {
		args = append(args, filepath.Join(buildPath, name))
	}
	args = append(args, "-lm", "-o", outputPath)
	environment := map[string]string{"TMP": temporaryPath, "TEMP": temporaryPath, "TMPDIR": temporaryPath}
	err = runCommandWithEnvironment(workPath, environment, clangPath, args...)
	if err != nil {
		return fmt.Errorf("clangRun: %w", err)
	}
	payload, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("artifactRead: %w", err)
	}
	fmt.Printf("%s SHA256 %x\n", outputName, sha256.Sum256(payload))
	return nil
}

func configureLuaCompiler(buildPath string, width int) error {
	configurationPath := filepath.Join(buildPath, "luaconf.h")
	payload, err := os.ReadFile(configurationPath)
	if err != nil {
		return fmt.Errorf("configRead: %w", err)
	}
	configuration := string(payload)
	pattern := regexp.MustCompile(`(?s)#if defined\(__cplusplus\)\s*/\* C\+\+ exceptions \*/.*?#endif`)
	if !pattern.MatchString(configuration) {
		return errors.New("Lua 5.1 exception configuration was not found")
	}
	configuration = pattern.ReplaceAllString(configuration, `void darkspin_compile_error(void *state);
#define LUAI_THROW(L,c) darkspin_compile_error(L)
#define LUAI_TRY(L,c,a) do { a } while (0)
#define luai_jmpbuf int`)
	if width == 32 {
		configuration = strings.NewReplacer(
			"#define LUA_NUMBER_DOUBLE", "#define LUA_NUMBER_FLOAT",
			"#define LUA_NUMBER\tdouble", "#define LUA_NUMBER\tfloat",
			`"%lf"`, `"%f"`, `"%.14g"`, `"%.9g"`,
			"strtod((s), (p))", "strtof((s), (p))",
		).Replace(configuration)
	}
	err = os.WriteFile(configurationPath, []byte(configuration), 0o644)
	if err != nil {
		return fmt.Errorf("configWrite: %w", err)
	}
	errorPath := filepath.Join(buildPath, "ldo.c")
	payload, err = os.ReadFile(errorPath)
	if err != nil {
		return fmt.Errorf("handlerRead: %w", err)
	}
	handler := strings.ReplaceAll(string(payload), "#include <setjmp.h>", "/* Disposable compiler guest uses no longjmp. */")
	err = os.WriteFile(errorPath, []byte(handler), 0o644)
	if err != nil {
		return fmt.Errorf("handlerWrite: %w", err)
	}
	return nil
}
