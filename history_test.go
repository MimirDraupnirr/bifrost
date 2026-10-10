package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryRecordReload(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	h := newHistory(cfg)
	h.record(histEvent{Kind: evBatchStart, Batch: "b1", Path: "/data"})
	h.record(histEvent{Kind: evDecision, Source: "local", Path: "/data/A", Name: "A", Size: 10, Status: stReview, Detail: "année absente"})
	h.record(histEvent{Kind: evDecision, Source: "local", Path: "/data/B", Name: "B", Size: 20, Status: stSimulated})
	h.record(histEvent{Kind: evPublish, Source: "local", Path: "/data/A", Name: "A", Size: 10, Status: stPublished, ID: "42", Data: map[string]any{"work_title": "A"}})

	if info, err := os.Stat(h.path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("history.jsonl attendu en 0600 : %v %v", info, err)
	}
	if raw, _ := os.ReadFile(h.path); !strings.Contains(string(raw), `"work_title"`) {
		t.Fatal("les choix du membre restent dans le fichier, seule la mémoire les oublie")
	}
	// Relu depuis le disque : la dernière décision l'emporte.
	h2 := newHistory(cfg)
	if e := h2.lastFor("local", "/data/A", 10); e == nil || e.Status != stPublished || e.ID != "42" || e.Data != nil {
		t.Fatalf("dernière décision de A : %+v", e)
	}
	if e := h2.lastFor("local", "/data/A", 11); e != nil {
		t.Fatal("taille différente : ce n'est plus la même release")
	}
	if e := h2.lastFor("me@box", "/data/A", 10); e != nil {
		t.Fatal("autre source : rien de connu")
	}
	if e := h2.lastFor("local", "/data/B", 20); e == nil || e.Status != stSimulated {
		t.Fatalf("B : %+v", e)
	}
}

func TestHistoryIgnoresBrokenLines(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	path := filepath.Join(filepath.Dir(cfg), "history.jsonl")
	line := `{"at":"2026-10-09T10:00:00Z","kind":"decision","source":"local","path":"/x","size":5,"status":"erreur"}`
	if err := os.WriteFile(path, []byte("pas du json\n"+line+"\n{tronqué"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHistory(cfg)
	if e := h.lastFor("local", "/x", 5); e == nil || e.Status != stError {
		t.Fatalf("ligne valide perdue : %+v", e)
	}
	h.record(histEvent{Kind: evDecision, Source: "local", Path: "/y", Size: 1, Status: stSimulated})
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "\n{\"at\"") {
		t.Fatalf("l'ajout doit repartir à la ligne : %q", raw)
	}
	if e := newHistory(cfg).lastFor("local", "/y", 1); e == nil {
		t.Fatal("ajout après une ligne tronquée illisible")
	}
}

func TestHistoryNilIsSilent(t *testing.T) {
	var h *history
	h.record(histEvent{Kind: evDecision})
	if h.lastFor("local", "/x", 1) != nil {
		t.Fatal("un historique absent ne renvoie rien")
	}
}

func TestBatchListCarriesLastDecision(t *testing.T) {
	dir := t.TempDir()
	rel := filepath.Join(dir, "Rel")
	_ = os.MkdirAll(rel, 0o755)
	_ = os.WriteFile(filepath.Join(rel, "a.mkv"), make([]byte, 5000), 0o644)
	s := newServer(&Config{Source: "local"}, filepath.Join(t.TempDir(), "config.json"))
	s.hist.record(histEvent{Kind: evPublish, Source: "local", Path: rel, Size: 5000, Status: stPublished, URL: "https://x/torrents/1"})

	rec := httptest.NewRecorder()
	s.batchListHandler(rec, httptest.NewRequest("GET", "/ui/batch/list?path="+dir, nil))
	var out struct {
		Entries []struct {
			Path string     `json:"path"`
			Last *histEvent `json:"last"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Entries) != 1 {
		t.Fatalf("liste : %v %s", err, rec.Body)
	}
	if l := out.Entries[0].Last; l == nil || l.Status != stPublished {
		t.Fatalf("dernière décision absente : %s", rec.Body)
	}
}

func TestBatchChoiceIsRememberedAndForgotten(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	s := newServer(&Config{Source: "local"}, cfg)
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.batchChoiceHandler(rec, httptest.NewRequest("POST", "/ui/batch/choice", strings.NewReader(body)))
		return rec
	}
	if rec := post(`{"path":"/d/Black Lagoon","size":10,"choice":{"tmdb_id":37854,"title":"Black Lagoon"}}`); rec.Code != 400 {
		t.Fatalf("type TMDB manquant : %d", rec.Code)
	}
	if rec := post(`{"path":"/d/Black Lagoon","name":"Black Lagoon","size":10,"choice":{"tmdb_id":37854,"tmdb_type":"tv","title":"Black Lagoon","year":2006}}`); rec.Code != 200 {
		t.Fatalf("choix refusé : %d %s", rec.Code, rec.Body)
	}
	// Relu depuis le disque : la ligne est « revu » et le lot reprend l'œuvre.
	h := newHistory(cfg)
	if e := h.lastFor("local", "/d/Black Lagoon", 10); e == nil || e.Status != stReviewed || e.TMDB != "Black Lagoon (2006)" || e.Choice != nil {
		t.Fatalf("état après choix : %+v", e)
	}
	if c := h.choiceFor("local", "/d/Black Lagoon", 10); c == nil || c.TMDBID != 37854 || c.TMDBType != "tv" {
		t.Fatalf("œuvre choisie : %+v", c)
	}
	if h.choiceFor("local", "/d/Black Lagoon", 11) != nil {
		t.Fatal("taille différente : le choix ne vaut plus")
	}
	// Une décision du lot ne fait pas oublier le choix.
	s.hist.record(histEvent{Kind: evDecision, Source: "local", Path: "/d/Black Lagoon", Size: 10, Status: stReviewed, Detail: "simulé, publiable : X"})
	if s.hist.choiceFor("local", "/d/Black Lagoon", 10) == nil {
		t.Fatal("choix perdu après une simulation")
	}
	post(`{"path":"/d/Black Lagoon","size":10}`)
	if s.hist.choiceFor("local", "/d/Black Lagoon", 10) != nil {
		t.Fatal("choix oublié mais encore appliqué")
	}
	if e := newHistory(cfg).lastFor("local", "/d/Black Lagoon", 10); e == nil || e.Status != stReview {
		t.Fatalf("après oubli : %+v", e)
	}
}

func TestReviewedStatusKeepsHumanChoices(t *testing.T) {
	for _, c := range []struct {
		in     rowStatus
		chosen bool
		want   rowStatus
	}{
		{stSimulated, true, stReviewed}, {stReview, true, stReviewed}, {stPublished, true, stPublished},
		{stError, true, stError}, {stSimulated, false, stSimulated},
	} {
		if got, _ := reviewedStatus(c.in, "x", c.chosen); got != c.want {
			t.Errorf("%s (choisi %v) : %s, attendu %s", c.in, c.chosen, got, c.want)
		}
	}
}

func TestEpisodeOf(t *testing.T) {
	for in, want := range map[string]string{
		"Alexandra.Ehle.S06.FRENCH.AD.1080p.WEB.H264-THESYNDiCATE": "S06",
		"Show.s01e03.720p":        "S01E03",
		"Show.S02E01-E02.MULTi":   "S02E01-E02",
		"Show S3 FRENCH":          "S03",
		"Film.2019.1080p.SSE":     "",
		"Blade.Runner.2049.1080p": "",
	} {
		if got := episodeOf(in); got != want {
			t.Errorf("episodeOf(%q) = %q, veut %q", in, got, want)
		}
	}
}
