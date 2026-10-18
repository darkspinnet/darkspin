//go:build scenario && !windows

package main

import (
	"errors"
	"os"
)

func openScenarioContent(path string) (*os.File, error) {
	return nil, errors.New("scenario content snapshots require the supported Windows deny-write handle")
}
