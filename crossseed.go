package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Cross-seed (docs/23) : ce que tu seedes déjà pour d'autres trackers et qui
// existe sur Draupnirr se remet en seed ici aussi, en un clic. Correspondance
// par taille EXACTE via /api/torrents/match (le critère de la page Seedbox du
// site), départage par le nom, puis vérification de l'arborescence sur les
// données avant d'ajouter au client — jamais de hachage.

type matchEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	InfoHash  string `json:"infohash"`
	Size      int64  `json:"size_bytes"`
	FileCount int    `json:"file_count"`
	Category  string `json:"category"`
	Seeders   int    `json:"seeders"`
}

func (c *Client) Match(ctx context.Context, sizes []int64) ([]matchEntry, error) {
	parts := make([]string, len(sizes))
	for i, s := range sizes {
		parts[i] = strconv.FormatInt(s, 10)
	}
	var out []matchEntry
	err := c.do(ctx, http.MethodGet, "/api/torrents/match", url.Values{"sizes": {strings.Join(parts, ",")}}, nil, "", &out)
	return out, err
}

type crossRow struct {
	ClientTorrent
	Match  *matchEntry `json:"match,omitempty"`
	Score  float64     `json:"score,omitempty"`
	hashes []string
}

var reTok = regexp.MustCompile(`[^a-z0-9]+`)

func tokens(name string) map[string]bool {
	out := map[string]bool{}
	for _, t := range reTok.Split(strings.ToLower(name), -1) {
		if len(t) > 1 {
			out[t] = true
		}
	}
	return out
}

// similarity : part des mots communs (Jaccard) entre deux noms de release.
func similarity(a, b string) float64 {
	ta, tb := tokens(a), tokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	common := 0
	for t := range ta {
		if tb[t] {
			common++
		}
	}
	return float64(common) / float64(len(ta)+len(tb)-common)
}

// crossSeedScan : les torrents complets du client qui ne sont pas encore sur
// Draupnirr, avec leur meilleure correspondance de taille s'il y en a une.
func crossSeedScan(ctx context.Context, c *Client, list []clientEntry) ([]crossRow, error) {
	var sizes []int64
	seen := map[int64]bool{}
	for _, t := range list {
		if t.Progress < 1 || t.OnDraupnirr || t.Size <= 0 {
			continue
		}
		if !seen[t.Size] {
			seen[t.Size] = true
			sizes = append(sizes, t.Size)
		}
	}
	bySize := map[int64][]matchEntry{}
	for i := 0; i < len(sizes); i += 500 {
		end := i + 500
		if end > len(sizes) {
			end = len(sizes)
		}
		found, err := c.Match(ctx, sizes[i:end])
		if err != nil {
			return nil, err
		}
		for _, m := range found {
			bySize[m.Size] = append(bySize[m.Size], m)
		}
	}
	rows := applyMatches(list, bySize)
	sort.SliceStable(rows, func(i, j int) bool {
		if (rows[i].Match != nil) != (rows[j].Match != nil) {
			return rows[i].Match != nil
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return rows, nil
}

// layoutProblems : l'arborescence du .torrent Draupnirr existe-t-elle telle
// quelle sous le dossier qui contient la release ? Renvoie ce qui manque.
func layoutProblems(ctx context.Context, src fileSource, contentPath string, t *Torrent) ([]string, string, error) {
	remote := false
	if _, ok := src.(*remoteSource); ok {
		remote = true
	}
	base := filepath.Base(contentPath)
	savePath := filepath.Dir(contentPath)
	if remote {
		base = path.Base(strings.TrimRight(contentPath, "/"))
		savePath = pathDir(contentPath)
	}
	if base != t.Name {
		return []string{fmt.Sprintf("nom différent : « %s » sur le disque, « %s » dans le .torrent", base, t.Name)}, savePath, nil
	}
	missing, err := src.Check(ctx, savePath, t)
	return missing, savePath, err
}

// checkFiles : vérifie fichier par fichier (chemin + taille) sous root.
func checkFiles(root string, t *Torrent) []string {
	var missing []string
	for _, f := range t.Files {
		p := filepath.Join(root, t.Name)
		if len(t.Files) > 1 || f.Path != t.Name {
			p = filepath.Join(root, t.Name, filepath.FromSlash(f.Path))
		}
		if len(t.Files) == 1 && f.Path == t.Name {
			p = filepath.Join(root, t.Name)
		}
		st, err := os.Stat(p)
		switch {
		case err != nil:
			missing = append(missing, f.Path+" (absent)")
		case st.Size() != f.Size:
			missing = append(missing, fmt.Sprintf("%s (%d octets au lieu de %d)", f.Path, st.Size(), f.Size))
		}
	}
	return missing
}

func (localSource) Check(_ context.Context, root string, t *Torrent) ([]string, error) {
	return checkFiles(root, t), nil
}

func (r *remoteSource) Check(ctx context.Context, root string, t *Torrent) ([]string, error) {
	c, err := r.client()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	payload, _ := json.Marshal(t)
	out, err := runSSH(ctx, c, agentPath+" agent check "+shellQuote(root), strings.NewReader(string(payload)), nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		Missing []string `json:"missing"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, errors.New("réponse de l'agent illisible")
	}
	return res.Missing, nil
}

// applyMatches : pour chaque release complète hors Draupnirr, la meilleure
// correspondance de taille — sauf si une de ses copies est déjà le torrent
// Draupnirr, ou si la correspondance est déjà dans le client.
func applyMatches(list []clientEntry, bySize map[int64][]matchEntry) []crossRow {
	known := map[string]bool{}
	for _, t := range list {
		for _, h := range t.Hashes {
			known[h] = true
		}
		known[strings.ToLower(t.Hash)] = true
	}
	var rows []crossRow
	for _, t := range list {
		if t.Progress < 1 || t.OnDraupnirr || t.Size <= 0 {
			continue
		}
		rows = append(rows, crossRow{ClientTorrent: t.ClientTorrent, hashes: t.Hashes})
	}
	kept := rows[:0]
	for i := range rows {
		cands := bySize[rows[i].Size]
		// Une des copies de cette release EST le torrent Draupnirr (même
		// infohash qu'au catalogue) : rien à cross-seeder, quel que soit le
		// domaine de son tracker.
		already := false
		var fresh []matchEntry
		for _, m := range cands {
			h := strings.ToLower(m.InfoHash)
			mine := h == strings.ToLower(rows[i].Hash)
			for _, rh := range rows[i].hashes {
				mine = mine || rh == h
			}
			if mine {
				already = true
			} else if !known[h] {
				fresh = append(fresh, m)
			}
		}
		if already {
			continue
		}
		kept = append(kept, rows[i])
		cands = fresh
		if len(cands) == 0 {
			continue
		}
		best, bestScore := cands[0], -1.0
		for _, m := range cands {
			if s := similarity(rows[i].Name, m.Name); s > bestScore {
				best, bestScore = m, s
			}
		}
		m := best
		kept[len(kept)-1].Match = &m
		kept[len(kept)-1].Score = bestScore
	}
	rows = kept
	return kept
}
