package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Mode lot (docs/23 §7, phase 4) : un dossier entier est passé au crible et
// tout ce qui n'est pas encore sur Draupnirr est préparé puis publié sans
// intervention — à condition que la fiche soit SÛRE. Dans le doute, la
// release est mise « à revoir » et rien n'est publié. Par défaut on simule.

// rowStatus : l'état d'une ligne du lot, tel que la page l'affiche (BS dans
// ui/app.js) et que l'historique le garde — ces libellés sont le contrat.
type rowStatus string

const (
	stWaiting   rowStatus = "attente"
	stRunning   rowStatus = "en cours"
	stPresent   rowStatus = "déjà présent"
	stPublished rowStatus = "publié"
	stSimulated rowStatus = "simulé"
	stReview    rowStatus = "à revoir"
	stError     rowStatus = "erreur"
	stIgnored   rowStatus = "ignoré"
	// Œuvre choisie par un membre : la ligne reste « revu » jusqu'à sa
	// publication, le détail dit ce que le dernier lot en a conclu.
	stReviewed rowStatus = "revu"
)

type batchRow struct {
	Path     string      `json:"path"`
	Name     string      `json:"name"`
	Size     int64       `json:"size"`
	Status   rowStatus   `json:"status"`
	Detail   string      `json:"detail,omitempty"`
	Category string      `json:"category,omitempty"`
	Built    string      `json:"built,omitempty"`
	TMDB     string      `json:"tmdb,omitempty"`
	Edition  string      `json:"edition,omitempty"` // édition MusicBrainz d'un album
	ID       string      `json:"id,omitempty"`
	URL      string      `json:"url,omitempty"`
	InfoHash string      `json:"infohash,omitempty"`
	choice   *workChoice // choix du membre (loupe) au moment où le lot examine la ligne
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
	Reviewed   int         `json:"reviewed"` // œuvre choisie à la main, encore à publier
	Skipped    int         `json:"skipped"`
	Note       string      `json:"note,omitempty"` // pause imposée par Draupnirr avant la première release
	cancel     context.CancelFunc
	catFilm    string
	catTV      string
	albumSrc   string // source d'un album quand rien ne l'indique (ni .log, ni .cue)
	publishGap time.Duration
	include    map[string]bool // nil = tout le dossier
	maxSize    int64           // 0 = aucun plafond
	source     string          // "local" ou user@host, pour l'historique
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
	// Include : entrées cochées dans la page (chemins tels que les liste la
	// source). Absent = tout le dossier ; présent, seules celles-ci, même si
	// d'autres sont apparues depuis : on ne publie que ce qui a été montré.
	Include []string `json:"include"`
	// MaxSize : au-delà (octets), l'entrée est ignorée sans être hachée — un
	// dossier de cross-seed ou de liens de plusieurs To n'est pas une release.
	// 0 = aucun plafond ; la page envoie 300 Go par défaut.
	MaxSize int64 `json:"max_size"`
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
		Running: true, Started: time.Now(), cancel: cancel, catFilm: opts.CatFilm, catTV: opts.CatTV, albumSrc: source, publishGap: 7 * time.Second, maxSize: max(opts.MaxSize, 0)}
	if opts.Include != nil {
		j.include = map[string]bool{}
		for _, p := range opts.Include {
			j.include[p] = true
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
		end := histEvent{Kind: evBatchEnd, Batch: batchID(j), Source: j.source, Path: j.Root, Detail: j.Error, DryRun: j.DryRun,
			Data: map[string]any{"published": j.Published, "review": j.Review, "skipped": j.Skipped, "stopped": errors.Is(err, context.Canceled)}}
		s.mu.Unlock()
		s.hist.record(end)
	}
	c, err := s.client()
	if err != nil {
		finish(err)
		return
	}
	src := s.source()
	s.mu.Lock()
	j.source = sourceName(src)
	s.mu.Unlock()
	s.hist.record(histEvent{Kind: evBatchStart, Batch: batchID(j), Source: sourceName(src), Path: j.Root, DryRun: j.DryRun,
		Data: map[string]any{"max": j.Max, "limit": j.Limit, "only_video": j.OnlyVideo, "music": j.Music, "music_source": j.albumSrc,
			"category_film": j.catFilm, "category_tv": j.catTV, "included": len(j.include), "all": j.include == nil, "max_size": j.maxSize}})
	s.mu.Lock()
	env := batchEnv{clientCfg: s.cfg.Client, remote: s.cfg.Source == "ssh"}
	env.sourceTag, _ = s.me["source_tag"].(string)
	env.uploader, _ = s.me["name"].(string)
	s.mu.Unlock()
	if env.sourceTag == "" {
		env.sourceTag = "DRAUPNIRR"
	}

	// Les appels du lot attendent les 429 ; avant la première release (déjà
	// présent ?, modèles), la pause s'affiche sous la progression.
	ctx = withRetry(ctx, s.pauseNotice(&j.Note))
	entries, err := batchEntries(ctx, src, j.Root, j.Music)
	if err != nil {
		finish(err)
		return
	}
	s.mu.Lock()
	j.Rows = append(j.Rows, batchRows(entries, j.include)...)
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
	var seen []histEvent
	s.mu.Lock()
	for _, r := range j.Rows {
		if name, ok := present[r.Size]; ok {
			r.Status, r.Detail = stPresent, name
			j.Skipped++
			seen = append(seen, rowEvent(j, r))
		}
	}
	s.mu.Unlock()
	for _, e := range seen {
		s.hist.record(e)
	}

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
		if r.Status != stWaiting {
			continue
		}
		if j.maxSize > 0 && r.Size > j.maxSize {
			s.mu.Lock()
			r.Status, r.Detail = stIgnored, "au-delà du plafond ("+humanSize(r.Size)+" > "+humanSize(j.maxSize)+") : pas haché"
			e := rowEvent(j, r)
			s.mu.Unlock()
			s.hist.record(e)
			continue
		}
		// Le choix du membre (loupe), lu une fois pour la ligne : ignorée, œuvre forcée, ou rien.
		s.mu.Lock()
		r.choice = s.hist.choiceFor(j.source, r.Path, r.Size)
		s.mu.Unlock()
		if r.choice != nil && r.choice.Ignored {
			s.mu.Lock()
			r.Status, r.Detail = stIgnored, ignoredByHand
			e := rowEvent(j, r)
			s.mu.Unlock()
			s.hist.record(e)
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
		r.Status, r.Detail = stRunning, "hachage"
		s.mu.Unlock()
		// Une pause imposée par Draupnirr (429) se voit dans la ligne, sinon elle reste figée sur l'étape.
		rctx := withRetry(ctx, s.pauseNotice(&r.Detail))
		status, detail := run(rctx, c, src, j, r, env)
		s.mu.Lock()
		status, detail = reviewedStatus(status, detail, r.choice != nil)
		switch status {
		case stPublished, stSimulated:
			j.Published++
		case stReview:
			j.Review++
		case stReviewed:
			j.Reviewed++
		}
		r.Status, r.Detail = status, detail
		e := rowEvent(j, r)
		s.mu.Unlock()
		// Un arrêt en cours de route n'est pas une décision : il ne doit pas
		// remplacer la précédente (« à revoir » et sa raison) dans l'historique.
		if status != stError || ctx.Err() == nil {
			s.hist.record(e)
		}
		if status == stPublished {
			select { // throttle api-write : 10 publications par minute
			case <-ctx.Done():
			case <-time.After(j.publishGap):
			}
		}
	}
	finish(nil)
}

// reviewedStatus : une release dont l'œuvre a été choisie à la main reste
// « revu » tant qu'elle n'est pas publiée ; le détail garde la conclusion du lot.
func reviewedStatus(status rowStatus, detail string, chosen bool) (rowStatus, string) {
	if !chosen {
		return status, detail
	}
	switch status {
	case stSimulated:
		return stReviewed, "simulé, " + detail
	case stReview:
		return stReviewed, "encore à revoir : " + detail
	}
	return status, detail
}

func batchID(j *batchJob) string { return j.Started.Format(time.RFC3339) }

// rowEvent : la décision du lot pour une ligne, telle qu'elle s'affiche (sous s.mu).
func rowEvent(j *batchJob, r *batchRow) histEvent {
	kind := evDecision
	if r.Status == stPublished {
		kind = evPublish
	}
	return histEvent{Kind: kind, Batch: batchID(j), Source: j.source, Path: r.Path, Name: r.Name, Size: r.Size, InfoHash: r.InfoHash,
		Status: r.Status, Detail: r.Detail, Category: r.Category, Built: r.Built, TMDB: r.TMDB, Edition: r.Edition, ID: r.ID, URL: r.URL, DryRun: j.DryRun}
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

// batchRows : une ligne « attente » par entrée retenue (include nil = toutes).
func batchRows(entries []DirEntry, include map[string]bool) []*batchRow {
	var rows []*batchRow
	for _, e := range entries {
		if include == nil || include[e.Path] {
			rows = append(rows, &batchRow{Path: e.Path, Name: e.Name, Size: e.Size, Status: stWaiting})
		}
	}
	return rows
}

const pauseMark = " · Draupnirr limite les appels, reprise dans "

// pauseNotice : ajoute à *detail l'attente imposée par Draupnirr, puis la
// retire à la reprise (d = 0).
func (s *server) pauseNotice(detail *string) func(time.Duration) {
	return func(d time.Duration) {
		s.mu.Lock()
		step, _, _ := strings.Cut(*detail, pauseMark)
		if d > 0 {
			step += pauseMark + fmt.Sprintf("%d s", int((d+time.Second-1)/time.Second))
		}
		*detail = step
		s.mu.Unlock()
	}
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
	if err == nil {
		s.mu.Lock()
		r.InfoHash = t.InfoHash
		s.mu.Unlock()
	}
	return raw, t, err
}

// batchOne : prépare une release vidéo et décide. Renvoie (statut, détail).
func (s *server) batchOne(ctx context.Context, c *Client, src fileSource, j *batchJob, r *batchRow, env batchEnv) (rowStatus, string) {
	// Aucun fichier au premier niveau : un dossier de releases (cross-seed,
	// collection), pas une release — le hacher d'un bloc prendrait des heures
	// pour rien. Un disque complet (BDMV, VIDEO_TS) y passe aussi : à la main.
	// Sauf si un membre a choisi l'œuvre dans la loupe : il a vu que c'est une
	// release (une saison rangée dans un sous-dossier, par exemple).
	ch := r.choice
	ext, err := mainExtension(ctx, src, r.Path)
	if errors.Is(err, errNoTopFile) && ch == nil {
		return stReview, "aucun fichier à la racine : un dossier de releases ? Coche-les une par une (un disque complet se publie à la main)"
	}
	if j.OnlyVideo && err == nil && !videoExt[ext] {
		return stIgnored, "pas une vidéo (." + ext + ")"
	}
	raw, t, err := s.batchTorrent(ctx, src, r, env.sourceTag)
	if err != nil {
		return stError, err.Error()
	}
	s.setDetail(r, "mediainfo")
	mi, _ := src.MediaInfo(ctx, src.MainFile(r.Path, t))

	// Première analyse : catégorie devinée depuis le type.
	s.setDetail(r, "analyse")
	var a analysis
	if err := analyzeInto(ctx, c, raw, map[string][]string{"category": {j.catFilm}, "mediainfo": {mi}}, &a); err != nil {
		return stError, err.Error()
	}
	kind := kindMovie
	if tmdbKind(a.GuessedType) == kindTV {
		kind = kindTV
	}

	// Œuvre TMDB : celle qu'un membre a choisie, sinon une trouvée seulement si sûre.
	var pick *tmdbResult
	pickReason := ""
	if ch != nil && ch.TMDBID > 0 {
		kind = ch.TMDBType
		pick = &tmdbResult{ID: ch.TMDBID, Title: ch.Title, Year: float64(ch.Year), Overview: ch.Overview, PosterURL: ch.PosterURL}
	} else {
		var results []tmdbResult
		if rawT, err := c.TMDB(ctx, a.CleanTitle, string(kind)); err == nil {
			var resp struct {
				Results []tmdbResult `json:"results"`
			}
			_ = json.Unmarshal(rawT, &resp)
			results = resp.Results
		}
		pick, pickReason = pickWork(&a, results)
	}
	category := j.catFilm
	if kind == kindTV {
		category = j.catTV
	}
	if ch != nil && ch.Category != "" {
		category = ch.Category
	}

	// Seconde analyse, avec ce que le nom déclare (source, team, édition) et
	// ce qu'un membre a corrigé dans la loupe.
	facets := map[string]string{}
	if a.Nomenclature != nil {
		for _, k := range []string{"source", "edition", "group"} {
			if v := a.Nomenclature.NameFacets[k]; v != "" {
				facets[k] = v
			}
		}
	}
	episode := episodeOf(r.Name)
	if ch != nil {
		for k, v := range ch.Facets {
			if v != "" {
				facets[k] = v
			}
		}
		if ch.Episode != "" {
			episode = ch.Episode
		}
	}
	fields := map[string][]string{"category": {category}, "mediainfo": {mi}, "year": {fmt.Sprint(yearOf(a.Year))}}
	for k, v := range facets {
		fields["facets["+k+"]"] = []string{v}
	}
	// La saison (ou l'épisode) du nom : sans elle, le nom canonique d'une série la perd.
	if kind == kindTV {
		fields["episode"] = []string{episode}
	}
	if pick != nil {
		fields["work_title"] = []string{pick.Title}
		fields["tmdb_id"] = []string{fmt.Sprint(pick.ID)}
		fields["tmdb_type"] = []string{string(kind)}
		if y := yearOf(pick.Year); y > 0 {
			fields["year"] = []string{fmt.Sprint(y)}
		}
	} else if a.CleanTitle != "" {
		fields["work_title"] = []string{a.CleanTitle}
	}
	if err := analyzeInto(ctx, c, raw, fields, &a); err != nil {
		return stError, err.Error()
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
	if ok && kind == kindTV && episode == "" {
		ok, reason = false, "saison absente du nom (S01, S01E03) : Draupnirr la refuserait"
	}
	if !ok {
		return stReview, reason
	}
	if j.DryRun {
		return stSimulated, "publiable : " + a.Nomenclature.BuiltName
	}

	// Publication, puis seed. La présentation suit l'œuvre retenue (une série
	// choisie à la main peut avoir été devinée comme un film) et sa saison.
	desc, format := batchDescription(&a, pick, kind, episode, env.templates, category, env.uploader)
	meta := map[string][]string{"category": {category}, "description": {desc}, "description_format": {format}, "mediainfo": {mi},
		"meta[work_title]": {pick.Title}, "meta[year]": {fmt.Sprint(yearOf(pick.Year))}, "meta[tmdb_id]": {fmt.Sprint(pick.ID)}, "meta[tmdb_type]": {string(kind)},
		"meta[poster_url]": {pick.PosterURL}, "meta[synopsis]": {pick.Overview}}
	if kind == kindTV {
		meta["meta[episode]"] = []string{episode}
	}
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
func (s *server) publishAndSeed(ctx context.Context, c *Client, r *batchRow, raw []byte, t *Torrent, fields map[string][]string, env batchEnv) (rowStatus, string) {
	res, err := c.Upload(ctx, raw, nil, fields)
	if err != nil {
		return stError, err.Error()
	}
	s.mu.Lock()
	r.ID = res.ID
	r.URL = s.cfg.SiteURL + "/torrents/" + res.ID
	outDir := s.cfg.OutDir
	s.mu.Unlock()
	personalized, err := c.Download(ctx, res.ID)
	if err != nil {
		return stPublished, "publié, mais .torrent non récupéré : " + err.Error()
	}
	saved := filepath.Join(outDir, sanitize(t.Name)+".torrent")
	_ = writeFileMkdir(saved, personalized)
	if tc, cerr := newTorrentClient(env.clientCfg); cerr == nil && tc != nil {
		savePath := filepath.Dir(r.Path)
		if env.remote {
			savePath = pathDir(r.Path)
		}
		if aerr := tc.Add(ctx, personalized, savePath, env.clientCfg.SkipCheck, env.clientCfg.Label); aerr != nil {
			return stPublished, "publié ; client : " + aerr.Error()
		}
		return stPublished, "publié et remis en seed"
	}
	return stPublished, "publié ; .torrent dans " + path.Base(saved)
}

// batchDescription : le modèle du membre pour la famille, sinon celui du
// site, sinon une présentation sobre. Les variables suivent docs/22 §3.
func batchDescription(a *analysis, pick *tmdbResult, kind tmdbKind, episode string, templates []presTemplate, category, uploader string) (string, string) {
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
		"titre": pick.Title, "annee": fmt.Sprint(yearOf(pick.Year)), "type": map[bool]string{true: "Série", false: "Film"}[kind == kindTV],
		"synopsis": pick.Overview, "affiche": pick.PosterURL, "tmdb_url": fmt.Sprintf("https://www.themoviedb.org/%s/%d", kind, pick.ID),
		"nom_release": a.Name, "taille": a.SizeHuman, "nb_fichiers": fmt.Sprint(a.FileCount), "tags": strings.Join(a.Tags, ", "),
		"source": v("source"), "edition": v("edition"), "team": v("group"), "langues": v("languages"), "resolution": v("resolution"),
		"codec_video": v("video_codec"), "profondeur": v("bit_depth"), "hdr": v("hdr"), "codec_audio": v("audio_codec"), "canaux": v("channels"),
		"duree": media("duration"), "debit": media("bitrate"), "sous_titres": media("subtitles"), "uploadeur": uploader, "date": today(),
	}
	data["episode"] = episode
	if n != nil {
		data["nom_release"] = n.BuiltName
		data["nfo"] = n.NFO
	}
	return describe(templates, strings.SplitN(category, "-", 2)[0], func(string) map[string]string { return data }, soberFilm)
}

const soberFilm = "[center][img]{{affiche}}[/img]\n[size=22][b]{{titre}}[/b][/size]{{#annee}} ({{annee}}){{/annee}}[/center]\n\n{{#synopsis}}[h2]Synopsis[/h2]\n[quote]{{synopsis}}[/quote]{{/synopsis}}\n\n[h2]Fiche technique[/h2]\n[list]\n[*][b]Release[/b] : [c]{{nom_release}}[/c]\n[*][b]Source[/b] : {{source}}{{#edition}} · {{edition}}{{/edition}}\n[*][b]Résolution[/b] : {{resolution}}{{#hdr}} · {{hdr}}{{/hdr}}\n[*][b]Vidéo[/b] : {{codec_video}} {{profondeur}}\n[*][b]Audio[/b] : {{codec_audio}} {{canaux}} — {{langues}}\n{{#sous_titres}}[*][b]Sous-titres[/b] : {{sous_titres}}{{/sous_titres}}\n{{#duree}}[*][b]Durée[/b] : {{duree}}{{#debit}} · {{debit}}{{/debit}}{{/duree}}\n[*][b]Taille[/b] : {{taille}} ({{nb_fichiers}} fichier(s))\n{{#team}}[*][b]Team[/b] : {{team}}{{/team}}\n[/list]\n\n[center][url={{tmdb_url}}]Fiche TMDB[/url] · publié par Bifröst, {{date}}[/center]"

var _ = sync.Mutex{}

var videoExt = map[string]bool{"mkv": true, "mp4": true, "avi": true, "ts": true, "m2ts": true, "mov": true, "wmv": true, "webm": true, "iso": true, "m4v": true, "mpg": true, "mpeg": true}

// humanSize : comme human() de la page (base 1024, « 2,5 To »).
func humanSize(n int64) string {
	units := []string{"o", "Ko", "Mo", "Go", "To"}
	f, i := float64(n), 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d o", n)
	}
	return strings.Replace(fmt.Sprintf("%.1f %s", f, units[i]), ".", ",", 1)
}

// reEpisode : « S06 », « S06E03 », « S06E01-E02 » (aussi écrit S06E01E02 ou S06E01-02) dans un nom de release.
var reEpisode = regexp.MustCompile(`(?i)(?:^|[ ._-])S(\d{1,2})(?:E(\d{1,3})(?:-?E?(\d{1,3}))?)?(?:[ ._-]|$)`)

// episodeOf : la saison ou l'épisode déclaré par le nom, dans la forme que Draupnirr
// attend (« S03 », « S03E07 », « S03E01-E02 », deux chiffres au moins) ; "" sinon.
func episodeOf(name string) string {
	m := reEpisode.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	pad := func(d string) string {
		if len(d) == 1 {
			return "0" + d
		}
		return d
	}
	out := "S" + pad(m[1])
	if m[2] != "" {
		out += "E" + pad(m[2])
	}
	if m[3] != "" {
		out += "-E" + pad(m[3])
	}
	return out
}

// ignoredByHand : le détail d'une release écartée depuis la loupe.
const ignoredByHand = "ignorée à la main"

var errNoTopFile = errors.New("dossier sans fichier à la racine")

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
		return "", errNoTopFile
	}
	return ext(best.Name), nil
}
