package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	compiled "github.com/darkspinnet/darkspin/content/animation"
	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/render/ds"
	"github.com/darkspinnet/darkspin/content/render/gltf"
	"github.com/darkspinnet/darkspin/content/render/gmsh"
	"github.com/darkspinnet/darkspin/content/render/rw4"
	"github.com/spf13/cobra"
)

func newConvertCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "convert <source[:entry]> [destination]",
		Short: "Convert a DBPF package to or from a strict .ds directory",
		Long: "Convert a DBPF package to or from a strict .ds directory.\n\n" +
			"Without a destination, Creatures.package converts to Creatures.ds in the\n" +
			"current directory, and a <name>.ds directory converts back to <name>.package\n" +
			"in the current directory. Existing destinations are not overwritten.\n" +
			"Audio bundles repack both packages into their parent directory by default.\n" +
			"Selected entries and other DS directories require an explicit destination.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(command *cobra.Command, args []string) error {
			startedAt := time.Now()
			target := parsePackageTarget(args[0])
			if target.Selector == "" && strings.EqualFold(filepath.Base(target.Path), "AudioProps.package") {
				_, err := fmt.Fprintf(command.ErrOrStderr(), "Skipping %s: convert Audio.package instead; it includes the sibling AudioProps.package in one Audio.ds bundle.\n", target.Path)
				if err != nil {
					return fmt.Errorf("convertWarning: %w", err)
				}
				return nil
			}
			destinationPath := ""
			if len(args) == 2 {
				destinationPath = args[1]
			} else {
				var destinationErr error
				destinationPath, destinationErr = defaultConvertDestination(target)
				if destinationErr != nil {
					return fmt.Errorf("convertDestination: %w", destinationErr)
				}
			}
			names, err := discoverRenderNames(target.Path)
			if err != nil {
				return fmt.Errorf("convertNames: %w", err)
			}
			names, audioAliases, err := discoverAudioNames(target.Path, names)
			if err != nil {
				return fmt.Errorf("convertAudioNames: %w", err)
			}
			if target.Selector != "" {
				ordinal, ordinalErr := packageTargetOrdinal(target)
				if ordinalErr != nil {
					return fmt.Errorf("convertSelector: %w", ordinalErr)
				}
				err = ds.ExtractPackageResourcePathWithNames(command.Context(), target.Path, destinationPath, ordinal, names)
				if err != nil {
					return fmt.Errorf("convertResource: %w", err)
				}
				_, err = fmt.Fprintf(command.OutOrStdout(), "Converted %s entry %d to %s in %.2f seconds\n", target.Path, ordinal, destinationPath, time.Since(startedAt).Seconds())
				if err != nil {
					return fmt.Errorf("convertOutput: %w", err)
				}
				return nil
			}
			if strings.EqualFold(filepath.Base(target.Path), "Audio.package") {
				propertyPath := filepath.Join(filepath.Dir(target.Path), "AudioProps.package")
				_, propertyErr := os.Stat(propertyPath)
				if propertyErr == nil {
					err = ds.ExtractAudioBundlePath(command.Context(), target.Path, propertyPath, destinationPath, names, audioAliases)
					if err != nil {
						return fmt.Errorf("convertAudioBundle: %w", err)
					}
					_, err = fmt.Fprintf(command.OutOrStdout(), "Converted %s and %s to %s in %.2f seconds\n", target.Path, propertyPath, destinationPath, time.Since(startedAt).Seconds())
					if err != nil {
						return fmt.Errorf("convertOutput: %w", err)
					}
					return nil
				}
				if !errors.Is(propertyErr, os.ErrNotExist) {
					return fmt.Errorf("convertAudioPropertyStat: %w", propertyErr)
				}
			}
			err = ds.ConvertPathWithNames(command.Context(), target.Path, destinationPath, names)
			if err != nil {
				return fmt.Errorf("convertPath: %w", err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Converted %s to %s in %.2f seconds\n", args[0], destinationPath, time.Since(startedAt).Seconds())
			if err != nil {
				return fmt.Errorf("convertOutput: %w", err)
			}
			return nil
		},
	}
	return command
}

func defaultConvertDestination(target packageTarget) (string, error) {
	if target.Selector != "" {
		return "", errors.New("destination is required for a selected entry")
	}
	fi, err := os.Stat(target.Path)
	if err != nil {
		return "", fmt.Errorf("sourceStat: %w", err)
	}
	sourceName := filepath.Base(filepath.Clean(target.Path))
	sourceExtension := filepath.Ext(sourceName)
	if !fi.IsDir() {
		return strings.TrimSuffix(sourceName, sourceExtension) + ".ds", nil
	}
	isAudioBundle, err := ds.IsAudioBundlePath(target.Path)
	if err != nil {
		return "", fmt.Errorf("bundleRead: %w", err)
	}
	if isAudioBundle {
		return filepath.Dir(filepath.Clean(target.Path)), nil
	}
	if strings.EqualFold(sourceExtension, ".ds") {
		return strings.TrimSuffix(sourceName, sourceExtension) + ".package", nil
	}
	return "", errors.New("destination is required unless the directory has a .ds extension or is an audio bundle")
}

func newGLTFCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "gltf <source[:entry]> <destination>",
		Short: "Project packages or a game Data directory into cohesive glTF assets",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			target := parsePackageTarget(args[0])
			isDataRoot, rootErr := isPackageDirectory(target.Path)
			if rootErr != nil {
				return fmt.Errorf("gltfRootDetect: %w", rootErr)
			}
			if target.Selector == "" && isDataRoot {
				stats, rootErr := writePackageDirectory(command.Context(), target.Path, args[1])
				if rootErr != nil {
					return fmt.Errorf("gltfRoot: %w", rootErr)
				}
				_, rootErr = fmt.Fprintf(command.OutOrStdout(), "Projected %d meshes from %d render packages into %d cohesive resource glTF files under %s; skipped %d unsupported meshes across %d resources\n", stats.MeshCount, stats.PackageCount, stats.ResourceCount, args[1], stats.SkippedMeshCount, stats.SkippedResourceCount)
				if rootErr != nil {
					return fmt.Errorf("gltfOutput: %w", rootErr)
				}
				return nil
			}
			if target.Selector != "" {
				animationDocument, ordinal, isAnimation, animationErr := readAnimationTarget(target)
				if animationErr != nil {
					return fmt.Errorf("gltfAnimationSource: %w", animationErr)
				}
				if isAnimation {
					animationErr = gltf.WriteAnimationPath(args[1], animationDocument)
					if animationErr != nil {
						return fmt.Errorf("gltfAnimationWrite: %w", animationErr)
					}
					_, animationErr = fmt.Fprintf(command.OutOrStdout(), "Projected animation resource %d from %s to %s (%d channels)\n", ordinal, args[0], args[1], len(animationDocument.Channels))
					if animationErr != nil {
						return fmt.Errorf("gltfOutput: %w", animationErr)
					}
					return nil
				}
			}
			resources, err := readRenderResources(target.Path, target.Selector)
			if err != nil {
				return fmt.Errorf("gltfSource: %w", err)
			}
			if target.Selector == "" {
				stats, writeErr := writeRenderResourceDirectory(args[1], resources)
				if writeErr != nil {
					return fmt.Errorf("gltfDirectory: %w", writeErr)
				}
				_, err = fmt.Fprintf(command.OutOrStdout(), "Projected %d meshes into %d resource glTF files under %s; skipped %d unsupported meshes across %d resources\n", stats.MeshCount, stats.ResourceCount, args[1], stats.SkippedMeshCount, stats.SkippedResourceCount)
				if err != nil {
					return fmt.Errorf("gltfOutput: %w", err)
				}
				return nil
			}
			resource := resources[0]
			primitives, _, err := projectRenderResource(resource, true)
			if err != nil {
				return fmt.Errorf("gltfProject[%d]: %w", resource.Ordinal, err)
			}
			prefixRenderPrimitives(resource, primitives)
			err = gltf.WritePath(args[1], primitives)
			if err != nil {
				return fmt.Errorf("gltfWrite: %w", err)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Projected render resource %d from %s to %s (%d meshes)\n", resource.Ordinal, args[0], args[1], len(primitives))
			if err != nil {
				return fmt.Errorf("gltfOutput: %w", err)
			}
			return nil
		},
	}
	return command
}

func readAnimationTarget(target packageTarget) (*compiled.Document, int, bool, error) {
	fi, err := os.Stat(target.Path)
	if err != nil {
		return nil, 0, false, fmt.Errorf("sourceStat: %w", err)
	}
	if fi.IsDir() {
		document, readErr := ds.ReadPath(target.Path)
		if readErr != nil {
			return nil, 0, false, fmt.Errorf("dsRead: %w", readErr)
		}
		entries := make([]dbpf.Entry, len(document.Manifest.Resources))
		for ordinal, resource := range document.Manifest.Resources {
			entries[ordinal] = resource.Entry
		}
		ordinal, resolveErr := resolvePackageOrdinal(entries, target.Selector)
		if resolveErr != nil {
			return nil, 0, false, fmt.Errorf("dsSelector: %w", resolveErr)
		}
		if entries[ordinal].Type != compiled.ResourceType {
			return nil, ordinal, false, nil
		}
		payload, _, payloadErr := document.StoredResource(target.Path, ordinal)
		if payloadErr != nil {
			return nil, ordinal, false, fmt.Errorf("dsResource[%d]: %w", ordinal, payloadErr)
		}
		animationDocument, decodeErr := compiled.Decode(payload)
		if decodeErr != nil {
			return nil, ordinal, false, fmt.Errorf("animationDecode[%d]: %w", ordinal, decodeErr)
		}
		return animationDocument, ordinal, true, nil
	}
	r, err := os.Open(target.Path)
	if err != nil {
		return nil, 0, false, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return nil, 0, false, fmt.Errorf("packageRead: %w", err)
	}
	ordinal, err := resolvePackageOrdinal(pkg.Entries, target.Selector)
	if err != nil {
		return nil, 0, false, fmt.Errorf("packageSelector: %w", err)
	}
	if pkg.Entries[ordinal].Type != compiled.ResourceType {
		return nil, ordinal, false, nil
	}
	payloadReader, err := pkg.Open(pkg.Entries[ordinal])
	if err != nil {
		return nil, ordinal, false, fmt.Errorf("resourceOpen[%d]: %w", ordinal, err)
	}
	payload, err := io.ReadAll(payloadReader)
	if err != nil {
		return nil, ordinal, false, fmt.Errorf("resourceRead[%d]: %w", ordinal, err)
	}
	animationDocument, err := compiled.Decode(payload)
	if err != nil {
		return nil, ordinal, false, fmt.Errorf("animationDecode[%d]: %w", ordinal, err)
	}
	return animationDocument, ordinal, true, nil
}

type renderProjectionStats struct {
	MeshCount            int
	ResourceCount        int
	SkippedMeshCount     int
	SkippedResourceCount int
	PackageCount         int
}

func isPackageDirectory(sourcePath string) (bool, error) {
	fi, err := os.Stat(sourcePath)
	if err != nil {
		return false, fmt.Errorf("sourceStat: %w", err)
	}
	if !fi.IsDir() {
		return false, nil
	}
	_, err = os.Stat(filepath.Join(sourcePath, ds.ManifestName))
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("manifestStat: %w", err)
	}
	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return false, fmt.Errorf("directoryRead: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".package") {
			return true, nil
		}
	}
	return false, nil
}

func writePackageDirectory(ctx context.Context, sourcePath, destinationPath string) (renderProjectionStats, error) {
	stats := renderProjectionStats{}
	packageNames := make([]string, 0)
	_, err := os.Stat(destinationPath)
	if err == nil {
		return stats, fmt.Errorf("destinationExists: %q", destinationPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return stats, fmt.Errorf("destinationStat: %w", err)
	}
	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return stats, fmt.Errorf("sourceRead: %w", err)
	}
	err = os.MkdirAll(destinationPath, 0o755)
	if err != nil {
		return stats, fmt.Errorf("destinationMkdir: %w", err)
	}
	isComplete := false
	defer func() {
		if !isComplete {
			_ = os.RemoveAll(destinationPath)
		}
	}()
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".package") {
			continue
		}
		select {
		case <-ctx.Done():
			return stats, fmt.Errorf("cancel: %w", ctx.Err())
		default:
		}
		packagePath := filepath.Join(sourcePath, entry.Name())
		resources, readErr := readRenderResources(packagePath, "")
		if readErr != nil {
			if strings.Contains(readErr.Error(), "resourceMissing:") {
				continue
			}
			return stats, fmt.Errorf("packageRead[%s]: %w", entry.Name(), readErr)
		}
		packageName := safeRenderName(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		packageStats, writeErr := writeRenderResourceDirectory(filepath.Join(destinationPath, packageName), resources)
		if writeErr != nil {
			return stats, fmt.Errorf("packageWrite[%s]: %w", entry.Name(), writeErr)
		}
		stats.MeshCount += packageStats.MeshCount
		stats.ResourceCount += packageStats.ResourceCount
		stats.SkippedMeshCount += packageStats.SkippedMeshCount
		stats.SkippedResourceCount += packageStats.SkippedResourceCount
		stats.PackageCount++
		packageNames = append(packageNames, packageName)
	}
	if stats.PackageCount == 0 {
		return stats, errors.New("packageMissing: no packages contain projectable RW4 or GMSH resources")
	}
	err = writePackageRootIndex(destinationPath, packageNames)
	if err != nil {
		return stats, fmt.Errorf("rootIndex: %w", err)
	}
	isComplete = true
	return stats, nil
}

func writePackageRootIndex(destinationPath string, packageNames []string) error {
	indexPath := filepath.Join(destinationPath, "_root.dse")
	w, err := os.OpenFile(indexPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("indexCreate: %w", err)
	}
	writer := bufio.NewWriter(w)
	_, err = fmt.Fprintln(writer, "// darkspin gltf package root dse v1")
	if err == nil {
		_, err = fmt.Fprintf(writer, "RENDERROOT %q\n\tVERSION 1\n\tNUMPACKAGES %d\n", filepath.Base(destinationPath), len(packageNames))
	}
	for _, packageName := range packageNames {
		if err == nil {
			_, err = fmt.Fprintf(writer, "\t\tPACKAGE %q\n", filepath.ToSlash(filepath.Join(packageName, "_root.dse")))
		}
	}
	if err == nil {
		err = writer.Flush()
	}
	closeErr := w.Close()
	if err != nil {
		return fmt.Errorf("indexWrite: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("indexClose: %w", closeErr)
	}
	return nil
}

func writeRenderResourceDirectory(destinationPath string, resources []renderResource) (renderProjectionStats, error) {
	stats := renderProjectionStats{}
	_, err := os.Stat(destinationPath)
	if err == nil {
		return stats, fmt.Errorf("destinationExists: %q", destinationPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return stats, fmt.Errorf("destinationStat: %w", err)
	}
	err = os.MkdirAll(destinationPath, 0o755)
	if err != nil {
		return stats, fmt.Errorf("destinationMkdir: %w", err)
	}
	isComplete := false
	defer func() {
		if !isComplete {
			_ = os.RemoveAll(destinationPath)
		}
	}()
	for _, resource := range resources {
		primitives, skippedMeshCount, projectErr := projectRenderResource(resource, false)
		if projectErr != nil {
			stats.SkippedResourceCount++
			continue
		}
		stats.SkippedMeshCount += skippedMeshCount
		if len(primitives) == 0 {
			stats.SkippedResourceCount++
			continue
		}
		prefixRenderPrimitives(resource, primitives)
		resourceName := renderResourceName(resource)
		resourcePath := filepath.Join(destinationPath, resourceName)
		err = os.MkdirAll(filepath.Dir(resourcePath), 0o755)
		if err != nil {
			return stats, fmt.Errorf("resourceMkdir[%d]: %w", resource.Ordinal, err)
		}
		err = gltf.WritePath(resourcePath, primitives)
		if err != nil {
			return stats, fmt.Errorf("resourceWrite[%d]: %w", resource.Ordinal, err)
		}
		stats.MeshCount += len(primitives)
		stats.ResourceCount++
	}
	if stats.ResourceCount == 0 {
		return stats, errors.New("resourceMissing: no projectable render meshes")
	}
	err = writeRenderDirectoryIndex(destinationPath, resources)
	if err != nil {
		return stats, fmt.Errorf("indexWrite: %w", err)
	}
	isComplete = true
	return stats, nil
}

func projectRenderResource(resource renderResource, isStrict bool) ([]rw4.Primitive, int, error) {
	switch resource.Entry.Type {
	case rw4.ResourceType:
		document, err := rw4.Decode(resource.Payload)
		if err != nil {
			return nil, 0, fmt.Errorf("rw4Decode: %w", err)
		}
		if !isStrict {
			primitives, skippedMeshCount := document.ProjectablePrimitives()
			return primitives, skippedMeshCount, nil
		}
		primitives, err := document.Primitives()
		if err != nil {
			return nil, 0, fmt.Errorf("rw4Project: %w", err)
		}
		return primitives, 0, nil
	case gmsh.ResourceType:
		document, err := gmsh.Decode(resource.Payload)
		if err != nil {
			return nil, 0, fmt.Errorf("gmshDecode: %w", err)
		}
		if !isStrict {
			primitives, skippedMeshCount := document.ProjectablePrimitives()
			return primitives, skippedMeshCount, nil
		}
		primitives, err := document.Primitives()
		if err != nil {
			return nil, 0, fmt.Errorf("gmshProject: %w", err)
		}
		return primitives, 0, nil
	default:
		return nil, 0, fmt.Errorf("resourceType: unsupported 0x%08X", resource.Entry.Type)
	}
}

type renderResource struct {
	Ordinal   int
	Entry     dbpf.Entry
	Payload   []byte
	Name      string
	GroupName string
	LODName   string
	Category  string
}

func readRenderResources(sourcePath string, selector string) ([]renderResource, error) {
	fi, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("sourceStat: %w", err)
	}
	if fi.IsDir() {
		document, readErr := ds.ReadPath(sourcePath)
		if readErr != nil {
			return nil, fmt.Errorf("dsRead: %w", readErr)
		}
		entries := make([]dbpf.Entry, len(document.Manifest.Resources))
		for ordinal, resource := range document.Manifest.Resources {
			entries[ordinal] = resource.Entry
		}
		requestedOrdinal, resolveErr := resolvePackageOrdinal(entries, selector)
		if resolveErr != nil {
			return nil, fmt.Errorf("dsSelector: %w", resolveErr)
		}
		ordinals, selectErr := selectRenderOrdinals(document.Manifest.Resources, requestedOrdinal)
		if selectErr != nil {
			return nil, fmt.Errorf("dsSelect: %w", selectErr)
		}
		resources := make([]renderResource, 0, len(ordinals))
		for _, ordinal := range ordinals {
			payload, entry, resourceErr := document.StoredResource(sourcePath, ordinal)
			if resourceErr != nil {
				return nil, fmt.Errorf("dsResource[%d]: %w", ordinal, resourceErr)
			}
			resource := renderResource{Ordinal: ordinal, Entry: entry, Payload: payload}
			applyDSRenderPath(&resource, document.Manifest.Resources[ordinal].PayloadPath)
			resources = append(resources, resource)
		}
		return resources, nil
	}
	r, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("packageRead: %w", err)
	}
	resources := pkg.ArchiveManifest().Resources
	requestedOrdinal, err := resolvePackageOrdinal(pkg.Entries, selector)
	if err != nil {
		return nil, fmt.Errorf("packageSelector: %w", err)
	}
	ordinals, err := selectRenderOrdinals(resources, requestedOrdinal)
	if err != nil {
		return nil, fmt.Errorf("packageSelect: %w", err)
	}
	renderResources := make([]renderResource, 0, len(ordinals))
	for _, ordinal := range ordinals {
		payloadReader, openErr := pkg.Open(pkg.Entries[ordinal])
		if openErr != nil {
			return nil, fmt.Errorf("resourceOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return nil, fmt.Errorf("resourceRead[%d]: %w", ordinal, readErr)
		}
		resource := renderResource{Ordinal: ordinal, Entry: pkg.Entries[ordinal], Payload: payload}
		renderResources = append(renderResources, resource)
	}
	names, err := discoverRenderNames(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("renderNames: %w", err)
	}
	applyRenderNames(renderResources, names)
	if selector != "" {
		applyRenderSelector(&renderResources[0], selector)
	}
	return renderResources, nil
}

func selectRenderOrdinals(resources []dbpf.Resource, requestedOrdinal int) ([]int, error) {
	if requestedOrdinal >= 0 {
		if requestedOrdinal >= len(resources) {
			return nil, fmt.Errorf("ordinalRange: got %d, resources %d", requestedOrdinal, len(resources))
		}
		if !isRenderResourceType(resources[requestedOrdinal].Entry.Type) {
			return nil, fmt.Errorf("ordinalType: %d is unsupported type 0x%08X", requestedOrdinal, resources[requestedOrdinal].Entry.Type)
		}
		return []int{requestedOrdinal}, nil
	}
	ordinals := make([]int, 0)
	for ordinal, resource := range resources {
		if !isRenderResourceType(resource.Entry.Type) {
			continue
		}
		ordinals = append(ordinals, ordinal)
	}
	if len(ordinals) == 0 {
		return nil, fmt.Errorf("resourceMissing: no RW4 or GMSH resources")
	}
	return ordinals, nil
}

func isRenderResourceType(typeCode uint32) bool {
	return typeCode == rw4.ResourceType || typeCode == gmsh.ResourceType
}

func prefixRenderPrimitives(resource renderResource, primitives []rw4.Primitive) {
	resourcePrefix := fmt.Sprintf("resource_%06d_%016x", resource.Ordinal, resource.Entry.Instance)
	if resource.Name != "" {
		resourcePrefix = resource.Name + "__" + resourcePrefix
	}
	for primitiveIndex := range primitives {
		primitive := &primitives[primitiveIndex]
		primitive.Name = resourcePrefix + "_" + primitive.Name
		if primitive.Material == nil {
			continue
		}
		primitive.Material.Name = resourcePrefix + "_" + primitive.Material.Name
		if primitive.Material.BaseColor != nil {
			primitive.Material.BaseColor.Name = resourcePrefix + "_" + primitive.Material.BaseColor.Name
		}
		if primitive.Material.Normal != nil {
			primitive.Material.Normal.Name = resourcePrefix + "_" + primitive.Material.Normal.Name
		}
	}
}
