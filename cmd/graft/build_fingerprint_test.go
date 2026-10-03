package main

import (
	"errors"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeFileInfo is the file stat a test hands buildFingerprint in place of
// the running executable's.
type fakeFileInfo struct {
	os.FileInfo
	size  int64
	mtime time.Time
}

func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) ModTime() time.Time { return f.mtime }

// withBuildIdentity swaps in a build info and an executable stat for the
// duration of the test, clears the fingerprint cache before and after, and
// restores every hook on cleanup. A nil stat makes the executable lookup
// fail.
func withBuildIdentity(t *testing.T, settings map[string]string, exe *fakeFileInfo) {
	t.Helper()
	prevRead, prevPath, prevStat := readBuildInfo, executablePath, statExecutable
	t.Cleanup(func() {
		readBuildInfo, executablePath, statExecutable = prevRead, prevPath, prevStat
		buildFingerprintOnce = sync.Once{}
	})
	readBuildInfo = func() (*debug.BuildInfo, bool) { return buildInfoWith(settings), true }
	executablePath = func() (string, error) { return "/opt/graft", nil }
	statExecutable = func(string) (os.FileInfo, error) {
		if exe == nil {
			return nil, errors.New("stat failed")
		}
		return *exe, nil
	}
	buildFingerprintOnce = sync.Once{}
}

func TestBuildFingerprintFollowsTheBuild(t *testing.T) {
	stamp := time.Unix(1700000000, 0)
	base := map[string]string{"vcs.revision": "aaaa111", "vcs.modified": "false"}

	withBuildIdentity(t, base, &fakeFileInfo{size: 100, mtime: stamp})
	first := buildFingerprint()

	for name, c := range map[string]struct {
		settings map[string]string
		exe      *fakeFileInfo
	}{
		"another revision":    {map[string]string{"vcs.revision": "bbbb222", "vcs.modified": "false"}, &fakeFileInfo{size: 100, mtime: stamp}},
		"a dirty tree":        {map[string]string{"vcs.revision": "aaaa111", "vcs.modified": "true"}, &fakeFileInfo{size: 100, mtime: stamp}},
		"another binary size": {base, &fakeFileInfo{size: 101, mtime: stamp}},
		"another binary time": {base, &fakeFileInfo{size: 100, mtime: stamp.Add(time.Second)}},
	} {
		withBuildIdentity(t, c.settings, c.exe)
		if got := buildFingerprint(); got == first {
			t.Errorf("%s: fingerprint %q equals the base build's", name, got)
		}
	}

	withBuildIdentity(t, base, &fakeFileInfo{size: 100, mtime: stamp})
	if got := buildFingerprint(); got != first {
		t.Errorf("equal build identity gave %q, want %q", got, first)
	}
}

func TestBuildFingerprintLeavesOutWhatItCannotRead(t *testing.T) {
	withBuildIdentity(t, nil, nil)
	got := buildFingerprint()
	if !strings.Contains(got, Version) {
		t.Errorf("fingerprint %q must still carry the version %q", got, Version)
	}
	if strings.Contains(got, "exe=") || strings.Contains(got, "rev=") {
		t.Errorf("fingerprint %q holds a part that could not be read", got)
	}
}

func TestBuildFingerprintIsComputedOnce(t *testing.T) {
	withBuildIdentity(t, map[string]string{"vcs.revision": "aaaa111"}, &fakeFileInfo{size: 1, mtime: time.Unix(1, 0)})
	first := buildFingerprint()

	statExecutable = func(string) (os.FileInfo, error) {
		t.Error("the executable was stat'd a second time")
		return nil, errors.New("unexpected")
	}
	if got := buildFingerprint(); got != first {
		t.Errorf("second call gave %q, want the cached %q", got, first)
	}
}

// TestCacheKeysFollowTheBuild shows that two builds sharing a Version
// string, which is every dev build cut from one release, no longer share
// parse-cache or output-cache keys.
func TestCacheKeysFollowTheBuild(t *testing.T) {
	data := []byte("a: 1\n")
	inputs := [][]byte{data}
	stamp := time.Unix(1700000000, 0)

	withBuildIdentity(t, map[string]string{"vcs.revision": "aaaa111"}, &fakeFileInfo{size: 100, mtime: stamp})
	parseA, outA := parseCacheKey(data), mergeOutputCacheKey(&mergeOpts{}, inputs, false)
	if parseA != parseCacheKey(data) || outA != mergeOutputCacheKey(&mergeOpts{}, inputs, false) {
		t.Fatal("equal inputs must give equal keys")
	}

	withBuildIdentity(t, map[string]string{"vcs.revision": "bbbb222"}, &fakeFileInfo{size: 100, mtime: stamp})
	if parseA == parseCacheKey(data) {
		t.Error("two builds with one Version share a parse cache key")
	}
	if outA == mergeOutputCacheKey(&mergeOpts{}, inputs, false) {
		t.Error("two builds with one Version share an output cache key")
	}
}
