package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// executablePath and statExecutable are indirected so tests can describe
// the running binary without depending on how the test binary was built.
var (
	executablePath = os.Executable
	statExecutable = os.Stat
)

var (
	buildFingerprintOnce  sync.Once
	buildFingerprintValue string
)

// buildFingerprint names the build that is running, for the cache keys.
// Version alone is not enough, because every dev build cut from one
// release shares it, and a persistent cache then replays one build's
// results to another. The fingerprint adds the revision and the
// modified flag the toolchain embeds, and the size and modification time
// of the running executable, which tell apart two dirty builds of one
// revision and a build made without VCS stamping. A part that cannot be
// read is left out. The value is computed once per process.
func buildFingerprint() string {
	buildFingerprintOnce.Do(func() {
		buildFingerprintValue = computeBuildFingerprint()
	})
	return buildFingerprintValue
}

func computeBuildFingerprint() string {
	parts := []string{"version=" + Version}
	if revision := vcsSetting("vcs.revision"); revision != "" {
		parts = append(parts, "rev="+revision)
	}
	if modified := vcsSetting("vcs.modified"); modified != "" {
		parts = append(parts, "modified="+modified)
	}
	if path, err := executablePath(); err == nil {
		if info, err := statExecutable(path); err == nil {
			parts = append(parts, fmt.Sprintf("exe=%d/%d", info.Size(), info.ModTime().UnixNano()))
		}
	}
	return strings.Join(parts, ";")
}
