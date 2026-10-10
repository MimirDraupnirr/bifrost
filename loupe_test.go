package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobMediaInfo(t *testing.T) {
	s := newServer(&Config{Source: "local"}, filepath.Join(t.TempDir(), "config.json"))
	s.jobs["a"] = &job{ID: "a", MediaInfo: `{"media":{"@ref":"/d/x.mkv"}}`}
	s.jobs["b"] = &job{ID: "b"}
	get := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/ui/job/"+id+"/mediainfo", nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		s.jobMediaInfo(rec, req)
		return rec
	}
	if rec := get("a"); rec.Code != 200 || rec.Body.String() != `{"media":{"@ref":"/d/x.mkv"}}` || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("rapport : %d %q", rec.Code, rec.Body)
	}
	if rec := get("b"); rec.Code != 404 || !strings.Contains(rec.Body.String(), "absent") {
		t.Fatalf("sans rapport : %d %s", rec.Code, rec.Body)
	}
	if rec := get("z"); rec.Code != 404 {
		t.Fatalf("préparation inconnue : %d", rec.Code)
	}
}

func TestListDirSizesSkipsFolders(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "Rel"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "Rel", "a.mkv"), make([]byte, 5000), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.mkv"), make([]byte, 300), 0o644)
	size := func(sizes bool) map[string]int64 {
		es, err := listDirSizes(dir, sizes)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]int64{}
		for _, e := range es {
			m[e.Name] = e.Size
		}
		return m
	}
	if m := size(true); m["Rel"] != 5000 || m["b.mkv"] != 300 {
		t.Fatalf("avec tailles : %v", m)
	}
	if m := size(false); m["Rel"] != 0 || m["b.mkv"] != 300 {
		t.Fatalf("dossiers sans taille, fichiers avec : %v", m)
	}
}

func TestSettingsBatchIntroHidden(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	s := newServer(&Config{Source: "local"}, cfgPath)
	post := func(body string) {
		rec := httptest.NewRecorder()
		s.settings(rec, httptest.NewRequest("POST", "/ui/settings", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("%s : %d %s", body, rec.Code, rec.Body)
		}
	}
	post(`{"batch_intro_hidden":true}`)
	raw, _ := os.ReadFile(cfgPath)
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil || !c.BatchIntroHidden {
		t.Fatalf("repliée, gardée dans config.json : %v %s", err, raw)
	}
	post(`{"auto_update":true}`) // un autre réglage ne la touche pas
	if !s.cfg.BatchIntroHidden {
		t.Fatal("état perdu par un autre réglage")
	}
	post(`{"batch_intro_hidden":false}`)
	if s.cfg.BatchIntroHidden {
		t.Fatal("dépliée de nouveau")
	}
}

func TestBatchListCarriesEpisode(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Show.S02E01-E02.MULTi", "Show S3 FRENCH", "Film.2019.1080p"} {
		_ = os.MkdirAll(filepath.Join(dir, n), 0o755)
		_ = os.WriteFile(filepath.Join(dir, n, "a.mkv"), make([]byte, 100), 0o644)
	}
	s := newServer(&Config{Source: "local"}, filepath.Join(t.TempDir(), "config.json"))
	rec := httptest.NewRecorder()
	s.batchListHandler(rec, httptest.NewRequest("GET", "/ui/batch/list?path="+dir, nil))
	var out struct {
		Entries []struct {
			Name    string `json:"name"`
			Episode string `json:"episode"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range out.Entries {
		got[e.Name] = e.Episode
	}
	want := map[string]string{"Show.S02E01-E02.MULTi": "S02E01-E02", "Show S3 FRENCH": "S03", "Film.2019.1080p": ""}
	for n, w := range want {
		if got[n] != w {
			t.Errorf("%s : %q, attendu %q", n, got[n], w)
		}
	}
}

func TestBatchDescriptionFollowsChosenWork(t *testing.T) {
	// Devinée comme un film, choisie comme série : la présentation suit le choix et sa saison.
	a := &analysis{Name: "X.1080p.WEB-GRP", GuessedType: "movie"}
	pick := &tmdbResult{ID: 7, Title: "X", Year: float64(2023)}
	fam := "series"
	tpl := []presTemplate{{Body: "{{type}} {{episode}}", Format: "bbcode", Family: &fam, IsDefault: true}}
	if desc, _ := batchDescription(a, pick, kindTV, "S02", tpl, "series-serie-tv", "nra"); desc != "Série S02" {
		t.Fatalf("présentation : %q", desc)
	}
	if desc, _ := batchDescription(a, pick, kindTV, "S02", nil, "series-serie-tv", "nra"); !strings.Contains(desc, "themoviedb.org/tv/7") {
		t.Fatalf("lien TMDB de la série : %s", desc)
	}
}
