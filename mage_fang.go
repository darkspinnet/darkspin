//go:build mage

package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/magefile/mage/sh"
)

// Fang is a native library. Go tooling only generates its embedded catalogs;
// no Go runtime or cgo archive is linked into the game process.
func buildNativeFang(isDiagnostics, isScenario bool, linkerFlags string) error {
	directory := filepath.Join("bin", "cache", "fang-native")
	err := os.MkdirAll(directory, 0755)
	if err != nil {
		return fmt.Errorf("nativeMkdir: %w", err)
	}
	var generated strings.Builder
	generated.WriteString("/* Generated from Dark Spin translation sources. */\n")
	generated.WriteString("#ifdef FANG_TRANSLATION\nstatic void fang_initialize_translation(void) {\n int count, index; wchar_t** args = CommandLineToArgvW(GetCommandLineW(), &count); const wchar_t* code = L\"en\";\n if (!args) return;\n for (index=1; index<count; index++) { if (!_wcsnicmp(args[index], L\"-locale:\",8)) code=args[index]+8; else if (!_wcsnicmp(args[index],L\"--locale:\",9)) code=args[index]+9; }\n")
	for _, code := range []string{"de-de", "fr-fr", "pl-pl", "ru-ru"} {
		payload, readErr := os.ReadFile(filepath.Join("app", "locale", code+".json"))
		if readErr != nil {
			return fmt.Errorf("catalogRead: %w", readErr)
		}
		var catalog struct {
			Messages map[string]string `json:"messages"`
		}
		err = json.Unmarshal(payload, &catalog)
		if err != nil {
			return fmt.Errorf("catalogDecode: %w", err)
		}
		fmt.Fprintf(&generated, "if (!_wcsnicmp(code,L%q,2) && (code[2]==0 || code[2]=='-' || code[2]=='_')) {\n", code[:2])
		sources := make([]string, 0, len(catalog.Messages))
		for source := range catalog.Messages {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		for _, source := range sources {
			translation := catalog.Messages[source]
			if translation == "" || translation == source {
				continue
			}
			fmt.Fprintf(&generated, "if (!fang_set_translation(%s,%s)) OutputDebugStringA(\"Fang: translation allocation failed\\n\");\n", strconv.QuoteToASCII(source), strconv.QuoteToASCII(translation))
		}
		generated.WriteString("}\n")
	}
	generated.WriteString("LocalFree(args);\n}\n#endif\n")
	buildID := ""
	for _, argument := range strings.Fields(linkerFlags) {
		if strings.HasPrefix(argument, "github.com/darkspinnet/darkspin/server/buildinfo.ID=") {
			buildID = strings.SplitN(argument, "=", 2)[1]
		}
	}
	capability, err := json.Marshal(map[string]any{"protocol_version": 1, "component": "fang", "build_id": buildID, "features": []string{"scenario-v1"}})
	if err != nil {
		return fmt.Errorf("capabilityMarshal: %w", err)
	}
	fmt.Fprintf(&generated, "\n#define FANG_CAPABILITY_JSON %s\n", strconv.Quote(string(capability)))
	err = os.WriteFile(filepath.Join(directory, "fang_generated.h"), []byte(generated.String()), 0600)
	if err != nil {
		return fmt.Errorf("nativeGenerate: %w", err)
	}
	compiler := "gcc"
	if runtime.GOOS != "windows" {
		compiler = "i686-w64-mingw32-gcc"
	}
	arguments := []string{"-shared", "-m32", "-O2", "-g0", "-std=gnu11", "-static-libgcc", "-Iapp/fang", "-I" + directory, "-o", filepath.Join("bin", "game", "fang.dll")}
	arguments = append(arguments, fmt.Sprintf("-DFANG_DIAGNOSTICS=%d", nativeFlag(isDiagnostics)), fmt.Sprintf("-DFANG_SCENARIO=%d", nativeFlag(isScenario)))
	err = filepath.WalkDir(filepath.Join("app", "fang"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("nativeWalk: %w", walkErr)
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".c") {
			arguments = append(arguments, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("nativeSources: %w", err)
	}
	arguments = append(arguments, "-lws2_32", "-lole32", "-luuid", "-lversion", "-lshell32", "-luser32", "-lgdi32", "-ladvapi32", "-Wl,--kill-at")
	err = sh.RunV(compiler, arguments...)
	if err != nil {
		return fmt.Errorf("nativeCompile: %w", err)
	}
	return nil
}

func nativeFlag(isEnabled bool) int {
	if isEnabled {
		return 1
	}
	return 0
}
