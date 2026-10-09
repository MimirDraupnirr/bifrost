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
