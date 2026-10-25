//go:build scenario && windows

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Create with the protected ACL already present, before writing credentials.
func createDebugPrivateDirectory(path string) error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("privateToken: %w", err)
	}
	defer closeDebugToken(token)
	user, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("privateUser: %w", err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return fmt.Errorf("privateDescriptor: %w", err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("privatePath: %w", err)
	}
	attribute := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	err = windows.CreateDirectory(name, &attribute)
	if err != nil {
		return fmt.Errorf("privateCreate: %w", err)
	}
	return nil
}

func closeDebugToken(token windows.Token) {
	err := token.Close()
	if err != nil {
		// The temporary query token has no resource-recovery operation after close.
		return
	}
}
