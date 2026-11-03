// Mkicon copies the approved terminal logo to the app and tray icon.
// Run from the repository root:
//
//	go run ./tools/mkicon
package main

import (
	"log"
	"os"
)

func main() {
	data, err := os.ReadFile("resources/gorex-terminal-logo.png")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("resources/icon.png", data, 0o644); err != nil {
		log.Fatal(err)
	}
}
