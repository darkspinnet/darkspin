package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
)

func unwrapUpdateManifest(contents []byte) ([]byte, error) {
	if !bytes.HasPrefix(contents, []byte("PK\x03\x04")) {
		return contents, nil
	}
	r, err := openUpdateEntry(
		bytes.NewReader(contents), int64(len(contents)),
		"darkspinner-update.json", maximumUpdateManifestSize,
	)
	if err != nil {
		return nil, fmt.Errorf("manifestEntry: %w", err)
	}
	manifest, readErr := io.ReadAll(io.LimitReader(r, maximumUpdateManifestSize+1))
	closeErr := r.Close()
	if readErr != nil {
		return nil, fmt.Errorf("manifestRead: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("manifestClose: %w", closeErr)
	}
	if len(manifest) > maximumUpdateManifestSize {
		return nil, errors.New("expanded manifest exceeds size limit")
	}
	return manifest, nil
}

func copyLauncherUpdate(w io.Writer, body io.Reader, format string) (int64, error) {
	if format == "" {
		written, err := io.Copy(w, io.LimitReader(body, maximumLauncherUpdateSize+1))
		if err != nil {
			return written, fmt.Errorf("binaryCopy: %w", err)
		}
		return written, nil
	}
	if format != "zip" {
		return 0, fmt.Errorf("unsupported update format %q", format)
	}
	archiveHandle, err := os.CreateTemp("", "darkspinner-update-*.zip")
	if err != nil {
		return 0, fmt.Errorf("archiveCreate: %w", err)
	}
	defer closeUpdateArchive(archiveHandle)
	archiveSize, err := io.Copy(archiveHandle, io.LimitReader(body, maximumLauncherUpdateSize+1))
	if err != nil {
		return 0, fmt.Errorf("archiveCopy: %w", err)
	}
	if archiveSize > maximumLauncherUpdateSize {
		return 0, errors.New("update archive exceeds size limit")
	}
	r, err := openUpdateEntry(archiveHandle, archiveSize, "darkspinner.exe", maximumLauncherUpdateSize)
	if err != nil {
		return 0, fmt.Errorf("binaryEntry: %w", err)
	}
	written, copyErr := io.Copy(w, io.LimitReader(r, maximumLauncherUpdateSize+1))
	closeErr := r.Close()
	if copyErr != nil {
		return written, fmt.Errorf("entryCopy: %w", copyErr)
	}
	if closeErr != nil {
		return written, fmt.Errorf("entryClose: %w", closeErr)
	}
	return written, nil
}

func openUpdateEntry(r io.ReaderAt, size int64, name string, maximumSize uint64) (io.ReadCloser, error) {
	archive, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("archiveRead: %w", err)
	}
	if len(archive.File) != 1 {
		return nil, errors.New("update archive must contain exactly one file")
	}
	entry := archive.File[0]
	if entry.Name != name || !entry.Mode().IsRegular() {
		return nil, fmt.Errorf("update archive must contain only %s", name)
	}
	if entry.UncompressedSize64 > maximumSize {
		return nil, errors.New("update entry exceeds size limit")
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("entryOpen: %w", err)
	}
	return reader, nil
}

func closeUpdateArchive(archiveHandle *os.File) {
	err := archiveHandle.Close()
	if err != nil {
		log.Printf("Launcher update archive close: %v", err)
	}
	err = os.Remove(archiveHandle.Name())
	if err != nil {
		log.Printf("Launcher update archive cleanup: %v", err)
	}
}
