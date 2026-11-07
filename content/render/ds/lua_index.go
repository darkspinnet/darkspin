package ds

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const luaIndexName = "lua-index.json"

type luaIndex struct {
	Version   int                `json:"version"`
	Resources []luaIndexResource `json:"resources"`
}

type luaIndexResource struct {
	Ordinal    int      `json:"ordinal"`
	Group      string   `json:"group"`
	Instance   string   `json:"instance"`
	Path       string   `json:"path"`
	Name       string   `json:"name,omitempty"`
	Evidence   string   `json:"evidence"`
	Symbols    []string `json:"symbols,omitempty"`
	References []string `json:"references,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

func writeLuaIndex(destinationPath string, resources []dbpf.Resource, names map[luaResourceKey]luaResourceName) error {
	index := luaIndex{Version: 1, Resources: make([]luaIndexResource, 0, len(names))}
	for ordinal, resource := range resources {
		if resource.Entry.Type != luaResourceType {
			continue
		}
		name := names[luaResourceKey{group: resource.Entry.Group, instance: resource.Entry.Instance}]
		index.Resources = append(index.Resources, luaIndexResource{
			Ordinal: ordinal, Group: fmt.Sprintf("0x%08X", resource.Entry.Group),
			Instance: fmt.Sprintf("0x%016X", resource.Entry.Instance), Path: resource.PayloadPath,
			Name: name.Name, Evidence: name.Evidence, Symbols: name.Symbols,
			References: name.References, Candidates: name.Candidates,
		})
	}
	if len(index.Resources) == 0 {
		return nil
	}
	payload, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("indexEncode: %w", err)
	}
	err = writeLuaFile(filepath.Join(destinationPath, luaIndexName), append(payload, '\n'))
	if err != nil {
		return fmt.Errorf("indexWrite: %w", err)
	}
	return nil
}

func copyLuaIndex(sourcePath, destinationPath string) error {
	path := filepath.Join(sourcePath, luaIndexName)
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("indexRead: %w", err)
	}
	err = writeLuaFile(filepath.Join(destinationPath, luaIndexName), payload)
	if err != nil {
		return fmt.Errorf("indexCopy: %w", err)
	}
	return nil
}
