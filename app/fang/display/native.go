//go:build windows && cgo

package display

/*
#cgo CFLAGS: -I${SRCDIR}/..
*/
import "C"

import (
	"log"
	"os"
	"strconv"

	"github.com/darkspinnet/darkspin/server/game"
)

//export fang_ui_scale_load
func fang_ui_scale_load() C.int {
	path := os.Getenv("DARKSPIN_CONFIG_PATH")
	if path == "" {
		return 100
	}
	config, isCreated, err := game.LoadConfig(path)
	if err != nil {
		log.Printf("native UI scale load: %v", err)
		return 100
	}
	if isCreated {
		log.Printf("native UI scale initialized configuration at %s", path)
	}
	percent, err := config.Uint32(game.ConfigNativeUIScalePercent)
	if err != nil || percent < 100 || percent > 300 || percent%5 != 0 {
		log.Printf("native UI scale invalid: %d (%v)", percent, err)
		return 100
	}
	return C.int(percent)
}

//export fang_ui_scale_save
func fang_ui_scale_save(percent C.int) C.int {
	path := os.Getenv("DARKSPIN_CONFIG_PATH")
	if path == "" || percent < 100 || percent > 300 || percent%5 != 0 {
		return 0
	}
	config, isCreated, err := game.LoadConfig(path)
	if err != nil {
		log.Printf("native UI scale reload: %v", err)
		return 0
	}
	if isCreated {
		log.Printf("native UI scale initialized configuration at %s", path)
	}
	err = config.Set(game.ConfigNativeUIScalePercent, strconv.Itoa(int(percent)))
	if err != nil {
		log.Printf("native UI scale setting: %v", err)
		return 0
	}
	err = config.Save(path)
	if err != nil {
		log.Printf("native UI scale save: %v", err)
		return 0
	}
	return 1
}
