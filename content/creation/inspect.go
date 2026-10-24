package creation

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

// PackageInspection exposes the shared read-only DBPF/DBBF reader to inspection
// tools. Entry names are stable resource identities, not original source paths.
// The caller owns the source handle and must keep it open while reading entries.
type PackageInspection struct {
	pkg          *creationPackage
	Entries      []dbpf.Entry
	Format       string
	MajorVersion uint32
	MinorVersion uint32
	IndexVersion uint32
}

func InspectPackage(r io.ReaderAt, size int64) (*PackageInspection, error) {
	pkg, err := openCreationPackage(r, size)
	if err != nil {
		return nil, fmt.Errorf("packageIndex: %w", err)
	}
	inspection := &PackageInspection{pkg: pkg, Entries: make([]dbpf.Entry, len(pkg.entries))}
	var header [96]byte
	byteCount, err := r.ReadAt(header[:], 0)
	if err != nil {
		return nil, fmt.Errorf("packageHeader: %w", err)
	}
	if byteCount != len(header) {
		return nil, fmt.Errorf("packageHeader: read %d of %d bytes", byteCount, len(header))
	}
	inspection.Format = string(header[:4])
	inspection.MajorVersion = binary.LittleEndian.Uint32(header[4:8])
	inspection.MinorVersion = binary.LittleEndian.Uint32(header[8:12])
	indexVersionOffset := 60
	if inspection.Format == "DBBF" {
		indexVersionOffset = 52
	}
	inspection.IndexVersion = binary.LittleEndian.Uint32(header[indexVersionOffset : indexVersionOffset+4])
	for ordinal, entry := range pkg.entries {
		inspection.Entries[ordinal] = entry.Entry
	}
	return inspection, nil
}

// ReadResource decodes exactly one indexed resource without extracting a file.
func (e *PackageInspection) ReadResource(ordinal int) ([]byte, error) {
	if ordinal < 0 || ordinal >= len(e.pkg.entries) {
		return nil, fmt.Errorf("ordinalRange: got %d, package has %d resources", ordinal, len(e.pkg.entries))
	}
	payload, err := readPackageResource(e.pkg, e.pkg.entries[ordinal])
	if err != nil {
		return nil, fmt.Errorf("resourceRead[%d]: %w", ordinal, err)
	}
	return payload, nil
}
