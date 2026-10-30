//go:build windows && cgo

package audio

/*
#cgo CFLAGS: -I${SRCDIR}/../..
*/
import "C"
