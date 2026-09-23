package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/audio"
)

func discoverAudioNames(sourcePath string, names map[uint32]string) (map[uint32]string, map[uint32]audio.SampleAlias, error) {
	sourceName := filepath.Base(sourcePath)
	if !isAudioPackageName(sourceName) {
		return names, nil, nil
	}
	if isSporeAudioPackageName(sourceName) {
		mergedNames := audio.SporeNames()
		for instanceID, name := range names {
			if mergedNames[instanceID] == "" {
				mergedNames[instanceID] = name
			}
		}
		return mergedNames, audio.SporeSampleAliases(), nil
	}
	propertyPath, err := findAudioPropertyPath(sourcePath)
	if err != nil {
		return nil, nil, fmt.Errorf("propertyPath: %w", err)
	}
	if propertyPath == "" {
		return names, nil, nil
	}
	mergedNames := make(map[uint32]string, len(names)+16)
	for nameID, name := range names {
		mergedNames[nameID] = name
	}
	for nameID, name := range mergedNames {
		if isGenericAudioHierarchyName(name) {
			delete(mergedNames, nameID)
		}
	}
	for nameID, name := range audio.DarksporeNames() {
		if mergedNames[nameID] == "" {
			mergedNames[nameID] = name
		}
	}
	for nameID, name := range audio.SporeNames() {
		if mergedNames[nameID] == "" {
			mergedNames[nameID] = name
		}
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
	patchPaths := []string{sourcePath}
	if !strings.EqualFold(sourcePath, propertyPath) {
		patchPaths = append(patchPaths, propertyPath)
	}
	for _, patchPath := range patchPaths {
		patchAliases, patchErr := audio.PatchParameterAliases(patchPath, mergedNames)
		if patchErr != nil {
			return nil, nil, fmt.Errorf("patchAliases: %w", patchErr)
		}
		for nameID, name := range patchAliases {
			if isGenericAudioHierarchyName(name) {
				continue
			}
			if mergedNames[nameID] == "" {
				mergedNames[nameID] = name
			}
		}
	}
	referenceAliases, err := audio.ReferencedPropertyAliases(filepath.Dir(propertyPath), propertyPath, mergedNames)
	if err != nil {
		return nil, nil, fmt.Errorf("propertyAliases: %w", err)
	}
	for nameID, name := range referenceAliases {
		if isGenericAudioHierarchyName(name) {
			continue
		}
		if mergedNames[nameID] == "" {
			mergedNames[nameID] = name
		}
	}
	inheritedAliases, err := audio.InheritedPropertyAliases(propertyPath, mergedNames)
	if err != nil {
		return nil, nil, fmt.Errorf("propertyInheritance: %w", err)
	}
	for nameID, name := range inheritedAliases {
		if mergedNames[nameID] == "" {
			mergedNames[nameID] = name
		}
	}
	if strings.EqualFold(sourceName, "AudioProps.package") {
		return mergedNames, nil, nil
	}
	sampleRecords, err := audio.SampleAliasRecords(propertyPath, mergedNames)
	if err != nil {
		return nil, nil, fmt.Errorf("sampleAliases: %w", err)
	}
	for nameID, record := range sampleRecords {
		if mergedNames[nameID] == "" {
			mergedNames[nameID] = record.Name
		}
	}
	return mergedNames, sampleRecords, nil
}

func isGenericAudioHierarchyName(name string) bool {
	return strings.HasPrefix(strings.TrimLeft(strings.ToLower(name), "_"), "footsteps_parent")
}

func isSporeAudioPackageName(name string) bool {
	return strings.EqualFold(name, "Spore_Audio1.package") ||
		strings.EqualFold(name, "Spore_Audio2.package")
}

func isAudioPackageName(name string) bool {
	return strings.EqualFold(name, "Audio.package") ||
		strings.EqualFold(name, "AudioProps.package") ||
		strings.EqualFold(name, "Spore_Audio1.package") ||
		strings.EqualFold(name, "Spore_Audio2.package")
}

func findAudioPropertyPath(sourcePath string) (string, error) {
	absoluteSourcePath, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", fmt.Errorf("sourcePath: %w", err)
	}
	sourceName := filepath.Base(absoluteSourcePath)
	propertyNames := []string{"AudioProps.package"}
	if strings.EqualFold(sourceName, "Spore_Audio1.package") || strings.EqualFold(sourceName, "Spore_Audio2.package") {
		propertyNames = []string{"Spore_Audio2.package"}
	}
	for directoryPath := filepath.Dir(absoluteSourcePath); ; directoryPath = filepath.Dir(directoryPath) {
		for _, propertyName := range propertyNames {
			candidatePath := filepath.Join(directoryPath, propertyName)
			fi, statErr := os.Stat(candidatePath)
			if statErr == nil && !fi.IsDir() {
				return candidatePath, nil
			}
			if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				return "", fmt.Errorf("propertyStat: %w", statErr)
			}
		}
		parentPath := filepath.Dir(directoryPath)
		if parentPath == directoryPath {
			break
		}
	}
	return "", nil
}
