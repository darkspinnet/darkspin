//go:build windows && cgo && (!scenario || !fangdebug)

package scenario

/*
#cgo CFLAGS: -DFANG_SCENARIO=0
*/
import "C"
