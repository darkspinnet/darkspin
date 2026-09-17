//go:build windows && 386

// Fangloader reports the x86 loader address for the amd64 launcher.
package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func main() {
	loader := windows.NewLazySystemDLL("kernel32.dll").NewProc("LoadLibraryW")
	err := loader.Find()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%x\n", loader.Addr())
}
