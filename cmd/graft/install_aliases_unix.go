//go:build !windows

package main

import "os"

// linkOrCopy makes link a symbolic link to target.
func linkOrCopy(target, link string) error {
	return os.Symlink(target, link)
}
