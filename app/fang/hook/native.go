//go:build windows && cgo

package hook

/*
#cgo CFLAGS: -I${SRCDIR}/..
#cgo scenario,fangdebug CFLAGS: -DFANG_SCENARIO=1
*/
import "C"
