package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ---- bencode : la stdlib n'en a pas, et il tient en 120 lignes. ----

func bencode(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case int64:
		fmt.Fprintf(buf, "i%de", x)
	case int:
		fmt.Fprintf(buf, "i%de", x)
	case string:
		fmt.Fprintf(buf, "%d:%s", len(x), x)
	case []byte:
		fmt.Fprintf(buf, "%d:", len(x))
		buf.Write(x)
	case []any:
		buf.WriteByte('l')
		for _, e := range x {
			if err := bencode(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys) // ordre lexicographique imposé par le format
		buf.WriteByte('d')
		for _, k := range keys {
			fmt.Fprintf(buf, "%d:%s", len(k), k)
			if err := bencode(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('e')
	default:
		return fmt.Errorf("bencode : type %T non géré", v)
	}
	return nil
}

type bdecoder struct {
	data []byte
	pos  int
}

func (d *bdecoder) next() (any, error) {
	if d.pos >= len(d.data) {
		return nil, errors.New("bencode : fin prématurée")
	}
	switch c := d.data[d.pos]; {
	case c == 'i':
		end := bytes.IndexByte(d.data[d.pos:], 'e')
		if end < 0 {
			return nil, errors.New("bencode : entier non terminé")
		}
		n, err := strconv.ParseInt(string(d.data[d.pos+1:d.pos+end]), 10, 64)
		if err != nil {
			return nil, err
		}
		d.pos += end + 1
		return n, nil
	case c == 'l':
		d.pos++
		list := []any{}
		for d.pos < len(d.data) && d.data[d.pos] != 'e' {
			v, err := d.next()
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		d.pos++
		return list, nil
	case c == 'd':
		d.pos++
		dict := map[string]any{}
		for d.pos < len(d.data) && d.data[d.pos] != 'e' {
			k, err := d.next()
			if err != nil {
				return nil, err
			}
			key, ok := k.(string)
			if !ok {
				return nil, errors.New("bencode : clé non textuelle")
			}
			v, err := d.next()
			if err != nil {
				return nil, err
			}
			dict[key] = v
		}
		d.pos++
		return dict, nil
	case c >= '0' && c <= '9':
		colon := bytes.IndexByte(d.data[d.pos:], ':')
		if colon < 0 {
			return nil, errors.New("bencode : chaîne sans longueur")
		}
		n, err := strconv.Atoi(string(d.data[d.pos : d.pos+colon]))
		if err != nil || n < 0 || d.pos+colon+1+n > len(d.data) {
			return nil, errors.New("bencode : longueur de chaîne invalide")
		}
		s := string(d.data[d.pos+colon+1 : d.pos+colon+1+n])
		d.pos += colon + 1 + n
		return s, nil
	}
	return nil, fmt.Errorf("bencode : octet inattendu %q", d.data[d.pos])
}

func bdecode(data []byte) (map[string]any, error) {
	d := &bdecoder{data: data}
	v, err := d.next()
	if err != nil {
		return nil, err
	}
	dict, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("bencode : la racine n'est pas un dictionnaire")
	}
	return dict, nil
}

// ---- lecture d'un .torrent ----

type TorrentFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type Torrent struct {
	Name     string        `json:"name"`
	Size     int64         `json:"size"`
	Files    []TorrentFile `json:"files"`
	Private  bool          `json:"private"`
	Source   string        `json:"source"`
	InfoHash string        `json:"infohash"`
}

func parseTorrent(raw []byte) (*Torrent, error) {
	root, err := bdecode(raw)
	if err != nil {
		return nil, err
	}
	info, ok := root["info"].(map[string]any)
	if !ok {
		return nil, errors.New("torrent sans info-dict")
	}
	t := &Torrent{Name: str(info["name"]), Source: str(info["source"])}
	if p, ok := info["private"].(int64); ok && p == 1 {
		t.Private = true
	}
	if files, ok := info["files"].([]any); ok {
		for _, f := range files {
			fm, _ := f.(map[string]any)
			parts := []string{}
			if ps, ok := fm["path"].([]any); ok {
				for _, p := range ps {
					parts = append(parts, str(p))
				}
			}
			size, _ := fm["length"].(int64)
			t.Files = append(t.Files, TorrentFile{Path: strings.Join(parts, "/"), Size: size})
			t.Size += size
		}
	} else {
		size, _ := info["length"].(int64)
		t.Files = []TorrentFile{{Path: t.Name, Size: size}}
		t.Size = size
	}
	// L'infohash se calcule sur l'info-dict RÉENCODÉ : un torrent canonique
	// redonne les mêmes octets, et c'est exactement ce que fait le serveur.
	var buf bytes.Buffer
	if err := bencode(&buf, info); err != nil {
		return nil, err
	}
	sum := sha1.Sum(buf.Bytes())
	t.InfoHash = hex.EncodeToString(sum[:])
	return t, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// resealTorrent pose private=1 et source=<tag> (nouvel infohash, propre à
// l'instance) et retire les trackers d'origine : le .torrent servi par
// Draupnirr après publication portera l'announce personnalisé du membre.
func resealTorrent(raw []byte, source string) ([]byte, error) {
	root, err := bdecode(raw)
	if err != nil {
		return nil, err
	}
	info, ok := root["info"].(map[string]any)
	if !ok {
		return nil, errors.New("torrent sans info-dict")
	}
	info["private"] = int64(1)
	info["source"] = source
	delete(root, "announce")
	delete(root, "announce-list")
	delete(root, "url-list")
	var buf bytes.Buffer
	if err := bencode(&buf, root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- création d'un .torrent depuis un fichier ou un dossier ----

// pieceLength vise ~1 500 pièces, bornée entre 16 Kio et 16 Mio (puissance de 2).
func pieceLength(total int64) int64 {
	var p int64 = 16 * 1024
	for p < 16*1024*1024 && total/p > 1500 {
		p *= 2
	}
	return p
}

type progressFunc func(done, total int64)

func makeTorrent(path, source string, progress progressFunc) ([]byte, error) {
	path = filepath.Clean(path)
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	type entry struct {
		abs  string
		rel  []string
		size int64
	}
	var entries []entry
	var total int64
	if st.IsDir() {
		// Liens suivis : la longueur déclarée est celle de la cible, celle
		// qu'os.Open lit ; abs passe par le chemin d'origine (ordre inchangé).
		err = walkFiles(path, func(rel string, size int64, err error) error {
			if err != nil {
				return err
			}
			entries = append(entries, entry{abs: filepath.Join(path, rel), rel: strings.Split(filepath.ToSlash(rel), "/"), size: size})
			total += size
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(entries) == 0 {
			return nil, errors.New("dossier vide")
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].abs < entries[j].abs })
	} else {
		entries = []entry{{abs: path, size: st.Size()}}
		total = st.Size()
	}
	if total == 0 {
		return nil, errors.New("rien à hacher : taille nulle")
	}

	plen := pieceLength(total)
	pieces := make([]byte, 0, (total/plen+1)*20)
	h := sha1.New()
	var inPiece, done int64
	buf := make([]byte, 1024*1024)
	for _, e := range entries {
		f, err := os.Open(e.abs)
		if err != nil {
			return nil, err
		}
		for {
			room := plen - inPiece
			if room > int64(len(buf)) {
				room = int64(len(buf))
			}
			n, rerr := f.Read(buf[:room])
			if n > 0 {
				h.Write(buf[:n])
				inPiece += int64(n)
				done += int64(n)
				if inPiece == plen {
					pieces = h.Sum(pieces)
					h.Reset()
					inPiece = 0
				}
				if progress != nil {
					progress(done, total)
				}
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				f.Close()
				return nil, rerr
			}
		}
		f.Close()
	}
	if inPiece > 0 {
		pieces = h.Sum(pieces)
	}

	info := map[string]any{
		"name":         filepath.Base(path),
		"piece length": plen,
		"pieces":       pieces,
		"private":      int64(1),
		"source":       source,
	}
	if st.IsDir() {
		files := make([]any, 0, len(entries))
		for _, e := range entries {
			parts := make([]any, len(e.rel))
			for i, p := range e.rel {
				parts[i] = p
			}
			files = append(files, map[string]any{"length": e.size, "path": parts})
		}
		info["files"] = files
	} else {
		info["length"] = total
	}
	var out bytes.Buffer
	if err := bencode(&out, map[string]any{"created by": "Bifröst " + Version, "info": info}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// mainFile : le plus gros fichier vidéo, celui que MediaInfo doit lire.
func mainFile(root string, t *Torrent) string {
	var best TorrentFile
	for _, f := range t.Files {
		if f.Size > best.Size {
			best = f
		}
	}
	if best.Path == "" {
		return ""
	}
	if len(t.Files) == 1 && !isDir(root) {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(best.Path))
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
