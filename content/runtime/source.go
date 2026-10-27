package runtime

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/content"
	"github.com/darkspinnet/darkspin/content/sqlite"
)

// RequireSource ensures headless servers retain the readable installation
// needed for future content rebuilds, regardless of the current cache state.
func RequireSource(ctx context.Context, gamePath string) error {
	err := sqlite.ValidateSource(ctx, gamePath)
	if err != nil {
		return fmt.Errorf("databaseSource: %w", err)
	}
	err = content.ValidateWebSource(ctx, gamePath)
	if err != nil {
		return fmt.Errorf("webSource: %w", err)
	}
	return nil
}
