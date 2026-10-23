package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

type modNounSize struct {
	scale  float64
	extent float64
}

// Read every noun's fixed graphics-scale/bounds header, including nouns outside
// the server's narrower curated physics catalog. Package bytes stay read-only.
func readModNounSizes(ctx context.Context, path string) (map[uint32]modNounSize, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("sizeOpen: %w", err)
	}
	defer func() {
		closeErr := r.Close()
		if closeErr != nil {
			log.Printf("Mod noun size reader close failed: %v", closeErr)
		}
	}()
	fi, err := r.Stat()
	if err != nil {
		return nil, fmt.Errorf("sizeStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("sizeIndex: %w", err)
	}
	sizes := make(map[uint32]modNounSize)
	for _, entry := range pkg.Entries {
		if entry.Type != 0x76a8f7d8 || entry.Group != 0 {
			continue
		}
		err = ctx.Err()
		if err != nil {
			return nil, fmt.Errorf("sizeContext: %w", err)
		}
		decoded, readErr := pkg.Open(entry)
		if readErr != nil {
			return nil, fmt.Errorf("sizeDecode: %w", readErr)
		}
		payload := make([]byte, 80)
		count, readErr := io.ReadFull(decoded, payload)
		if readErr != nil {
			return nil, fmt.Errorf("sizeHeader[%d]: %w", count, readErr)
		}
		size := modNounSize{scale: float64(math.Float32frombits(binary.LittleEndian.Uint32(payload[20:])))}
		for axis := 0; axis < 3; axis++ {
			minimum := math.Float32frombits(binary.LittleEndian.Uint32(payload[56+axis*4:]))
			maximum := math.Float32frombits(binary.LittleEndian.Uint32(payload[68+axis*4:]))
			span := float64(maximum - minimum)
			if math.IsNaN(span) || math.IsInf(span, 0) {
				continue
			}
			size.extent = max(size.extent, span)
		}
		sizes[uint32(entry.Instance)] = size
	}
	return sizes, nil
}
