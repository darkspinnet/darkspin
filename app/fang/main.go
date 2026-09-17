//go:build windows && cgo

package main

/*
#cgo windows LDFLAGS: -lws2_32 -static-libgcc
#include <stdlib.h>
#include "fang.h"

// fang_install receives hostname, physical and logical party ports, trace path,
// cinematic options, the launch credential, and the branded game-window title.
*/
import "C"

import (
	"net"
	"os"
	"strconv"
	"unsafe"
)

const (
	serverHost                    = "localhost"
	serverPort                    = 42127
	darkSpinnerVersionEnvironment = "DARKSPINNER_VERSION"
	defaultDarkSpinnerVersion     = "0.0.0"
)

//export GoRecapInitialize
func GoRecapInitialize() C.int {
	configuredAddress := os.Getenv("DARKSPIN_SERVER_ADDRESS")
	if configuredAddress == "" {
		configuredAddress = net.JoinHostPort(serverHost, strconv.Itoa(serverPort))
	}
	configuredHost, configuredPort := fangServerEndpoint(configuredAddress)
	host := C.CString(configuredHost)
	defer C.free(unsafe.Pointer(host))
	tracePath := C.CString(os.Getenv("DARKSPIN_CLIENT_TRACE"))
	defer C.free(unsafe.Pointer(tracePath))
	isIntroSkipped := C.int(0)
	if os.Getenv("DARKSPIN_SKIP_INTRO") == "1" {
		isIntroSkipped = 1
	}
	isCinematicSkipped := C.int(0)
	if os.Getenv("DARKSPIN_SKIP_CINEMATIC") == "1" {
		isCinematicSkipped = 1
	}
	jwt := C.CString(os.Getenv("DARKSPIN_LAUNCH_JWT"))
	defer C.free(unsafe.Pointer(jwt))
	_ = os.Unsetenv("DARKSPIN_LAUNCH_JWT")
	spinnerVersion := os.Getenv(darkSpinnerVersionEnvironment)
	if spinnerVersion == "" {
		spinnerVersion = defaultDarkSpinnerVersion
	}
	windowTitle := C.CString("Dark Spin v" + spinnerVersion)
	defer C.free(unsafe.Pointer(windowTitle))
	result := C.fang_install(
		host, C.ushort(configuredPort), C.ushort(configuredPort+1), tracePath,
		isIntroSkipped, isCinematicSkipped, jwt, windowTitle,
	)
	return C.int(result)
}

func fangServerEndpoint(address string) (string, uint16) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return serverHost, serverPort
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 || port == 65535 {
		return serverHost, serverPort
	}
	return host, uint16(port)
}

func main() {}
