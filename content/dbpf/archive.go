package dbpf

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ManifestName is the metadata document written beside extracted resources.
const ManifestName = "dbpf.json"

// ResourceName returns the stable filename used for an indexed package member.
// DBPF archives identify resources by type, group, and instance rather than by
// retaining their original authored filesystem paths.
func ResourceName(ordinal int, entry Entry) string {
	return fmt.Sprintf("%06d_%08x_%08x_%016x.bin", ordinal, entry.Type, entry.Group, entry.Instance)
}

// Manifest preserves the archive-wide metadata required for lossless repacking.
type Manifest struct {
	HeaderBytes      []byte     `json:"header_bytes"`
	IndexFlags       uint32     `json:"index_flags"`
	SharedType       uint32     `json:"shared_type,omitempty"`
	SharedGroup      uint32     `json:"shared_group,omitempty"`
	SharedInstanceHi uint32     `json:"shared_instance_high,omitempty"`
	Resources        []Resource `json:"resource"`
}

// Resource connects one DBPF entry to its extracted stored payload.
type Resource struct {
	Entry            Entry  `json:"entry"`
	PayloadPath      string `json:"payload_path"`
	IsStoredSizeFlag bool   `json:"is_stored_size_flag"`
}

// ResourceID identifies one package member independently of its ordinal.
type ResourceID struct {
	Type     uint32
	Group    uint32
	Instance uint64
}

// ArchiveManifest returns the archive metadata and deterministic stored-payload
// paths used by lossless extracted representations.
func (reader *Reader) ArchiveManifest() *Manifest {
	if reader == nil {
		return nil
	}
	manifest := &Manifest{
		HeaderBytes:      append([]byte(nil), reader.headerBytes[:]...),
		IndexFlags:       reader.indexFlags,
		SharedType:       reader.sharedType,
		SharedGroup:      reader.sharedGroup,
		SharedInstanceHi: reader.sharedInstHi,
		Resources:        make([]Resource, 0, len(reader.Entries)),
	}
	for ordinal, entry := range reader.Entries {
		payloadPath := filepath.ToSlash(filepath.Join("resource", ResourceName(ordinal, entry)))
		manifest.Resources = append(manifest.Resources, Resource{
			Entry:            entry,
			PayloadPath:      payloadPath,
			IsStoredSizeFlag: entry.isStoredSizeFlag,
		})
	}
	return manifest
}

// ReplaceDecodedResourcePath atomically replaces one uniquely identified
// member with an uncompressed decoded payload while preserving every other
// archive member exactly as stored.
func ReplaceDecodedResourcePath(ctx context.Context, packagePath string, resourceID ResourceID, decodedPayload []byte) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if packagePath == "" {
		return errors.New("empty package path")
	}
	if uint64(len(decodedPayload)) > uint64(^uint32(0)) {
		return fmt.Errorf("payloadSize: %d exceeds DBPF limit", len(decodedPayload))
	}
	packageContents, err := os.ReadFile(packagePath)
	if err != nil {
		return fmt.Errorf("packageRead: %w", err)
	}
	reader, err := NewReader(bytes.NewReader(packageContents), int64(len(packageContents)))
	if err != nil {
		return fmt.Errorf("packageDecode: %w", err)
	}
	targetOrdinal := -1
	for ordinal, entry := range reader.Entries {
		if entry.Type != resourceID.Type || entry.Group != resourceID.Group || entry.Instance != resourceID.Instance {
			continue
		}
		if targetOrdinal >= 0 {
			return errors.New("resource identity is not unique")
		}
		targetOrdinal = ordinal
	}
	if targetOrdinal < 0 {
		return errors.New("resource not found")
	}
	current, err := reader.Open(reader.Entries[targetOrdinal])
	if err != nil {
		return fmt.Errorf("currentOpen: %w", err)
	}
	currentPayload, err := io.ReadAll(current)
	if err != nil {
		return fmt.Errorf("currentRead: %w", err)
	}
	if bytes.Equal(currentPayload, decodedPayload) {
		return nil
	}
	temporary, err := os.CreateTemp(filepath.Dir(packagePath), ".dbpf-replace-*.package")
	if err != nil {
		return fmt.Errorf("temporaryCreate: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	err = writeDecodedReplacement(ctx, temporary, reader, targetOrdinal, decodedPayload)
	if err != nil {
		_ = temporary.Close()
		return fmt.Errorf("replacementWrite: %w", err)
	}
	err = temporary.Close()
	if err != nil {
		return fmt.Errorf("temporaryClose: %w", err)
	}
	err = verifyDecodedReplacement(temporaryPath, resourceID, decodedPayload)
	if err != nil {
		return fmt.Errorf("replacementVerify: %w", err)
	}
	err = replaceArchivePath(temporaryPath, packagePath)
	if err != nil {
		return fmt.Errorf("replacementInstall: %w", err)
	}
	return nil
}

func writeDecodedReplacement(
	ctx context.Context, w io.WriteSeeker, reader *Reader, targetOrdinal int, decodedPayload []byte,
) error {
	headerBytes := append([]byte(nil), reader.headerBytes[:]...)
	_, err := w.Write(headerBytes)
	if err != nil {
		return fmt.Errorf("headerWrite: %w", err)
	}
	entries := append([]Entry(nil), reader.Entries...)
	buffer := make([]byte, 128*1024)
	for ordinal, originalEntry := range reader.Entries {
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("payloadContext[%d]: %w", ordinal, err)
		}
		position, seekErr := w.Seek(0, io.SeekCurrent)
		if seekErr != nil {
			return fmt.Errorf("payloadPosition[%d]: %w", ordinal, seekErr)
		}
		if position > int64(^uint32(0)) {
			return fmt.Errorf("payloadOffset[%d]: %d exceeds DBPF limit", ordinal, position)
		}
		entry := originalEntry
		entry.Offset = uint32(position)
		if ordinal == targetOrdinal {
			_, err = w.Write(decodedPayload)
			if err != nil {
				return fmt.Errorf("payloadWrite[%d]: %w", ordinal, err)
			}
			entry.StoredSize = uint32(len(decodedPayload))
			entry.Size = uint32(len(decodedPayload))
			entry.Compression = 0
			entry.isStoredSizeFlag = false
			entries[ordinal] = entry
			continue
		}
		r, openErr := reader.OpenRaw(originalEntry)
		if openErr != nil {
			return fmt.Errorf("payloadOpen[%d]: %w", ordinal, openErr)
		}
		count, copyErr := io.CopyBuffer(w, r, buffer)
		if copyErr != nil {
			return fmt.Errorf("payloadCopy[%d]: %w", ordinal, copyErr)
		}
		if count != int64(originalEntry.StoredSize) {
			return fmt.Errorf("payloadSize[%d]: got %d, want %d", ordinal, count, originalEntry.StoredSize)
		}
		entry.StoredSize = uint32(count)
		entries[ordinal] = entry
	}
	indexPosition, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("indexPosition: %w", err)
	}
	manifest := &Manifest{
		HeaderBytes:      headerBytes,
		IndexFlags:       reader.indexFlags,
		SharedType:       reader.sharedType,
		SharedGroup:      reader.sharedGroup,
		SharedInstanceHi: reader.sharedInstHi,
	}
	indexContents, err := encodeIndex(manifest, entries)
	if err != nil {
		return fmt.Errorf("indexEncode: %w", err)
	}
	_, err = w.Write(indexContents)
	if err != nil {
		return fmt.Errorf("indexWrite: %w", err)
	}
	binary.LittleEndian.PutUint32(headerBytes[entryCountOffset:entryCountOffset+4], uint32(len(entries)))
	binary.LittleEndian.PutUint32(headerBytes[indexSizeOffset:indexSizeOffset+4], uint32(len(indexContents)))
	binary.LittleEndian.PutUint64(headerBytes[indexOffset:indexOffset+8], uint64(indexPosition))
	_, err = w.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("headerSeek: %w", err)
	}
	_, err = w.Write(headerBytes)
	if err != nil {
		return fmt.Errorf("headerRewrite: %w", err)
	}
	return nil
}

func verifyDecodedReplacement(packagePath string, resourceID ResourceID, decodedPayload []byte) error {
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("packageStat: %w", err)
	}
	reader, err := NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("packageRead: %w", err)
	}
	for _, entry := range reader.Entries {
		if entry.Type != resourceID.Type || entry.Group != resourceID.Group || entry.Instance != resourceID.Instance {
			continue
		}
		decoded, openErr := reader.Open(entry)
		if openErr != nil {
			return fmt.Errorf("payloadOpen: %w", openErr)
		}
		contents, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("payloadRead: %w", readErr)
		}
		if !bytes.Equal(contents, decodedPayload) {
			return errors.New("payload differs after rewrite")
		}
		return nil
	}
	return errors.New("resource missing after rewrite")
}

func replaceArchivePath(temporaryPath, packagePath string) error {
	backupPath := packagePath + ".darkspin-previous"
	err := os.Remove(backupPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backupRemove: %w", err)
	}
	err = os.Rename(packagePath, backupPath)
	if err != nil {
		return fmt.Errorf("backupRename: %w", err)
	}
	err = os.Rename(temporaryPath, packagePath)
	if err != nil {
		restoreErr := os.Rename(backupPath, packagePath)
		if restoreErr != nil {
			return fmt.Errorf("packageRestore: %w", errors.Join(err, restoreErr))
		}
		return fmt.Errorf("packageRename: %w", err)
	}
	err = os.Remove(backupPath)
	if err != nil {
		return fmt.Errorf("backupCleanup: %w", err)
	}
	return nil
}

// ExtractPath expands a package into its lossless archive representation.
func ExtractPath(ctx context.Context, sourcePath, destinationPath string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	r, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("sourceOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("sourceStat: %w", err)
	}
	reader, err := NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("packageRead: %w", err)
	}
	err = Extract(ctx, reader, destinationPath)
	if err != nil {
		return fmt.Errorf("packageExtract: %w", err)
	}
	return nil
}

// Extract streams stored payloads to disk and records enough metadata to repack them.
func Extract(ctx context.Context, reader *Reader, destinationPath string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if reader == nil {
		return errors.New("nil package reader")
	}
	if destinationPath == "" {
		return errors.New("empty destination")
	}
	_, err := os.Stat(destinationPath)
	if err == nil {
		return fmt.Errorf("destinationExists: %q", destinationPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("destinationStat: %w", err)
	}
	err = os.MkdirAll(destinationPath, 0o755)
	if err != nil {
		return fmt.Errorf("destinationMkdir: %w", err)
	}

	manifest := reader.ArchiveManifest()
	resourcePath := filepath.Join(destinationPath, "resource")
	err = os.MkdirAll(resourcePath, 0o755)
	if err != nil {
		return fmt.Errorf("resourceMkdir: %w", err)
	}
	buffer := make([]byte, 128*1024)
	for ordinal, entry := range reader.Entries {
		select {
		case <-ctx.Done():
			return fmt.Errorf("extractContext: %w", ctx.Err())
		default:
		}
		payloadName := ResourceName(ordinal, entry)
		payloadRelativePath := filepath.ToSlash(filepath.Join("resource", payloadName))
		payloadPath := filepath.Join(destinationPath, filepath.FromSlash(payloadRelativePath))
		payload, openErr := reader.OpenRaw(entry)
		if openErr != nil {
			return fmt.Errorf("payloadOpen[%d]: %w", ordinal, openErr)
		}
		w, createErr := os.OpenFile(payloadPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if createErr != nil {
			return fmt.Errorf("payloadCreate[%d]: %w", ordinal, createErr)
		}
		_, copyErr := io.CopyBuffer(w, payload, buffer)
		closeErr := w.Close()
		if copyErr != nil {
			return fmt.Errorf("payloadCopy[%d]: %w", ordinal, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("payloadClose[%d]: %w", ordinal, closeErr)
		}
	}
	manifestContents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("manifestMarshal: %w", err)
	}
	manifestContents = append(manifestContents, '\n')
	err = os.WriteFile(filepath.Join(destinationPath, ManifestName), manifestContents, 0o644)
	if err != nil {
		return fmt.Errorf("manifestWrite: %w", err)
	}
	return nil
}

// PackPath rebuilds a package from an extracted directory without recompressing resources.
func PackPath(ctx context.Context, sourcePath, destinationPath string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if sourcePath == "" || destinationPath == "" {
		return errors.New("empty archive path")
	}
	_, err := os.Stat(destinationPath)
	if err == nil {
		return fmt.Errorf("destinationExists: %q", destinationPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("destinationStat: %w", err)
	}
	manifestContents, err := os.ReadFile(filepath.Join(sourcePath, ManifestName))
	if err != nil {
		return fmt.Errorf("manifestRead: %w", err)
	}
	manifest := &Manifest{}
	err = json.Unmarshal(manifestContents, manifest)
	if err != nil {
		return fmt.Errorf("manifestDecode: %w", err)
	}
	err = PackManifestPath(ctx, sourcePath, destinationPath, manifest)
	if err != nil {
		return fmt.Errorf("manifestPack: %w", err)
	}
	return nil
}

// PackManifestPath rebuilds a package from stored payloads and an in-memory
// lossless archive manifest.
func PackManifestPath(ctx context.Context, sourcePath, destinationPath string, manifest *Manifest) error {
	return PackManifestReaders(ctx, destinationPath, manifest, func(ordinal int, resource Resource) (io.ReadCloser, Resource, error) {
		payloadPath, err := archivePath(sourcePath, resource.PayloadPath)
		if err != nil {
			return nil, Resource{}, fmt.Errorf("payloadPath[%d]: %w", ordinal, err)
		}
		r, err := os.Open(payloadPath)
		if err != nil {
			return nil, Resource{}, fmt.Errorf("payloadOpen[%d]: %w", ordinal, err)
		}
		return r, resource, nil
	})
}

// ResourceOpener provides one stored payload and its rebuilt index metadata.
type ResourceOpener func(ordinal int, resource Resource) (io.ReadCloser, Resource, error)

// PackManifestReaders rebuilds a package directly from resource readers.
func PackManifestReaders(ctx context.Context, destinationPath string, manifest *Manifest, openResource ResourceOpener) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if destinationPath == "" {
		return errors.New("empty archive path")
	}
	if manifest == nil {
		return errors.New("nil archive manifest")
	}
	if openResource == nil {
		return errors.New("nil resource opener")
	}
	_, err := os.Stat(destinationPath)
	if err == nil {
		return fmt.Errorf("destinationExists: %q", destinationPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("destinationStat: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destinationPath), ".dbpf-*.package")
	if err != nil {
		return fmt.Errorf("temporaryCreate: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	err = WriteReaders(ctx, temporary, manifest, openResource)
	if err != nil {
		_ = temporary.Close()
		return fmt.Errorf("packageWrite: %w", err)
	}
	err = temporary.Close()
	if err != nil {
		return fmt.Errorf("packageClose: %w", err)
	}
	r, err := os.Open(temporaryPath)
	if err != nil {
		return fmt.Errorf("verifyOpen: %w", err)
	}
	fi, err := r.Stat()
	if err != nil {
		_ = r.Close()
		return fmt.Errorf("verifyStat: %w", err)
	}
	reader, err := NewReader(r, fi.Size())
	closeErr := r.Close()
	if err != nil {
		return fmt.Errorf("verifyRead: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("verifyClose: %w", closeErr)
	}
	if len(reader.Entries) != len(manifest.Resources) {
		return fmt.Errorf("verifyCount: got %d, want %d", len(reader.Entries), len(manifest.Resources))
	}
	err = os.Rename(temporaryPath, destinationPath)
	if err != nil {
		return fmt.Errorf("packageInstall: %w", err)
	}
	return nil
}

// Write streams archive resources and rebuilds the DBPF index.
func Write(ctx context.Context, w io.WriteSeeker, sourcePath string, manifest *Manifest) error {
	return WriteReaders(ctx, w, manifest, func(ordinal int, resource Resource) (io.ReadCloser, Resource, error) {
		payloadPath, err := archivePath(sourcePath, resource.PayloadPath)
		if err != nil {
			return nil, Resource{}, fmt.Errorf("payloadPath[%d]: %w", ordinal, err)
		}
		r, err := os.Open(payloadPath)
		if err != nil {
			return nil, Resource{}, fmt.Errorf("payloadOpen[%d]: %w", ordinal, err)
		}
		return r, resource, nil
	})
}

// WriteReaders streams supplied archive resources and rebuilds the DBPF index.
func WriteReaders(ctx context.Context, w io.WriteSeeker, manifest *Manifest, openResource ResourceOpener) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if w == nil || manifest == nil {
		return errors.New("nil package output")
	}
	if openResource == nil {
		return errors.New("nil resource opener")
	}
	if len(manifest.HeaderBytes) != HeaderSize {
		return fmt.Errorf("manifestHeader: got %d bytes", len(manifest.HeaderBytes))
	}
	if string(manifest.HeaderBytes[:len(magic)]) != string(magic[:]) {
		return errors.New("manifestMagic: not DBPF")
	}
	if uint64(len(manifest.Resources)) > uint64(^uint32(0)) {
		return fmt.Errorf("manifestCount: %d exceeds DBPF limit", len(manifest.Resources))
	}
	headerBytes := append([]byte(nil), manifest.HeaderBytes...)
	_, err := w.Write(headerBytes)
	if err != nil {
		return fmt.Errorf("headerWrite: %w", err)
	}
	buffer := make([]byte, 128*1024)
	entries := make([]Entry, len(manifest.Resources))
	for ordinal, resource := range manifest.Resources {
		select {
		case <-ctx.Done():
			return fmt.Errorf("packContext: %w", ctx.Err())
		default:
		}
		position, seekErr := w.Seek(0, io.SeekCurrent)
		if seekErr != nil {
			return fmt.Errorf("payloadPosition[%d]: %w", ordinal, seekErr)
		}
		if position > int64(^uint32(0)) {
			return fmt.Errorf("payloadOffset[%d]: %d exceeds DBPF limit", ordinal, position)
		}
		r, rebuiltResource, openErr := openResource(ordinal, resource)
		if openErr != nil {
			return fmt.Errorf("payloadSource[%d]: %w", ordinal, openErr)
		}
		count, copyErr := io.CopyBuffer(w, r, buffer)
		closeErr := r.Close()
		if copyErr != nil {
			return fmt.Errorf("payloadCopy[%d]: %w", ordinal, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("payloadClose[%d]: %w", ordinal, closeErr)
		}
		if count > int64(0x7fffffff) {
			return fmt.Errorf("payloadSize[%d]: %d exceeds DBPF limit", ordinal, count)
		}
		entry := rebuiltResource.Entry
		entry.Offset = uint32(position)
		entry.StoredSize = uint32(count)
		entry.isStoredSizeFlag = rebuiltResource.IsStoredSizeFlag
		entries[ordinal] = entry
	}
	indexPosition, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("indexPosition: %w", err)
	}
	indexContents, err := encodeIndex(manifest, entries)
	if err != nil {
		return fmt.Errorf("indexEncode: %w", err)
	}
	_, err = w.Write(indexContents)
	if err != nil {
		return fmt.Errorf("indexWrite: %w", err)
	}
	binary.LittleEndian.PutUint32(headerBytes[entryCountOffset:entryCountOffset+4], uint32(len(entries)))
	binary.LittleEndian.PutUint32(headerBytes[indexSizeOffset:indexSizeOffset+4], uint32(len(indexContents)))
	binary.LittleEndian.PutUint64(headerBytes[indexOffset:indexOffset+8], uint64(indexPosition))
	_, err = w.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("headerSeek: %w", err)
	}
	_, err = w.Write(headerBytes)
	if err != nil {
		return fmt.Errorf("headerRewrite: %w", err)
	}
	return nil
}

func encodeIndex(manifest *Manifest, entries []Entry) ([]byte, error) {
	index := &bytes.Buffer{}
	fields := []any{manifest.IndexFlags}
	if manifest.IndexFlags&indexHasType != 0 {
		fields = append(fields, manifest.SharedType)
	}
	if manifest.IndexFlags&indexHasGroup != 0 {
		fields = append(fields, manifest.SharedGroup)
	}
	if manifest.IndexFlags&indexHasInstanceHi != 0 {
		fields = append(fields, manifest.SharedInstanceHi)
	}
	for _, field := range fields {
		err := binary.Write(index, binary.LittleEndian, field)
		if err != nil {
			return nil, fmt.Errorf("prefixWrite: %w", err)
		}
	}
	for ordinal, entry := range entries {
		entryFields := make([]any, 0, 9)
		if manifest.IndexFlags&indexHasType == 0 {
			entryFields = append(entryFields, entry.Type)
		}
		if manifest.IndexFlags&indexHasGroup == 0 {
			entryFields = append(entryFields, entry.Group)
		}
		if manifest.IndexFlags&indexHasInstanceHi == 0 {
			entryFields = append(entryFields, uint32(entry.Instance>>32))
		}
		storedSize := entry.StoredSize
		if entry.isStoredSizeFlag {
			storedSize |= 0x80000000
		}
		entryFields = append(entryFields, uint32(entry.Instance), entry.Offset, storedSize, entry.Size, entry.Compression, entry.Flags)
		for _, field := range entryFields {
			err := binary.Write(index, binary.LittleEndian, field)
			if err != nil {
				return nil, fmt.Errorf("entryWrite[%d]: %w", ordinal, err)
			}
		}
	}
	return index.Bytes(), nil
}

func archivePath(rootPath, relativePath string) (string, error) {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return "", errors.New("invalid relative path")
	}
	rootPath, err := filepath.Abs(rootPath)
	if err != nil {
		return "", fmt.Errorf("rootResolve: %w", err)
	}
	candidatePath, err := filepath.Abs(filepath.Join(rootPath, filepath.FromSlash(relativePath)))
	if err != nil {
		return "", fmt.Errorf("pathResolve: %w", err)
	}
	relativeCandidate, err := filepath.Rel(rootPath, candidatePath)
	if err != nil {
		return "", fmt.Errorf("pathRelative: %w", err)
	}
	if relativeCandidate == ".." || filepath.IsAbs(relativeCandidate) || strings.HasPrefix(relativeCandidate, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes archive")
	}
	return candidatePath, nil
}
