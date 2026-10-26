//go:build windows

package main

// linkOrCopy makes link a copy of target, because a link needs privileges
// most Windows users lack. Runs here set copyMode and copy directly, so
// this is the form used when a caller leaves copyMode off.
func linkOrCopy(target, link string) error {
	return copyFile(target, link)
}
