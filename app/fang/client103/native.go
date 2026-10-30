//go:build windows && cgo

package client103

/*
#cgo CFLAGS: -I${SRCDIR}/..
#cgo windows LDFLAGS: -lversion
*/
import "C"
