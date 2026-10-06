package main

import "testing"

func TestRenderTemplate(t *testing.T) {
	body := "[b]{{titre}}[/b]{{#annee}} ({{annee}}){{/annee}}\n\n\n{{#synopsis}}[quote]{{synopsis}}[/quote]{{/synopsis}}  \n{{inconnue}}fin"
	got := renderTemplate(body, map[string]string{"titre": "Dune", "annee": "2024"})
	want := "[b]Dune[/b] (2024)\n\nfin"
	if got != want {
		t.Fatalf("rendu :\n%q\nattendu :\n%q", got, want)
	}
	if wrapCaptures("html", []string{"a", " ", "b"}) != `<img src="a" alt="">`+"\n"+`<img src="b" alt="">` {
		t.Fatal("captures html")
	}
	if wrapCaptures("bbcode", []string{"a"}) != "[img]a[/img]" {
		t.Fatal("captures bbcode")
	}
}
