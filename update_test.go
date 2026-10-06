package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestSemverLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{{"v0.3.2", "v0.4.0", true}, {"v0.4.0", "v0.4.0", false}, {"v1.0.0", "v0.9.9", false}, {"dev", "v1.0.0", false}, {"0.1.0", "v0.1.1", true}}
	for _, c := range cases {
		if semverLess(c.a, c.b) != c.want {
			t.Fatalf("%s < %s devrait être %v", c.a, c.b, c.want)
		}
	}
}

func TestVerifyChecksums(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	asset := []byte("binaire")
	sum := sha256.Sum256(asset)
	sums := []byte("deadbeef  autre\n" + hex.EncodeToString(sum[:]) + "  bifrost_1.0.0_linux_amd64\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)))
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	if err := verifyChecksums(pubB64, sums, sig, "bifrost_1.0.0_linux_amd64", asset); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksums(pubB64, sums, sig, "bifrost_1.0.0_linux_amd64", []byte("autre binaire")); err == nil {
		t.Fatal("un SHA différent doit être refusé")
	}
	if err := verifyChecksums(pubB64, append(sums, 'x'), sig, "bifrost_1.0.0_linux_amd64", asset); err == nil {
		t.Fatal("un checksums.txt altéré doit être refusé")
	}
	if err := verifyChecksums(pubB64, sums, sig, "inconnu", asset); err == nil {
		t.Fatal("un asset absent doit être refusé")
	}
}
