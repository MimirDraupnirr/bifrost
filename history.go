package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Historique : ce que Bifröst a fait et décidé (lots, décisions, publications,
// cross-seeds), une ligne JSON par événement dans history.jsonl, à côté de
// config.json. Ajout seul, aucune dépendance ; la dernière décision par
// release est gardée en mémoire pour la liste à cocher du lot. Une erreur
// d'écriture ne bloque jamais le flux : au pire, l'historique manque.
// Jamais de secret ici (ni jeton, ni mot de passe).
// ponytail: pas de purge — quelques centaines d'octets par release ; tronquer
// le fichier à la main s'il devient encombrant.

type histKind string

const (
	evBatchStart histKind = "batch_start"
	evBatchEnd   histKind = "batch_end"
	evDecision   histKind = "decision"
	evPublish    histKind = "publish"
	evCrossSeed  histKind = "cross_seed"
)

type histEvent struct {
	At       time.Time `json:"at"`
	Kind     histKind  `json:"kind"`
	Batch    string    `json:"batch,omitempty"`
	Source   string    `json:"source,omitempty"` // "local" ou user@host, comme cache.go
	Path     string    `json:"path,omitempty"`
	Name     string    `json:"name,omitempty"`
	Size     int64     `json:"size,omitempty"`
	InfoHash string    `json:"infohash,omitempty"`
	Status   rowStatus `json:"status,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	Category string    `json:"category,omitempty"`
	Built    string    `json:"built,omitempty"`
	TMDB     string    `json:"tmdb,omitempty"`
	Edition  string    `json:"edition,omitempty"`
	ID       string    `json:"id,omitempty"`
	URL      string    `json:"url,omitempty"`
	DryRun   bool      `json:"dry_run,omitempty"`
	Data     any       `json:"data,omitempty"` // options du lot, compteurs, choix du membre
}

type history struct {
	mu   sync.Mutex
	path string
	last map[string]histEvent // source|path → dernière décision ou publication
	// Dernière ligne tronquée (arrêt brutal) : le prochain ajout repart à la ligne.
	needNL bool
}

func newHistory(configPath string) *history {
	h := &history{path: filepath.Join(filepath.Dir(configPath), "history.jsonl"), last: map[string]histEvent{}}
	h.load()
	return h
}

func histKey(source, p string) string { return source + "|" + p }

func (h *history) load() {
	f, err := os.Open(h.path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var e histEvent
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			h.remember(e)
		}
	}
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		b := make([]byte, 1)
		if _, err := f.ReadAt(b, info.Size()-1); err == nil && b[0] != '\n' {
			h.needNL = true
		}
	}
}

// remember : seules les décisions et publications font l'état d'une release.
// Data (les choix du membre) reste dans le fichier : la liste du lot n'en a pas besoin.
func (h *history) remember(e histEvent) {
	if (e.Kind == evDecision || e.Kind == evPublish) && e.Path != "" {
		e.Data = nil
		h.last[histKey(e.Source, e.Path)] = e
	}
}

func (h *history) record(e histEvent) {
	if h == nil {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.remember(e)
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "historique :", err)
		return
	}
	f, err := os.OpenFile(h.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "historique :", err)
		return
	}
	defer f.Close()
	if h.needNL {
		line = append([]byte{'\n'}, line...)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "historique :", err)
		return
	}
	h.needNL = false
}

// lastFor : la dernière décision connue pour ce chemin, si la taille n'a pas
// changé depuis (sinon ce n'est plus la même release).
func (h *history) lastFor(source, p string, size int64) *histEvent {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.last[histKey(source, p)]
	if !ok || (size > 0 && e.Size > 0 && e.Size != size) {
		return nil
	}
	return &e
}

// sourceName : la source des fichiers telle que l'historique et le cache la nomment.
func sourceName(src fileSource) string {
	if r, ok := src.(*remoteSource); ok {
		return r.cfg.User + "@" + r.cfg.Host
	}
	return "local"
}
