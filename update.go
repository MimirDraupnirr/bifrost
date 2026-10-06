package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Mise à jour (docs/23 §9.3) : dernière release GitHub, asset de la
// plateforme, checksums.txt signé en ed25519 avec la clé ci-dessous (la
// privée vit dans le secret BIFROST_SIGNING_KEY du dépôt). Rien n'est
// installé si la signature ou le SHA-256 ne colle pas.
const (
	releaseRepo = "MimirDraupnirr/bifrost"
	// Clé publique ed25519 (base64). Remplacée au premier `bifrost agent keygen`.
	signingPublicKey = "y42TnWbIiw6PDHYIOVjHssVQpN+UYfYuRpxWzfFYSBM="
)

type release struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
	URL     string `json:"url"`
	asset   string
	sums    string
	sig     string
}

func inDocker() bool { return os.Getenv("BIFROST_IN_DOCKER") != "" }

// semverLess : "v0.3.2" < "v0.4.0". Les versions non numériques (dev) ne
// sont jamais « plus vieilles » : un build local ne s'auto-remplace pas.
func semverLess(a, b string) bool {
	pa, oka := semverParts(a)
	pb, okb := semverParts(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func semverParts(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func assetName(version string) string {
	name := fmt.Sprintf("bifrost_%s_%s_%s", strings.TrimPrefix(version, "v"), runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// checkUpdate interroge GitHub ; nil si rien de plus récent.
func checkUpdate(ctx context.Context) (*release, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+releaseRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Bifrost/"+Version)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub : HTTP %d", res.StatusCode)
	}
	var gh struct {
		TagName    string `json:"tag_name"`
		Body       string `json:"body"`
		HTMLURL    string `json:"html_url"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&gh); err != nil {
		return nil, err
	}
	if gh.Prerelease || !semverLess(Version, gh.TagName) {
		return nil, nil
	}
	rel := &release{Version: gh.TagName, Notes: firstLines(gh.Body, 5), URL: gh.HTMLURL}
	want := assetName(gh.TagName)
	for _, a := range gh.Assets {
		switch a.Name {
		case want:
			rel.asset = a.URL
		case "checksums.txt":
			rel.sums = a.URL
		case "checksums.txt.sig":
			rel.sig = a.URL
		}
	}
	return rel, nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "Bifrost/"+Version)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s : HTTP %d", url, res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, limit))
}

// verifyChecksums : signature ed25519 de checksums.txt, puis SHA-256 de l'asset.
func verifyChecksums(pubKeyB64 string, sums, sig []byte, asset string, data []byte) error {
	pub, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("clé publique de signature invalide dans ce build")
	}
	rawSig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), sums, rawSig) {
		return errors.New("signature de checksums.txt invalide : mise à jour refusée")
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			if f[0] == want {
				return nil
			}
			return errors.New("SHA-256 de l'asset différent de checksums.txt : mise à jour refusée")
		}
	}
	return errors.New("asset absent de checksums.txt")
}

// applyUpdate télécharge, vérifie, remplace le binaire et le relance.
func applyUpdate(ctx context.Context, rel *release) error {
	if inDocker() {
		return errors.New("en Docker : mets l'image à jour (docker pull) ou laisse Watchtower s'en charger")
	}
	if rel.asset == "" || rel.sums == "" || rel.sig == "" {
		return errors.New("release incomplète (asset, checksums.txt ou checksums.txt.sig manquant)")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	data, err := fetch(ctx, rel.asset, 200<<20)
	if err != nil {
		return err
	}
	sums, err := fetch(ctx, rel.sums, 1<<20)
	if err != nil {
		return err
	}
	sig, err := fetch(ctx, rel.sig, 1<<10)
	if err != nil {
		return err
	}
	if err := verifyChecksums(signingPublicKey, sums, sig, assetName(rel.Version), data); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	tmp := exe + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// Un exécutable en cours ne se remplace pas : on le renomme, le
		// prochain lancement nettoie l'ancien.
		_ = os.Remove(exe + ".old")
		if err := os.Rename(exe, exe+".old"); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, exe); err != nil {
		return err
	}
	return restart(exe)
}

func restart(exe string) error {
	if runtime.GOOS == "windows" {
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		os.Exit(0)
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

func cleanupOldBinary() {
	if runtime.GOOS == "windows" {
		if exe, err := os.Executable(); err == nil {
			_ = os.Remove(exe + ".old")
		}
	}
}

// ---- outils de release (CI) : keygen et sign ----

func keygen() int {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("PUBLIC  (à coller dans signingPublicKey) :", base64.StdEncoding.EncodeToString(pub))
	fmt.Println("PRIVATE (secret GitHub BIFROST_SIGNING_KEY) :", base64.StdEncoding.EncodeToString(priv.Seed()))
	return 0
}

func sign(file string) int {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("BIFROST_SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		fmt.Fprintln(os.Stderr, "BIFROST_SIGNING_KEY absent ou invalide")
		return 1
	}
	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
	if err := os.WriteFile(file+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(file + ".sig")
	return 0
}
