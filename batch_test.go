package main

import (
	"encoding/json"
	"testing"
)

func TestPickWorkAndDecide(t *testing.T) {
	a := &analysis{OK: true, CleanTitle: "Marinette", Year: float64(2023), GuessedType: "movie",
		Nomenclature: &nomenclature{BuiltName: "Marinette.2023.FRENCH.1080p.WEB.H265-GL0P"}}
	results := []tmdbResult{{ID: 1, Title: "Avec Marinette", Year: float64(1999)}, {ID: 2, Title: "Marinette", Year: float64(2023)}}
	// Titre français différent, original identique : retenu quand même.
	a2 := &analysis{OK: true, CleanTitle: "American Fiction", Year: float64(2023)}
	if p, _ := pickWork(a2, []tmdbResult{{ID: 9, Title: "Fiction à l'américaine", OriginalTitle: "American Fiction", Year: float64(2023)}}); p == nil || p.ID != 9 {
		t.Fatal("le titre original doit compter")
	}
	a3 := &analysis{OK: true, CleanTitle: "Dont Get Out", Year: float64(2018)}
	if p, _ := pickWork(a3, []tmdbResult{{ID: 8, Title: "Don't. Get. Out!", OriginalTitle: "Don't. Get. Out!", Year: float64(2018)}}); p == nil {
		t.Fatal("apostrophes et ponctuation ne doivent pas compter")
	}
	pick, why := pickWork(a, results)
	if pick == nil || pick.ID != 2 || why != "" {
		t.Fatalf("choix : %+v %q", pick, why)
	}
	if ok, _ := decide(a, pick, ""); !ok {
		t.Fatal("publiable attendu")
	}
	a.Nomenclature.Missing = []string{"Langues"}
	if ok, why := decide(a, pick, ""); ok || why == "" {
		t.Fatal("facette manquante → à revoir")
	}
	a.Nomenclature.Missing = nil
	a.Warnings = []string{"Ce torrent existe déjà sur Draupnirr (même infohash)."}
	if ok, _ := decide(a, pick, ""); ok {
		t.Fatal("doublon → à revoir")
	}
	a.Warnings = nil
	a.Year = nil
	if p, why := pickWork(a, results); p != nil || why == "" {
		t.Fatal("sans année, pas de choix automatique")
	}
	a.Year = float64(2023)
	if p, _ := pickWork(a, []tmdbResult{{ID: 3, Title: "Marinette", Year: float64(2010)}}); p != nil {
		t.Fatal("mauvaise année → pas de choix")
	}
	if ok, why := decide(a, nil, "œuvre introuvable sur TMDB"); ok || why != "œuvre introuvable sur TMDB" {
		t.Fatal("sans œuvre → à revoir avec la raison")
	}
}

func TestBatchDescriptionFallsBackToDefault(t *testing.T) {
	a := &analysis{Name: "X.2023.1080p.WEB-GRP", SizeHuman: "1 Go", FileCount: 1, GuessedType: "movie"}
	pick := &tmdbResult{ID: 7, Title: "X", Year: float64(2023), Overview: "Synopsis.", PosterURL: "https://img/p.jpg"}
	desc, format := batchDescription(a, pick, nil, "films-film")
	if format != "bbcode" || !contains(desc, "[b]X[/b]") || !contains(desc, "themoviedb.org/movie/7") || contains(desc, "{{") {
		t.Fatalf("description par défaut : %s", desc)
	}
	fam := "films"
	tpl := map[string]presTemplate{"films": {Body: "Mon modèle {{titre}} {{annee}}", Format: "html", Family: &fam, IsDefault: true}}
	desc, format = batchDescription(a, pick, tpl, "films-film")
	if format != "html" || desc != "Mon modèle X 2023" {
		t.Fatalf("modèle du membre : %q %s", desc, format)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestLooseMapAcceptsEmptyPHPArray(t *testing.T) {
	var a analysis
	raw := `{"ok":true,"nomenclature":{"built_name":"X","missing":[],"name_facets":[],"facets":[],"media":{"duration":"1h32"}}}`
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("un tableau vide PHP doit passer : %v", err)
	}
	if a.Nomenclature.Media["duration"] != "1h32" || len(a.Nomenclature.NameFacets) != 0 {
		t.Fatalf("décodage : %+v", a.Nomenclature)
	}
	raw = `{"ok":true,"nomenclature":{"name_facets":{"source":"WEB"},"facets":{"source":{"value":"WEB","origin":"declared"}}}}`
	if err := json.Unmarshal([]byte(raw), &a); err != nil || a.Nomenclature.NameFacets["source"] != "WEB" || a.Nomenclature.Facets["source"].Origin != "declared" {
		t.Fatalf("objet normal : %v %+v", err, a.Nomenclature)
	}
}
