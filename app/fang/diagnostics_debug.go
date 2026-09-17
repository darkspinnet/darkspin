//go:build windows && cgo && fangdebug

package main

/*
#cgo CFLAGS: -DFANG_DIAGNOSTICS=1
*/
import "C"
