package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	creationreader "github.com/darkspinnet/darkspin/content/creation"
	"github.com/darkspinnet/darkspin/content/render/rw4"
)

type meshInspection struct {
	Source       string
	SectionCount int
	Meshes       []meshInspectionEntry
}

type meshInspectionEntry struct {
	Name          string
	VertexCount   int
	TriangleCount int
	Minimums      [3]float32
	Maximums      [3]float32
}

func writeInspectedMesh(output io.Writer, sourcePath string, pkg *creationreader.PackageInspection, ordinal int, meshName string) error {
	payload, err := pkg.ReadResource(ordinal)
	if err != nil {
		return fmt.Errorf("rw4Read: %w", err)
	}
	document, err := rw4.Decode(payload)
	if err != nil {
		return fmt.Errorf("rw4Decode: %w", err)
	}
	if meshName == "" {
		table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
		_, err = fmt.Fprintln(table, "\nSECTION\tTYPE\tOFFSET\tSIZE")
		if err != nil {
			return fmt.Errorf("sectionHeader: %w", err)
		}
		for _, section := range document.Sections {
			_, err = fmt.Fprintf(table, "%d\t0x%08X\t%d\t%d\n", section.Ordinal, section.TypeCode, section.Offset, section.Size)
			if err != nil {
				return fmt.Errorf("sectionWrite: %w", err)
			}
		}
		err = table.Flush()
		if err != nil {
			return fmt.Errorf("sectionFlush: %w", err)
		}
		if len(document.Meshes) == 0 {
			return nil
		}
	}
	inspection, err := inspectMeshDocument(sourcePath, document, meshName)
	if err != nil {
		return fmt.Errorf("meshInspect: %w", err)
	}
	err = writeMeshInspection(output, inspection)
	if err != nil {
		return fmt.Errorf("meshWrite: %w", err)
	}
	if meshName == "" {
		_, err = fmt.Fprintf(output, "Next:\t%s:<mesh name>\n", sourcePath)
		if err != nil {
			return fmt.Errorf("hintWrite: %w", err)
		}
	}
	return nil
}

func inspectMeshDocument(sourcePath string, document *rw4.Document, meshName string) (meshInspection, error) {
	primitives, err := document.Primitives()
	if err != nil {
		return meshInspection{}, fmt.Errorf("meshProject: %w", err)
	}
	inspection := meshInspection{Source: sourcePath, SectionCount: len(document.Sections)}
	for _, primitive := range primitives {
		if meshName != "" && primitive.Name != meshName {
			continue
		}
		if len(primitive.Positions) == 0 {
			return meshInspection{}, fmt.Errorf("meshEmpty: %s has no vertices", primitive.Name)
		}
		entry := meshInspectionEntry{
			Name: primitive.Name, VertexCount: len(primitive.Positions),
			TriangleCount: len(primitive.Indices) / 3,
			Minimums:      primitive.Positions[0], Maximums: primitive.Positions[0],
		}
		for _, position := range primitive.Positions[1:] {
			for axis, coordinate := range position {
				entry.Minimums[axis] = min(entry.Minimums[axis], coordinate)
				entry.Maximums[axis] = max(entry.Maximums[axis], coordinate)
			}
		}
		inspection.Meshes = append(inspection.Meshes, entry)
	}
	if len(inspection.Meshes) == 0 {
		return meshInspection{}, fmt.Errorf("meshMissing: %q", meshName)
	}
	return inspection, nil
}

func writeMeshInspection(output io.Writer, inspection meshInspection) error {
	_, err := fmt.Fprintf(output, "Source:\t%s\nSections:\t%d\nBounds:\tlocal mesh coordinates\n\n", inspection.Source, inspection.SectionCount)
	if err != nil {
		return fmt.Errorf("summaryWrite: %w", err)
	}
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	_, err = fmt.Fprintln(table, "MESH\tVERTICES\tTRIANGLES\tMINIMUM\tMAXIMUM")
	if err != nil {
		return fmt.Errorf("headerWrite: %w", err)
	}
	for _, mesh := range inspection.Meshes {
		_, err = fmt.Fprintf(table, "%s\t%d\t%d\t%v\t%v\n", mesh.Name, mesh.VertexCount, mesh.TriangleCount, mesh.Minimums, mesh.Maximums)
		if err != nil {
			return fmt.Errorf("meshWrite: %w", err)
		}
	}
	err = table.Flush()
	if err != nil {
		return fmt.Errorf("tableFlush: %w", err)
	}
	return nil
}
