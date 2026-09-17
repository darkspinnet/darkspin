package dbpf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

func Fingerprint(ctx context.Context, source io.ReaderAt, size int64) (string, error) {
	if ctx == nil {
		return "", errors.New("nil context")
	}
	hash := sha256.New()
	r := io.NewSectionReader(source, 0, size)
	buffer := make([]byte, 128*1024)
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("hashContext: %w", ctx.Err())
		default:
		}
		count, err := r.Read(buffer)
		if count > 0 {
			_, writeErr := hash.Write(buffer[:count])
			if writeErr != nil {
				return "", fmt.Errorf("hashWrite: %w", writeErr)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("hashRead: %w", err)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
