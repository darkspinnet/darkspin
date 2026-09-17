package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	serverutil "github.com/darkspinnet/darkspin/server/util"
)

type packageTarget struct {
	Path     string
	Selector string
}

func packageTargetOrdinal(target packageTarget) (int, error) {
	r, err := os.Open(target.Path)
	if err != nil {
		return -1, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return -1, fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return -1, fmt.Errorf("packageRead: %w", err)
	}
	ordinal, err := resolvePackageOrdinal(pkg.Entries, target.Selector)
	if err != nil {
		return -1, fmt.Errorf("entryResolve: %w", err)
	}
	return ordinal, nil
}

func parsePackageTarget(argument string) packageTarget {
	lowerArgument := strings.ToLower(argument)
	for _, extension := range []string{".package:", ".ds:"} {
		separator := strings.Index(lowerArgument, extension)
		if separator < 0 {
			continue
		}
		pathEnd := separator + len(extension) - 1
		return packageTarget{Path: argument[:pathEnd], Selector: argument[pathEnd+1:]}
	}
	return packageTarget{Path: argument}
}

func resolvePackageOrdinal(entries []dbpf.Entry, selector string) (int, error) {
	if selector == "" {
		return -1, nil
	}
	ordinal, err := strconv.Atoi(selector)
	if err == nil {
		if ordinal < 0 || ordinal >= len(entries) {
			return -1, fmt.Errorf("selectorRange: got %d, resources %d", ordinal, len(entries))
		}
		return ordinal, nil
	}
	cleanSelector := strings.TrimSuffix(filepath.Base(selector), filepath.Ext(selector))
	for entryOrdinal, entry := range entries {
		resourceName := strings.TrimSuffix(dbpf.ResourceName(entryOrdinal, entry), ".bin")
		if strings.EqualFold(cleanSelector, resourceName) {
			return entryOrdinal, nil
		}
	}
	group := uint32(0)
	isGroupRestricted := false
	name := cleanSelector
	if strings.HasSuffix(strings.ToUpper(name), "_LOD0") {
		name = name[:len(name)-5]
		group, isGroupRestricted = 0x40606000, true
	} else if strings.HasSuffix(strings.ToUpper(name), "_LOD1") {
		name = name[:len(name)-5]
		group, isGroupRestricted = 0x40606100, true
	}
	instance := uint64(serverutil.HashID(name))
	matches := make([]int, 0, 1)
	for entryOrdinal, entry := range entries {
		if entry.Instance != instance || isGroupRestricted && entry.Group != group {
			continue
		}
		matches = append(matches, entryOrdinal)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return -1, fmt.Errorf("selectorAmbiguous: %q matches ordinals %v; use the stable resource identity", selector, matches)
	}
	return -1, fmt.Errorf("selectorMissing: %q", selector)
}
