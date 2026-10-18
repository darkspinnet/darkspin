//go:build windows && cgo && scenario && fangdebug

package main

/*
#cgo CFLAGS: -DFANG_SCENARIO=1
*/
import "C"
