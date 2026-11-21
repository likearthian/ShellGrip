//go:build !linux

package main

import "fmt"

func main() { fmt.Println("wslwatch runs on Linux; build it with go run ./tools/mkwslwatch") }
