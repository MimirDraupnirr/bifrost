package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExcludesMatch(t *testing.T) {
	x := excludes{"*.trickplay", "extrafanart", "*.NFO"}
	for name, want := range map[string]bool{
		"Heat.1995.1080p.MULTi.BluRay.x264-NOWiNHD.trickplay": true,
		"ExtraFanart":       true,
		"film.nfo":          true,
		"Heat.1995.mkv":     false,
		"extrafanart-bonus": false,
	} {
		if x.match(name) != want {
			t.Errorf("%s : %v attendu", name, want)
		}
	}
	if _, err := (excludes{"[abc"}).clean(); err == nil {
		t.Fatal("motif mal formé accepté")
	}
	if got, _ := (excludes{" extrathumbs ", "", "  "}).clean(); len(got) != 1 || got[0] != "extrathumbs" {
		t.Fatalf("nettoyage : %q", got)
	}
}

// Liste, taille et torrent écartent les mêmes fichiers : la taille listée
// reste celle du torrent (cache, plafond, /api/torrents/match).
func TestExcludesKeepSizesAndTorrentInStep(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join(root, "Heat.1995.1080p")
	write := func(p string, n int) {
		full := filepath.Join(rel, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Heat.1995.1080p.mkv", 1000)
	write("Heat.1995.1080p.nfo", 10)
	write("Heat.1995.1080p.trickplay/320 - 10x10/0.jpg", 300)
	write("extrafanart/a.jpg", 200)
	write("Extras/extrathumbs/thumb1.jpg", 50) // à toute profondeur
	write("Extras/bonus.mkv", 100)

	skip := defaultExcludes
	if got := dirSize(rel, skip); got != 1110 {
		t.Fatalf("taille : %d, attendu 1110", got)
	}
	raw, err := makeTorrent(rel, "DRAUPNIRR", skip, nil)
	if err != nil {
		t.Fatal(err)
	}
	tor, err := parseTorrent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tor.Size != 1110 || len(tor.Files) != 3 {
		t.Fatalf("torrent : %d octets, %d fichiers", tor.Size, len(tor.Files))
	}
	entries, err := listDir(rel, skip)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "Extras,Heat.1995.1080p.mkv,Heat.1995.1080p.nfo" {
		t.Fatalf("liste : %v", names)
	}
	if got := dirSize(rel, nil); got != 1660 {
		t.Fatalf("sans motif, tout compte : %d", got)
	}
}

func TestAgentExcludes(t *testing.T) {
	args, x := agentExcludes([]string{"mktorrent", "--exclude", "*.trickplay", "--source", "DRAUPNIRR", "--exclude", "extrafanart", "/d/Heat"})
	if strings.Join(args, " ") != "mktorrent --source DRAUPNIRR /d/Heat" || len(x) != 2 || x[1] != "extrafanart" {
		t.Fatalf("%q %q", args, x)
	}
	if got := (excludes{"*.trickplay", "it's"}).flags(); got != `--exclude '*.trickplay' --exclude 'it'\''s' ` {
		t.Fatalf("flags : %s", got)
	}
}

func TestSettingsExclude(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg, err := loadConfig(cfgPath)
	if err != nil || strings.Join(cfg.Exclude, ",") != "*.trickplay,extrafanart,extrathumbs" {
		t.Fatalf("motifs par défaut : %q %v", cfg.Exclude, err)
	}
	s := newServer(cfg, cfgPath)
	post := func(body string) int {
		rec := httptest.NewRecorder()
		s.settings(rec, httptest.NewRequest("POST", "/ui/settings", strings.NewReader(body)))
		return rec.Code
	}
	if code := post(`{"exclude":["[abc"]}`); code != 400 || len(s.cfg.Exclude) != 3 {
		t.Fatalf("motif invalide : %d, %q", code, s.cfg.Exclude)
	}
	if code := post(`{"exclude":[]}`); code != 200 {
		t.Fatalf("liste vide : %d", code)
	}
	if again, _ := loadConfig(cfgPath); again.Exclude == nil || len(again.Exclude) != 0 {
		t.Fatalf("une liste vidée doit le rester au rechargement : %q", again.Exclude)
	}
	post(`{"exclude":["extrafanart"]}`)
	if src, ok := s.source().(localSource); !ok || !src.skip.match("extrafanart") {
		t.Fatal("la source ne reçoit pas les motifs enregistrés")
	}
}
