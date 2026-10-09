package main

import "testing"

func TestGroupCrossSeeds(t *testing.T) {
	list := []ClientTorrent{
		{Hash: "a", Name: "Rel", Path: "/d/Rel", Progress: 1, Trackers: []string{"tracker.other"}},
		{Hash: "b", Name: "Rel", Path: "/d/Rel", Progress: 1, Trackers: []string{"draupnirr.xyz"}},
		{Hash: "c", Name: "Autre", Path: "/d/Autre", Progress: 0.5},
	}
	out := groupCrossSeeds(list, "https://draupnirr.xyz")
	if len(out) != 2 || out[0].Copies != 2 || !out[0].OnDraupnirr || out[0].Hash != "a" || out[1].OnDraupnirr {
		t.Fatalf("groupement : %+v", out)
	}
	if len(out[0].Hashes) != 2 || out[0].Hashes[1] != "b" {
		t.Fatalf("les infohash des copies fusionnées doivent être conservés : %v", out[0].Hashes)
	}
	if h := magnetTrackers("magnet:?xt=urn:btih:x&tr=https%3A%2F%2Fdraupnirr.xyz%2Fannounce%2Fk&tr=udp%3A%2F%2Ft.example%3A80"); len(h) != 2 || h[0] != "draupnirr.xyz" {
		t.Fatalf("magnet : %v", h)
	}
}

func TestMapPath(t *testing.T) {
	maps := []PathMap{
		{From: "/media/D_Test", To: `D:\Test`},
		{From: "/media/D_Test/Séries", To: `E:\Séries`},
		{From: "/data", To: "/home/moi/downloads/"},
	}
	cases := []struct {
		in       string
		toClient bool
		want     string
	}{
		{"/media/D_Test", true, `D:\Test`},
		{"/media/D_Test/Film (2024)", true, `D:\Test\Film (2024)`},
		{"/media/D_Test/Séries/Show S01", true, `E:\Séries\Show S01`}, // le plus long gagne
		{"/media/D_Test2/Film", true, "/media/D_Test2/Film"},          // pas à une frontière
		{"/data/Film", true, "/home/moi/downloads/Film"},
		{"/ailleurs/Film", true, "/ailleurs/Film"},
		{`D:\Test\Film (2024)`, false, "/media/D_Test/Film (2024)"},
		{`d:\test\Film`, false, "/media/D_Test/Film"}, // Windows : casse ignorée
		{`D:\Test/Film`, false, "/media/D_Test/Film"}, // séparateurs mêlés
		{"/home/moi/downloads/Film", false, "/data/Film"},
		{"/home/moi/Downloads/Film", false, "/home/moi/Downloads/Film"}, // Linux : casse respectée
	}
	for _, c := range cases {
		if got := mapPath(c.in, maps, c.toClient); got != c.want {
			t.Errorf("mapPath(%q, client=%v) = %q, attendu %q", c.in, c.toClient, got, c.want)
		}
	}
}
