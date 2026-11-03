//go:build !windows

package main

func wslDirectory(*Pane) string { return "" }
