package cmd

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type databaseBinaryGetResult struct {
	Database   string `json:"database"`
	Table      string `json:"table"`
	Column     string `json:"column"`
	OutputPath string `json:"output_path"`
	Decode     string `json:"decode,omitempty"`
	StoredSize int    `json:"stored_size"`
	OutputSize int    `json:"output_size"`
	SHA256     string `json:"sha256"`
}

func databaseBinaryGet(
	ctx context.Context,
	output io.Writer,
	target *databaseTarget,
	table string,
	columns map[string]struct{},
	parameters []string,
	options databaseCommandOptions,
) error {
	if len(parameters) < 3 {
		return errors.New("usage: db <table> bget <column> where <field><operator><value> --output <path>")
	}
	column := strings.ToLower(parameters[0])
	if _, isFound := columns[column]; !isFound {
		return fmt.Errorf("columnMissing: %q", column)
	}
	if !strings.EqualFold(parameters[1], "where") {
		return errors.New("bget requires an explicit where clause")
	}
	if options.binaryOutputPath == "" {
		return errors.New("bget requires --output")
	}
	predicates, err := parseDatabasePredicates(parameters[2:], columns, false)
	if err != nil {
		return fmt.Errorf("predicateParse: %w", err)
	}
	query, queryArguments := buildDatabaseWhere(
		`SELECT `+quoteDatabaseIdentifier(column)+` FROM `+quoteDatabaseIdentifier(table), predicates,
	)
	query += ` LIMIT 2`
	rows, err := target.database.QueryContext(ctx, query, queryArguments...)
	if err != nil {
		return fmt.Errorf("queryExecute: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		err = rows.Err()
		if err != nil {
			return fmt.Errorf("rowRead: %w", err)
		}
		return errors.New("rowMissing: predicate matched no rows")
	}
	var blob any
	err = rows.Scan(&blob)
	if err != nil {
		return fmt.Errorf("blobScan: %w", err)
	}
	if rows.Next() {
		return errors.New("rowAmbiguous: predicate matched more than one row")
	}
	err = rows.Err()
	if err != nil {
		return fmt.Errorf("rowRead: %w", err)
	}
	storedPayload, isBlob := blob.([]byte)
	if !isBlob {
		return fmt.Errorf("columnType: %q is %T, want BLOB", column, blob)
	}
	payload, err := decodeDatabaseBlob(storedPayload, options.binaryDecode)
	if err != nil {
		return fmt.Errorf("blobDecode: %w", err)
	}
	outputPath, err := filepath.Abs(options.binaryOutputPath)
	if err != nil {
		return fmt.Errorf("outputPath: %w", err)
	}
	err = writeDatabaseBlob(outputPath, payload, options.isBinaryForce)
	if err != nil {
		return fmt.Errorf("blobWrite: %w", err)
	}
	digest := sha256.Sum256(payload)
	result := databaseBinaryGetResult{
		Database: target.name, Table: table, Column: column, OutputPath: outputPath,
		Decode:     strings.ToLower(strings.TrimSpace(options.binaryDecode)),
		StoredSize: len(storedPayload), OutputSize: len(payload), SHA256: hex.EncodeToString(digest[:]),
	}
	err = writeDatabaseJSON(output, result)
	if err != nil {
		return fmt.Errorf("resultWrite: %w", err)
	}
	return nil
}

func decodeDatabaseBlob(storedPayload []byte, codec string) ([]byte, error) {
	codec = strings.ToLower(strings.TrimSpace(codec))
	if codec == "" {
		return append([]byte(nil), storedPayload...), nil
	}
	if codec != "zlib" {
		return nil, fmt.Errorf("codecUnsupported: %q", codec)
	}
	r, err := zlib.NewReader(bytes.NewReader(storedPayload))
	if err != nil {
		return nil, fmt.Errorf("zlibOpen: %w", err)
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("zlibRead: %w", err)
	}
	err = r.Close()
	if err != nil {
		return nil, fmt.Errorf("zlibClose: %w", err)
	}
	return payload, nil
}

func writeDatabaseBlob(outputPath string, payload []byte, isForce bool) error {
	err := os.MkdirAll(filepath.Dir(outputPath), 0o755)
	if err != nil {
		return fmt.Errorf("outputMkdir: %w", err)
	}
	flag := os.O_WRONLY | os.O_CREATE
	if isForce {
		flag |= os.O_TRUNC
	} else {
		flag |= os.O_EXCL
	}
	w, err := os.OpenFile(outputPath, flag, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("outputExists: %s; pass --force to replace it", outputPath)
		}
		return fmt.Errorf("outputOpen: %w", err)
	}
	_, writeErr := w.Write(payload)
	closeErr := w.Close()
	if writeErr != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("outputWrite: %w", writeErr)
	}
	if closeErr != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("outputClose: %w", closeErr)
	}
	return nil
}
