package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
)

// Commit holds the git revision graft was built from, overridden at build
// time via `-ldflags "-X main.Commit=..."` (see the Makefile and
// .goreleaser.yaml). When it is empty the revision the Go toolchain
// embeds in the binary is used instead, so a plain `go build` still
// reports something real; "unknown" is the last resort (`go run`, or a
// build from a tarball with no VCS metadata).
var Commit = ""

// BuildDate holds the RFC3339 UTC build timestamp, with the same
// `-ldflags "-X main.BuildDate=..."` override and the same
// toolchain-embedded fallback (`vcs.time`) as Commit.
var BuildDate = ""

// unknownVersionField is reported for a commit or build date that neither
// the linker flags nor the embedded build info can supply.
const unknownVersionField = "unknown"

// shortCommitLen is how many hex characters of a full commit hash the
// version line shows, matching `git rev-parse --short=7`.
const shortCommitLen = 7

// readBuildInfo is indirected so tests can drive the fallback paths
// without depending on how the test binary itself was built.
var readBuildInfo = debug.ReadBuildInfo

// vcsSetting returns the named build setting the Go toolchain embedded in
// the binary ("vcs.revision", "vcs.time", "vcs.modified"), or "" when the
// build info is unavailable or carries no such setting.
func vcsSetting(key string) string {
	info, ok := readBuildInfo()
	if !ok || info == nil {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}

// commitField resolves the commit shown in the version line: the linker
// value first, then the toolchain's embedded revision, then "unknown".
// The "-dirty" marker is only appended to an embedded revision, since
// that is the only source that also tells us whether the tree was clean.
func commitField() string {
	if Commit != "" {
		return shortenCommit(Commit)
	}
	revision := vcsSetting("vcs.revision")
	if revision == "" {
		return unknownVersionField
	}
	short := shortenCommit(revision)
	if vcsSetting("vcs.modified") == "true" {
		short += "-dirty"
	}
	return short
}

// shortenCommit truncates a full hash to shortCommitLen characters and
// leaves anything already shorter (an abbreviated hash from the Makefile,
// say) untouched.
func shortenCommit(commit string) string {
	if len(commit) > shortCommitLen {
		return commit[:shortCommitLen]
	}
	return commit
}

// buildDateField resolves the timestamp shown in the version line, with
// the same linker-then-toolchain-then-"unknown" precedence as commitField.
func buildDateField() string {
	if BuildDate != "" {
		return BuildDate
	}
	if buildTime := vcsSetting("vcs.time"); buildTime != "" {
		return buildTime
	}
	return unknownVersionField
}

// graftVersionLine renders graft's own version line. The name is always
// "graft" even under a differently named symlink: the line describes the
// binary, and the invoked name is already covered by the spruce-compatible
// line that precedes it in spruce mode.
func graftVersionLine() string {
	return fmt.Sprintf("graft version %s (commit: %s, built: %s, go: %s, os/arch: %s/%s)",
		Version, commitField(), buildDateField(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// spruceVersionLine renders the line spruce itself prints, byte for byte:
// `fmt.Printf("%s - Version %s\n", os.Args[0], Version)`, argv[0] verbatim
// (a full path stays a full path) so genesis's version probe sees exactly
// what it would from a real spruce.
func spruceVersionLine(argv0 string) string {
	return fmt.Sprintf("%s - Version %s", argv0, Version)
}

// invokedAsSpruce reports whether argv[0] names the binary "spruce", the
// drop-in deployment described in docs/spruce/genesis-compat-contract.md
// (a symlink, copy, or hardlink on PATH). A `.exe` suffix is tolerated for
// Windows, and the comparison is case-insensitive for case-preserving
// filesystems.
func invokedAsSpruce(argv0 string) bool {
	base := strings.ToLower(filepath.Base(argv0))
	base = strings.TrimSuffix(base, ".exe")
	return base == "spruce"
}

// versionOutput is everything `-v`/`--version` writes to stdout, trailing
// newline included. Under a spruce name the spruce-compatible line comes
// first so that genesis's `qr(.*version\s+(\S+).*)i` probe matches it
// before anything graft adds, with graft's own line following it.
func versionOutput(argv0 string) string {
	if invokedAsSpruce(argv0) {
		return spruceVersionLine(argv0) + "\n" + graftVersionLine() + "\n"
	}
	return graftVersionLine() + "\n"
}
