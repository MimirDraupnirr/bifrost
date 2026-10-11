package main

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// fileSource : d'où viennent les fichiers de la release. Deux mises en
// œuvre, le disque de ce poste et une seedbox par SSH (l'agent y exécute
// exactement les mêmes fonctions).
type fileSource interface {
	List(ctx context.Context, path string) ([]DirEntry, error)
	// Dirs : comme List, sans la taille des dossiers (parcours complet de
	// chacun) : pour naviguer vite dans un dossier énorme (sélecteur du lot).
	Dirs(ctx context.Context, path string) ([]DirEntry, error)
	MakeTorrent(ctx context.Context, path, source string, progress progressFunc) ([]byte, error)
	MediaInfo(ctx context.Context, path string) (string, error)
	// MainFile : chemin du plus gros fichier, dans la convention de la source.
	MainFile(root string, t *Torrent) string
	// Home : dossier de départ quand la page n'en a pas encore.
	Home() string
	// Check : les fichiers du torrent existent-ils sous root, à la bonne taille ? (cross-seed)
	Check(ctx context.Context, root string, t *Torrent) ([]string, error)
	// Albums : les dossiers d'album sous root, à toute profondeur (lot musique).
	Albums(ctx context.Context, root string) ([]DirEntry, error)
}

// excludes : motifs glob (insensibles à la casse) des fichiers et dossiers
// qu'une release n'emporte jamais, comparés au nom seul (« *.trickplay »,
// « extrafanart »). Traités comme les fichiers cachés : ni listés, ni
// comptés dans la taille, ni hachés ; un dossier exclu l'est avec tout son
// contenu. Le même parcours (walkFiles) sert la liste, la taille et le
// torrent : elles restent d'accord.
type excludes []string

// defaultExcludes : ce que les médiathèques (Jellyfin, Kodi, Emby) déposent à
// côté d'un film et qui n'a rien à faire dans un torrent.
var defaultExcludes = excludes{"*.trickplay", "extrafanart", "extrathumbs"}

func (x excludes) match(name string) bool {
	name = strings.ToLower(name)
	for _, p := range x {
		if ok, _ := path.Match(strings.ToLower(p), name); ok {
			return true
		}
	}
	return false
}

// clean : motifs sans espaces autour ni lignes vides ; un motif mal formé
// (« [abc ») est une erreur, pas un filtre muet.
func (x excludes) clean() (excludes, error) {
	out := excludes{}
	for _, p := range x {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		if _, err := path.Match(p, ""); err != nil {
			return nil, fmt.Errorf("motif invalide : %s", p)
		}
		out = append(out, p)
	}
	return out, nil
}

// flags : les motifs pour l'agent de la seedbox (« --exclude 'motif' » chacun).
func (x excludes) flags() string {
	var b strings.Builder
	for _, p := range x {
		b.WriteString("--exclude " + shellQuote(p) + " ")
	}
	return b.String()
}

// agentExcludes : retire de args les « --exclude motif » (n'importe où après la
// sous-commande) et les renvoie à part.
func agentExcludes(args []string) ([]string, excludes) {
	var rest []string
	var x excludes
	for i := 0; i < len(args); i++ {
		if args[i] == "--exclude" && i > 0 && i+1 < len(args) {
			x = append(x, args[i+1])
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	return rest, x
}

type localSource struct{ skip excludes }

func (l localSource) List(_ context.Context, path string) ([]DirEntry, error) {
	return listDir(path, l.skip)
}

func (l localSource) Dirs(_ context.Context, path string) ([]DirEntry, error) {
	return listDirSizes(path, false, l.skip)
}

func (l localSource) MakeTorrent(_ context.Context, path, source string, progress progressFunc) ([]byte, error) {
	return makeTorrent(path, source, l.skip, progress)
}

func (localSource) MediaInfo(ctx context.Context, path string) (string, error) {
	return mediaInfo(ctx, path)
}

func (localSource) MainFile(root string, t *Torrent) string { return mainFile(root, t) }

func (localSource) Home() string {
	home, _ := filepath.Abs(".")
	if h, err := userHome(); err == nil {
		home = h
	}
	return home
}
