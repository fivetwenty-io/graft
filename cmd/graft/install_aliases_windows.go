//go:build windows

package main

// linkOrCopy makes link a copy of target, because a link needs privileges
// most Windows users lack.
func linkOrCopy(target, link string) error {
	return copyFile(target, link)
}
