package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSimilarity(t *testing.T) {
	if similarity("Marinette.2023.FRENCH.1080p.WEB.H265-GL0P.mkv", "Marinette.2023.FRENCH.1080p.WEB.10bits.EAC3.5.1.H265-GL0P") < 0.5 {
		t.Fatal("même release, score trop bas")
	}
	if similarity("Dune.Part.Two.2024", "Oppenheimer.2023") > 0.2 {
		t.Fatal("releases différentes, score trop haut")
	}
}

func TestCheckFiles(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "Rel", "Subs"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "Rel", "film.mkv"), make([]byte, 10), 0o644)
	_ = os.WriteFile(filepath.Join(root, "Rel", "Subs", "fr.srt"), make([]byte, 3), 0o644)
	tor := &Torrent{Name: "Rel", Files: []TorrentFile{{Path: "film.mkv", Size: 10}, {Path: "Subs/fr.srt", Size: 3}}}
	if m := checkFiles(root, tor); len(m) != 0 {
		t.Fatalf("tout devrait être là : %v", m)
	}
	tor.Files[0].Size = 11
	tor.Files = append(tor.Files, TorrentFile{Path: "absent.nfo", Size: 1})
	if m := checkFiles(root, tor); len(m) != 2 {
		t.Fatalf("deux problèmes attendus : %v", m)
	}
	single := &Torrent{Name: "film.mkv", Files: []TorrentFile{{Path: "film.mkv", Size: 10}}}
	if m := checkFiles(filepath.Join(root, "Rel"), single); len(m) != 0 {
		t.Fatalf("fichier seul : %v", m)
	}
}
