//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var primaryDisplaySettings = windows.NewLazySystemDLL("user32.dll").NewProc("EnumDisplaySettingsW")

func primaryDisplayMode() (int, int, int, error) {
	// DEVMODEW is 220 bytes; these offsets are dmSize and the current mode fields.
	var mode [220]byte
	binary.LittleEndian.PutUint16(mode[68:70], uint16(len(mode)))
	result, secondaryResult, callErr := primaryDisplaySettings.Call(0, 0xffffffff, uintptr(unsafe.Pointer(&mode[0])))
	if result == 0 {
		return 0, 0, 0, fmt.Errorf("displayQuery[%d]: %w", secondaryResult, callErr)
	}
	width := int(binary.LittleEndian.Uint32(mode[172:176]))
	height := int(binary.LittleEndian.Uint32(mode[176:180]))
	refresh := int(binary.LittleEndian.Uint32(mode[184:188]))
	// Build 103 rejects modes smaller than 1024x720 or wider than 2:1.
	if width < 1024 || height < 720 || width > height*2 || refresh < 24 || refresh > 1000 {
		return 0, 0, 0, fmt.Errorf("displayMode: unsupported %dx%d at %d Hz", width, height, refresh)
	}
	return width, height, refresh, nil
}
