package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/audio"
)

func discoverAudioNames(sourcePath string, names map[uint32]string) (map[uint32]string, error) {
	sourceName := filepath.Base(sourcePath)
	if !strings.EqualFold(sourceName, "Audio.package") && !strings.EqualFold(sourceName, "AudioProps.package") {
		return names, nil
	}
	propertyPath, err := findAudioPropertyPath(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("propertyPath: %w", err)
	}
	if propertyPath == "" {
		return names, nil
	}
	mergedNames := make(map[uint32]string, len(names)+16)
	for nameID, name := range names {
		mergedNames[nameID] = name
	}
	for nameID, name := range audio.ClientNames() {
		if mergedNames[nameID] == "" {
			mergedNames[nameID] = name
		}
	}
	for nameID, name := range audio.InferredNames() {
		if mergedNames[nameID] == "" {
			mergedNames[nameID] = name
		}
	}
	if strings.EqualFold(sourceName, "Audio.package") {
		patchAliases, patchErr := audio.PatchParameterAliases(sourcePath, mergedNames)
		if patchErr != nil {
			return nil, fmt.Errorf("patchAliases: %w", patchErr)
		}
		for nameID, name := range patchAliases {
			if mergedNames[nameID] == "" {
				mergedNames[nameID] = name
			}
		}
	}
	referenceAliases, err := audio.ReferencedPropertyAliases(filepath.Dir(propertyPath), propertyPath, mergedNames)
	if err != nil {
		return nil, fmt.Errorf("propertyAliases: %w", err)
	}
	for nameID, name := range referenceAliases {
		if mergedNames[nameID] == "" {
			mergedNames[nameID] = name
		}
	}
	inheritedAliases, err := audio.InheritedPropertyAliases(propertyPath, mergedNames)
	if err != nil {
		return nil, fmt.Errorf("propertyInheritance: %w", err)
	}
	for nameID, name := range inheritedAliases {
		if mergedNames[nameID] == "" {
			mergedNames[nameID] = name
		}
	}
	if strings.EqualFold(sourceName, "AudioProps.package") {
		return mergedNames, nil
	}
	sampleAliases, err := audio.SampleAliases(propertyPath, mergedNames)
	if err != nil {
		return nil, fmt.Errorf("sampleAliases: %w", err)
	}
	for nameID, name := range sampleAliases {
		if mergedNames[nameID] == "" {
			mergedNames[nameID] = name
		}
	}
	return mergedNames, nil
}

func findAudioPropertyPath(sourcePath string) (string, error) {
	absoluteSourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", fmt.Errorf("sourcePath: %w", err)
	}
	for directoryPath := filepath.Dir(absoluteSourcePath); ; directoryPath = filepath.Dir(directoryPath) {
		candidatePath := filepath.Join(directoryPath, "AudioProps.package")
		fi, statErr := os.Stat(candidatePath)
		if statErr == nil && !fi.IsDir() {
			return candidatePath, nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("propertyStat: %w", statErr)
		}
		parentPath := filepath.Dir(directoryPath)
		if parentPath == directoryPath {
			break
		}
	}
	return "", nil
}
