package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Réponse de /api/upload/analyze pour l'album de MusicUploadTest (Draupnirr),
// telle que PHP l'encode : tableaux vides en [], valeurs absentes en null.
const albumAnalysis = `{"ok":true,"name":"Daft Punk - Random Access Memories (2013) [24-88]","size_human":"346.2 MB","file_count":4,
"warnings":[],"executable":false,"nomenclature":null,
"bot":[{"level":"warn","code":"naming_spaces","message":"Le nom contient des espaces"},{"level":"info","code":"summary","message":"4 fichier(s)"}],
"music":{"sheet":true,"artist":"Daft Punk","album":"Random Access Memories","year":2013,"label":"Columbia","format":"FLAC",
"format_label":"FLAC 24 bits / 88,2 kHz","lossless":true,"bit_depth":24,"sample_rate":88200,"profile":null,"source":"WEB","types":[],
"group":null,"discs":1,"track_count":3,"duration":"14 min",
"tracks":[{"disc":1,"number":1,"title":"Give Life Back to Music","duration":"4:31"},{"disc":1,"number":2,"title":"The Game of Love","duration":"4:32"},{"disc":1,"number":null,"title":"Giorgio <by> Moroder","duration":"4:33"}],
"musicbrainz_id":"5000a285-b67e-4cfc-b54b-2b98f1810d2e","musicbrainz_url":"https://musicbrainz.org/release/5000a285-b67e-4cfc-b54b-2b98f1810d2e",
"cover_url":"https://coverartarchive.org/release/5000a285-b67e-4cfc-b54b-2b98f1810d2e/front-500",
"built_name":"Daft.Punk.Random.Access.Memories.24BIT.88KHZ.WEB.FLAC.2013","missing":[],"tags":["FLAC","24BIT","WEB"],
"suggested_category":"musique-flac","nfo":"====","issues":[],"rip_log":false,
"vocabulary":{"source":["WEB","CD","VINYL"],"type":["EP","SINGLE","OST","LIVE","BOOTLEG","Deluxe.Edition","Remastered"]},"probe_error":null}}`

func albumFixture(t *testing.T) *analysis {
	t.Helper()
	var a analysis
	if err := json.Unmarshal([]byte(albumAnalysis), &a); err != nil {
		t.Fatalf("réponse d'analyse musique illisible : %v", err)
	}
	return &a
}

func TestFindAlbumsGroupsDiscFolders(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, size int) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Daft Punk/Discovery/01 - One More Time.flac", 100)
	write("Daft Punk/Discovery/02 - Aerodynamic.flac", 100)
	write("Daft Punk/Discovery/cover.jpg", 10)
	write("Daft Punk/Discovery/.DS_Store", 7) // hors torrent, hors taille
	write("Daft Punk/Alive 2007/CD1/01.flac", 50)
	write("Daft Punk/Alive 2007/CD 2/01.flac", 50)
	write("Daft Punk/Alive 2007/Scans/front.jpg", 5)
	write("VA/Best Of/Disque 1/a.mp3", 20)
	write("VA/Best Of/Disc 2 - Bonus/b.mp3", 20)
	write("VA/Best Of/Disc 2 - Bonus/Extra/c.mp3", 20) // album dans l'album : déjà dans le torrent du parent
	write("Films/Dune.2021.mkv", 1000)
	write(".cache/x.flac", 1)

	albums, err := findAlbums(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, a := range albums {
		got[a.Name] = a.Size
	}
	want := map[string]int64{"Daft Punk/Alive 2007": 105, "Daft Punk/Discovery": 210, "VA/Best Of": 60}
	if len(got) != len(want) {
		t.Fatalf("albums : %v", got)
	}
	for name, size := range want {
		if got[name] != size {
			t.Fatalf("%s : taille %d, attendu %d (tous : %v)", name, got[name], size, got)
		}
	}
	// Le dossier d'un album donné tel quel : c'est l'album.
	one, _ := findAlbums(filepath.Join(root, "Daft Punk", "Alive 2007"))
	if len(one) != 1 || one[0].Name != "Alive 2007" {
		t.Fatalf("album racine : %+v", one)
	}
	// La taille est celle du torrent : même valeur que makeTorrent.
	raw, err := makeTorrent(filepath.Join(root, "Daft Punk", "Discovery"), "DRAUPNIRR", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tt, _ := parseTorrent(raw); tt.Size != want["Daft Punk/Discovery"] || hasRipLog(tt) {
		t.Fatalf("torrent : %d octets, rip log %v", tt.Size, hasRipLog(tt))
	}
	if !hasRipLog(&Torrent{Files: []TorrentFile{{Path: "a.flac"}, {Path: "Rip.LOG"}}}) {
		t.Fatal("un .log de rip doit être vu")
	}
}

func TestDecideMusic(t *testing.T) {
	a := albumFixture(t)
	if ok, why := decideMusic(a); !ok {
		t.Fatalf("album complet publiable, les espaces du dossier ne comptent pas : %s", why)
	}
	cases := []struct {
		name string
		mut  func(a *analysis)
		want string
	}{
		{"source manquante", func(a *analysis) { a.Music.Missing = []string{"source"}; a.Music.BuiltName = "" }, "à compléter : source"},
		{"formats mélangés", func(a *analysis) {
			a.Music.Issues = []issue{{"warn", "mixed_formats", "Formats mélangés dans l'album (2 FLAC, 1 MP3)"}}
		}, "Formats mélangés dans l'album (2 FLAC, 1 MP3)"},
		{"tags manquants : signalé, pas bloquant", func(a *analysis) { a.Music.Issues = []issue{{"warn", "missing_tags", "1 piste"}} }, ""},
		{"doublon d'infohash", func(a *analysis) { a.Warnings = []string{"Ce torrent existe déjà sur Draupnirr (même infohash)."} }, "Ce torrent existe déjà sur Draupnirr (même infohash)."},
		{"doublon Ratatosk", func(a *analysis) { a.Bot = append(a.Bot, issue{"warn", "duplicate_release", "doublon probable"}) }, "Ratatosk : doublon probable"},
		{"rapport illisible", func(a *analysis) { a.Music = &musicBlock{ProbeError: "Rapport MediaInfo non reconnu"} }, "Rapport MediaInfo non reconnu"},
		{"hors Musique", func(a *analysis) { a.Music = nil }, "catégorie Musique introuvable sur Draupnirr"},
	}
	for _, c := range cases {
		a := albumFixture(t)
		c.mut(a)
		ok, why := decideMusic(a)
		if ok != (c.want == "") || why != c.want {
			t.Fatalf("%s : %v %q", c.name, ok, why)
		}
	}
}

func TestPickEditionOnlyWhenSure(t *testing.T) {
	m := albumFixture(t).Music
	same := mbRelease{ID: "a", Title: "Random Access Memories", Artist: "Daft Punk", TrackCount: 3, TracksMatch: true}
	deluxe := mbRelease{ID: "b", Title: "Random Access Memories (10th Anniversary Edition)", Artist: "Daft Punk", TrackCount: 22}
	if p, _ := pickEdition(m, []mbRelease{deluxe, same}); p == nil || p.ID != "a" {
		t.Fatalf("une seule édition sûre : %+v", p)
	}
	other := same
	other.ID = "c"
	if p, why := pickEdition(m, []mbRelease{same, other}); p != nil || !strings.Contains(why, "2 éditions") {
		t.Fatalf("deux éditions possibles : pas de choix (%v, %q)", p, why)
	}
	cover := same
	cover.Artist = "Pentatonix"
	if p, _ := pickEdition(m, []mbRelease{cover, deluxe}); p != nil {
		t.Fatal("autre artiste ou autre nombre de pistes : pas de choix")
	}
}

func TestMusicPresentation(t *testing.T) {
	a := albumFixture(t)
	bb := trackList("bbcode", a.Music.Tracks)
	if bb != "[list=1]\n[*]Give Life Back to Music (4:31)\n[*]The Game of Love (4:32)\n[*]Giorgio <by> Moroder (4:33)\n[/list]" {
		t.Fatalf("pistes BBCode :\n%s", bb)
	}
	if h := trackList("html", a.Music.Tracks[2:]); h != "<ol><li>Giorgio &lt;by&gt; Moroder (4:33)</li></ol>" {
		t.Fatalf("pistes HTML : %s", h)
	}
	if trackList("bbcode", nil) != "" {
		t.Fatal("sans pistes, la variable est vide (le bloc {{#pistes}} tombe)")
	}

	// Le modèle « Fiche album » du site (migration add_site_presentation_templates).
	musique, films := "musique", "films"
	site := presTemplate{Name: "Fiche album", Family: &musique, Format: "bbcode", IsDefault: true, Site: true,
		Body: "[center]{{#affiche}}[img]{{affiche}}[/img]\n{{/affiche}}[size=22][b]{{artiste}} — {{album}}[/b][/size]{{#annee}} [size=14]({{annee}})[/size]{{/annee}}[/center]\n\n[h2]Fiche[/h2]\n[list]\n[*][b]Release[/b] : [c]{{nom_release}}[/c]\n[*][b]Format[/b] : {{format_audio}}{{#source}} · {{source}}{{/source}}\n{{#label}}[*][b]Label[/b] : {{label}}{{/label}}\n{{#duree}}[*][b]Durée[/b] : {{duree}} · {{nb_pistes}} pistes{{/duree}}\n[*][b]Taille[/b] : {{taille}}\n{{#team}}[*][b]Team[/b] : {{team}}{{/team}}\n[/list]\n\n{{#pistes}}[h2]Pistes[/h2]\n{{pistes}}{{/pistes}}\n\n[hr]\n[center]{{#musicbrainz_url}}[url={{musicbrainz_url}}]Fiche MusicBrainz[/url] · {{/musicbrainz_url}}présentation de {{uploadeur}}, {{date}}[/center]"}
	mine := presTemplate{Name: "Mes films", Family: &films, Format: "html", IsDefault: true, Body: "film"}
	data := func(f string) map[string]string { return musicData(a, f, "skadi") }

	desc, format := describe([]presTemplate{mine, site}, "musique", data, soberAlbum)
	for _, want := range []string{"[img]https://coverartarchive.org/release/5000a285-b67e-4cfc-b54b-2b98f1810d2e/front-500[/img]",
		"[b]Daft Punk — Random Access Memories[/b][/size] [size=14](2013)", "FLAC 24 bits / 88,2 kHz · WEB", "Columbia",
		"14 min · 3 pistes", "[h2]Pistes[/h2]\n[list=1]\n[*]Give Life Back to Music (4:31)", "[url=https://musicbrainz.org/release/", "présentation de skadi"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("modèle du site : %q absent de\n%s", want, desc)
		}
	}
	if format != "bbcode" || strings.Contains(desc, "{{") || strings.Contains(desc, "Team") {
		t.Fatalf("rendu : %s\n%s", format, desc)
	}

	// Un modèle musique du membre passe devant celui du site, avec ses pistes au format HTML.
	own := presTemplate{Name: "Mes albums", Family: &musique, Format: "html", IsDefault: true, Body: "<h1>{{titre}}</h1>{{pistes}}"}
	if desc, format = describe([]presTemplate{own, site}, "musique", data, soberAlbum); format != "html" || !strings.HasPrefix(desc, "<h1>Random Access Memories</h1><ol><li>Give Life") {
		t.Fatalf("modèle du membre : %s %s", format, desc)
	}
	// Son défaut « toutes catégories » aussi (comme presDefault du site).
	all := presTemplate{Name: "Partout", Format: "bbcode", IsDefault: true, Body: "{{titre}}"}
	if p := pickTemplate([]presTemplate{all, site}, "musique"); p == nil || p.Name != "Partout" {
		t.Fatalf("défaut toutes catégories : %+v", p)
	}
	// Ni modèle ni modèle du site : présentation sobre, sans variable restée brute.
	if desc, _ = describe(nil, "musique", data, soberAlbum); strings.Contains(desc, "{{") || !strings.Contains(desc, "[*]The Game of Love (4:32)") {
		t.Fatalf("présentation sobre :\n%s", desc)
	}
}

func TestMediaInfoReadableKeepsTheTracks(t *testing.T) {
	dir := `[{"media":{"@ref":"01.flac","track":[{"@type":"General"},{"@type":"Audio"}]}},{"media":null},{"media":{"@ref":"02.flac","track":[{"@type":"General"}]}}]`
	out, ok := mediaInfoReadable(dir)
	var items []map[string]any
	if !ok || json.Unmarshal([]byte(out), &items) != nil || len(items) != 2 {
		t.Fatalf("un fichier illisible est écarté, les pistes restent : %v %s", ok, out)
	}
	if _, ok := mediaInfoReadable(`[{"media":null}]`); ok {
		t.Fatal("rien de lisible : erreur")
	}
	if _, ok := mediaInfoReadable(`{"media":{"@ref":"film.mkv","track":[{"@type":"General"}]}}`); !ok {
		t.Fatal("un fichier seul reste accepté")
	}
}

func TestMultipartSendsAnEmptyAlbumType(t *testing.T) {
	body, _, err := multipartForm(map[string][]string{"facets[type]": {""}, "year": {""}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `name="facets[type]"`) || strings.Contains(string(body), `name="year"`) {
		t.Fatalf("type vidé exprès envoyé, les autres vides non :\n%s", body)
	}
}
