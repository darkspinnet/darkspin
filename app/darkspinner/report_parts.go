package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Use decimal MB and reserve room for ZIP headers and the central directory.
const maximumReportPartSize = 25_000_000
const reportChunkSize = 24_000_000

const reportPartInstructions = `Attach every numbered ZIP for this report.
Each ZIP opens independently. Extract all parts into the same directory.
Ordinary entries keep their original paths. Oversized entries are stored under
split-files/<original path>/part-000001, part-000002, ... . Concatenate those
chunks in numerical order as binary data to restore the original file exactly.
Chunks may split a line or UTF-8 character; no bytes have been discarded.
report.json describes the complete report, across all ZIPs.
`

type reportPartWriter struct {
	stem      string
	paths     []string
	buffer    bytes.Buffer
	archive   *zip.Writer
	estimated uint64
}

func (e *reportPartWriter) start() error {
	e.buffer.Reset()
	e.archive = zip.NewWriter(&e.buffer)
	w, err := e.archive.Create("REPORT-PARTS.txt")
	if err != nil {
		return fmt.Errorf("instructionsCreate: %w", err)
	}
	_, err = io.WriteString(w, reportPartInstructions)
	if err != nil {
		return fmt.Errorf("instructionsWrite: %w", err)
	}
	e.estimated = 65536
	return nil
}

func (e *reportPartWriter) finish() error {
	err := e.archive.Close()
	if err != nil {
		return fmt.Errorf("partClose: %w", err)
	}
	if e.buffer.Len() > maximumReportPartSize {
		return errors.New("report part exceeds 25 MB")
	}
	path := fmt.Sprintf("%s-part-%03d.zip", e.stem, len(e.paths)+1)
	w, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("partCreate: %w", err)
	}
	// Track the path immediately so a failed write is cleaned up too.
	e.paths = append(e.paths, path)
	_, writeErr := e.buffer.WriteTo(w)
	closeErr := w.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("partWrite: %w", errors.Join(writeErr, closeErr))
	}
	return nil
}

func (e *reportPartWriter) add(entry *zip.File) error {
	// Copy preserves compressed data. Account conservatively for both headers,
	// ZIP64 extras, descriptor, and the end-of-directory record before writing.
	size := entry.CompressedSize64 + uint64(2*len(entry.Name)+2*len(entry.Extra)+len(entry.Comment)+256)
	if size > maximumReportPartSize-65536 {
		return errors.New("report entry exceeds part capacity")
	}
	if e.estimated+size > maximumReportPartSize {
		err := e.finish()
		if err != nil {
			return fmt.Errorf("partFinish: %w", err)
		}
		err = e.start()
		if err != nil {
			return fmt.Errorf("partStart: %w", err)
		}
	}
	err := e.archive.Copy(entry)
	if err != nil {
		return fmt.Errorf("entryCopy: %w", err)
	}
	e.estimated += size
	return nil
}

func (e *reportPartWriter) split(entry *zip.File) (err error) {
	r, err := entry.Open()
	if err != nil {
		return fmt.Errorf("entryOpen: %w", err)
	}
	defer func() {
		closeErr := r.Close()
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("entryClose: %w", closeErr))
		}
	}()
	payload := make([]byte, reportChunkSize)
	for index := 1; ; index++ {
		count, readErr := io.ReadFull(r, payload)
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			return fmt.Errorf("chunkRead: %w", readErr)
		}
		if count == 0 {
			return nil
		}
		var buffer bytes.Buffer
		archive := zip.NewWriter(&buffer)
		header := &zip.FileHeader{
			Name:   fmt.Sprintf("split-files/%s/part-%06d", entry.Name, index),
			Method: zip.Deflate,
		}
		header.SetModTime(entry.Modified)
		w, createErr := archive.CreateHeader(header)
		if createErr != nil {
			return fmt.Errorf("chunkCreate: %w", createErr)
		}
		_, writeErr := w.Write(payload[:count])
		closeErr := archive.Close()
		if writeErr != nil || closeErr != nil {
			return fmt.Errorf("chunkWrite: %w", errors.Join(writeErr, closeErr))
		}
		chunk, openErr := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
		if openErr != nil {
			return fmt.Errorf("chunkOpen: %w", openErr)
		}
		addErr := e.add(chunk.File[0])
		if addErr != nil {
			return fmt.Errorf("chunkAdd: %w", addErr)
		}
		if readErr != nil {
			return nil
		}
	}
}

func segmentReportArchive(path string) (paths []string, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("archiveStat: %w", err)
	}
	if fi.Size() <= maximumReportPartSize {
		return []string{path}, nil
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("archiveOpen: %w", err)
	}
	parts := reportPartWriter{stem: strings.TrimSuffix(path, filepath.Ext(path))}
	defer func() {
		closeErr := r.Close()
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("archiveClose: %w", closeErr))
		}
		if err == nil {
			removeErr := os.Remove(path)
			if removeErr != nil {
				err = fmt.Errorf("archiveRemove: %w", removeErr)
			}
		}
		if err != nil {
			for _, partPath := range parts.paths {
				removeErr := os.Remove(partPath)
				if removeErr != nil {
					err = errors.Join(err, fmt.Errorf("partRemove: %w", removeErr))
				}
			}
		}
	}()
	err = parts.start()
	if err != nil {
		return nil, fmt.Errorf("partsStart: %w", err)
	}
	for _, entry := range r.File {
		if entry.CompressedSize64 > reportChunkSize {
			err = parts.split(entry)
		} else {
			err = parts.add(entry)
		}
		if err != nil {
			return nil, fmt.Errorf("partsAdd: %w", err)
		}
	}
	err = parts.finish()
	if err != nil {
		return nil, fmt.Errorf("partsFinish: %w", err)
	}
	return parts.paths, nil
}
