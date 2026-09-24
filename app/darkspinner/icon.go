package main

import _ "embed"

//go:embed build/appicon.png
var darkSpinnerIcon []byte

//go:embed build/windows/icon.ico
var darkSpinnerTrayIcon []byte
