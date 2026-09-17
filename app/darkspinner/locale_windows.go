//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	localeSystemDefault = 0x0800
	localeName          = 0x005a
)

var localeInfoProcedure = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetLocaleInfoW")

func systemLocaleName() string {
	buffer := make([]uint16, 85)
	length, _, callErr := localeInfoProcedure.Call(
		localeSystemDefault, localeName,
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)),
	)
	if length == 0 || callErr != windows.ERROR_SUCCESS {
		return ""
	}
	return windows.UTF16ToString(buffer)
}
