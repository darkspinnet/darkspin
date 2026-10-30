//go:build windows && cgo

package zoom

/*
#cgo CFLAGS: -I${SRCDIR}/..
#cgo scenario,fangdebug CFLAGS: -DFANG_SCENARIO=1
*/
import "C"
