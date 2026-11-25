//go:build !windows

package main

func start(path string) error {
	return foreground(path)
}
