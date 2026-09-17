// Package content prepares immutable runtime content from an installed client.
package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

// WebOptions identifies the installed client and generated static cache.
type WebOptions struct {
	GamePath   string
	StaticPath string
}

type webAsset struct {
	Target   string
	Package  string
	Type     uint32
	Group    uint32
	Instance uint64
	SHA256   string
	Size     uint32
}

// PrepareWeb reconstructs server-facing static assets from client packages.
func PrepareWeb(ctx context.Context, options WebOptions) error {
	err := prepareWebAssets(ctx, options, webAssetRecipe)
	if err != nil {
		return err
	}
	installPath, err := webInstallPath(options.GamePath)
	if err != nil {
		return fmt.Errorf("installPath: %w", err)
	}
	staticPath, err := filepath.Abs(options.StaticPath)
	if err != nil {
		return fmt.Errorf("staticPath: %w", err)
	}
	err = prepareLootImages(ctx, installPath, staticPath)
	if err != nil {
		return fmt.Errorf("lootPrepare: %w", err)
	}
	return nil
}

func prepareWebAssets(ctx context.Context, options WebOptions, assets []webAsset) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if options.StaticPath == "" {
		return errors.New("empty static path")
	}
	installPath, err := webInstallPath(options.GamePath)
	if err != nil {
		return fmt.Errorf("installPath: %w", err)
	}
	staticPath, err := filepath.Abs(options.StaticPath)
	if err != nil {
		return fmt.Errorf("staticPath: %w", err)
	}
	err = os.MkdirAll(staticPath, 0o755)
	if err != nil {
		return fmt.Errorf("staticCreate: %w", err)
	}

	packageAssets := make(map[string][]webAsset)
	for _, asset := range assets {
		packageAssets[asset.Package] = append(packageAssets[asset.Package], asset)
	}
	for packageName, assets := range packageAssets {
		err = preparePackageAssets(ctx, installPath, staticPath, packageName, assets)
		if err != nil {
			return fmt.Errorf("packagePrepare[%s]: %w", packageName, err)
		}
	}
	return nil
}

func preparePackageAssets(ctx context.Context, installPath, staticPath, packageName string, assets []webAsset) error {
	packagePath := filepath.Join(installPath, "Data", packageName)
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("packageRead: %w", err)
	}
	entriesByID := make(map[string]dbpf.Entry, len(pkg.Entries))
	for _, entry := range pkg.Entries {
		entriesByID[webResourceID(entry.Type, entry.Group, entry.Instance)] = entry
	}
	for index, asset := range assets {
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("assetContext[%d]: %w", index, err)
		}
		entry, isFound := entriesByID[webResourceID(asset.Type, asset.Group, asset.Instance)]
		if !isFound {
			return fmt.Errorf("assetMissing[%d]: type 0x%08X group 0x%08X instance 0x%016X", index, asset.Type, asset.Group, asset.Instance)
		}
		err = writeWebAsset(pkg, entry, staticPath, asset)
		if err != nil {
			return fmt.Errorf("assetWrite[%d]: %w", index, err)
		}
	}
	return nil
}

func writeWebAsset(pkg *dbpf.Reader, entry dbpf.Entry, staticPath string, asset webAsset) error {
	targetPath, err := webTargetPath(staticPath, asset.Target)
	if err != nil {
		return fmt.Errorf("targetPath: %w", err)
	}
	isCurrent, err := isWebAssetCurrent(targetPath, asset)
	if err != nil {
		return fmt.Errorf("targetCheck: %w", err)
	}
	if isCurrent {
		return nil
	}
	decoded, err := pkg.Open(entry)
	if err != nil {
		return fmt.Errorf("payloadOpen: %w", err)
	}
	payload, err := io.ReadAll(decoded)
	if err != nil {
		return fmt.Errorf("payloadRead: %w", err)
	}
	if uint32(len(payload)) != asset.Size {
		return fmt.Errorf("payloadSize: got %d, want %d", len(payload), asset.Size)
	}
	digest := sha256.Sum256(payload)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), asset.SHA256) {
		return fmt.Errorf("payloadHash: got %x, want %s", digest, asset.SHA256)
	}
	err = os.MkdirAll(filepath.Dir(targetPath), 0o755)
	if err != nil {
		return fmt.Errorf("targetCreate: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(targetPath), ".web-*.tmp")
	if err != nil {
		return fmt.Errorf("temporaryCreate: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	_, err = temporary.Write(payload)
	if err != nil {
		_ = temporary.Close()
		return fmt.Errorf("temporaryWrite: %w", err)
	}
	err = temporary.Close()
	if err != nil {
		return fmt.Errorf("temporaryClose: %w", err)
	}
	err = os.Remove(targetPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("targetRemove: %w", err)
	}
	err = os.Rename(temporaryPath, targetPath)
	if err != nil {
		return fmt.Errorf("targetInstall: %w", err)
	}
	return nil
}

func isWebAssetCurrent(targetPath string, asset webAsset) (bool, error) {
	contents, err := os.ReadFile(targetPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("targetRead: %w", err)
	}
	if uint32(len(contents)) != asset.Size {
		return false, nil
	}
	digest := sha256.Sum256(contents)
	return strings.EqualFold(hex.EncodeToString(digest[:]), asset.SHA256), nil
}

func webTargetPath(staticPath, target string) (string, error) {
	cleanTarget := filepath.Clean(filepath.FromSlash(target))
	if cleanTarget == "." || filepath.IsAbs(cleanTarget) || cleanTarget == ".." || strings.HasPrefix(cleanTarget, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe target %q", target)
	}
	return filepath.Join(staticPath, cleanTarget), nil
}

func webResourceID(resourceType, group uint32, instance uint64) string {
	return fmt.Sprintf("%08x:%08x:%016x", resourceType, group, instance)
}

func webInstallPath(gamePath string) (string, error) {
	if gamePath == "" {
		return "", errors.New("empty game path")
	}
	installPath, err := filepath.Abs(gamePath)
	if err != nil {
		return "", fmt.Errorf("gameResolve: %w", err)
	}
	if strings.EqualFold(filepath.Base(installPath), "DarksporeBin") {
		installPath = filepath.Dir(installPath)
	}
	fi, err := os.Stat(filepath.Join(installPath, "DarksporeBin"))
	if err != nil {
		return "", fmt.Errorf("binStat: %w", err)
	}
	if !fi.IsDir() {
		return "", errors.New("DarksporeBin is not a directory")
	}
	return installPath, nil
}
