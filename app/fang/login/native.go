//go:build windows && cgo

package login

/*
#cgo CFLAGS: -I${SRCDIR}/..
#cgo scenario,fangdebug CFLAGS: -DFANG_SCENARIO=1
*/
import "C"
