package main

import (
	"context"
	"path/filepath"
)

// fileSource : d'où viennent les fichiers de la release. Deux mises en
// œuvre, le disque de ce poste et une seedbox par SSH (l'agent y exécute
// exactement les mêmes fonctions).
type fileSource interface {
	List(ctx context.Context, path string) ([]DirEntry, error)
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

type localSource struct{}

func (localSource) List(_ context.Context, path string) ([]DirEntry, error) {
	return listDir(path)
}

func (localSource) MakeTorrent(_ context.Context, path, source string, progress progressFunc) ([]byte, error) {
	return makeTorrent(path, source, progress)
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
