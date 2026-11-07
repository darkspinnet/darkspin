//go:build mage

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const luaRustVersion = "1.99.1"
const luaSourceSHA256 = "2640fc56a795f29d28ef15e13c34a47e223960b0240e8cb0a82d9b0738695333"
const luaWindowsSDKSHA256 = "ebd4868f95a6e764bf29283e3bdf54df0df3902a29737176da9c17da218b939b"

var luaArtifactNames = []string{"compiler32.wasm", "compiler64.wasm", "decompiler.wasm"}

type luaArtifactManifest struct {
	Fingerprint string            `json:"fingerprint"`
	Artifacts   map[string]string `json:"artifacts"`
}

// PrepareLua installs fingerprinted, verified Lua WASM build inputs. A cache
// miss builds from pinned sources; a CI artifact cache hit needs only Go/Mage.
func (Darkrun) PrepareLua() error {
	fingerprint, err := luaArtifactFingerprint()
	if err != nil {
		return fmt.Errorf("inputFingerprint: %w", err)
	}
	bundlePath := filepath.Join("bin", "cache", "lua-wasm")
	manifestPath := filepath.Join(bundlePath, "manifest.json")
	payload, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		err = rebuildLuaArtifacts(fingerprint)
		if err != nil {
			return fmt.Errorf("cacheBuild: %w", err)
		}
		err = activateLuaOverlay()
		if err != nil {
			return fmt.Errorf("cacheOverlay: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("manifestRead: %w", err)
	}
	manifest := luaArtifactManifest{}
	err = json.Unmarshal(payload, &manifest)
	if err != nil {
		return fmt.Errorf("manifestDecode: %w", err)
	}
	if manifest.Fingerprint != fingerprint {
		err = rebuildLuaArtifacts(fingerprint)
		if err != nil {
			return fmt.Errorf("cacheRebuild: %w", err)
		}
		err = activateLuaOverlay()
		if err != nil {
			return fmt.Errorf("rebuildOverlay: %w", err)
		}
		return nil
	}
	if len(manifest.Artifacts) != len(luaArtifactNames) {
		return errors.New("Lua WASM artifact manifest has an invalid artifact count; run mage darkrun:buildluawasm")
	}
	for _, name := range luaArtifactNames {
		payload, readErr := os.ReadFile(filepath.Join(bundlePath, name))
		if readErr != nil {
			return fmt.Errorf("artifactRead[%s]: %w", name, readErr)
		}
		if luaArtifactDigest(payload) != manifest.Artifacts[name] {
			return fmt.Errorf("artifactHash[%s]: mismatch; run mage darkrun:buildluawasm", name)
		}
	}
	err = activateLuaOverlay()
	if err != nil {
		return fmt.Errorf("overlayActivate: %w", err)
	}
	return nil
}

// BuildLuaWasm rebuilds all Lua WASM artifacts, bypassing the generated cache.
func (Darkrun) BuildLuaWasm() error {
	fingerprint, err := luaArtifactFingerprint()
	if err != nil {
		return fmt.Errorf("inputFingerprint: %w", err)
	}
	err = rebuildLuaArtifacts(fingerprint)
	if err != nil {
		return fmt.Errorf("artifactRebuild: %w", err)
	}
	err = activateLuaOverlay()
	if err != nil {
		return fmt.Errorf("overlayActivate: %w", err)
	}
	return nil
}

func luaArtifactFingerprint() (string, error) {
	paths, err := filepath.Glob("mage_lua*.go")
	if err != nil {
		return "", fmt.Errorf("mageGlob: %w", err)
	}
	paths = append(paths,
		"content/lua51/compiler/wasm/compile.c",
		"content/lua51/decompiler/wasm/Cargo.toml",
		"content/lua51/decompiler/wasm/Cargo.lock",
	)
	sourcePaths, err := filepath.Glob("content/lua51/decompiler/wasm/src/*.rs")
	if err != nil {
		return "", fmt.Errorf("rustGlob: %w", err)
	}
	paths = append(paths, sourcePaths...)
	sort.Strings(paths)
	inputs := make([]byte, 0)
	for _, path := range paths {
		payload, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", fmt.Errorf("inputRead[%s]: %w", path, readErr)
		}
		inputs = append(inputs, filepath.ToSlash(path)...)
		inputs = append(inputs, 0)
		inputs = append(inputs, bytes.ReplaceAll(payload, []byte("\r\n"), []byte("\n"))...)
		inputs = append(inputs, 0)
	}
	return luaArtifactDigest(inputs), nil
}

func luaArtifactDigest(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func activateLuaOverlay() error {
	rootPath, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("rootPath: %w", err)
	}
	bundlePath := filepath.Join(rootPath, "bin", "cache", "lua-wasm")
	replacements := make(map[string]string, len(luaArtifactNames))
	for _, name := range luaArtifactNames {
		role := "compiler"
		if name == "decompiler.wasm" {
			role = "decompiler"
		}
		replacements[filepath.Join(rootPath, "content", "lua51", role, name)] = filepath.Join(bundlePath, name)
	}
	replacements[filepath.Join(rootPath, "content", "lua51", "decompiler", "ATTRIBUTION.txt")] = filepath.Join(rootPath, "ATTRIBUTION.txt")
	overlay := struct {
		Replacements map[string]string `json:"Replace"`
	}{Replacements: replacements}
	payload, err := json.MarshalIndent(overlay, "", "  ")
	if err != nil {
		return fmt.Errorf("overlayEncode: %w", err)
	}
	overlayPath := filepath.Join(bundlePath, "overlay.json")
	err = os.WriteFile(overlayPath, append(payload, '\n'), 0o644)
	if err != nil {
		return fmt.Errorf("overlayWrite: %w", err)
	}
	flag := strconv.Quote("-overlay=" + overlayPath)
	flags := os.Getenv("GOFLAGS")
	if strings.Contains(flags, flag) {
		return nil
	}
	if strings.Contains(flags, "-overlay") {
		return errors.New("GOFLAGS already specifies a different Go overlay; merge its Replace entries with bin/cache/lua-wasm/overlay.json")
	}
	err = os.Setenv("GOFLAGS", strings.TrimSpace(flags+" "+flag))
	if err != nil {
		return fmt.Errorf("goFlagsSet: %w", err)
	}
	return nil
}

func rebuildLuaArtifacts(fingerprint string) error {
	sdkPath, sourcePath, err := luaCompilerPrerequisites()
	if err != nil {
		return fmt.Errorf("compilerPrerequisite: %w", err)
	}
	err = (Darkrun{}).BuildLuaCompiler(sdkPath, sourcePath)
	if err != nil {
		return fmt.Errorf("compilerBuild: %w", err)
	}
	err = buildLuaDecompiler()
	if err != nil {
		return fmt.Errorf("decompilerBuild: %w", err)
	}
	bundlePath := filepath.Join("bin", "cache", "lua-wasm")
	err = os.MkdirAll(bundlePath, 0o755)
	if err != nil {
		return fmt.Errorf("bundleMkdir: %w", err)
	}
	manifest := luaArtifactManifest{Fingerprint: fingerprint, Artifacts: make(map[string]string)}
	for _, name := range luaArtifactNames {
		artifactPath := filepath.Join("bin", "cache", "lua-compiler", name)
		if name == "decompiler.wasm" {
			artifactPath = filepath.Join("bin", "cache", "lua-decompiler", "target", "wasm32-wasip1", "release", "darkspin-lua-decompiler.wasm")
		}
		payload, readErr := os.ReadFile(artifactPath)
		if readErr != nil {
			return fmt.Errorf("generatedRead[%s]: %w", name, readErr)
		}
		manifest.Artifacts[name] = luaArtifactDigest(payload)
		err = os.WriteFile(filepath.Join(bundlePath, name), payload, 0o644)
		if err != nil {
			return fmt.Errorf("bundleWrite[%s]: %w", name, err)
		}
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("manifestEncode: %w", err)
	}
	err = os.WriteFile(filepath.Join(bundlePath, "manifest.json"), append(payload, '\n'), 0o644)
	if err != nil {
		return fmt.Errorf("manifestWrite: %w", err)
	}
	return nil
}

func buildLuaDecompiler() error {
	rootPath, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("rootPath: %w", err)
	}
	cachePath := filepath.Join(rootPath, "bin", "cache", "lua-decompiler")
	environmentVariables := map[string]string{"CARGO_TARGET_DIR": filepath.Join(cachePath, "target")}
	temporaryPath := filepath.Join(cachePath, "temporary")
	err = os.MkdirAll(temporaryPath, 0o755)
	if err != nil {
		return fmt.Errorf("temporaryMkdir: %w", err)
	}
	environmentVariables["TMP"] = temporaryPath
	environmentVariables["TEMP"] = temporaryPath
	environmentVariables["TMPDIR"] = temporaryPath
	rustupPath, lookupErr := exec.LookPath("rustup")
	localRustup := filepath.Join(cachePath, "cargo", "bin", "rustup")
	if runtime.GOOS == "windows" {
		localRustup += ".exe"
	}
	if lookupErr != nil {
		_, statErr := os.Stat(localRustup)
		if statErr != nil {
			return fmt.Errorf("rustupLookup: install Rust %s and target wasm32-wasip1: %w", luaRustVersion, lookupErr)
		}
		rustupPath = localRustup
		environmentVariables["RUSTUP_HOME"] = filepath.Join(cachePath, "rustup")
		environmentVariables["CARGO_HOME"] = filepath.Join(cachePath, "cargo")
	}
	toolchain := luaRustVersion
	if runtime.GOOS == "windows" {
		toolchain += "-x86_64-pc-windows-gnu"
	}
	args := []string{"run", toolchain, "cargo", "build", "--locked", "--release", "--target", "wasm32-wasip1", "--manifest-path", filepath.Join(rootPath, "content", "lua51", "decompiler", "wasm", "Cargo.toml")}
	err = runCommandWithEnvironment(rootPath, environmentVariables, rustupPath, args...)
	if err != nil {
		return fmt.Errorf("cargoBuild: %w", err)
	}
	return nil
}

func luaCompilerPrerequisites() (string, string, error) {
	cachePath, err := filepath.Abs(filepath.Join("bin", "cache", "lua-decompiler"))
	if err != nil {
		return "", "", fmt.Errorf("cachePath: %w", err)
	}
	sourcePath := os.Getenv("LUA_SOURCE")
	if sourcePath == "" {
		sourcePath = filepath.Join(cachePath, "official-lua", "lua-5.1.5")
		_, statErr := os.Stat(filepath.Join(sourcePath, "src", "lua.h"))
		if errors.Is(statErr, os.ErrNotExist) {
			err = fetchLuaArchive("https://distfiles.macports.org/lua/lua-5.1.5.tar.gz", filepath.Join(cachePath, "lua-5.1.5.tar.gz"), luaSourceSHA256, filepath.Dir(sourcePath))
			if err != nil {
				return "", "", fmt.Errorf("luaFetch: %w", err)
			}
		} else if statErr != nil {
			return "", "", fmt.Errorf("luaStat: %w", statErr)
		}
	}
	sdkPath := os.Getenv("WASI_SDK")
	if sdkPath == "" {
		if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
			return "", "", errors.New("set WASI_SDK to an extracted WASI SDK 25.0 for this host; install Rust 1.99.1 with target wasm32-wasip1")
		}
		sdkName := "wasi-sdk-25.0-x86_64-windows"
		sdkPath = filepath.Join(cachePath, "sdk-extract", sdkName)
		_, statErr := os.Stat(filepath.Join(sdkPath, "bin", "clang.exe"))
		if errors.Is(statErr, os.ErrNotExist) {
			err = fetchLuaArchive("https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-25/"+sdkName+".tar.gz", filepath.Join(cachePath, sdkName+".tar.gz"), luaWindowsSDKSHA256, filepath.Dir(sdkPath))
			if err != nil {
				return "", "", fmt.Errorf("sdkFetch: %w", err)
			}
		} else if statErr != nil {
			return "", "", fmt.Errorf("sdkStat: %w", statErr)
		}
	}
	return sdkPath, sourcePath, nil
}

func fetchLuaArchive(url, archivePath, expectedHash, extractionPath string) error {
	err := os.MkdirAll(filepath.Dir(archivePath), 0o755)
	if err != nil {
		return fmt.Errorf("archiveMkdir: %w", err)
	}
	payload, err := os.ReadFile(archivePath)
	if errors.Is(err, os.ErrNotExist) {
		client := http.Client{Timeout: 15 * time.Minute}
		response, requestErr := client.Get(url)
		if requestErr != nil {
			return fmt.Errorf("archiveRequest: %w", requestErr)
		}
		payload, err = io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil {
			return fmt.Errorf("archiveRead: %w", errors.Join(err, closeErr))
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("archiveStatus: HTTP %d", response.StatusCode)
		}
	} else if err != nil {
		return fmt.Errorf("archiveOpen: %w", err)
	}
	if luaArtifactDigest(payload) != expectedHash {
		return fmt.Errorf("archiveHash: mismatch for %s", filepath.Base(archivePath))
	}
	err = os.WriteFile(archivePath, payload, 0o644)
	if err != nil {
		return fmt.Errorf("archiveWrite: %w", err)
	}
	sevenZipPath, err := exec.LookPath("7z")
	if err != nil {
		sevenZipPath, err = exec.LookPath("7zz")
	}
	if err != nil && runtime.GOOS == "windows" {
		sevenZipPath = filepath.Join(os.Getenv("ProgramFiles"), "7-Zip", "7z.exe")
		_, err = os.Stat(sevenZipPath)
	}
	if err != nil {
		return fmt.Errorf("sevenZipLookup: install native 7-Zip: %w", err)
	}
	err = os.MkdirAll(extractionPath, 0o755)
	if err != nil {
		return fmt.Errorf("extractMkdir: %w", err)
	}
	err = runCommand(extractionPath, sevenZipPath, "x", archivePath, "-o"+extractionPath, "-y")
	if err != nil {
		return fmt.Errorf("gzipExtract: %w", err)
	}
	tarPath := filepath.Join(extractionPath, strings.TrimSuffix(filepath.Base(archivePath), ".gz"))
	err = runCommand(extractionPath, sevenZipPath, "x", tarPath, "-o"+extractionPath, "-y")
	if err != nil {
		return fmt.Errorf("tarExtract: %w", err)
	}
	return nil
}
