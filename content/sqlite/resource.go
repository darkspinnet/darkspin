package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const resourceInsertBatchSize = 100

type contentResourceRow struct {
	ID                     int64
	ContentSourcePackageID int64
	Ordinal                int
	Entry                  dbpf.Entry
	RawSHA256              string
	DecodedSHA256          string
	RawPayload             []byte
}

type serverDataResourceRow struct {
	ContentSourceResourceID int64
	ResourceGroup           string
	ResourceName            string
	Format                  string
	DecodedSize             int
	DecodedPayload          []byte
	IsCompiledLua           bool
}

func loadContentPackageIDs(ctx context.Context, transaction *sql.Tx) (map[string]int64, error) {
	rows, err := transaction.QueryContext(ctx, "SELECT id, package_name FROM content_source_package ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("packageQuery: %w", err)
	}
	packageID := make(map[string]int64, len(buildPackages))
	for rows.Next() {
		var id int64
		var packageName string
		err = rows.Scan(&id, &packageName)
		if err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("packageScan: %w", err)
		}
		packageID[packageName] = id
	}
	err = rows.Err()
	if err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("packageRows: %w", err)
	}
	err = rows.Close()
	if err != nil {
		return nil, fmt.Errorf("packageClose: %w", err)
	}
	return packageID, nil
}

func insertResourceBatch(ctx context.Context, transaction *sql.Tx, resourceRows []contentResourceRow, serverDataRows []serverDataResourceRow) error {
	query := strings.Builder{}
	query.WriteString(`INSERT INTO content_source_resource
		(id, content_source_package_id, ordinal, type_id, group_id, instance_id, stored_size,
		 decoded_size, compression, entry_flag, raw_sha256, decoded_sha256, raw_payload) VALUES `)
	arguments := make([]any, 0, len(resourceRows)*13)
	for index, row := range resourceRows {
		if index > 0 {
			query.WriteString(", ")
		}
		query.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		arguments = append(arguments,
			row.ID,
			row.ContentSourcePackageID,
			row.Ordinal,
			int64(row.Entry.Type),
			int64(row.Entry.Group),
			int64(row.Entry.Instance),
			row.Entry.StoredSize,
			row.Entry.Size,
			row.Entry.Compression,
			row.Entry.Flags,
			row.RawSHA256,
			row.DecodedSHA256,
			row.RawPayload,
		)
	}
	_, err := transaction.ExecContext(ctx, query.String(), arguments...)
	if err != nil {
		return fmt.Errorf("contentInsert: %w", err)
	}
	if len(serverDataRows) == 0 {
		return nil
	}
	query.Reset()
	query.WriteString(`INSERT INTO server_data
		(content_source_resource_id, resource_group, resource_name, format, decoded_size,
		 decoded_compression, decoded_payload, is_compiled_lua) VALUES `)
	arguments = make([]any, 0, len(serverDataRows)*8)
	for index, row := range serverDataRows {
		if index > 0 {
			query.WriteString(", ")
		}
		query.WriteString("(?, ?, ?, ?, ?, ?, ?, ?)")
		arguments = append(arguments,
			row.ContentSourceResourceID,
			row.ResourceGroup,
			row.ResourceName,
			row.Format,
			row.DecodedSize,
			"zlib",
			row.DecodedPayload,
			row.IsCompiledLua,
		)
	}
	_, err = transaction.ExecContext(ctx, query.String(), arguments...)
	if err != nil {
		return fmt.Errorf("serverDataInsert: %w", err)
	}
	return nil
}
