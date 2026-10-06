package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Mode lot (docs/23 §7, phase 4) : un dossier entier est passé au crible et
// tout ce qui n'est pas encore sur Draupnirr est préparé puis publié sans
// intervention — à condition que la fiche soit SÛRE. Dans le doute, la
// release est mise « à revoir » et rien n'est publié. Par défaut on simule.

type batchRow struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Status   string `json:"status"` // attente · en cours · déjà présent · publié · à revoir · erreur · simulé
	Detail   string `json:"detail,omitempty"`
	Category string `json:"category,omitempty"`
	Built    string `json:"built,omitempty"`
	TMDB     string `json:"tmdb,omitempty"`
	ID       string `json:"id,omitempty"`
	URL      string `json:"url,omitempty"`
}

type batchJob struct {
	Root       string      `json:"root"`
	DryRun     bool        `json:"dry_run"`
	Max        int         `json:"max"`
	Limit      int         `json:"limit"`
	OnlyVideo  bool        `json:"only_video"`
	Running    bool        `json:"running"`
	Done       bool        `json:"done"`
	Error      string      `json:"error,omitempty"`
	Started    time.Time   `json:"started"`
	Rows       []*batchRow `json:"rows"`
	Published  int         `json:"published"`
	Review     int         `json:"review"`
	Skipped    int         `json:"skipped"`
	cancel     context.CancelFunc
	catFilm    string
	catTV      string
	publishGap time.Duration
}

type batchOpts struct {
	Root      string `json:"path"`
	DryRun    bool   `json:"dry_run"`
	Max       int    `json:"max"`
	Limit     int    `json:"limit"`      // releases examinées au plus (0 = toutes)
	OnlyVideo bool   `json:"only_video"` // ignorer ce qui n'est pas une vidéo (ebooks, logiciels, musique)
	CatFilm   string `json:"category_film"`
	CatTV     string `json:"category_tv"`
}

// analysis : la partie de la réponse de /api/upload/analyze que le lot lit.
type analysis struct {
	OK          bool     `json:"ok"`
	Name        string   `json:"name"`
	InfoHash    string   `json:"infohash"`
	CleanTitle  string   `json:"clean_title"`
	Year        any      `json:"year"`
	GuessedType string   `json:"guessed_type"`
	Warnings    []string `json:"warnings"`
	Executable  bool     `json:"executable"`
	SizeHuman   string   `json:"size_human"`
	FileCount   int      `json:"file_count"`
	Tags        []string `json:"suggested_tags"`
	Bot         []struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	} `json:"bot"`
	Nomenclature *struct {
		BuiltName    string            `json:"built_name"`
		Missing      []string          `json:"missing"`
		MissingTitle bool              `json:"missing_title"`
		NameFacets   map[string]string `json:"name_facets"`
		Media        map[string]any    `json:"media"`
		NFO          string            `json:"nfo"`
		Facets       map[string]struct {
			Value  string `json:"value"`
			Origin string `json:"origin"`
		} `json:"facets"`
	} `json:"nomenclature"`
}

type tmdbResult struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Year      any    `json:"year"`
	Overview  string `json:"overview"`
	PosterURL string `json:"poster_url"`
}

func yearOf(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		var n int
		fmt.Sscan(x, &n)
		return n
	}
	return 0
}

// pickWork : l'œuvre n'est retenue que si elle est SÛRE — même année (à un
// an près, les dates de sortie varient) et titre très proche. Sinon « à
// revoir » : mieux vaut une release en attente qu'une fiche fausse.
func pickWork(a *analysis, results []tmdbResult) (*tmdbResult, string) {
	if a.CleanTitle == "" || len(results) == 0 {
		return nil, "œuvre introuvable sur TMDB"
	}
	want := yearOf(a.Year)
	for i := range results {
		r := &results[i]
		if want == 0 {
			break
		}
		dy := yearOf(r.Year) - want
		if dy >= -1 && dy <= 1 && similarity(a.CleanTitle, r.Title) >= 0.6 {
			return r, ""
		}
	}
	if want == 0 {
		return nil, "année absente du nom : impossible de choisir l'œuvre sans risque"
	}
	return nil, "aucune œuvre TMDB avec le même titre et la même année"
}

// decide : publiable ? Une seule raison suffit à envoyer en revue.
func decide(a *analysis, pick *tmdbResult, pickReason string) (ok bool, reason string) {
	if !a.OK {
		return false, "analyse refusée par Draupnirr"
	}
	for _, w := range a.Warnings {
		return false, w // doublon d'infohash, private/source : Draupnirr a parlé
	}
	if a.Executable {
		return false, "contenu exécutable : rapport VirusTotal à fournir à la main"
	}
	for _, b := range a.Bot {
		if b.Level == "error" || b.Level == "warning" {
			return false, "Ratatosk : " + b.Message
		}
	}
	n := a.Nomenclature
	if n == nil {
		return false, "catégorie sans nomenclature automatique"
	}
	if pick == nil {
		return false, pickReason
	}
	if len(n.Missing) > 0 {
		return false, "facettes manquantes : " + strings.Join(n.Missing, ", ")
	}
	if n.BuiltName == "" {
		return false, "nom canonique non calculé"
	}
	return true, ""
}

func (s *server) batchStart(opts batchOpts) (*batchJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batch != nil && s.batch.Running {
		return nil, errors.New("un lot est déjà en cours")
	}
	if opts.Root == "" {
		return nil, errors.New("dossier requis")
	}
	if opts.CatFilm == "" {
		opts.CatFilm = "films-film"
	}
	if opts.CatTV == "" {
		opts.CatTV = "series-serie-tv"
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &batchJob{Root: opts.Root, DryRun: opts.DryRun, Max: opts.Max, Limit: opts.Limit, OnlyVideo: opts.OnlyVideo, Running: true, Started: time.Now(),
		cancel: cancel, catFilm: opts.CatFilm, catTV: opts.CatTV, publishGap: 7 * time.Second}
	s.batch = j
	go s.runBatch(ctx, j)
	return j, nil
}

func (s *server) batchSnapshot() *batchJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batch == nil {
		return nil
	}
	cp := *s.batch
	cp.Rows = make([]*batchRow, len(s.batch.Rows))
	for i, r := range s.batch.Rows {
		rc := *r
		cp.Rows[i] = &rc
	}
	return &cp
}

func (s *server) runBatch(ctx context.Context, j *batchJob) {
	finish := func(err error) {
		s.mu.Lock()
		j.Running, j.Done = false, true
		if err != nil && !errors.Is(err, context.Canceled) {
			j.Error = err.Error()
		}
		s.mu.Unlock()
	}
	c, err := s.client()
	if err != nil {
		finish(err)
		return
	}
	src := s.source()
	s.mu.Lock()
	sourceTag, _ := s.me["source_tag"].(string)
	clientCfg := s.cfg.Client
	remote := s.cfg.Source == "ssh"
	s.mu.Unlock()
	if sourceTag == "" {
		sourceTag = "DRAUPNIRR"
	}

	entries, err := src.List(ctx, j.Root)
	if err != nil {
		finish(err)
		return
	}
	s.mu.Lock()
	for _, e := range entries {
		if e.Size > 0 {
			j.Rows = append(j.Rows, &batchRow{Path: e.Path, Name: e.Name, Size: e.Size, Status: "attente"})
		}
	}
	s.mu.Unlock()

	// 1. Déjà sur Draupnirr ? Taille exacte, 500 par appel.
	var sizes []int64
	for _, r := range j.Rows {
		sizes = append(sizes, r.Size)
	}
	present := map[int64]string{}
	for i := 0; i < len(sizes); i += 500 {
		end := min(i+500, len(sizes))
		found, err := c.Match(ctx, sizes[i:end])
		if err != nil {
			finish(err)
			return
		}
		for _, m := range found {
			present[m.Size] = m.Name
		}
	}
	s.mu.Lock()
	for _, r := range j.Rows {
		if name, ok := present[r.Size]; ok {
			r.Status, r.Detail = "déjà présent", name
			j.Skipped++
		}
	}
	s.mu.Unlock()

	// 2. Une release à la fois.
	templates := s.loadTemplates(ctx, c)
	examined := 0
	for _, r := range j.Rows {
		if ctx.Err() != nil {
			finish(ctx.Err())
			return
		}
		if r.Status != "attente" {
			continue
		}
		if j.Limit > 0 && examined >= j.Limit {
			break
		}
		examined++
		if j.Max > 0 && j.Published >= j.Max {
			s.mu.Lock()
			r.Status, r.Detail = "à revoir", "plafond du lot atteint"
			j.Review++
			s.mu.Unlock()
			continue
		}
		s.mu.Lock()
		r.Status, r.Detail = "en cours", "hachage"
		s.mu.Unlock()
		status, detail := s.batchOne(ctx, c, src, j, r, sourceTag, clientCfg, remote, templates)
		s.mu.Lock()
		r.Status, r.Detail = status, detail
		switch status {
		case "publié", "simulé":
			j.Published++
		case "à revoir":
			j.Review++
		}
		s.mu.Unlock()
		if status == "publié" {
			time.Sleep(j.publishGap) // throttle api-write : 10 publications par minute
		}
	}
	finish(nil)
}

func (s *server) loadTemplates(ctx context.Context, c *Client) map[string]presTemplate {
	out := map[string]presTemplate{}
	raw, err := c.Presentations(ctx)
	if err != nil {
		return out
	}
	var resp struct {
		Templates []presTemplate `json:"templates"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return out
	}
	for _, t := range resp.Templates {
		if t.IsDefault {
			key := "*"
			if t.Family != nil {
				key = *t.Family
			}
			out[key] = t
		}
	}
	return out
}

type presTemplate struct {
	Name      string  `json:"name"`
	Family    *string `json:"family"`
	Format    string  `json:"format"`
	IsDefault bool    `json:"is_default"`
	Body      string  `json:"body"`
}

// batchOne : prépare une release et décide. Renvoie (statut, détail).
func (s *server) batchOne(ctx context.Context, c *Client, src fileSource, j *batchJob, r *batchRow, sourceTag string, clientCfg ClientConfig, remote bool, templates map[string]presTemplate) (string, string) {
	if j.OnlyVideo {
		if ext, err := mainExtension(ctx, src, r.Path); err == nil && !videoExt[ext] {
			return "ignoré", "pas une vidéo (." + ext + ")"
		}
	}
	raw, err := src.MakeTorrent(ctx, r.Path, sourceTag, func(done, total int64) {
		s.mu.Lock()
		if total > 0 {
			r.Detail = fmt.Sprintf("hachage %d %%", done*100/total)
		}
		s.mu.Unlock()
	})
	if err != nil {
		return "erreur", err.Error()
	}
	t, err := parseTorrent(raw)
	if err != nil {
		return "erreur", err.Error()
	}
	s.mu.Lock()
	r.Detail = "mediainfo"
	s.mu.Unlock()
	mi, _ := src.MediaInfo(ctx, src.MainFile(r.Path, t))

	// Première analyse : catégorie devinée depuis le type.
	s.mu.Lock()
	r.Detail = "analyse"
	s.mu.Unlock()
	first, err := c.Analyze(ctx, raw, map[string][]string{"category": {j.catFilm}, "mediainfo": {mi}})
	if err != nil {
		return "erreur", err.Error()
	}
	var a analysis
	if err := json.Unmarshal(first, &a); err != nil {
		return "erreur", "analyse illisible : " + err.Error()
	}
	category, kind := j.catFilm, "movie"
	if a.GuessedType == "tv" {
		category, kind = j.catTV, "tv"
	}

	// Œuvre TMDB, seulement si sûre.
	var results []tmdbResult
	if rawT, err := c.TMDB(ctx, a.CleanTitle, kind); err == nil {
		var resp struct {
			Results []tmdbResult `json:"results"`
		}
		_ = json.Unmarshal(rawT, &resp)
		results = resp.Results
	}
	pick, pickReason := pickWork(&a, results)

	// Seconde analyse, avec ce que le nom déclare (source, team, édition).
	facets := map[string]string{}
	if a.Nomenclature != nil {
		for _, k := range []string{"source", "edition", "group"} {
			if v := a.Nomenclature.NameFacets[k]; v != "" {
				facets[k] = v
			}
		}
	}
	fields := map[string][]string{"category": {category}, "mediainfo": {mi}, "year": {fmt.Sprint(yearOf(a.Year))}}
	for k, v := range facets {
		fields["facets["+k+"]"] = []string{v}
	}
	if pick != nil {
		fields["work_title"] = []string{pick.Title}
		fields["tmdb_id"] = []string{fmt.Sprint(pick.ID)}
		fields["tmdb_type"] = []string{kind}
		if y := yearOf(pick.Year); y > 0 {
			fields["year"] = []string{fmt.Sprint(y)}
		}
	} else if a.CleanTitle != "" {
		fields["work_title"] = []string{a.CleanTitle}
	}
	second, err := c.Analyze(ctx, raw, fields)
	if err != nil {
		return "erreur", err.Error()
	}
	if err := json.Unmarshal(second, &a); err != nil {
		return "erreur", "analyse illisible : " + err.Error()
	}
	s.mu.Lock()
	r.Category = category
	if a.Nomenclature != nil {
		r.Built = a.Nomenclature.BuiltName
	}
	if pick != nil {
		r.TMDB = fmt.Sprintf("%s (%d)", pick.Title, yearOf(pick.Year))
	}
	s.mu.Unlock()

	ok, reason := decide(&a, pick, pickReason)
	if !ok {
		return "à revoir", reason
	}
	if j.DryRun {
		return "simulé", "publiable : " + a.Nomenclature.BuiltName
	}

	// Publication, puis seed.
	desc, format := batchDescription(&a, pick, templates, category)
	meta := map[string][]string{"category": {category}, "description": {desc}, "description_format": {format}, "mediainfo": {mi},
		"meta[work_title]": {pick.Title}, "meta[year]": {fmt.Sprint(yearOf(pick.Year))}, "meta[tmdb_id]": {fmt.Sprint(pick.ID)}, "meta[tmdb_type]": {kind},
		"meta[poster_url]": {pick.PosterURL}, "meta[synopsis]": {pick.Overview}}
	for k, v := range facets {
		meta["meta[facets]["+k+"]"] = []string{v}
	}
	for i, tag := range a.Tags {
		meta[fmt.Sprintf("meta[tags][%d]", i)] = []string{tag}
	}
	res, err := c.Upload(ctx, raw, nil, meta)
	if err != nil {
		return "erreur", err.Error()
	}
	s.mu.Lock()
	r.ID = res.ID
	r.URL = s.cfg.SiteURL + "/torrents/" + res.ID
	outDir := s.cfg.OutDir
	s.mu.Unlock()
	personalized, err := c.Download(ctx, res.ID)
	if err != nil {
		return "publié", "publié, mais .torrent non récupéré : " + err.Error()
	}
	saved := filepath.Join(outDir, sanitize(t.Name)+".torrent")
	_ = writeFileMkdir(saved, personalized)
	if tc, cerr := newTorrentClient(clientCfg); cerr == nil && tc != nil {
		savePath := filepath.Dir(r.Path)
		if remote {
			savePath = pathDir(r.Path)
		}
		if aerr := tc.Add(ctx, personalized, savePath, clientCfg.SkipCheck, clientCfg.Label); aerr != nil {
			return "publié", "publié ; client : " + aerr.Error()
		}
		return "publié", "publié et remis en seed"
	}
	return "publié", "publié ; .torrent dans " + path.Base(saved)
}

// batchDescription : le modèle par défaut du membre pour la famille, sinon
// une présentation sobre. Les variables suivent docs/22 §3.
func batchDescription(a *analysis, pick *tmdbResult, templates map[string]presTemplate, category string) (string, string) {
	family := strings.SplitN(category, "-", 2)[0]
	n := a.Nomenclature
	v := func(k string) string {
		if n == nil {
			return ""
		}
		return n.Facets[k].Value
	}
	media := func(k string) string {
		if n == nil || n.Media == nil || n.Media[k] == nil {
			return ""
		}
		return fmt.Sprint(n.Media[k])
	}
	data := map[string]string{
		"titre": pick.Title, "annee": fmt.Sprint(yearOf(pick.Year)), "type": map[bool]string{true: "Série", false: "Film"}[a.GuessedType == "tv"],
		"synopsis": pick.Overview, "affiche": pick.PosterURL, "tmdb_url": fmt.Sprintf("https://www.themoviedb.org/%s/%d", map[bool]string{true: "tv", false: "movie"}[a.GuessedType == "tv"], pick.ID),
		"nom_release": a.Name, "taille": a.SizeHuman, "nb_fichiers": fmt.Sprint(a.FileCount), "tags": strings.Join(a.Tags, ", "),
		"source": v("source"), "edition": v("edition"), "team": v("group"), "langues": v("languages"), "resolution": v("resolution"),
		"codec_video": v("video_codec"), "profondeur": v("bit_depth"), "hdr": v("hdr"), "codec_audio": v("audio_codec"), "canaux": v("channels"),
		"duree": media("duration"), "debit": media("bitrate"), "sous_titres": media("subtitles"), "uploadeur": "", "date": time.Now().Format("02/01/2006"),
	}
	if n != nil {
		data["nom_release"] = n.BuiltName
		data["nfo"] = n.NFO
	}
	tpl, ok := templates[family]
	if !ok {
		tpl, ok = templates["*"]
	}
	if ok {
		return renderTemplate(tpl.Body, data), tpl.Format
	}
	body := "[center][img]{{affiche}}[/img]\n[size=22][b]{{titre}}[/b][/size]{{#annee}} ({{annee}}){{/annee}}[/center]\n\n{{#synopsis}}[h2]Synopsis[/h2]\n[quote]{{synopsis}}[/quote]{{/synopsis}}\n\n[h2]Fiche technique[/h2]\n[list]\n[*][b]Release[/b] : [c]{{nom_release}}[/c]\n[*][b]Source[/b] : {{source}}{{#edition}} · {{edition}}{{/edition}}\n[*][b]Résolution[/b] : {{resolution}}{{#hdr}} · {{hdr}}{{/hdr}}\n[*][b]Vidéo[/b] : {{codec_video}} {{profondeur}}\n[*][b]Audio[/b] : {{codec_audio}} {{canaux}} — {{langues}}\n{{#sous_titres}}[*][b]Sous-titres[/b] : {{sous_titres}}{{/sous_titres}}\n{{#duree}}[*][b]Durée[/b] : {{duree}}{{#debit}} · {{debit}}{{/debit}}{{/duree}}\n[*][b]Taille[/b] : {{taille}} ({{nb_fichiers}} fichier(s))\n{{#team}}[*][b]Team[/b] : {{team}}{{/team}}\n[/list]\n\n[center][url={{tmdb_url}}]Fiche TMDB[/url] · publié par Bifröst, {{date}}[/center]"
	return renderTemplate(body, data), "bbcode"
}

var _ = sync.Mutex{}

var videoExt = map[string]bool{"mkv": true, "mp4": true, "avi": true, "ts": true, "m2ts": true, "mov": true, "wmv": true, "webm": true, "iso": true, "m4v": true, "mpg": true, "mpeg": true}

// mainExtension : extension du plus gros fichier de l'entrée, sans hacher.
func mainExtension(ctx context.Context, src fileSource, p string) (string, error) {
	ext := func(name string) string { return strings.ToLower(strings.TrimPrefix(path.Ext(name), ".")) }
	entries, err := src.List(ctx, p)
	if err != nil {
		return ext(p), nil // un fichier seul : List échoue, l'extension est dans le nom
	}
	var best DirEntry
	for _, e := range entries {
		if !e.IsDir && e.Size > best.Size {
			best = e
		}
	}
	if best.Name == "" {
		return "", errors.New("dossier sans fichier")
	}
	return ext(best.Name), nil
}
