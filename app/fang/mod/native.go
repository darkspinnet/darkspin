//go:build windows && cgo

package mod

/*
#cgo CFLAGS: -I${SRCDIR}/..
*/
import "C"

import (
	_ "github.com/darkspinnet/darkspin/app/fang/mod/animation"
	_ "github.com/darkspinnet/darkspin/app/fang/mod/appearance"
	_ "github.com/darkspinnet/darkspin/app/fang/mod/audio"
	_ "github.com/darkspinnet/darkspin/app/fang/mod/editor"
	_ "github.com/darkspinnet/darkspin/app/fang/mod/palette"
	_ "github.com/darkspinnet/darkspin/app/fang/mod/resource"
	_ "github.com/darkspinnet/darkspin/app/fang/mod/scene"
	_ "github.com/darkspinnet/darkspin/app/fang/mod/ui"
)
