//go:build windows && cgo

package main

/*
#include <stdlib.h>
#include "hook/text.h"
*/
import "C"

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"github.com/darkspinnet/darkspin/app/locale"
)

// initializeTranslation follows the same -locale argument used by build 103.
// It only supplies Dark Spin-owned captions; installed game text stays native.
func initializeTranslation() error {
	code := "en-us"
	for _, argument := range os.Args[1:] {
		lowerArgument := strings.ToLower(argument)
		if strings.HasPrefix(lowerArgument, "-locale:") {
			code = strings.TrimPrefix(lowerArgument, "-locale:")
		}
		if strings.HasPrefix(lowerArgument, "--locale:") {
			code = strings.TrimPrefix(lowerArgument, "--locale:")
		}
	}
	translations, err := locale.Load(code)
	if err != nil {
		return fmt.Errorf("translationLoad: %w", err)
	}
	for source, translation := range translations {
		if translation == "" || translation == source {
			continue
		}
		original := C.CString(source)
		translated := C.CString(translation)
		isStored := C.fang_set_translation(original, translated)
		C.free(unsafe.Pointer(original))
		C.free(unsafe.Pointer(translated))
		if isStored == 0 {
			return fmt.Errorf("translationStore: cannot retain %q", source)
		}
	}
	return nil
}
