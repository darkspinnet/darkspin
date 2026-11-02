//go:build !windows

package main

import "errors"

func primaryDisplayMode() (int, int, int, error) {
	return 0, 0, 0, errors.New("primary display unavailable")
}
