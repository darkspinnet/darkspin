//go:build windows && cgo && !fangdebug

package main

/*
#cgo CFLAGS: -DFANG_DIAGNOSTICS=0
*/
import "C"
