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
	if h := magnetTrackers("magnet:?xt=urn:btih:x&tr=https%3A%2F%2Fdraupnirr.xyz%2Fannounce%2Fk&tr=udp%3A%2F%2Ft.example%3A80"); len(h) != 2 || h[0] != "draupnirr.xyz" {
		t.Fatalf("magnet : %v", h)
	}
}
