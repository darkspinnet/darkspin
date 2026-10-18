package sqlite

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

// readSpikeSpatialRadius supplies the sphere indexed by sub_9DCF60 for an
// ordinary spike noun: the farthest authored bound on each axis, then marker
// scale at placement. Exclusion uses this radius as well as the trigger radius.
func readSpikeSpatialRadius(ctx context.Context, pkg *dbpf.Reader) (float32, error) {
	instance := hashID("SpawnPoint_DirectorSpike")
	for _, entry := range pkg.Entries {
		if entry.Type != nounAssetType || entry.Group != nounAssetGroup || uint32(entry.Instance) != instance {
			continue
		}
		payload, err := readDecodedResource(ctx, pkg, entry)
		if err != nil {
			return 0, fmt.Errorf("spikeRead: %w", err)
		}
		if len(payload) < 0x50 {
			return 0, errors.New("spikeBounds: truncated")
		}
		var sum float32
		for index := range 3 {
			minimum := readFloat32(payload, 0x38+index*4)
			maximum := readFloat32(payload, 0x44+index*4)
			if !isFinite(minimum) || !isFinite(maximum) || minimum > maximum {
				return 0, fmt.Errorf("spikeBound[%d]: invalid", index)
			}
			sum += max(minimum*minimum, maximum*maximum)
		}
		return float32(math.Sqrt(float64(sum))), nil
	}
	return 0, errors.New("spikeNoun: missing")
}
