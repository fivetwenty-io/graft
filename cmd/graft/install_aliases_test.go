package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// writeFakeBinary writes a stand-in for the graft binary in dir and returns its path.
func writeFakeBinary(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "graft")
	if err := os.WriteFile(path, []byte("fake graft binary"), 0o755); err != nil { // #nosec G306 -- the stand-in must be executable
		t.Fatal(err)
	}
	return path
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- the test reads its own temporary file
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// useGraftExecutable points the installer at path for the test and
// switches color off, so diagnostics compare as plain text.
func useGraftExecutable(t *testing.T, path string) {
	t.Helper()
	old, oldColor := graftExecutablePath, ansi.IsColorEnabled()
	graftExecutablePath = func() (string, error) { return path, nil }
	ansi.Color(false)
	t.Cleanup(func() {
		graftExecutablePath = old
		ansi.Color(oldColor)
	})
}

func TestInstallAliases(t *testing.T) {
	Convey("install-aliases", t, func() {
		dir := t.TempDir()
		graft := writeFakeBinary(t, dir)
		useGraftExecutable(t, graft)

		So(handleInstallAliases(installAliasesOpts{Dir: dir, Names: []string{"jq"}}, io.Discard, io.Discard), ShouldEqual, 0)
		st, err := inspectAliasTarget(filepath.Join(dir, "jq"), graft)
		So(err, ShouldBeNil)
		So(st, ShouldEqual, aliasLinkToThisGraft)
		So(handleInstallAliases(installAliasesOpts{Dir: dir, Names: []string{"jq"}}, io.Discard, io.Discard), ShouldEqual, 0) // idempotent
		So(os.WriteFile(filepath.Join(dir, "yq"), []byte("real"), 0o755), ShouldBeNil)                                        // #nosec G306
		var errOut bytes.Buffer
		So(handleInstallAliases(installAliasesOpts{Dir: dir, Names: []string{"yq"}}, io.Discard, &errOut), ShouldEqual, 1)
		So(errOut.String(), ShouldEqual, "Refusing to replace "+filepath.Join(dir, "yq")+": exists and is not a link to graft (use --force to replace it)\n")
		So(handleInstallAliases(installAliasesOpts{Dir: dir, Names: []string{"yq"}, Force: true}, io.Discard, io.Discard), ShouldEqual, 0)
		So(handleInstallAliases(installAliasesOpts{Dir: dir, Remove: true, Names: []string{"jq", "yq"}}, io.Discard, io.Discard), ShouldEqual, 0)
		_, err = os.Lstat(filepath.Join(dir, "jq"))
		So(os.IsNotExist(err), ShouldBeTrue)

		var out bytes.Buffer
		handleInstallAliases(installAliasesOpts{Dir: dir, DryRun: true, Names: []string{"jq"}}, &out, io.Discard)
		So(out.String(), ShouldContainSubstring, "would link")
		_, err = os.Lstat(filepath.Join(dir, "jq"))
		So(os.IsNotExist(err), ShouldBeTrue)

		So(os.WriteFile(filepath.Join(dir, "spruce"), []byte("real"), 0o755), ShouldBeNil) // #nosec G306
		out.Reset()
		errOut.Reset()
		// A foreign spruce among the defaults is skipped, never replaced,
		// and never an error.
		So(handleInstallAliases(installAliasesOpts{Dir: dir}, &out, &errOut), ShouldEqual, 0)
		So(out.String(), ShouldContainSubstring, "linked jq -> "+graft+"\n")
		So(out.String(), ShouldContainSubstring, "linked yq -> "+graft+"\n")
		So(out.String(), ShouldContainSubstring, "skipped spruce: "+filepath.Join(dir, "spruce")+" exists and is not a link to graft (use --force to replace it)\n")
		So(out.String(), ShouldEndWith, "linked 2, already installed 0, skipped 1\n")
		So(errOut.String(), ShouldBeEmpty)
		So(string(mustReadFile(t, filepath.Join(dir, "spruce"))), ShouldEqual, "real")
		So(defaultAliasNames(), ShouldResemble, []string{"jq", "yq", "spruce"})
	})

	Convey("install-aliases in copy mode", t, func() {
		dir := t.TempDir()
		useGraftExecutable(t, writeFakeBinary(t, dir))
		cdir := t.TempDir()
		var out, errOut bytes.Buffer
		jq := installAliasesOpts{Dir: cdir, Names: []string{"jq"}, copyMode: true}
		So(handleInstallAliases(jq, &out, &errOut), ShouldEqual, 0)
		So(handleInstallAliases(jq, &out, &errOut), ShouldEqual, 0)
		So(out.String(), ShouldContainSubstring, "already installed jq") // recognized by content hash
		writeOtherGraft(t, filepath.Join(cdir, "jq.exe"))
		So(handleInstallAliases(jq, &out, &errOut), ShouldEqual, 1)
		So(errOut.String(), ShouldContainSubstring, "is a copy of another graft (use --force to replace it)")
		jq.Force = true
		So(handleInstallAliases(jq, &out, &errOut), ShouldEqual, 0)
		So(string(mustReadFile(t, filepath.Join(cdir, "jq.exe"))), ShouldEqual, "fake graft binary")
		So(out.String(), ShouldContainSubstring, "copies")
		jq.Force, jq.Remove = false, true
		So(handleInstallAliases(jq, &out, &errOut), ShouldEqual, 0)
		_, err := os.Stat(filepath.Join(cdir, "jq.exe"))
		So(os.IsNotExist(err), ShouldBeTrue)
	})

	Convey("a link is judged by where it leads, never by what it says", t, func() {
		dir := t.TempDir()
		graft := writeFakeBinary(t, dir)
		useGraftExecutable(t, graft)
		// A link that shadows graft and answers --version like it must
		// still be foreign, and the check must not run it.
		marker := filepath.Join(dir, "ran")
		script := filepath.Join(dir, "impostor")
		So(os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+marker+"'\necho jq-1.8.2 graft\n"), 0o755), ShouldBeNil) // #nosec G306
		ldir := t.TempDir()
		So(os.Symlink(script, filepath.Join(ldir, "jq")), ShouldBeNil)
		st, err := inspectAliasTarget(filepath.Join(ldir, "jq"), graft)
		So(err, ShouldBeNil)
		So(st, ShouldEqual, aliasForeign)
		_, statErr := os.Stat(marker)
		So(os.IsNotExist(statErr), ShouldBeTrue)

		var errOut bytes.Buffer
		So(handleInstallAliases(installAliasesOpts{Dir: ldir, Names: []string{"jq"}}, io.Discard, &errOut), ShouldEqual, 1)
		So(errOut.String(), ShouldContainSubstring, "Refusing to replace")

		// A chain of links that ends at graft is graft's.
		hop := filepath.Join(ldir, "hop")
		So(os.Symlink(graft, hop), ShouldBeNil)
		So(os.Symlink(hop, filepath.Join(ldir, "spruce")), ShouldBeNil)
		st, err = inspectAliasTarget(filepath.Join(ldir, "spruce"), graft)
		So(err, ShouldBeNil)
		So(st, ShouldEqual, aliasLinkToThisGraft)

		// A dangling link is foreign too.
		So(os.Symlink(filepath.Join(ldir, "nowhere"), filepath.Join(ldir, "yq")), ShouldBeNil)
		st, err = inspectAliasTarget(filepath.Join(ldir, "yq"), graft)
		So(err, ShouldBeNil)
		So(st, ShouldEqual, aliasForeign)
	})

	Convey("an unknown name is refused", t, func() {
		useGraftExecutable(t, writeFakeBinary(t, t.TempDir()))
		var errOut bytes.Buffer
		So(handleInstallAliases(installAliasesOpts{Dir: t.TempDir(), Names: []string{"awk"}}, io.Discard, &errOut), ShouldEqual, 2)
		So(errOut.String(), ShouldContainSubstring, "Unknown alias: awk")
	})
}

func TestInstallAliasesThroughBinary(t *testing.T) {
	Convey("install-aliases through the binary", t, func() {
		graft := buildGraftBinary(t)
		dir := t.TempDir()
		r := execIn(t, ".", graft, []string{"install-aliases", "--dir", dir}, "")
		So(r.Code, ShouldEqual, 0)
		So(execIn(t, ".", filepath.Join(dir, "jq"), []string{"-n", "1"}, "").Stdout, ShouldEqual, "1\n")
		// The default set includes spruce, and spruce leads with the name it
		// was run by.
		So(execIn(t, ".", filepath.Join(dir, "spruce"), []string{"-v"}, "").Stdout, ShouldStartWith, filepath.Join(dir, "spruce")+" - Version ")
	})
}

// writeOtherGraft writes a real graft build that differs from the running
// one by a trailing byte, as an older release would.
func writeOtherGraft(t *testing.T, path string) {
	t.Helper()
	data := append(mustReadFile(t, buildGraftBinary(t)), 0)
	if err := os.WriteFile(path, data, 0o755); err != nil { // #nosec G306 -- an executable stand-in
		t.Fatal(err)
	}
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("permission checks need an unprivileged user on a Unix host")
	}
}

func TestInstallAliasesNeverTouchesWhatIsNotGrafts(t *testing.T) {
	Convey("--remove removes only graft's own links and copies, whatever --force says", t, func() {
		dir := t.TempDir()
		useGraftExecutable(t, writeFakeBinary(t, dir))
		var out, errOut bytes.Buffer
		foreign := filepath.Join(dir, "jq")
		So(os.WriteFile(foreign, []byte("real"), 0o755), ShouldBeNil) // #nosec G306
		So(handleInstallAliases(installAliasesOpts{Dir: dir, Names: []string{"jq"}, Remove: true, Force: true}, &out, &errOut), ShouldEqual, 1)
		So(errOut.String(), ShouldContainSubstring, "ot removing")
		So(string(mustReadFile(t, foreign)), ShouldEqual, "real")
		out.Reset()
		errOut.Reset()
		// A default name is skipped, with exit 0.
		So(os.WriteFile(filepath.Join(dir, "spruce"), []byte("real"), 0o755), ShouldBeNil) // #nosec G306
		So(handleInstallAliases(installAliasesOpts{Dir: dir, Remove: true, Force: true}, &out, &errOut), ShouldEqual, 0)
		So(out.String(), ShouldContainSubstring, "not removing")
		So(string(mustReadFile(t, filepath.Join(dir, "spruce"))), ShouldEqual, "real")

		cdir := t.TempDir()
		other := filepath.Join(cdir, "jq.exe")
		writeOtherGraft(t, other)
		before := mustReadFile(t, other)
		So(handleInstallAliases(installAliasesOpts{Dir: cdir, Names: []string{"jq"}, Remove: true, Force: true, copyMode: true}, io.Discard, io.Discard), ShouldEqual, 1)
		So(mustReadFile(t, other), ShouldResemble, before)
	})

	Convey("a file that merely mentions graft is foreign, not another graft", t, func() {
		dir := t.TempDir()
		useGraftExecutable(t, writeFakeBinary(t, dir))
		cdir := t.TempDir()
		So(os.WriteFile(filepath.Join(cdir, "jq.exe"), []byte("#!/bin/sh\n# wraps graft\n"), 0o755), ShouldBeNil) // #nosec G306
		var errOut bytes.Buffer
		So(handleInstallAliases(installAliasesOpts{Dir: cdir, Names: []string{"jq"}, copyMode: true}, io.Discard, &errOut), ShouldEqual, 1)
		So(errOut.String(), ShouldContainSubstring, "exists and is not a link to graft")
		So(errOut.String(), ShouldNotContainSubstring, "another graft")
	})

	Convey("a failed replacement leaves the existing file in place", t, func() {
		skipIfRoot(t)
		dir := t.TempDir()
		graft := writeFakeBinary(t, dir)
		useGraftExecutable(t, graft)
		cdir := t.TempDir()
		target := filepath.Join(cdir, "jq.exe")
		So(os.WriteFile(target, []byte("real"), 0o755), ShouldBeNil) // #nosec G306
		So(os.Chmod(graft, 0o000), ShouldBeNil)
		t.Cleanup(func() { _ = os.Chmod(graft, 0o755) }) // #nosec G302
		So(handleInstallAliases(installAliasesOpts{Dir: cdir, Names: []string{"jq"}, copyMode: true, Force: true}, io.Discard, io.Discard), ShouldEqual, 1)
		So(string(mustReadFile(t, target)), ShouldEqual, "real")
		entries, err := os.ReadDir(cdir)
		So(err, ShouldBeNil)
		So(entries, ShouldHaveLength, 1) // no temporary file is left behind
	})

	Convey("the running binary is never removed or relinked", t, func() {
		skipIfRoot(t)
		root := t.TempDir()
		realDir := filepath.Join(root, "real")
		So(os.Mkdir(realDir, 0o755), ShouldBeNil) // #nosec G301
		jq := writeFakeBinary(t, realDir)
		So(os.Rename(jq, filepath.Join(realDir, "jq")), ShouldBeNil)
		jq = filepath.Join(realDir, "jq")
		running := filepath.Join(root, "graft")
		So(os.Symlink(jq, running), ShouldBeNil)
		useGraftExecutable(t, running)
		var errOut bytes.Buffer
		So(handleInstallAliases(installAliasesOpts{Dir: realDir, Names: []string{"jq"}, Force: true}, io.Discard, &errOut), ShouldEqual, 1)
		So(string(mustReadFile(t, jq)), ShouldEqual, "fake graft binary")
		So(errOut.String(), ShouldContainSubstring, "running graft")

		// The legacy host, where graft is a link to a file named spruce.
		legacy := t.TempDir()
		spruce := filepath.Join(legacy, "spruce")
		So(os.WriteFile(spruce, []byte("fake graft binary"), 0o755), ShouldBeNil) // #nosec G306
		gl := filepath.Join(legacy, "graft")
		So(os.Symlink(spruce, gl), ShouldBeNil)
		useGraftExecutable(t, gl)
		So(handleInstallAliases(installAliasesOpts{Dir: legacy, Force: true}, io.Discard, io.Discard), ShouldEqual, 0)
		So(string(mustReadFile(t, spruce)), ShouldEqual, "fake graft binary")
		_, err := os.Stat(gl)
		So(err, ShouldBeNil)
	})

	Convey("a directory at the target is never replaced", t, func() {
		dir := t.TempDir()
		useGraftExecutable(t, writeFakeBinary(t, dir))
		So(os.Mkdir(filepath.Join(dir, "jq"), 0o755), ShouldBeNil) // #nosec G301
		So(handleInstallAliases(installAliasesOpts{Dir: dir, Names: []string{"jq"}, Force: true}, io.Discard, io.Discard), ShouldEqual, 1)
		info, err := os.Lstat(filepath.Join(dir, "jq"))
		So(err, ShouldBeNil)
		So(info.IsDir(), ShouldBeTrue)
	})
}

func TestInstallAliasesReports(t *testing.T) {
	Convey("--remove --dry-run says what it would remove and changes nothing", t, func() {
		dir := t.TempDir()
		useGraftExecutable(t, writeFakeBinary(t, dir))
		So(handleInstallAliases(installAliasesOpts{Dir: dir, Names: []string{"jq"}}, io.Discard, io.Discard), ShouldEqual, 0)
		var out bytes.Buffer
		So(handleInstallAliases(installAliasesOpts{Dir: dir, Names: []string{"jq"}, Remove: true, DryRun: true}, &out, io.Discard), ShouldEqual, 0)
		So(out.String(), ShouldContainSubstring, "would remove jq\n")
		So(out.String(), ShouldEndWith, "would remove 1, skipped 0\n")
		_, err := os.Lstat(filepath.Join(dir, "jq"))
		So(err, ShouldBeNil)
	})

	Convey("a permission error is reported with a hint and counted", t, func() {
		skipIfRoot(t)
		dir := t.TempDir()
		useGraftExecutable(t, writeFakeBinary(t, dir))
		ro := t.TempDir()
		So(os.Chmod(ro, 0o555), ShouldBeNil)          // #nosec G302
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) }) // #nosec G302
		var out, errOut bytes.Buffer
		So(handleInstallAliases(installAliasesOpts{Dir: ro, Names: []string{"jq"}}, &out, &errOut), ShouldEqual, 1)
		So(errOut.String(), ShouldStartWith, "Unable to write "+filepath.Join(ro, "jq")+": permission denied\n")
		So(errOut.String(), ShouldContainSubstring, "sudo")
		So(strings.Count(errOut.String(), "/graft"), ShouldEqual, 0)
		So(out.String(), ShouldEndWith, "linked 0, already installed 0, skipped 0, failed 1\n")
	})
}

func TestInstallAliasesBinary(t *testing.T) {
	Convey("the default directory and link target keep the path graft was run by", t, func() {
		if runtime.GOOS == "linux" {
			t.Skip("Linux reports the resolved path as the executable")
		}
		graft := buildGraftBinary(t)
		root := t.TempDir()
		keg := filepath.Join(root, "keg")
		bin := filepath.Join(root, "bin")
		So(os.Mkdir(keg, 0o755), ShouldBeNil) // #nosec G301
		So(os.Mkdir(bin, 0o755), ShouldBeNil) // #nosec G301
		So(os.Symlink(graft, filepath.Join(keg, "graft")), ShouldBeNil)
		runner := filepath.Join(bin, "graft")
		So(os.Symlink(filepath.Join(keg, "graft"), runner), ShouldBeNil)
		r := execIn(t, ".", runner, []string{"install-aliases", "jq"}, "")
		So(r.Code, ShouldEqual, 0)
		dest, err := os.Readlink(filepath.Join(bin, "jq"))
		So(err, ShouldBeNil)
		So(dest, ShouldEqual, runner)
	})

	Convey("a jq link ahead of a real jq on PATH answers as graft", t, func() {
		if runtime.GOOS == "windows" {
			t.Skip("the PATH lookup here needs a Unix shell")
		}
		graft := buildGraftBinary(t)
		first, second := t.TempDir(), t.TempDir()
		So(execIn(t, ".", graft, []string{"install-aliases", "--dir", first, "jq"}, "").Code, ShouldEqual, 0)
		So(os.WriteFile(filepath.Join(second, "jq"), []byte("#!/bin/sh\necho jq-1.8.2\n"), 0o755), ShouldBeNil) // #nosec G306
		path := first + string(os.PathListSeparator) + second + string(os.PathListSeparator) + "/usr/bin:/bin"
		r := execInEnv(t, []string{"PATH=" + path}, ".", "/bin/sh", []string{"-c", "jq --version"}, "")
		lines := strings.Split(r.Stdout, "\n")
		So(r.Code, ShouldEqual, 0)
		So(lines[0], ShouldEqual, "jq-"+jqCompatVersion)
		So(lines[1], ShouldContainSubstring, "graft")
	})
}

func TestInvokedGraftPath(t *testing.T) {
	Convey("the path graft was run by", t, func() {
		if runtime.GOOS == "windows" {
			SkipSo("symbolic links need a Unix host")
			return
		}
		// A package manager's layout: the binary in a versioned directory,
		// and a link to it in the bin directory on PATH. Linux reports only
		// the versioned path as the executable.
		root := t.TempDir()
		cellar := filepath.Join(root, "Caskroom", "graft", "1.43.0")
		bin := filepath.Join(root, "bin")
		So(os.MkdirAll(cellar, 0o750), ShouldBeNil)
		So(os.MkdirAll(bin, 0o750), ShouldBeNil)
		exe := writeFakeBinary(t, cellar)
		link := filepath.Join(bin, "graft")
		So(os.Symlink(exe, link), ShouldBeNil)
		lookPath := func(name string) (string, error) {
			if name == "graft" {
				return link, nil
			}
			return "", os.ErrNotExist
		}

		Convey("follows PATH when graft was run by its bare name", func() {
			So(invokedGraftPath("graft", exe, lookPath), ShouldEqual, link)
		})
		Convey("keeps a path graft was run by", func() {
			So(invokedGraftPath(link, exe, lookPath), ShouldEqual, link)
		})
		Convey("makes a relative path absolute", func() {
			t.Chdir(bin)
			So(invokedGraftPath("./graft", exe, lookPath), ShouldEqual, link)
		})
		Convey("falls back to the executable when PATH has no graft", func() {
			So(invokedGraftPath("graft", exe, func(string) (string, error) { return "", os.ErrNotExist }), ShouldEqual, exe)
		})
		Convey("falls back to the executable when the name leads to another file", func() {
			other := writeFakeBinary(t, t.TempDir())
			So(invokedGraftPath(other, exe, lookPath), ShouldEqual, exe)
		})
	})
}
