package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
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
	evChoice     histKind = "choice" // œuvre ou édition choisie à la main ; Choice nil = choix oublié
)

// tmdbKind : type d'œuvre TMDB, tel que l'API et Draupnirr l'écrivent.
type tmdbKind string

const (
	kindMovie tmdbKind = "movie"
	kindTV    tmdbKind = "tv"
)

func (k tmdbKind) valid() bool { return k == kindMovie || k == kindTV }

// workChoice : l'œuvre TMDB ou l'édition MusicBrainz qu'un membre a choisie
// pour une release ; le lot la prend telle quelle au lieu de chercher.
type workChoice struct {
	TMDBID        int      `json:"tmdb_id,omitempty"`
	TMDBType      tmdbKind `json:"tmdb_type,omitempty"`
	Title         string   `json:"title"`
	Year          int      `json:"year,omitempty"`
	Overview      string   `json:"overview,omitempty"`
	PosterURL     string   `json:"poster_url,omitempty"`
	MusicBrainzID string   `json:"musicbrainz_id,omitempty"`
	Artist        string   `json:"artist,omitempty"`
	// Fiche corrigée dans la loupe : saison et facettes retouchées (langues,
	// source…), qui priment sur ce que le nom et MediaInfo donnent.
	Episode string            `json:"episode,omitempty"`
	Facets  map[string]string `json:"facets,omitempty"`
	// Category : sous-catégorie choisie (série animée…) au lieu de celle du lot.
	Category string `json:"category,omitempty"`
	// Ignored : écartée à la main ; les lots la sautent sans la hacher.
	Ignored bool `json:"ignored,omitempty"`
}

type histEvent struct {
	At       time.Time   `json:"at"`
	Kind     histKind    `json:"kind"`
	Batch    string      `json:"batch,omitempty"`
	Source   string      `json:"source,omitempty"` // "local" ou user@host, comme cache.go
	Path     string      `json:"path,omitempty"`
	Name     string      `json:"name,omitempty"`
	Size     int64       `json:"size,omitempty"`
	InfoHash string      `json:"infohash,omitempty"`
	Status   rowStatus   `json:"status,omitempty"`
	Detail   string      `json:"detail,omitempty"`
	Category string      `json:"category,omitempty"`
	Built    string      `json:"built,omitempty"`
	TMDB     string      `json:"tmdb,omitempty"`
	Edition  string      `json:"edition,omitempty"`
	ID       string      `json:"id,omitempty"`
	URL      string      `json:"url,omitempty"`
	DryRun   bool        `json:"dry_run,omitempty"`
	Choice   *workChoice `json:"choice,omitempty"`
	Data     any         `json:"data,omitempty"` // options du lot, compteurs
}

type history struct {
	mu   sync.Mutex
	path string
	last map[string]histEvent // source|path → dernière décision, publication ou choix
	// source|path → choix du membre encore valable (œuvre ou édition forcée).
	choices map[string]histEvent
	// Dernière ligne tronquée (arrêt brutal) : le prochain ajout repart à la ligne.
	needNL bool
}

func newHistory(configPath string) *history {
	h := &history{path: filepath.Join(filepath.Dir(configPath), "history.jsonl"), last: map[string]histEvent{}, choices: map[string]histEvent{}}
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

// remember : décisions, publications et choix font l'état d'une release.
// Data reste dans le fichier : la liste du lot n'en a pas besoin. Le choix
// garde son œuvre à part, pour le lot suivant.
func (h *history) remember(e histEvent) {
	if (e.Kind != evDecision && e.Kind != evPublish && e.Kind != evChoice) || e.Path == "" {
		return
	}
	e.Data = nil
	k := histKey(e.Source, e.Path)
	if e.Kind == evChoice {
		if e.Choice == nil {
			delete(h.choices, k)
		} else {
			h.choices[k] = e
		}
	}
	e.Choice = nil
	h.last[k] = e
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

// choiceFor : l'œuvre choisie à la main pour ce chemin, si la taille n'a pas changé.
func (h *history) choiceFor(source, p string, size int64) *workChoice {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.choices[histKey(source, p)]
	if !ok || (size > 0 && e.Size > 0 && e.Size != size) {
		return nil
	}
	c := *e.Choice
	c.Facets = maps.Clone(c.Facets)
	return &c
}

// sourceName : la source des fichiers telle que l'historique et le cache la nomment.
func sourceName(src fileSource) string {
	if r, ok := src.(*remoteSource); ok {
		return r.cfg.User + "@" + r.cfg.Host
	}
	return "local"
}
