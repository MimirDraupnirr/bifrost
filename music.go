package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Musique (docs/25 §6) : un album = une release. Le serveur lit les pistes
// (rapport MediaInfo JSON du dossier), nomme, compose le NFO et identifie
// l'édition MusicBrainz ; Bifröst trouve les albums, lance mediainfo sur le
// dossier et choisit l'édition seulement quand elle est sûre.

// audioExt : ce qui fait d'un dossier un album.
var audioExt = map[string]bool{"flac": true, "mp3": true, "m4a": true, "ogg": true, "opus": true, "wav": true, "ape": true, "wv": true, "aiff": true, "aif": true}

func extOf(name string) string {
	return strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
}

// reDisc : sous-dossier de disque (CD1, CD 2, Disc 1, Disque 2, Disk1…),
// rattaché à son parent — un album à deux disques est UNE release.
var reDisc = regexp.MustCompile(`(?i)^(cd|dis[ck]|disque)[\s._-]*\d{1,2}\b`)

// findAlbums : chaque dossier qui contient directement des pistes est un
// album ; un dossier de disque compte pour son parent ; un album rangé dans
// un autre (bonus, sous-dossier) est déjà dans le torrent du parent. Name =
// chemin relatif à root, pour distinguer deux « Greatest Hits ».
func findAlbums(root string) ([]DirEntry, error) {
	root = filepath.Clean(root)
	withTracks := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && p != root {
				return filepath.SkipDir // dossier illisible : on passe, le reste du lot continue
			}
			return err
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if audioExt[extOf(d.Name())] {
			withTracks[filepath.Dir(p)] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	albums := map[string]bool{}
	for dir := range withTracks {
		if dir != root && reDisc.MatchString(filepath.Base(dir)) {
			dir = filepath.Dir(dir)
		}
		albums[dir] = true
	}
	var out []DirEntry
	for p := range albums {
		nested := false
		for q := p; q != root && filepath.Dir(q) != q; {
			q = filepath.Dir(q)
			nested = nested || albums[q]
		}
		if nested {
			continue
		}
		name, _ := filepath.Rel(root, p)
		if p == root {
			name = filepath.Base(root)
		}
		out = append(out, DirEntry{Name: filepath.ToSlash(name), Path: p, IsDir: true, Size: dirSize(p)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path) })
	return out, nil
}

func (localSource) Albums(_ context.Context, root string) ([]DirEntry, error) {
	return findAlbums(root)
}

func (r *remoteSource) Albums(ctx context.Context, root string) ([]DirEntry, error) {
	c, err := r.client()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	out, err := runSSH(ctx, c, agentPath+" agent albums "+shellQuote(root), nil, nil)
	if err != nil {
		return nil, err
	}
	var entries []DirEntry
	return entries, json.Unmarshal(out, &entries)
}

// hasRipLog : un .log ou un .cue accompagne les pistes — le serveur en
// déduit la source CD ; la source par défaut du lot ne doit pas l'écraser.
func hasRipLog(t *Torrent) bool {
	for _, f := range t.Files {
		if e := extOf(f.Path); e == "log" || e == "cue" {
			return true
		}
	}
	return false
}

// ---- le bloc `music` de /api/upload/analyze ----

type musicTrack struct {
	Title    string `json:"title"`
	Duration string `json:"duration"`
}

// issue : un constat de Ratatosk (`bot`) ou un contrôle d'album (`issues`).
type issue struct {
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type musicBlock struct {
	Sheet             bool         `json:"sheet"`
	Artist            string       `json:"artist"`
	Album             string       `json:"album"`
	Year              int          `json:"year"`
	Label             string       `json:"label"`
	FormatLabel       string       `json:"format_label"`
	Source            string       `json:"source"`
	Group             string       `json:"group"`
	TrackCount        int          `json:"track_count"`
	Duration          string       `json:"duration"`
	Tracks            []musicTrack `json:"tracks"`
	MusicBrainzID     string       `json:"musicbrainz_id"`
	MusicBrainzURL    string       `json:"musicbrainz_url"`
	CoverURL          string       `json:"cover_url"`
	BuiltName         string       `json:"built_name"`
	Missing           []string     `json:"missing"`
	Tags              []string     `json:"tags"`
	SuggestedCategory string       `json:"suggested_category"`
	NFO               string       `json:"nfo"`
	Issues            []issue      `json:"issues"`
	RipLog            bool         `json:"rip_log"`
	ProbeError        string       `json:"probe_error"`
}

// mbRelease : une édition de GET /api/musicbrainz.
type mbRelease struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Year        int    `json:"year"`
	TrackCount  int    `json:"track_count"`
	TracksMatch bool   `json:"tracks_match"`
}

// musicBrainzSearch : les éditions d'un album, `tracks_match` posé par le serveur.
func musicBrainzSearch(ctx context.Context, c *Client, m *musicBlock) ([]mbRelease, error) {
	q := url.Values{"artist": {m.Artist}, "album": {m.Album}}
	if m.TrackCount > 0 {
		q.Set("tracks", fmt.Sprint(m.TrackCount))
	}
	raw, err := c.MusicBrainz(ctx, q)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results []mbRelease `json:"results"`
	}
	return resp.Results, json.Unmarshal(raw, &resp)
}

// pickEdition : l'édition n'est retenue que si elle est SEULE à avoir le même
// nombre de pistes que le dossier, le même titre et le même artiste. Sinon
// on publie sans édition (le nom vient alors des tags), jamais une fausse.
func pickEdition(m *musicBlock, results []mbRelease) (*mbRelease, string) {
	var found []*mbRelease
	for i := range results {
		r := &results[i]
		if r.TracksMatch && similarity(m.Album, r.Title) >= 0.6 && similarity(m.Artist, r.Artist) >= 0.6 {
			found = append(found, r)
		}
	}
	switch len(found) {
	case 1:
		return found[0], ""
	case 0:
		return nil, "aucune édition MusicBrainz sûre (titre, artiste et nombre de pistes)"
	}
	return nil, fmt.Sprintf("%d éditions MusicBrainz possibles, aucune choisie", len(found))
}

// decideMusic : publiable ? Mêmes garde-fous qu'en vidéo (analyse, doublon,
// Ratatosk), puis la fiche d'album : rien ne manque, un seul format, et
// l'édition (lue dans les tags) a autant de pistes que le dossier.
func decideMusic(a *analysis) (bool, string) {
	// Le nom de dossier a des espaces, mais c'est le nom CALCULÉ qui sera publié.
	if ok, why := analysisBlocker(a, "naming_spaces"); !ok {
		return false, why
	}
	m := a.Music
	if m == nil {
		return false, "catégorie Musique introuvable sur Draupnirr"
	}
	if !m.Sheet {
		if m.ProbeError != "" {
			return false, m.ProbeError
		}
		return false, "pistes illisibles : rapport MediaInfo absent"
	}
	if len(m.Missing) > 0 {
		return false, "à compléter : " + strings.Join(m.Missing, ", ")
	}
	for _, is := range m.Issues {
		if is.Code == "mixed_formats" || is.Code == "edition_tracks" {
			return false, is.Message
		}
	}
	if m.BuiltName == "" {
		return false, "nom canonique non calculé"
	}
	return true, ""
}

// ---- présentation ----

// trackList : la variable {{pistes}}, liste numérotée selon le format du modèle.
func trackList(format string, tracks []musicTrack) string {
	if len(tracks) == 0 {
		return ""
	}
	items := make([]string, len(tracks))
	for i, t := range tracks {
		title := t.Title
		if format == "html" {
			title = html.EscapeString(title)
		}
		if t.Duration != "" {
			title += " (" + t.Duration + ")"
		}
		items[i] = title
	}
	if format == "html" {
		return "<ol><li>" + strings.Join(items, "</li><li>") + "</li></ol>"
	}
	return "[list=1]\n[*]" + strings.Join(items, "\n[*]") + "\n[/list]"
}

// musicData : les variables d'un album (docs/25 §6.7) ; titre, affiche et
// durée sont posés aussi pour qu'un modèle film reste lisible.
func musicData(a *analysis, format, uploader string) map[string]string {
	m := a.Music
	year := ""
	if m.Year > 0 {
		year = fmt.Sprint(m.Year)
	}
	return map[string]string{
		"artiste": m.Artist, "album": m.Album, "titre": m.Album, "annee": year, "type": "Album", "label": m.Label,
		"format_audio": m.FormatLabel, "pistes": trackList(format, m.Tracks), "nb_pistes": fmt.Sprint(m.TrackCount),
		"musicbrainz_url": m.MusicBrainzURL, "affiche": m.CoverURL, "duree": m.Duration, "nom_release": m.BuiltName,
		"taille": a.SizeHuman, "nb_fichiers": fmt.Sprint(a.FileCount), "source": m.Source, "team": m.Group,
		"tags": strings.Join(m.Tags, ", "), "nfo": m.NFO, "uploadeur": uploader, "date": today(),
	}
}

const soberAlbum = "[center]{{#affiche}}[img]{{affiche}}[/img]\n{{/affiche}}[size=22][b]{{artiste}} — {{album}}[/b][/size]{{#annee}} ({{annee}}){{/annee}}[/center]\n\n[h2]Fiche[/h2]\n[list]\n[*][b]Release[/b] : [c]{{nom_release}}[/c]\n[*][b]Format[/b] : {{format_audio}}{{#source}} · {{source}}{{/source}}\n{{#label}}[*][b]Label[/b] : {{label}}{{/label}}\n[*][b]Durée[/b] : {{duree}} · {{nb_pistes}} pistes\n[*][b]Taille[/b] : {{taille}}\n{{#team}}[*][b]Team[/b] : {{team}}{{/team}}\n[/list]\n\n{{#pistes}}[h2]Pistes[/h2]\n{{pistes}}{{/pistes}}\n\n[center]{{#musicbrainz_url}}[url={{musicbrainz_url}}]Fiche MusicBrainz[/url] · {{/musicbrainz_url}}publié par Bifröst, {{date}}[/center]"

// ---- le lot, version musique ----

// batchAlbum : un album du lot. Analyse sans édition, recherche MusicBrainz
// (sauf édition lue dans les tags), seconde analyse avec la catégorie
// proposée et l'édition sûre, décision, publication.
func (s *server) batchAlbum(ctx context.Context, c *Client, src fileSource, j *batchJob, r *batchRow, env batchEnv) (string, string) {
	raw, t, err := s.batchTorrent(ctx, src, r, env.sourceTag)
	if err != nil {
		return "erreur", err.Error()
	}
	s.setDetail(r, "mediainfo des pistes")
	mi, err := src.MediaInfo(ctx, r.Path)
	if err != nil {
		return "à revoir", "MediaInfo : " + err.Error()
	}
	fields := map[string][]string{"category": {"musique-album"}, "mediainfo": {mi}}
	if j.albumSrc != "" && !hasRipLog(t) {
		fields["facets[source]"] = []string{j.albumSrc}
	}
	s.setDetail(r, "analyse")
	var a analysis
	if err := analyzeInto(ctx, c, raw, fields, &a); err != nil {
		return "erreur", err.Error()
	}
	m := a.Music
	if m == nil || !m.Sheet {
		_, why := decideMusic(&a)
		return "à revoir", why
	}

	var pick *mbRelease
	why := ""
	if m.MusicBrainzID == "" {
		s.setDetail(r, "MusicBrainz")
		results, err := musicBrainzSearch(ctx, c, m)
		if err != nil {
			why = "MusicBrainz : " + err.Error()
		} else {
			pick, why = pickEdition(m, results)
		}
	}
	if m.SuggestedCategory != "" {
		fields["category"] = []string{m.SuggestedCategory}
	}
	if pick != nil {
		fields["musicbrainz_id"] = []string{pick.ID}
	}
	// Ratatosk juge le nom qui sera publié (doublon par le nom), pas le dossier.
	if m.BuiltName != "" {
		fields["release_name"] = []string{m.BuiltName}
	}
	s.setDetail(r, "analyse")
	if err := analyzeInto(ctx, c, raw, fields, &a); err != nil {
		return "erreur", err.Error()
	}
	category := fields["category"][0]
	s.mu.Lock()
	r.Category = category
	if a.Music != nil {
		r.Built = a.Music.BuiltName
		if a.Music.MusicBrainzID != "" {
			r.Edition = fmt.Sprintf("%s — %s", a.Music.Artist, a.Music.Album)
		}
	}
	s.mu.Unlock()

	ok, reason := decideMusic(&a)
	if !ok {
		if why != "" {
			reason += " ; " + why
		}
		return "à revoir", reason
	}
	if j.DryRun {
		if why != "" {
			return "simulé", "publiable sans édition MusicBrainz : " + a.Music.BuiltName
		}
		return "simulé", "publiable : " + a.Music.BuiltName
	}

	desc, format := describe(env.templates, "musique", func(f string) map[string]string { return musicData(&a, f, env.uploader) }, soberAlbum)
	meta := map[string][]string{"category": {category}, "description": {desc}, "description_format": {format}, "mediainfo": {mi},
		"meta[musicbrainz_id]": {a.Music.MusicBrainzID}}
	if v := fields["facets[source]"]; len(v) > 0 {
		meta["meta[facets][source]"] = v
	}
	return s.publishAndSeed(ctx, c, r, raw, t, meta, env)
}

func analyzeInto(ctx context.Context, c *Client, raw []byte, fields map[string][]string, a *analysis) error {
	out, err := c.Analyze(ctx, raw, fields)
	if err != nil {
		return err
	}
	*a = analysis{}
	if err := json.Unmarshal(out, a); err != nil {
		return errors.New("analyse illisible : " + err.Error())
	}
	return nil
}
