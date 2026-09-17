package sqlite

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const (
	MetaRecipeVersion = 1
	MetaRelease       = "build-103-meta-v1"
	MetadataRole      = "research-metadata"
	MetaFilename      = "meta.db"
)

var metaPackages = []packageSpec{
	{name: "CompiledAnimData.package", relativePath: filepath.Join("Data", "CompiledAnimData.package")},
	{name: "Levels.package", relativePath: filepath.Join("Data", "Levels.package")},
}

// MetaVerification summarizes a validated research database.
type MetaVerification struct {
	MetaRelease             string
	SourceBuild             int
	PackageCount            int
	CompiledAnimationCount  int
	CompiledLuaCount        int
	IsCompiledArchiveStored bool
}

// BuildMeta creates the optional research database from a vanilla installation.
// It includes the runtime projection, package classification, and compiled
// animation payloads without copying the 217 MB Levels.package payload.
func BuildMeta(ctx context.Context, options BuildOptions) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if options.DatabasePath == "" {
		return errors.New("empty database path")
	}
	_, err := os.Stat(options.DatabasePath)
	if err == nil {
		return fmt.Errorf("databaseExists: %q", options.DatabasePath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("databaseStat: %w", err)
	}
	installPath, err := resolveInstallPath(options.GamePath)
	if err != nil {
		return fmt.Errorf("installResolve: %w", err)
	}
	err = validateSourceVersion(installPath)
	if err != nil {
		return fmt.Errorf("sourceVersion: %w", err)
	}
	databasePath, err := filepath.Abs(options.DatabasePath)
	if err != nil {
		return fmt.Errorf("databasePath: %w", err)
	}
	err = os.MkdirAll(filepath.Dir(databasePath), 0o755)
	if err != nil {
		return fmt.Errorf("databaseMkdir: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(databasePath), ".meta-*.db")
	if err != nil {
		return fmt.Errorf("temporaryCreate: %w", err)
	}
	temporaryPath := temporary.Name()
	err = temporary.Close()
	if err != nil {
		return fmt.Errorf("temporaryClose: %w", err)
	}
	err = os.Remove(temporaryPath)
	if err != nil {
		return fmt.Errorf("temporaryRemove: %w", err)
	}
	defer removeDatabaseFiles(temporaryPath)

	err = Build(ctx, BuildOptions{GamePath: installPath, DatabasePath: temporaryPath})
	if err != nil {
		return fmt.Errorf("contentBuild: %w", err)
	}
	err = enrichMeta(ctx, temporaryPath, installPath)
	if err != nil {
		return fmt.Errorf("metaEnrich: %w", err)
	}
	_, err = VerifyMeta(ctx, temporaryPath)
	if err != nil {
		return fmt.Errorf("metaVerify: %w", err)
	}
	err = os.Rename(temporaryPath, databasePath)
	if err != nil {
		return fmt.Errorf("databaseInstall: %w", err)
	}
	return nil
}

func enrichMeta(ctx context.Context, databasePath, installPath string) error {
	packages, err := inspectPackageSet(ctx, installPath, metaPackages)
	if err != nil {
		return fmt.Errorf("packageInspect: %w", err)
	}
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return fmt.Errorf("databaseOpen: %w", err)
	}
	defer database.Close()
	_, err = database.ExecContext(ctx, `
		PRAGMA foreign_keys=ON;
		CREATE TABLE package_inventory (
			id INTEGER PRIMARY KEY,
			package_name TEXT NOT NULL UNIQUE,
			source_file TEXT NOT NULL UNIQUE,
			sha256 TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			resource_count INTEGER NOT NULL,
			compressed_resource_count INTEGER NOT NULL,
			stored_resource_count INTEGER NOT NULL,
			is_payload_stored INTEGER NOT NULL CHECK (is_payload_stored IN (0, 1))
		);
		CREATE TABLE package_resource_type (
			package_inventory_id INTEGER NOT NULL REFERENCES package_inventory(id) ON DELETE CASCADE,
			resource_type INTEGER NOT NULL,
			resource_count INTEGER NOT NULL,
			PRIMARY KEY (package_inventory_id, resource_type)
		);
		CREATE TABLE package_archive (
			package_inventory_id INTEGER PRIMARY KEY REFERENCES package_inventory(id) ON DELETE CASCADE,
			sha256 TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			payload BLOB NOT NULL
		);
		CREATE TABLE compiled_animation_resource (
			id INTEGER PRIMARY KEY,
			package_inventory_id INTEGER NOT NULL REFERENCES package_inventory(id) ON DELETE CASCADE,
			ordinal INTEGER NOT NULL,
			resource_type INTEGER NOT NULL,
			resource_group TEXT NOT NULL,
			resource_name TEXT NOT NULL,
			resource_format TEXT NOT NULL,
			resource_id INTEGER,
			magic TEXT NOT NULL,
			decoded_size INTEGER NOT NULL,
			decoded_compression TEXT NOT NULL,
			decoded_sha256 TEXT NOT NULL,
			decoded_payload BLOB NOT NULL,
			UNIQUE (package_inventory_id, ordinal)
		);
	`)
	if err != nil {
		return fmt.Errorf("schemaCreate: %w", err)
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("transactionBegin: %w", err)
	}
	isCommitted := false
	defer func() {
		if !isCommitted {
			_ = transaction.Rollback()
		}
	}()
	allPackages, err := inspectPackages(ctx, installPath)
	if err != nil {
		return fmt.Errorf("contentInspect: %w", err)
	}
	allPackages = append(allPackages, packages...)
	sort.Slice(allPackages, func(left, right int) bool {
		return allPackages[left].spec.name < allPackages[right].spec.name
	})
	_, err = transaction.ExecContext(ctx,
		"UPDATE database_manifest SET database_role=?, content_release=?, recipe_version=?, input_fingerprint=? WHERE id=1",
		MetadataRole, MetaRelease, MetaRecipeVersion, sourceFingerprint(allPackages),
	)
	if err != nil {
		return fmt.Errorf("manifestUpdate: %w", err)
	}
	for index, packageInst := range packages {
		packageID := index + 1
		err = insertMetaPackage(ctx, transaction, installPath, packageID, packageInst)
		if err != nil {
			return fmt.Errorf("packageInsert[%s]: %w", packageInst.spec.name, err)
		}
	}
	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("transactionCommit: %w", err)
	}
	isCommitted = true
	return nil
}

func insertMetaPackage(ctx context.Context, transaction *sql.Tx, installPath string, packageID int, packageInst inspectedPackage) error {
	compressedCount := 0
	typeCount := make(map[uint32]int)
	for _, entry := range packageInst.pkg.Entries {
		if entry.Compression != 0 {
			compressedCount++
		}
		typeCount[entry.Type]++
	}
	isPayloadStored := packageInst.spec.name == "CompiledAnimData.package"
	_, err := transaction.ExecContext(ctx, `
		INSERT INTO package_inventory
		(id, package_name, source_file, sha256, file_size, resource_count,
		 compressed_resource_count, stored_resource_count, is_payload_stored)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		packageID, packageInst.spec.name, filepath.ToSlash(packageInst.spec.relativePath), packageInst.sha256,
		packageInst.fileSize, len(packageInst.pkg.Entries), compressedCount,
		len(packageInst.pkg.Entries)-compressedCount, isPayloadStored,
	)
	if err != nil {
		return fmt.Errorf("inventoryInsert: %w", err)
	}
	typeIDs := make([]uint32, 0, len(typeCount))
	for typeID := range typeCount {
		typeIDs = append(typeIDs, typeID)
	}
	sort.Slice(typeIDs, func(left, right int) bool { return typeIDs[left] < typeIDs[right] })
	for _, typeID := range typeIDs {
		_, err = transaction.ExecContext(ctx,
			"INSERT INTO package_resource_type (package_inventory_id, resource_type, resource_count) VALUES (?, ?, ?)",
			packageID, int64(typeID), typeCount[typeID],
		)
		if err != nil {
			return fmt.Errorf("typeInsert[%08x]: %w", typeID, err)
		}
	}
	if !isPayloadStored {
		return nil
	}
	packagePath := filepath.Join(installPath, packageInst.spec.relativePath)
	packagePayload, err := os.ReadFile(packagePath)
	if err != nil {
		return fmt.Errorf("archiveRead: %w", err)
	}
	_, err = transaction.ExecContext(ctx,
		"INSERT INTO package_archive (package_inventory_id, sha256, file_size, payload) VALUES (?, ?, ?, ?)",
		packageID, packageInst.sha256, len(packagePayload), packagePayload,
	)
	if err != nil {
		return fmt.Errorf("archiveInsert: %w", err)
	}
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("resourceOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("resourceStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("resourcePackage: %w", err)
	}
	for ordinal, entry := range pkg.Entries {
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("decodedOpen[%d]: %w", ordinal, openErr)
		}
		decodedPayload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("decodedRead[%d]: %w", ordinal, readErr)
		}
		compressedPayload, compressErr := compressContent(decodedPayload)
		if compressErr != nil {
			return fmt.Errorf("decodedCompress[%d]: %w", ordinal, compressErr)
		}
		digest := sha256.Sum256(decodedPayload)
		resourceGroup, resourceName, resourceFormat, resourceID, magic := animationIdentity(entry, decodedPayload)
		_, err = transaction.ExecContext(ctx, `
			INSERT INTO compiled_animation_resource
			(package_inventory_id, ordinal, resource_type, resource_group, resource_name,
			 resource_format, resource_id, magic, decoded_size, decoded_compression,
			 decoded_sha256, decoded_payload)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			packageID, ordinal, int64(entry.Type), resourceGroup, resourceName, resourceFormat,
			resourceID, magic, len(decodedPayload), "zlib", hex.EncodeToString(digest[:]), compressedPayload,
		)
		if err != nil {
			return fmt.Errorf("animationInsert[%d]: %w", ordinal, err)
		}
	}
	return nil
}

func animationIdentity(entry dbpf.Entry, decodedPayload []byte) (string, string, string, int64, string) {
	resourceGroup := fmt.Sprintf("0x%08X", entry.Group)
	resourceFormat := fmt.Sprintf("0x%08x", entry.Type)
	switch entry.Type {
	case 0xee17c6ad:
		resourceGroup = "animations~"
		resourceFormat = "animation"
	case 0x25df0112:
		resourceGroup = "animations~"
		resourceFormat = "gait"
	case 0x4aeb6bc6:
		resourceFormat = "tlsa"
	case 0x7c19aa7a:
		resourceFormat = "pctp"
	}
	magicSize := min(4, len(decodedPayload))
	magic := string(decodedPayload[:magicSize])
	return resourceGroup, fmt.Sprintf("0x%08X", entry.Instance), resourceFormat, int64(entry.Instance), magic
}

// VerifyMeta validates the optional research projection and its stored payloads.
func VerifyMeta(ctx context.Context, path string) (*MetaVerification, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("pathResolve: %w", err)
	}
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(absolutePath)+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("databaseOpen: %w", err)
	}
	defer database.Close()
	var role string
	var sourceBuild int
	var release string
	err = database.QueryRowContext(ctx,
		"SELECT database_role, source_build, content_release FROM database_manifest WHERE id=1",
	).Scan(&role, &sourceBuild, &release)
	if err != nil {
		return nil, fmt.Errorf("manifestRead: %w", err)
	}
	if role != MetadataRole || sourceBuild != SourceBuild || release != MetaRelease {
		return nil, fmt.Errorf("manifestMismatch: role=%q build=%d release=%q", role, sourceBuild, release)
	}
	var integrity string
	err = database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity)
	if err != nil {
		return nil, fmt.Errorf("integrityRead: %w", err)
	}
	if integrity != "ok" {
		return nil, fmt.Errorf("integrityCheck: %s", integrity)
	}
	foreignKeyRows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return nil, fmt.Errorf("foreignKeyRead: %w", err)
	}
	if foreignKeyRows.Next() {
		_ = foreignKeyRows.Close()
		return nil, errors.New("foreignKeyCheck: violation")
	}
	err = foreignKeyRows.Err()
	if err != nil {
		_ = foreignKeyRows.Close()
		return nil, fmt.Errorf("foreignKeyRows: %w", err)
	}
	err = foreignKeyRows.Close()
	if err != nil {
		return nil, fmt.Errorf("foreignKeyClose: %w", err)
	}
	var packageCount int
	var animationCount int
	var compiledLuaCount int
	var expectedAnimationCount int
	var serverDataResourceCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM package_inventory").Scan(&packageCount)
	if err != nil {
		return nil, fmt.Errorf("packageCount: %w", err)
	}
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM compiled_animation_resource").Scan(&animationCount)
	if err != nil {
		return nil, fmt.Errorf("animationCount: %w", err)
	}
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_data WHERE is_compiled_lua=1").Scan(&compiledLuaCount)
	if err != nil {
		return nil, fmt.Errorf("luaCount: %w", err)
	}
	err = database.QueryRowContext(ctx,
		"SELECT resource_count FROM package_inventory WHERE package_name='CompiledAnimData.package'",
	).Scan(&expectedAnimationCount)
	if err != nil {
		return nil, fmt.Errorf("animationExpected: %w", err)
	}
	err = database.QueryRowContext(ctx,
		"SELECT resource_count FROM content_source_package WHERE package_name='ServerData.package'",
	).Scan(&serverDataResourceCount)
	if err != nil {
		return nil, fmt.Errorf("luaExpected: %w", err)
	}
	expectedLuaCount := 0
	if serverDataResourceCount == 1101 {
		expectedLuaCount = 1029
	}
	if packageCount != 2 || animationCount != expectedAnimationCount || compiledLuaCount != expectedLuaCount {
		return nil, fmt.Errorf("countMismatch: packages=%d animations=%d/%d lua=%d/%d",
			packageCount, animationCount, expectedAnimationCount, compiledLuaCount, expectedLuaCount)
	}
	if expectedAnimationCount != 0 && expectedAnimationCount != 2407 {
		return nil, fmt.Errorf("animationSourceCount: got %d, want 2407", expectedAnimationCount)
	}
	var levelResourceCount int
	err = database.QueryRowContext(ctx,
		"SELECT resource_count FROM package_inventory WHERE package_name='Levels.package'",
	).Scan(&levelResourceCount)
	if err != nil {
		return nil, fmt.Errorf("levelExpected: %w", err)
	}
	if levelResourceCount != 0 && levelResourceCount != 62232 {
		return nil, fmt.Errorf("levelSourceCount: got %d, want 62232", levelResourceCount)
	}
	var classifiedResourceCount int
	err = database.QueryRowContext(ctx, "SELECT COALESCE(SUM(resource_count), 0) FROM package_resource_type").Scan(&classifiedResourceCount)
	if err != nil {
		return nil, fmt.Errorf("typeCount: %w", err)
	}
	if classifiedResourceCount != expectedAnimationCount+levelResourceCount {
		return nil, fmt.Errorf("typeCount: got %d, want %d", classifiedResourceCount, expectedAnimationCount+levelResourceCount)
	}
	var archiveCount int
	err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM package_archive").Scan(&archiveCount)
	if err != nil {
		return nil, fmt.Errorf("archiveCount: %w", err)
	}
	if archiveCount != 1 {
		return nil, fmt.Errorf("archiveCount: got %d, want 1", archiveCount)
	}
	var archiveDigest string
	var archiveSize int
	var archivePayload []byte
	err = database.QueryRowContext(ctx,
		"SELECT sha256, file_size, payload FROM package_archive",
	).Scan(&archiveDigest, &archiveSize, &archivePayload)
	if err != nil {
		return nil, fmt.Errorf("archiveRead: %w", err)
	}
	digest := sha256.Sum256(archivePayload)
	if len(archivePayload) != archiveSize || hex.EncodeToString(digest[:]) != archiveDigest {
		return nil, errors.New("archiveVerify: payload mismatch")
	}
	rows, err := database.QueryContext(ctx,
		"SELECT decoded_size, decoded_compression, decoded_sha256, decoded_payload FROM compiled_animation_resource ORDER BY id",
	)
	if err != nil {
		return nil, fmt.Errorf("animationRead: %w", err)
	}
	defer rows.Close()
	ordinal := 0
	for rows.Next() {
		var decodedSize int
		var compression string
		var wantedDigest string
		var compressedPayload []byte
		err = rows.Scan(&decodedSize, &compression, &wantedDigest, &compressedPayload)
		if err != nil {
			return nil, fmt.Errorf("animationScan[%d]: %w", ordinal, err)
		}
		if compression != "zlib" {
			return nil, fmt.Errorf("animationCompression[%d]: %q", ordinal, compression)
		}
		r, readerErr := zlib.NewReader(bytes.NewReader(compressedPayload))
		if readerErr != nil {
			return nil, fmt.Errorf("animationZlib[%d]: %w", ordinal, readerErr)
		}
		decodedPayload, readErr := io.ReadAll(r)
		closeErr := r.Close()
		if readErr != nil {
			return nil, fmt.Errorf("animationDecode[%d]: %w", ordinal, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("animationClose[%d]: %w", ordinal, closeErr)
		}
		digest := sha256.Sum256(decodedPayload)
		if len(decodedPayload) != decodedSize || hex.EncodeToString(digest[:]) != wantedDigest {
			return nil, fmt.Errorf("animationVerify[%d]: payload mismatch", ordinal)
		}
		ordinal++
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("animationRows: %w", err)
	}
	return &MetaVerification{
		MetaRelease:             release,
		SourceBuild:             sourceBuild,
		PackageCount:            packageCount,
		CompiledAnimationCount:  animationCount,
		CompiledLuaCount:        compiledLuaCount,
		IsCompiledArchiveStored: archiveCount == 1,
	}, nil
}

func removeDatabaseFiles(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + "-shm")
	_ = os.Remove(path + "-wal")
}
