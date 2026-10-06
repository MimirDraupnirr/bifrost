package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestBencodeRoundTrip(t *testing.T) {
	in := map[string]any{"b": int64(42), "a": "x", "l": []any{"y", int64(1)}, "d": map[string]any{"k": "v"}}
	var buf bytes.Buffer
	if err := bencode(&buf, in); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "d1:a1:x1:bi42e1:dd1:k1:ve1:ll1:yi1eee" {
		t.Fatalf("encodage inattendu : %s", got)
	}
	out, err := bdecode(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var again bytes.Buffer
	_ = bencode(&again, out)
	if !bytes.Equal(buf.Bytes(), again.Bytes()) {
		t.Fatal("aller-retour non stable")
	}
}

func TestMakeParseReseal(t *testing.T) {
	dir := t.TempDir()
	rel := filepath.Join(dir, "Une.Release.2026")
	_ = os.MkdirAll(filepath.Join(rel, "Subs"), 0o755)
	_ = os.WriteFile(filepath.Join(rel, "film.mkv"), bytes.Repeat([]byte{7}, 40_000), 0o644)
	_ = os.WriteFile(filepath.Join(rel, "Subs", "fr.srt"), []byte("sous-titres"), 0o644)

	var last int64
	raw, err := makeTorrent(rel, "DRAUPNIRR", func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	if last != 40_011 {
		t.Fatalf("progression finale %d", last)
	}
	tor, err := parseTorrent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tor.Name != "Une.Release.2026" || tor.Size != 40_011 || len(tor.Files) != 2 || !tor.Private || tor.Source != "DRAUPNIRR" {
		t.Fatalf("torrent lu : %+v", tor)
	}
	if tor.Files[1].Path != "film.mkv" || tor.Files[0].Path != "Subs/fr.srt" {
		t.Fatalf("chemins : %+v", tor.Files)
	}
	if got := mainFile(rel, tor); got != filepath.Join(rel, "film.mkv") {
		t.Fatalf("fichier principal : %s", got)
	}

	// L'infohash est bien le SHA-1 de l'info-dict tel qu'encodé.
	root, _ := bdecode(raw)
	var info bytes.Buffer
	_ = bencode(&info, root["info"])
	sum := sha1.Sum(info.Bytes())
	if tor.InfoHash != hex.EncodeToString(sum[:]) {
		t.Fatal("infohash incohérent")
	}

	// Re-sceller avec un autre tag change l'infohash et retire l'announce.
	withTracker := map[string]any{"announce": "http://ailleurs/annonce", "info": root["info"]}
	var buf bytes.Buffer
	_ = bencode(&buf, withTracker)
	resealed, err := resealTorrent(buf.Bytes(), "AUTRE")
	if err != nil {
		t.Fatal(err)
	}
	t2, _ := parseTorrent(resealed)
	if t2.Source != "AUTRE" || t2.InfoHash == tor.InfoHash || bytes.Contains(resealed, []byte("ailleurs")) {
		t.Fatalf("re-scellement : %+v", t2)
	}
}

func TestPieceLength(t *testing.T) {
	if pieceLength(1000) != 16*1024 {
		t.Fatal("petit fichier")
	}
	if pieceLength(25<<30) != 16*1024*1024 {
		t.Fatal("25 Go doit plafonner à 16 Mio")
	}
}
