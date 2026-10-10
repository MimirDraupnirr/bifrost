package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

func listDir(path string) ([]DirEntry, error) { return listDirSizes(path, true) }

// listDirSizes : dirSizes faux = dossiers sans taille (0), sans les parcourir.
func listDirSizes(path string, dirSizes bool) ([]DirEntry, error) {
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
		e := DirEntry{Name: it.Name(), Path: filepath.Join(path, it.Name())}
		// os.Stat suit les liens : un lien vers un dossier est un dossier, de la
		// taille de sa cible. Un lien cassé reste un fichier vide.
		if fi, err := os.Stat(e.Path); err == nil {
			e.IsDir, e.Size = fi.IsDir(), fi.Size()
			if e.IsDir {
				e.Size = 0
				if dirSizes {
					e.Size = dirSize(e.Path)
				}
			}
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

// dirSize : somme des fichiers, parcourus comme par makeTorrent (walkFiles :
// fichiers cachés exclus, liens suivis) : la taille est celle du torrent,
// celle que /api/torrents/match, le cache et le plafond du lot comparent.
// ponytail: parcours complet ; mettre en cache si un dossier à 100k fichiers traîne.
func dirSize(path string) int64 {
	var total int64
	_ = walkFiles(path, func(_ string, size int64, _ error) error {
		total += size // une erreur arrive avec une taille nulle : on passe
		return nil
	})
	return total
}

// walkFiles : les fichiers de root (fichiers cachés exclus), liens symboliques
// SUIVIS — taille de la cible, dossiers liés parcourus. C'est le parcours
// unique de makeTorrent, dirSize et findAlbums : la taille listée, celle du
// torrent et ce qui est haché concordent (avant, un lien comptait pour la
// taille du lien et le hachage lisait la cible : torrent refusé par le site).
// rel est relatif à root ("" = root). Une erreur arrive à fn avec une taille
// nulle : le hachage s'arrête, les tailles et les albums passent. Un lien
// cassé est ignoré (rien à seeder), de même qu'un lien vers un dossier déjà
// parcouru, contenu dans l'un d'eux ou au-dessus : pas de boucle, aucun
// fichier compté deux fois, et un lien vers « / » ne part pas hacher le disque.
func walkFiles(root string, fn func(rel string, size int64, err error) error) error {
	var walked []string
	var walk func(dir, prefix string) error
	walk = func(dir, prefix string) error {
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return fn(prefix, 0, err)
		}
		for _, w := range walked {
			if within(real, w) || within(w, real) {
				return nil
			}
		}
		walked = append(walked, real)
		return filepath.WalkDir(real, func(p string, d fs.DirEntry, err error) error {
			rel := prefix
			if r, _ := filepath.Rel(real, p); r != "." {
				rel = filepath.Join(prefix, r)
			}
			if err != nil {
				return fn(rel, 0, err)
			}
			if d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			if d.Type()&fs.ModeSymlink == 0 {
				fi, err := d.Info()
				if err != nil {
					return fn(rel, 0, err)
				}
				return fn(rel, fi.Size(), nil)
			}
			fi, err := os.Stat(p)
			switch {
			case err != nil:
				return nil // lien cassé
			case fi.IsDir():
				return walk(p, rel)
			}
			return fn(rel, fi.Size(), nil)
		})
	}
	return walk(root, "")
}

// within : p est dir ou se trouve dessous.
func within(p, dir string) bool {
	sep := string(filepath.Separator)
	return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, sep)+sep)
}
