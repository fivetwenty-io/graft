package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
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
		So(out.String(), ShouldContainSubstring, "skipped spruce: "+filepath.Join(dir, "spruce")+" exists and is not a link to graft (use --force to replace it)\n")
		So(out.String(), ShouldEndWith, "linked 1, already installed 0, skipped 1\n")
		So(errOut.String(), ShouldBeEmpty)
		So(string(mustReadFile(t, filepath.Join(dir, "spruce"))), ShouldEqual, "real")
		So(defaultAliasNames(), ShouldResemble, []string{"jq", "spruce"})
	})

	Convey("install-aliases in copy mode", t, func() {
		dir := t.TempDir()
		useGraftExecutable(t, writeFakeBinary(t, dir))
		cdir := t.TempDir()
		var out, errOut bytes.Buffer
		jq := installAliasesOpts{Dir: cdir, Names: []string{"jq"}, copyMode: true}
		So(handleInstallAliases(jq, &out, &errOut), ShouldEqual, 0)
		So(handleInstallAliases(jq, &out, &errOut), ShouldEqual, 0)
		So(out.String(), ShouldContainSubstring, "already installed jq")                           // recognized by content hash
		So(os.WriteFile(filepath.Join(cdir, "jq.exe"), []byte("older graft"), 0o755), ShouldBeNil) // #nosec G306
		So(handleInstallAliases(jq, &out, &errOut), ShouldEqual, 1)
		So(errOut.String(), ShouldContainSubstring, "is a copy of another graft (use --force to replace it)")
		jq.Force = true
		So(handleInstallAliases(jq, &out, &errOut), ShouldEqual, 0)
		So(string(mustReadFile(t, filepath.Join(cdir, "jq.exe"))), ShouldEqual, "fake graft binary")
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
		// The default set includes spruce.
		So(execIn(t, ".", filepath.Join(dir, "spruce"), []string{"-v"}, "").Stdout, ShouldContainSubstring, "spruce - Version")
	})
}
