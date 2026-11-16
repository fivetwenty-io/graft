package vaultca_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/vaultca"
	"github.com/fivetwenty-io/graft/internal/vaultca/vaultcatest"
)

// secretContent is planted in every malformed input, so a test can prove
// no error message ever repeats what it read.
const secretContent = "TOPSECRET-PAYLOAD-7f3a"

var fakePEM = []byte("-----BEGIN CERTIFICATE-----\n" +
	"VE9QU0VDUkVULVBBWUxPQUQtN2YzYQ==\n" +
	"-----END CERTIFICATE-----\n" + secretContent + "\n")

func source() vaultca.Source {
	return vaultca.Source{
		CACertField:      "Cfg.CACert",
		CACertBytesField: "Cfg.CACertBytes",
		CAPathField:      "Cfg.CAPath",
	}
}

func TestLoadUnconfiguredReturnsNilPool(t *testing.T) {
	pool, err := vaultca.Load(source())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if pool != nil {
		t.Fatal("expected a nil pool when no CA source is set")
	}
}

func TestLoadEachSourceHoldsOnlyItsCertificates(t *testing.T) {
	a := vaultcatest.NewCA(t, "ca-a")
	b := vaultcatest.NewCA(t, "ca-b")
	dir := t.TempDir()
	file := vaultcatest.WriteFile(t, dir, "file/a.pem", a.PEM)
	vaultcatest.WriteFile(t, dir, "tree/a.pem", a.PEM)
	vaultcatest.WriteFile(t, dir, "tree/nested/deeper/b.crt", b.PEM)

	cases := []struct {
		name string
		set  func(*vaultca.Source)
		want []*vaultcatest.CA
	}{
		{"CACert", func(s *vaultca.Source) { s.CACert = file }, []*vaultcatest.CA{a}},
		{"CACertBytes", func(s *vaultca.Source) { s.CACertBytes = b.PEM }, []*vaultcatest.CA{b}},
		{"CAPath nested", func(s *vaultca.Source) {
			s.CAPath = filepath.Join(dir, "tree")
		}, []*vaultcatest.CA{a, b}},
		{"CACertBytes bundle", func(s *vaultca.Source) {
			s.CACertBytes = append(append([]byte{}, a.PEM...), b.PEM...)
		}, []*vaultcatest.CA{a, b}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := source()
			tc.set(&src)

			pool, err := vaultca.Load(src)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			if !pool.Equal(vaultcatest.Pool(tc.want...)) {
				t.Fatal("pool does not hold exactly the configured CAs")
			}
		})
	}
}

func TestLoadPrecedence(t *testing.T) {
	a := vaultcatest.NewCA(t, "ca-a")
	b := vaultcatest.NewCA(t, "ca-b")
	c := vaultcatest.NewCA(t, "ca-c")
	dir := t.TempDir()
	fileA := vaultcatest.WriteFile(t, dir, "a.pem", a.PEM)
	vaultcatest.WriteFile(t, dir, "path/c.pem", c.PEM)
	pathC := filepath.Join(dir, "path")
	missing := filepath.Join(dir, "missing")

	cases := []struct {
		name string
		src  vaultca.Source
		want *vaultcatest.CA
	}{
		{"CACert beats CACertBytes and CAPath",
			vaultca.Source{CACert: fileA, CACertBytes: b.PEM, CAPath: pathC}, a},
		{"CACertBytes beats CAPath",
			vaultca.Source{CACertBytes: b.PEM, CAPath: pathC}, b},
		{"ignored sources are never read",
			vaultca.Source{CACert: fileA, CACertBytes: []byte(secretContent), CAPath: missing}, a},
		{"ignored CAPath is never read",
			vaultca.Source{CACertBytes: b.PEM, CAPath: missing}, b},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool, err := vaultca.Load(tc.src)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			if !pool.Equal(vaultcatest.Pool(tc.want)) {
				t.Fatal("pool does not hold exactly the winning source's CA")
			}
		})
	}
}

func TestLoadFailsLoudlyWithoutLeakingContent(t *testing.T) {
	dir := t.TempDir()
	badFile := vaultcatest.WriteFile(t, dir, "bad.pem", fakePEM)
	missingFile := filepath.Join(dir, "missing.pem")
	emptyDir := filepath.Join(dir, "empty")
	if err := os.MkdirAll(filepath.Join(emptyDir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}

	ca := vaultcatest.NewCA(t, "ca")
	mixedDir := filepath.Join(dir, "mixed")
	vaultcatest.WriteFile(t, mixedDir, "good.pem", ca.PEM)
	mixedBad := vaultcatest.WriteFile(t, mixedDir, "sub/README", []byte(secretContent))

	cases := []struct {
		name     string
		src      vaultca.Source
		field    string
		path     string
		notExist bool
	}{
		{"bad PEM file", vaultca.Source{CACert: badFile}, "Cfg.CACert", badFile, false},
		{"missing file", vaultca.Source{CACert: missingFile}, "Cfg.CACert", missingFile, true},
		{"bad PEM bytes", vaultca.Source{CACertBytes: fakePEM}, "Cfg.CACertBytes", "", false},
		{"empty dir", vaultca.Source{CAPath: emptyDir}, "Cfg.CAPath", emptyDir, false},
		{"missing dir", vaultca.Source{CAPath: missingFile}, "Cfg.CAPath", missingFile, true},
		{"non-PEM file in dir", vaultca.Source{CAPath: mixedDir}, "Cfg.CAPath", mixedBad, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := source()
			src.CACert, src.CACertBytes, src.CAPath = tc.src.CACert, tc.src.CACertBytes, tc.src.CAPath

			pool, err := vaultca.Load(src)
			if err == nil {
				t.Fatal("expected an error")
			}

			if pool != nil {
				t.Fatal("expected no pool alongside the error")
			}

			if !errors.Is(err, vaultca.ErrCA) {
				t.Fatalf("errors.Is(err, ErrCA) is false for %v", err)
			}

			var caErr *vaultca.CAError
			if !errors.As(err, &caErr) {
				t.Fatalf("errors.As(err, *CAError) is false for %v", err)
			}

			if caErr.Field != tc.field || caErr.Path != tc.path {
				t.Fatalf("got Field=%q Path=%q, want %q %q", caErr.Field, caErr.Path, tc.field, tc.path)
			}

			if tc.notExist && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("expected the cause to match fs.ErrNotExist: %v", err)
			}

			msg := err.Error()
			if !strings.Contains(msg, tc.field) {
				t.Fatalf("message does not name the field: %s", msg)
			}

			for _, leak := range []string{secretContent, "VE9QU0VD", "BEGIN"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("message repeats input content %q: %s", leak, msg)
				}
			}
		})
	}
}

func TestLoadCAPathFollowsSymlinkedFilesAndRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}

	a := vaultcatest.NewCA(t, "ca-a")
	b := vaultcatest.NewCA(t, "ca-b")
	dir := t.TempDir()
	target := vaultcatest.WriteFile(t, dir, "store/b.pem", b.PEM)
	certs := filepath.Join(dir, "certs")
	vaultcatest.WriteFile(t, certs, "a.pem", a.PEM)

	if err := os.Symlink(target, filepath.Join(certs, "b-link.0")); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "certs-link")
	if err := os.Symlink(certs, link); err != nil {
		t.Fatal(err)
	}

	src := source()
	src.CAPath = link

	pool, err := vaultca.Load(src)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !pool.Equal(vaultcatest.Pool(a, b)) {
		t.Fatal("pool does not hold both the file and the symlinked file")
	}
}

func TestCAErrorFormat(t *testing.T) {
	src := source()
	src.CACert = filepath.Join(t.TempDir(), "nope.pem")

	_, err := vaultca.Load(src)
	if err == nil {
		t.Fatal("expected an error")
	}

	want := `vault: Cfg.CACert "` + src.CACert + `": cannot read CA certificate: open: `
	if !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("got %q, want prefix %q", err.Error(), want)
	}
}
