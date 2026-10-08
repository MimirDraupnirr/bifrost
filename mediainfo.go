package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// MediaInfo n'est pas embarqué (bibliothèque C++) : on utilise le binaire du
// système, et on dit comment l'installer s'il manque (docs/23 §6).
func mediaInfoAvailable() bool {
	_, err := exec.LookPath("mediainfo")
	return err == nil
}

func mediaInfoInstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install mediainfo"
	case "windows":
		return "winget install MediaArea.MediaInfo"
	default:
		return "sudo apt install mediainfo   (ou l'équivalent de ta distribution)"
	}
}

// mediaInfo renvoie le rapport JSON, celui que /api/upload accepte tel quel.
// Un dossier (album) rend un tableau, un objet par fichier.
func mediaInfo(ctx context.Context, path string) (string, error) {
	if !mediaInfoAvailable() {
		return "", errors.New("mediainfo introuvable — installe-le : " + mediaInfoInstallHint())
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mediainfo", "--Output=JSON", "--", path)
	// Chemin accentué sous locale POSIX : MediaInfo rend {"media":null} en
	// code 0. Forcer l'UTF-8 évite ce silence.
	cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("mediainfo : %w", err)
	}
	s, ok := mediaInfoReadable(string(out))
	if !ok {
		return "", errors.New("mediainfo n'a rien lu : chemin introuvable ou fichier illisible")
	}
	return s, nil
}

// mediaInfoReadable : un objet doit avoir des pistes ; dans le tableau d'un
// dossier, un fichier illisible ("media":null) est écarté sans faire tomber
// les autres.
func mediaInfoReadable(s string) (string, bool) {
	var items []json.RawMessage
	if json.Unmarshal([]byte(s), &items) != nil {
		return s, !strings.Contains(s, `"media":null`) && strings.Contains(s, `"track"`)
	}
	kept := items[:0]
	for _, it := range items {
		var f struct {
			Media *struct {
				Track []json.RawMessage `json:"track"`
			} `json:"media"`
		}
		if json.Unmarshal(it, &f) == nil && f.Media != nil && len(f.Media.Track) > 0 {
			kept = append(kept, it)
		}
	}
	if len(kept) == 0 {
		return "", false
	}
	out, err := json.Marshal(kept)
	return string(out), err == nil
}

// ---- parcours de dossiers (page locale et `agent ls`) ----

type DirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

func listDir(path string) ([]DirEntry, error) {
	if path == "" {
		home, _ := os.UserHomeDir()
		path = home
	}
	path = filepath.Clean(path)
	items, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, 0, len(items))
	for _, it := range items {
		if strings.HasPrefix(it.Name(), ".") {
			continue
		}
		e := DirEntry{Name: it.Name(), Path: filepath.Join(path, it.Name()), IsDir: it.IsDir()}
		if it.IsDir() {
			e.Size = dirSize(e.Path)
		} else if fi, err := it.Info(); err == nil {
			e.Size = fi.Size()
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// dirSize : somme des fichiers, bornée à un niveau de profondeur raisonnable
// pour que la liste reste instantanée sur une seedbox pleine. Les fichiers
// cachés (.DS_Store…) sont exclus comme par makeTorrent : la taille est
// celle du torrent, celle que /api/torrents/match et le cache comparent.
// ponytail: parcours complet ; mettre en cache si un dossier à 100k fichiers traîne.
func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total
}
