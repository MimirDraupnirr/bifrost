package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func appArchive(t *testing.T, entries ...tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		body := []byte("x")
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(body))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			_, _ = tw.Write(body)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestUntarAppStaysInsideTheApp(t *testing.T) {
	ok := appArchive(t,
		tar.Header{Name: macApp + "/", Typeflag: tar.TypeDir, Mode: 0o755},
		tar.Header{Name: macApp + "/Contents/MacOS/bifrost-desktop", Typeflag: tar.TypeReg, Mode: 0o755},
		tar.Header{Name: macApp + "/Contents/Current", Typeflag: tar.TypeSymlink, Linkname: "MacOS"},
	)
	dir := t.TempDir()
	if err := untarApp(ok, dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, macApp, "Contents", "MacOS", "bifrost-desktop"))
	if err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("binaire absent ou non exécutable : %v %v", fi, err)
	}

	for name, bad := range map[string][]byte{
		"remontée":     appArchive(t, tar.Header{Name: "../evil", Typeflag: tar.TypeReg, Mode: 0o644}),
		"hors de .app": appArchive(t, tar.Header{Name: "Autre.app/x", Typeflag: tar.TypeReg, Mode: 0o644}),
		"lien absolu":  appArchive(t, tar.Header{Name: macApp + "/l", Typeflag: tar.TypeSymlink, Linkname: "/etc"}),
		"lien sortant": appArchive(t, tar.Header{Name: macApp + "/Contents/l", Typeflag: tar.TypeSymlink, Linkname: "../../x"}),
	} {
		if err := untarApp(bad, t.TempDir()); err == nil {
			t.Errorf("%s : archive acceptée", name)
		}
	}
}

func TestDesktopAssetNames(t *testing.T) {
	for _, c := range []struct{ goos, goarch, want string }{
		{"windows", "amd64", "Bifrost_0.6.1_x64-setup.exe"},
		{"darwin", "arm64", "Bifrost_aarch64.app.tar.gz"},
		{"darwin", "amd64", "Bifrost_x64.app.tar.gz"},
		{"linux", "amd64", ""}, // seedbox, sans écran : le binaire reste
		{"windows", "arm64", ""},
	} {
		if got := desktopAsset(c.goos, c.goarch, "v0.6.1"); got != c.want {
			t.Errorf("%s/%s : %q, attendu %q", c.goos, c.goarch, got, c.want)
		}
	}
}
