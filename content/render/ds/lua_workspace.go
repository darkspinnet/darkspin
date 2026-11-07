package ds

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/lua51"
)

const luaWorkspaceConfig = ".luarc.json"
const luaWorkspaceDefinitions = "definitions/game.d.lua"

type luaSourceCollector struct {
	root         string
	sources      [][]byte
	isLuaPresent bool
}

func finalizeLuaWorkspace(destinationPath string) error {
	collector := luaSourceCollector{root: filepath.Clean(destinationPath)}
	err := filepath.WalkDir(destinationPath, collector.visit)
	if err != nil {
		return fmt.Errorf("sourceWalk: %w", err)
	}
	if len(collector.sources) == 0 && !collector.isLuaPresent {
		return nil
	}
	payload, err := lua51.Definitions(collector.sources)
	if err != nil {
		return fmt.Errorf("definitionGenerate: %w", err)
	}
	definitionPath := filepath.Join(destinationPath, filepath.FromSlash(luaWorkspaceDefinitions))
	err = os.MkdirAll(filepath.Dir(definitionPath), 0o755)
	if err != nil {
		return fmt.Errorf("definitionMkdir: %w", err)
	}
	err = os.WriteFile(definitionPath, payload, 0o644)
	if err != nil {
		return fmt.Errorf("definitionWrite: %w", err)
	}
	config := []byte("{\n  \"runtime.version\": \"Lua 5.1\",\n  \"workspace.library\": [\"definitions\"],\n  \"workspace.checkThirdParty\": false\n}\n")
	err = os.WriteFile(filepath.Join(destinationPath, luaWorkspaceConfig), config, 0o644)
	if err != nil {
		return fmt.Errorf("configWrite: %w", err)
	}
	return nil
}

func (e *luaSourceCollector) visit(path string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return fmt.Errorf("sourceEntry: %w", walkErr)
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return fmt.Errorf("sourceSymlink: %q", path)
	}
	if entry.IsDir() {
		if filepath.Clean(path) == filepath.Join(e.root, "definitions") {
			return fs.SkipDir
		}
		return nil
	}
	if !strings.EqualFold(filepath.Ext(path), ".lua") {
		if strings.HasSuffix(path, ".lua.txt") {
			e.isLuaPresent = true
		}
		return nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("sourceRead: %w", err)
	}
	e.sources = append(e.sources, payload)
	return nil
}
