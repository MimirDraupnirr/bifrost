package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Cache des .torrent créés (suggestion d'un membre) : relancer un lot ou
// re-préparer une release ne re-hache pas des gigaoctets déjà hachés. Clé =
// source + chemin + taille totale + tag source ; une taille différente rend
// l'entrée caduque. Un .torrent pèse quelques Ko : pas de purge nécessaire.
// ponytail: pas de mtime dans la clé — un fichier réécrit à taille identique
// garderait un ancien torrent ; ajouter les mtimes si ça arrive.
type torrentCache struct{ dir string }

func newTorrentCache(configPath string) *torrentCache {
	return &torrentCache{dir: filepath.Join(filepath.Dir(configPath), "torrents-cache")}
}

func (c *torrentCache) key(source, p string, size int64, tag string) string {
	sum := sha256.Sum256([]byte(source + "|" + p + "|" + strconv.FormatInt(size, 10) + "|" + tag))
	return hex.EncodeToString(sum[:16])
}

func (c *torrentCache) get(source, p string, size int64, tag string) []byte {
	raw, err := os.ReadFile(filepath.Join(c.dir, c.key(source, p, size, tag)+".torrent"))
	if err != nil {
		return nil
	}
	// Garde-fou : l'entrée doit décrire ce chemin-là.
	t, err := parseTorrent(raw)
	if err != nil || t.Name != baseName(p) || t.Size != size {
		return nil
	}
	return raw
}

func (c *torrentCache) put(source, p string, size int64, tag string, raw []byte) {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(c.dir, c.key(source, p, size, tag)+".torrent"), raw, 0o644)
}

func baseName(p string) string {
	p = strings.TrimRight(p, "/")
	if strings.Contains(p, "/") {
		return path.Base(p)
	}
	return filepath.Base(p)
}

// entrySize : taille totale d'une entrée (fichier ou dossier) sans hacher.
func entrySize(ctx context.Context, src fileSource, p string) (int64, error) {
	if entries, err := src.List(ctx, p); err == nil {
		var total int64
		for _, e := range entries {
			total += e.Size
		}
		return total, nil
	}
	// Un fichier seul : il figure dans la liste de son dossier.
	parent := filepath.Dir(p)
	if _, remote := src.(*remoteSource); remote {
		parent = pathDir(p)
	}
	entries, err := src.List(ctx, parent)
	if err != nil {
		return 0, err
	}
	want := baseName(p)
	for _, e := range entries {
		if e.Name == want {
			return e.Size, nil
		}
	}
	return 0, errors.New("entrée introuvable : " + p)
}

// makeTorrentCached : le .torrent depuis le cache, sinon haché puis mis en cache.
// `size` peut être 0 : il est alors mesuré. Renvoie aussi si le cache a servi.
func (s *server) makeTorrentCached(ctx context.Context, src fileSource, p, tag string, size int64, progress progressFunc) ([]byte, bool, error) {
	source := "local"
	if r, ok := src.(*remoteSource); ok {
		source = r.cfg.User + "@" + r.cfg.Host
	}
	if size <= 0 {
		if sz, err := entrySize(ctx, src, p); err == nil {
			size = sz
		}
	}
	if size > 0 {
		if raw := s.cache.get(source, p, size, tag); raw != nil {
			if progress != nil {
				progress(size, size)
			}
			return raw, true, nil
		}
	}
	raw, err := src.MakeTorrent(ctx, p, tag, progress)
	if err != nil {
		return nil, false, err
	}
	if t, perr := parseTorrent(raw); perr == nil {
		s.cache.put(source, p, t.Size, tag, raw)
	}
	return raw, false, nil
}
