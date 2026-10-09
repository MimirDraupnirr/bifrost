package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
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
	Edition  string `json:"edition,omitempty"` // édition MusicBrainz d'un album
	ID       string `json:"id,omitempty"`
	URL      string `json:"url,omitempty"`
}

type batchJob struct {
	Root       string      `json:"root"`
	DryRun     bool        `json:"dry_run"`
	Max        int         `json:"max"`
	Limit      int         `json:"limit"`
	OnlyVideo  bool        `json:"only_video"`
	Music      bool        `json:"music"`
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
	albumSrc   string // source d'un album quand rien ne l'indique (ni .log, ni .cue)
	publishGap time.Duration
	exclude    map[string]bool
}

type batchOpts struct {
	Root      string `json:"path"`
	DryRun    bool   `json:"dry_run"`
	Max       int    `json:"max"`
	Limit     int    `json:"limit"`      // releases examinées au plus (0 = toutes)
	OnlyVideo bool   `json:"only_video"` // ignorer ce qui n'est pas une vidéo (ebooks, logiciels, musique)
	// Musique : chaque dossier de pistes est un album (CD1/CD2 regroupés), une release par album.
	Music       bool   `json:"music"`
	MusicSource string `json:"music_source"` // WEB, CD, VINYL ; vide = « à revoir » faute de source
	CatFilm     string `json:"category_film"`
	CatTV       string `json:"category_tv"`
	// Exclude : entrées du dossier (chemins tels que les liste la source) que
	// l'utilisateur a décochées. Vide = tout le dossier.
	Exclude []string `json:"exclude"`
}

// analysis : la partie de la réponse de /api/upload/analyze que le lot lit.
type analysis struct {
	OK           bool          `json:"ok"`
	Name         string        `json:"name"`
	InfoHash     string        `json:"infohash"`
	CleanTitle   string        `json:"clean_title"`
	Year         any           `json:"year"`
	GuessedType  string        `json:"guessed_type"`
	Warnings     []string      `json:"warnings"`
	Executable   bool          `json:"executable"`
	SizeHuman    string        `json:"size_human"`
	FileCount    int           `json:"file_count"`
	Tags         []string      `json:"suggested_tags"`
	Bot          []issue       `json:"bot"`
	Nomenclature *nomenclature `json:"nomenclature"`
	Music        *musicBlock   `json:"music"`
}

type facetValue struct {
	Value  string `json:"value"`
	Origin string `json:"origin"`
}

type nomenclature struct {
	BuiltName    string               `json:"built_name"`
	Missing      []string             `json:"missing"`
	MissingTitle bool                 `json:"missing_title"`
	NameFacets   looseMap[string]     `json:"name_facets"`
	Media        looseMap[any]        `json:"media"`
	NFO          string               `json:"nfo"`
	Facets       looseMap[facetValue] `json:"facets"`
}

// looseMap : PHP encode un tableau associatif VIDE en « [] », pas en « {} ».
// Un map Go refuse « [] » ; ici on l'accepte comme un map vide.
type looseMap[T any] map[string]T

func (m *looseMap[T]) UnmarshalJSON(b []byte) error {
	t := strings.TrimSpace(string(b))
	if t == "null" || strings.HasPrefix(t, "[") {
		*m = looseMap[T]{}
		return nil
	}
	var raw map[string]T
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*m = raw
	return nil
}

type tmdbResult struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title"`
	Year          any    `json:"year"`
	Overview      string `json:"overview"`
	PosterURL     string `json:"poster_url"`
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
		// Titre français OU titre original : les noms de release portent
		// presque toujours l'original.
		if dy >= -1 && dy <= 1 && (similarity(a.CleanTitle, r.Title) >= 0.6 || similarity(a.CleanTitle, r.OriginalTitle) >= 0.6) {
			return r, ""
		}
	}
	if want == 0 {
		return nil, "année absente du nom : impossible de choisir l'œuvre sans risque"
	}
	return nil, "aucune œuvre TMDB avec le même titre et la même année"
}

// analysisBlocker : ce qui envoie en revue quelle que soit la famille —
// analyse refusée, doublon, exécutable, réserve de Ratatosk (sauf `ignore`).
// Ratatosk parle en block / warn / info (ReviewIssue) : seul info laisse passer.
func analysisBlocker(a *analysis, ignore ...string) (ok bool, reason string) {
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
		if b.Level != "info" && !slices.Contains(ignore, b.Code) {
			return false, "Ratatosk : " + b.Message
		}
	}
	return true, ""
}

// decide : publiable ? Une seule raison suffit à envoyer en revue.
func decide(a *analysis, pick *tmdbResult, pickReason string) (ok bool, reason string) {
	// Espaces dans le nom du dossier : sans objet, c'est le nom CALCULÉ qui sera publié.
	if ok, why := analysisBlocker(a, "naming_spaces"); !ok {
		return false, why
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
	source := strings.ToUpper(strings.TrimSpace(opts.MusicSource))
	if source != "" && !slices.Contains([]string{"WEB", "CD", "VINYL"}, source) {
		return nil, errors.New("source d'album inconnue : " + opts.MusicSource + " (WEB, CD ou VINYL)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &batchJob{Root: opts.Root, DryRun: opts.DryRun, Max: opts.Max, Limit: opts.Limit, OnlyVideo: opts.OnlyVideo && !opts.Music, Music: opts.Music,
		Running: true, Started: time.Now(), cancel: cancel, catFilm: opts.CatFilm, catTV: opts.CatTV, albumSrc: source, publishGap: 7 * time.Second}
	if len(opts.Exclude) > 0 {
		j.exclude = map[string]bool{}
		for _, p := range opts.Exclude {
			j.exclude[p] = true
		}
	}
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

// batchEnv : ce qui ne change pas d'une release à l'autre du lot.
type batchEnv struct {
	sourceTag string
	clientCfg ClientConfig
	remote    bool
	templates []presTemplate
	uploader  string
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
	env := batchEnv{clientCfg: s.cfg.Client, remote: s.cfg.Source == "ssh"}
	env.sourceTag, _ = s.me["source_tag"].(string)
	env.uploader, _ = s.me["name"].(string)
	s.mu.Unlock()
	if env.sourceTag == "" {
		env.sourceTag = "DRAUPNIRR"
	}

	entries, err := batchEntries(ctx, src, j.Root, j.Music)
	if err != nil {
		finish(err)
		return
	}
	s.mu.Lock()
	j.Rows = append(j.Rows, batchRows(entries, j.exclude)...)
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
	env.templates = s.loadTemplates(ctx, c)
	run := s.batchOne
	if j.Music {
		run = s.batchAlbum
	}
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
			s.setDetail(r, "plafond du lot atteint, non traité")
			continue
		}
		s.mu.Lock()
		r.Status, r.Detail = "en cours", "hachage"
		s.mu.Unlock()
		// Une pause imposée par Draupnirr (429) se voit dans la ligne, sinon elle reste figée sur l'étape.
		rctx := withWaitNotice(ctx, func(d time.Duration) { s.setPause(r, d) })
		status, detail := run(rctx, c, src, j, r, env)
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

// batchEntries : les releases candidates du dossier, telles que la page les
// propose à cocher et que le lot les parcourt — ses entrées non vides ; en
// musique, ses albums à toute profondeur.
func batchEntries(ctx context.Context, src fileSource, root string, music bool) ([]DirEntry, error) {
	var entries []DirEntry
	var err error
	if music {
		entries, err = src.Albums(ctx, root)
	} else {
		entries, err = src.List(ctx, root)
	}
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(entries, func(e DirEntry) bool { return e.Size <= 0 }), nil
}

// batchRows : une ligne « attente » par entrée restée cochée.
func batchRows(entries []DirEntry, exclude map[string]bool) []*batchRow {
	var rows []*batchRow
	for _, e := range entries {
		if !exclude[e.Path] {
			rows = append(rows, &batchRow{Path: e.Path, Name: e.Name, Size: e.Size, Status: "attente"})
		}
	}
	return rows
}

const pauseMark = " · Draupnirr limite les appels, reprise dans "

// setPause : ajoute à l'étape en cours l'attente imposée par Draupnirr.
func (s *server) setPause(r *batchRow, d time.Duration) {
	s.mu.Lock()
	step, _, _ := strings.Cut(r.Detail, pauseMark)
	r.Detail = step + pauseMark + fmt.Sprintf("%d s", int(d.Round(time.Second)/time.Second))
	s.mu.Unlock()
}

func (s *server) setDetail(r *batchRow, detail string) {
	s.mu.Lock()
	r.Detail = detail
	s.mu.Unlock()
}

// loadTemplates : les modèles du membre, puis ceux du site (`site: true`).
func (s *server) loadTemplates(ctx context.Context, c *Client) []presTemplate {
	raw, err := c.Presentations(ctx)
	if err != nil {
		return nil
	}
	var resp struct {
		Templates []presTemplate `json:"templates"`
	}
	_ = json.Unmarshal(raw, &resp)
	return resp.Templates
}

type presTemplate struct {
	Name      string  `json:"name"`
	Family    *string `json:"family"`
	Format    string  `json:"format"`
	IsDefault bool    `json:"is_default"`
	Site      bool    `json:"site"`
	Body      string  `json:"body"`
}

// pickTemplate : le défaut du membre pour la famille, puis son défaut
// toutes catégories, sinon le modèle du site de la famille (docs/25 §4.2).
// Comme presDefault() du site, à un écart près : un lot ne rédige pas à la
// main, donc un membre qui a des modèles sans défaut reçoit celui du site.
func pickTemplate(templates []presTemplate, family string) *presTemplate {
	var own, ownAll, site *presTemplate
	for i := range templates {
		t := &templates[i]
		fam := ""
		if t.Family != nil {
			fam = *t.Family
		}
		switch {
		case t.Site:
			if fam == family && (site == nil || (t.IsDefault && !site.IsDefault)) {
				site = t
			}
		case t.IsDefault && fam == family && own == nil:
			own = t
		case t.IsDefault && fam == "" && ownAll == nil:
			ownAll = t
		}
	}
	for _, t := range []*presTemplate{own, ownAll, site} {
		if t != nil {
			return t
		}
	}
	return nil
}

// describe : le modèle choisi, rendu avec les variables de son format ;
// sans aucun modèle, une présentation sobre.
func describe(templates []presTemplate, family string, data func(format string) map[string]string, fallback string) (string, string) {
	if t := pickTemplate(templates, family); t != nil {
		return renderTemplate(t.Body, data(t.Format)), t.Format
	}
	return renderTemplate(fallback, data("bbcode")), "bbcode"
}

func today() string { return time.Now().Format("02/01/2006") }

// batchTorrent : le .torrent de l'entrée (cache ou hachage), progression dans la ligne.
func (s *server) batchTorrent(ctx context.Context, src fileSource, r *batchRow, sourceTag string) ([]byte, *Torrent, error) {
	raw, cached, err := s.makeTorrentCached(ctx, src, r.Path, sourceTag, r.Size, func(done, total int64) {
		if total > 0 {
			s.setDetail(r, fmt.Sprintf("hachage %d %%", done*100/total))
		}
	})
	if err != nil {
		return nil, nil, err
	}
	if cached {
		s.setDetail(r, "torrent repris du cache")
	}
	t, err := parseTorrent(raw)
	return raw, t, err
}

// batchOne : prépare une release vidéo et décide. Renvoie (statut, détail).
func (s *server) batchOne(ctx context.Context, c *Client, src fileSource, j *batchJob, r *batchRow, env batchEnv) (string, string) {
	if j.OnlyVideo {
		if ext, err := mainExtension(ctx, src, r.Path); err == nil && !videoExt[ext] {
			return "ignoré", "pas une vidéo (." + ext + ")"
		}
	}
	raw, t, err := s.batchTorrent(ctx, src, r, env.sourceTag)
	if err != nil {
		return "erreur", err.Error()
	}
	s.setDetail(r, "mediainfo")
	mi, _ := src.MediaInfo(ctx, src.MainFile(r.Path, t))

	// Première analyse : catégorie devinée depuis le type.
	s.setDetail(r, "analyse")
	var a analysis
	if err := analyzeInto(ctx, c, raw, map[string][]string{"category": {j.catFilm}, "mediainfo": {mi}}, &a); err != nil {
		return "erreur", err.Error()
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
	if err := analyzeInto(ctx, c, raw, fields, &a); err != nil {
		return "erreur", err.Error()
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
	desc, format := batchDescription(&a, pick, env.templates, category, env.uploader)
	meta := map[string][]string{"category": {category}, "description": {desc}, "description_format": {format}, "mediainfo": {mi},
		"meta[work_title]": {pick.Title}, "meta[year]": {fmt.Sprint(yearOf(pick.Year))}, "meta[tmdb_id]": {fmt.Sprint(pick.ID)}, "meta[tmdb_type]": {kind},
		"meta[poster_url]": {pick.PosterURL}, "meta[synopsis]": {pick.Overview}}
	for k, v := range facets {
		meta["meta[facets]["+k+"]"] = []string{v}
	}
	for i, tag := range a.Tags {
		meta[fmt.Sprintf("meta[tags][%d]", i)] = []string{tag}
	}
	return s.publishAndSeed(ctx, c, r, raw, t, meta, env)
}

// publishAndSeed : publication, .torrent personnalisé gardé, remise en seed
// sur les mêmes données.
func (s *server) publishAndSeed(ctx context.Context, c *Client, r *batchRow, raw []byte, t *Torrent, fields map[string][]string, env batchEnv) (string, string) {
	res, err := c.Upload(ctx, raw, nil, fields)
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
	if tc, cerr := newTorrentClient(env.clientCfg); cerr == nil && tc != nil {
		savePath := filepath.Dir(r.Path)
		if env.remote {
			savePath = pathDir(r.Path)
		}
		if aerr := tc.Add(ctx, personalized, savePath, env.clientCfg.SkipCheck, env.clientCfg.Label); aerr != nil {
			return "publié", "publié ; client : " + aerr.Error()
		}
		return "publié", "publié et remis en seed"
	}
	return "publié", "publié ; .torrent dans " + path.Base(saved)
}

// batchDescription : le modèle du membre pour la famille, sinon celui du
// site, sinon une présentation sobre. Les variables suivent docs/22 §3.
func batchDescription(a *analysis, pick *tmdbResult, templates []presTemplate, category, uploader string) (string, string) {
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
		"duree": media("duration"), "debit": media("bitrate"), "sous_titres": media("subtitles"), "uploadeur": uploader, "date": today(),
	}
	if n != nil {
		data["nom_release"] = n.BuiltName
		data["nfo"] = n.NFO
	}
	return describe(templates, strings.SplitN(category, "-", 2)[0], func(string) map[string]string { return data }, soberFilm)
}

const soberFilm = "[center][img]{{affiche}}[/img]\n[size=22][b]{{titre}}[/b][/size]{{#annee}} ({{annee}}){{/annee}}[/center]\n\n{{#synopsis}}[h2]Synopsis[/h2]\n[quote]{{synopsis}}[/quote]{{/synopsis}}\n\n[h2]Fiche technique[/h2]\n[list]\n[*][b]Release[/b] : [c]{{nom_release}}[/c]\n[*][b]Source[/b] : {{source}}{{#edition}} · {{edition}}{{/edition}}\n[*][b]Résolution[/b] : {{resolution}}{{#hdr}} · {{hdr}}{{/hdr}}\n[*][b]Vidéo[/b] : {{codec_video}} {{profondeur}}\n[*][b]Audio[/b] : {{codec_audio}} {{canaux}} — {{langues}}\n{{#sous_titres}}[*][b]Sous-titres[/b] : {{sous_titres}}{{/sous_titres}}\n{{#duree}}[*][b]Durée[/b] : {{duree}}{{#debit}} · {{debit}}{{/debit}}{{/duree}}\n[*][b]Taille[/b] : {{taille}} ({{nb_fichiers}} fichier(s))\n{{#team}}[*][b]Team[/b] : {{team}}{{/team}}\n[/list]\n\n[center][url={{tmdb_url}}]Fiche TMDB[/url] · publié par Bifröst, {{date}}[/center]"

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
