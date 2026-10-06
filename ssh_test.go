package main

import "testing"

func TestShellQuote(t *testing.T) {
	if got := shellQuote("L'été/2026"); got != `'L'\''été/2026'` {
		t.Fatalf("quote : %s", got)
	}
}

func TestRemoteMainFile(t *testing.T) {
	r := &remoteSource{}
	multi := &Torrent{Name: "Rel", Files: []TorrentFile{{Path: "Subs/fr.srt", Size: 1}, {Path: "film.mkv", Size: 9}}}
	if got := r.MainFile("/data/Rel", multi); got != "/data/Rel/film.mkv" {
		t.Fatalf("multi : %s", got)
	}
	single := &Torrent{Name: "film.mkv", Files: []TorrentFile{{Path: "film.mkv", Size: 9}}}
	if got := r.MainFile("/data/film.mkv", single); got != "/data/film.mkv" {
		t.Fatalf("single : %s", got)
	}
}

func TestEmbeddedAgentMissingIsExplicit(t *testing.T) {
	if _, err := embeddedAgent("mips"); err == nil {
		t.Fatal("une architecture inconnue doit échouer clairement")
	}
}
