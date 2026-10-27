package dbpf

import (
	"errors"
	"fmt"
	"os"
)

// VerifyPath checks that a source package is a readable regular file with a
// valid, nonempty DBPF index. Payload decoding remains the importer's job.
func VerifyPath(path string) (resultErr error) {
	r, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("sourceOpen: %w", err)
	}
	defer func() {
		closeErr := r.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("sourceClose: %w", closeErr))
		}
	}()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("sourceStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return errors.New("source package must be a regular file")
	}
	pkg, err := NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("sourceIndex: %w", err)
	}
	if len(pkg.Entries) == 0 {
		return errors.New("source package has no resources")
	}
	return nil
}
