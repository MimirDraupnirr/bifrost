package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTorrentCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rel := filepath.Join(dir, "Rel")
	_ = os.MkdirAll(rel, 0o755)
	_ = os.WriteFile(filepath.Join(rel, "a.mkv"), make([]byte, 5000), 0o644)
	s := &server{cache: &torrentCache{dir: filepath.Join(dir, "cache")}}
	calls := 0
	raw1, hit1, err := s.makeTorrentCached(context.Background(), localSource{}, rel, "DRAUPNIRR", 0, func(_, _ int64) { calls++ })
	if err != nil || hit1 || calls == 0 {
		t.Fatalf("premier passage : hachage attendu (%v, hit=%v, calls=%d)", err, hit1, calls)
	}
	raw2, hit2, err := s.makeTorrentCached(context.Background(), localSource{}, rel, "DRAUPNIRR", 0, nil)
	if err != nil || !hit2 || string(raw1) != string(raw2) {
		t.Fatalf("second passage : cache attendu (%v, hit=%v)", err, hit2)
	}
	// Les données changent de taille → le cache est ignoré.
	_ = os.WriteFile(filepath.Join(rel, "a.mkv"), make([]byte, 6000), 0o644)
	_, hit3, _ := s.makeTorrentCached(context.Background(), localSource{}, rel, "DRAUPNIRR", 0, nil)
	if hit3 {
		t.Fatal("taille différente : le cache ne doit pas servir")
	}
	// Autre tag source → autre entrée.
	_, hit4, _ := s.makeTorrentCached(context.Background(), localSource{}, rel, "AUTRE", 0, nil)
	if hit4 {
		t.Fatal("autre tag : pas de cache")
	}
}
