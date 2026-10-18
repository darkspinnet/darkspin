//go:build windows && cgo && (!scenario || !fangdebug)

package main

/*
#cgo CFLAGS: -DFANG_SCENARIO=0
*/
import "C"
