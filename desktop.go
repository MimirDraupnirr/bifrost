package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Passage du binaire seul à l'application de bureau (desktop/, Windows et macOS) :
// un membre qui relance son binaire retrouve Bifröst dans l'application, sans rien
// faire. L'installeur vient de la même release et passe la même signature ed25519
// que les mises à jour (desktop-checksums.txt). Toute erreur laisse le binaire
// tourner comme avant.

// macApp : le dossier de l'application dans l'archive de mise à jour Tauri.
const macApp = "Bifröst.app"

// desktopSupported : une application existe pour cette plateforme.
func desktopSupported() bool {
	return desktopAsset(runtime.GOOS, runtime.GOARCH, "") != ""
}

// desktopAsset : le nom de l'installeur dans une release donnée ("" s'il n'y en a pas).
func desktopAsset(goos, goarch, version string) string {
	switch {
	case goos == "windows" && goarch == "amd64":
		return "Bifrost_" + strings.TrimPrefix(version, "v") + "_x64-setup.exe"
	case goos == "darwin" && goarch == "arm64":
		return "Bifrost_aarch64.app.tar.gz"
	case goos == "darwin" && goarch == "amd64":
		return "Bifrost_x64.app.tar.gz"
	}
	return ""
}

// desktopApp : l'application déjà installée, "" sinon. Windows : l'installeur NSIS
// « pour toi seul » pose bifrost-desktop.exe dans un dossier de %LOCALAPPDATA%.
func desktopApp() string {
	switch runtime.GOOS {
	case "windows":
		if m, _ := filepath.Glob(filepath.Join(os.Getenv("LOCALAPPDATA"), "*", "bifrost-desktop.exe")); len(m) > 0 {
			return m[0]
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		for _, p := range []string{filepath.Join(home, "Applications", macApp), filepath.Join("/Applications", macApp)} {
			if _, err := os.Stat(filepath.Join(p, "Contents", "MacOS", "bifrost-desktop")); err == nil {
				return p
			}
		}
	}
	return ""
}

// toDesktop : au lancement d'un binaire seul. true = l'application a pris le relais,
// le binaire doit s'arrêter.
func toDesktop(ctx context.Context) bool {
	app := desktopApp()
	if app == "" {
		fmt.Println("Installation de l'application de bureau…")
		var err error
		if app, err = installDesktop(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "application de bureau :", err, "— on continue avec ce binaire.")
			return false
		}
	}
	open := exec.Command(app)
	if runtime.GOOS == "darwin" {
		open = exec.Command("open", app)
	}
	if err := open.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "application de bureau :", err, "— on continue avec ce binaire.")
		return false
	}
	fmt.Println("Bifröst continue dans l'application de bureau :", app)
	return true
}

// installDesktop : télécharge l'installeur de la dernière release, le vérifie,
// l'installe et renvoie le chemin de l'application.
func installDesktop(ctx context.Context) (string, error) {
	gh, err := latestRelease(ctx)
	if err != nil {
		return "", err
	}
	name := desktopAsset(runtime.GOOS, runtime.GOARCH, gh.TagName)
	url, sumsURL, sigURL := gh.asset(name), gh.asset("desktop-checksums.txt"), gh.asset("desktop-checksums.txt.sig")
	if name == "" || url == "" || sumsURL == "" || sigURL == "" {
		return "", errors.New("pas d'installeur signé dans la release " + gh.TagName)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	data, err := fetch(ctx, url, 300<<20)
	if err != nil {
		return "", err
	}
	sums, err := fetch(ctx, sumsURL, 1<<20)
	if err != nil {
		return "", err
	}
	sig, err := fetch(ctx, sigURL, 1<<10)
	if err != nil {
		return "", err
	}
	if err := verifyChecksums(signingPublicKey, sums, sig, name, data); err != nil {
		return "", err
	}

	switch runtime.GOOS {
	case "windows":
		setup := filepath.Join(os.TempDir(), name)
		if err := os.WriteFile(setup, data, 0o755); err != nil {
			return "", err
		}
		defer os.Remove(setup)
		// /S : installation silencieuse, pour l'utilisateur courant (sans droit administrateur).
		if out, err := exec.CommandContext(ctx, setup, "/S").CombinedOutput(); err != nil {
			return "", fmt.Errorf("installeur : %v %s", err, bytes.TrimSpace(out))
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if err := installMacApp(data, filepath.Join(home, "Applications")); err != nil {
			return "", err
		}
	}
	if app := desktopApp(); app != "" {
		return app, nil
	}
	return "", errors.New("installée, mais introuvable")
}

// installMacApp : extrait l'archive Tauri (un dossier Bifröst.app) dans dir, en
// remplaçant d'un coup une version déjà là. Rien hors de Bifröst.app n'est écrit.
func installMacApp(archive []byte, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(dir, ".bifrost-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := untarApp(archive, tmp); err != nil {
		return err
	}
	dst := filepath.Join(dir, macApp)
	_ = os.RemoveAll(dst)
	return os.Rename(filepath.Join(tmp, macApp), dst)
}

// untarApp : dossiers, fichiers et liens de l'archive, sous dst/Bifröst.app seulement.
func untarApp(archive []byte, dst string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(h.Name)
		if name != macApp && !strings.HasPrefix(name, macApp+string(filepath.Separator)) {
			return fmt.Errorf("archive : chemin inattendu %q", h.Name)
		}
		path := filepath.Join(dst, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, h.FileInfo().Mode().Perm())
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, 300<<20))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Un lien ne sort pas de l'application.
			target := filepath.Clean(filepath.Join(filepath.Dir(name), h.Linkname))
			if filepath.IsAbs(h.Linkname) || !strings.HasPrefix(target, macApp+string(filepath.Separator)) {
				return fmt.Errorf("archive : lien inattendu %q", h.Linkname)
			}
			if err := os.Symlink(h.Linkname, path); err != nil {
				return err
			}
		}
	}
}
