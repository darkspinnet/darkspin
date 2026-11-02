package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const webManifestFilename = "web-manifest.json"
const webRecipeVersion = 1

type webManifest struct {
	SourceBuild   int        `json:"source_build"`
	RecipeVersion int        `json:"recipe_version"`
	LootImages    []webAsset `json:"loot_images"`
}

// VerifyWeb checks prepared build-103 web assets without opening the installation.
func VerifyWeb(ctx context.Context, staticPath string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if staticPath == "" {
		return errors.New("empty static path")
	}
	for index, asset := range webAssetRecipe {
		err := verifyWebAsset(ctx, staticPath, asset)
		if err != nil {
			return fmt.Errorf("recipeAsset[%d]: %w", index, err)
		}
	}
	payload, err := os.ReadFile(filepath.Join(staticPath, webManifestFilename))
	if err != nil {
		return fmt.Errorf("manifestRead: %w", err)
	}
	var manifest webManifest
	err = json.Unmarshal(payload, &manifest)
	if err != nil {
		return fmt.Errorf("manifestDecode: %w", err)
	}
	if manifest.SourceBuild != 103 || manifest.RecipeVersion != webRecipeVersion || len(manifest.LootImages) == 0 {
		return errors.New("missing or stale build-103 loot-image manifest")
	}
	for index, asset := range manifest.LootImages {
		if !strings.HasPrefix(asset.Target, "assets/images/loot/") || !strings.HasSuffix(asset.Target, ".png") {
			return fmt.Errorf("lootTarget[%d]: invalid path %q", index, asset.Target)
		}
		err = verifyWebAsset(ctx, staticPath, asset)
		if err != nil {
			return fmt.Errorf("lootAsset[%d]: %w", index, err)
		}
	}
	return nil
}

func verifyWebAsset(ctx context.Context, staticPath string, asset webAsset) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("assetContext: %w", err)
	}
	targetPath, err := webTargetPath(staticPath, asset.Target)
	if err != nil {
		return fmt.Errorf("targetPath: %w", err)
	}
	isCurrent, err := isWebAssetCurrent(targetPath, asset)
	if err != nil {
		return fmt.Errorf("assetCheck: %w", err)
	}
	if !isCurrent {
		return fmt.Errorf("missing or stale web asset %s", targetPath)
	}
	return nil
}

func writeWebManifest(ctx context.Context, staticPath string) error {
	lootPath := filepath.Join(staticPath, "assets", "images", "loot")
	images, err := os.ReadDir(lootPath)
	if err != nil {
		return fmt.Errorf("lootList: %w", err)
	}
	manifest := webManifest{SourceBuild: 103, RecipeVersion: webRecipeVersion}
	for _, image := range images {
		if image.IsDir() || !strings.HasSuffix(image.Name(), ".png") {
			continue
		}
		err = ctx.Err()
		if err != nil {
			return fmt.Errorf("manifestContext: %w", err)
		}
		payload, readErr := os.ReadFile(filepath.Join(lootPath, image.Name()))
		if readErr != nil {
			return fmt.Errorf("lootRead: %w", readErr)
		}
		digest := sha256.Sum256(payload)
		manifest.LootImages = append(manifest.LootImages, webAsset{
			Target: "assets/images/loot/" + image.Name(),
			Size:   uint32(len(payload)), SHA256: hex.EncodeToString(digest[:]),
		})
	}
	if len(manifest.LootImages) == 0 {
		return errors.New("no prepared loot images")
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("manifestEncode: %w", err)
	}
	err = writePreparedWebPayload(staticPath, webManifestFilename, payload)
	if err != nil {
		return fmt.Errorf("manifestInstall: %w", err)
	}
	return nil
}
