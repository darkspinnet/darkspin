package main

import (
	"fmt"
	"os"

	"github.com/darkspinnet/darkspin/app/darkrun/cmd"
)

func main() {
	err := cmd.Execute()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
