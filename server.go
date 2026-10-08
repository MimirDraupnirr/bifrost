package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed ui
var uiFS embed.FS

// server : la page locale et ses points d'entrée JSON (/ui/*). Les /api/*
// sont ceux de Draupnirr, atteints via le Client — jamais exposés ici.
type server struct {
	mux        *http.ServeMux
	configPath string

	mu     sync.Mutex
	cfg    *Config
	me     map[string]any
	jobs   map[string]*job
	nextID int
	// Mot de passe SSH de la session : jamais écrit sur disque.
	sshPassword string
	// Hors loopback : la page exige le mot de passe local (auth.go).
	requireAuth bool
	sess        *sessions
	// Lot en cours ou terminé (batch.go).
	batch *batchJob
	// .torrent déjà créés (cache.go).
	cache *torrentCache
	// Dernière vérification de mise à jour (24 h de cache).
	update        *release
	updateChecked time.Time
}

// job : une préparation en cours (hachage, mediainfo, analyse), interrogée
// par la page jusqu'à `done`.
type job struct {
	ID        string          `json:"id"`
	Path      string          `json:"path"`
	Step      string          `json:"step"`
	Done      bool            `json:"done"`
	Error     string          `json:"error,omitempty"`
	Progress  [2]int64        `json:"progress"`
	Torrent   *Torrent        `json:"torrent,omitempty"`
	MainFile  string          `json:"main_file,omitempty"`
	MediaInfo string          `json:"-"`
	Exported  bool            `json:"exported"`
	MediaErr  string          `json:"mediainfo_error,omitempty"`
	Analysis  json.RawMessage `json:"analysis,omitempty"`
	// Album : mediainfo de tout le dossier, analyse en catégorie Musique.
	Music    bool   `json:"music"`
	Category string `json:"category,omitempty"`
	raw      []byte
}

func newServer(cfg *Config, configPath string) *server {
	s := &server{mux: http.NewServeMux(), cfg: cfg, configPath: configPath, jobs: map[string]*job{}, sess: newSessions(), cache: newTorrentCache(configPath)}
	sub, _ := fs.Sub(uiFS, "ui")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))
	// Jeton déjà enregistré : on se connecte tout de suite, sans attendre la
	// page — la connexion ne se demande qu'une fois, au premier lancement.
	if cfg.SiteURL != "" && cfg.APIToken != "" {
		go func() {
			if me, err := newClient(cfg.SiteURL, cfg.APIToken).Me(context.Background()); err == nil {
				s.mu.Lock()
				s.me = me
				s.mu.Unlock()
			}
		}()
	}
	s.mux.HandleFunc("GET /ui/state", s.state)
	s.mux.HandleFunc("POST /ui/connect", s.connect)
	s.mux.HandleFunc("POST /ui/settings", s.settings)
	s.mux.HandleFunc("POST /ui/login", s.login)
	s.mux.HandleFunc("POST /ui/password", s.password)
	s.mux.HandleFunc("POST /ui/client/test", s.clientTest)
	s.mux.HandleFunc("GET /ui/client/list", s.clientList)
	s.mux.HandleFunc("GET /ui/client/detect", s.clientDetect)
	s.mux.HandleFunc("POST /ui/update", s.doUpdate)
	s.mux.HandleFunc("POST /ui/batch/start", s.batchStartHandler)
	s.mux.HandleFunc("POST /ui/batch/stop", s.batchStopHandler)
	s.mux.HandleFunc("GET /ui/batch/status", s.batchStatusHandler)
	s.mux.HandleFunc("GET /ui/crossseed/scan", s.crossScan)
	s.mux.HandleFunc("POST /ui/crossseed/add", s.crossAdd)
	s.mux.HandleFunc("POST /ui/ssh/test", s.sshTest)
	s.mux.HandleFunc("POST /ui/ssh/accept", s.sshAccept)
	s.mux.HandleFunc("GET /ui/browse", s.browse)
	s.mux.HandleFunc("POST /ui/prepare", s.prepare)
	s.mux.HandleFunc("GET /ui/job/{id}", s.jobStatus)
	s.mux.HandleFunc("POST /ui/analyze", s.analyze)
	s.mux.HandleFunc("GET /ui/categories", s.categories)
	s.mux.HandleFunc("GET /ui/tmdb", s.tmdb)
	s.mux.HandleFunc("GET /ui/tmdb-images", s.tmdbImages)
	s.mux.HandleFunc("GET /ui/musicbrainz", s.musicbrainz)
	s.mux.HandleFunc("GET /ui/presentations", s.presentations)
	s.mux.HandleFunc("POST /ui/preview", s.preview)
	s.mux.HandleFunc("POST /ui/publish", s.publish)
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// La page n'est servie qu'à son propre navigateur : refuser les requêtes
	// venant d'un autre site (un onglet malveillant ne doit pas publier).
	if origin := r.Header.Get("Origin"); origin != "" && !strings.HasSuffix(origin, "//"+r.Host) {
		http.Error(w, "origine refusée", http.StatusForbidden)
		return
	}
	// Barrière du mot de passe local : la page et son état passent (ils
	// affichent l'écran de connexion), tout le reste exige la session.
	if s.requireAuth && strings.HasPrefix(r.URL.Path, "/ui/") && !s.sess.valid(r) {
		s.mu.Lock()
		noPassword := s.cfg.UIPasswordHash == ""
		s.mu.Unlock()
		allowed := r.URL.Path == "/ui/state" || r.URL.Path == "/ui/login" || (noPassword && r.URL.Path == "/ui/password")
		if !allowed {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "mot de passe local requis"})
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	var ae *apiError
	if errors.As(err, &ae) {
		status = ae.Status
	} else if _, ok := err.(*os.PathError); ok {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (s *server) client() (*Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.SiteURL == "" || s.cfg.APIToken == "" {
		return nil, errors.New("pas encore connecté à Draupnirr")
	}
	return newClient(s.cfg.SiteURL, s.cfg.APIToken), nil
}

func (s *server) state(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := *s.cfg
	me := s.me
	s.mu.Unlock()
	cfg.APIToken = "" // jamais renvoyé à la page
	cfg.Client.Password = ""
	cfg.UIPasswordHash = ""
	locked := s.requireAuth && !s.sess.valid(r)
	var upd *release
	if !locked {
		upd = s.pendingUpdate(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":          Version,
		"config":           cfg,
		"connected":        me != nil,
		"me":               me,
		"mediainfo":        mediaInfoAvailable(),
		"mediainfo_hint":   mediaInfoInstallHint(),
		"has_token":        s.cfg.APIToken != "",
		"ssh_password_set": s.sshPassword != "",
		"locked":           locked,
		"needs_password":   s.requireAuth && s.cfg.UIPasswordHash == "",
		"in_docker":        inDocker(),
		"auto_update":      s.cfg.autoUpdate(),
		"update":           upd,
	})
}

func (s *server) connect(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SiteURL string `json:"site_url"`
		Token   string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, err)
		return
	}
	in.SiteURL = strings.TrimRight(strings.TrimSpace(in.SiteURL), "/")
	in.Token = strings.TrimSpace(in.Token)
	if in.Token == "" {
		s.mu.Lock()
		in.Token = s.cfg.APIToken
		s.mu.Unlock()
	}
	if !strings.HasPrefix(in.SiteURL, "http") || in.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "adresse du site et jeton requis"})
		return
	}
	me, err := newClient(in.SiteURL, in.Token).Me(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	s.mu.Lock()
	s.cfg.SiteURL, s.cfg.APIToken, s.me = in.SiteURL, in.Token, me
	err = saveConfig(s.configPath, s.cfg)
	s.mu.Unlock()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"me": me})
}

func (s *server) settings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OutDir     string        `json:"out_dir"`
		Source     string        `json:"source"`
		SSH        *SSHConfig    `json:"ssh"`
		Password   string        `json:"password"`
		Client     *ClientConfig `json:"client"`
		AutoUpdate *bool         `json:"auto_update"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, err)
		return
	}
	s.mu.Lock()
	s.applySettings(in.OutDir, in.Source, in.SSH, in.Password)
	if in.Client != nil {
		if in.Client.Password == "" {
			in.Client.Password = s.cfg.Client.Password // champ vide = inchangé
		}
		s.cfg.Client = *in.Client
	}
	if in.AutoUpdate != nil {
		s.cfg.AutoUpdate = in.AutoUpdate
	}
	err := saveConfig(s.configPath, s.cfg)
	s.mu.Unlock()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

// applySettings : à appeler sous s.mu. Un changement de source oublie le
// dernier dossier (il appartenait à l'autre machine).
func (s *server) applySettings(outDir, source string, sshCfg *SSHConfig, password string) {
	if strings.TrimSpace(outDir) != "" {
		s.cfg.OutDir = strings.TrimSpace(outDir)
	}
	if source == "local" || source == "ssh" {
		if source != s.cfg.Source {
			s.cfg.LastDir = ""
		}
		s.cfg.Source = source
	}
	if sshCfg != nil {
		hostKey := s.cfg.SSH.HostKey
		if sshCfg.Host != s.cfg.SSH.Host || sshCfg.Port != s.cfg.SSH.Port {
			hostKey = "" // autre machine, autre clé d'hôte
		}
		s.cfg.SSH = *sshCfg
		s.cfg.SSH.HostKey = hostKey
	}
	if password != "" {
		s.sshPassword = password
	}
}

// source : la mise en œuvre courante (ce poste ou la seedbox).
func (s *server) source() fileSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Source == "ssh" {
		return &remoteSource{cfg: s.cfg.SSH, password: s.sshPassword}
	}
	return localSource{}
}

func (s *server) sshTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SSH      SSHConfig `json:"ssh"`
		Password string    `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, err)
		return
	}
	s.mu.Lock()
	s.applySettings("", "", &in.SSH, in.Password)
	_ = saveConfig(s.configPath, s.cfg)
	cfg, password := s.cfg.SSH, s.sshPassword
	s.mu.Unlock()

	client, err := dialSSH(&cfg, password)
	if err != nil {
		var hk *hostKeyError
		if errors.As(err, &hk) {
			writeJSON(w, http.StatusOK, map[string]any{"steps": []testStep{{Label: "Clé d'hôte", OK: false, Detail: hk.Error()}}, "hostkey": hk})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"steps": []testStep{{Label: "Connexion SSH", OK: false, Detail: err.Error()}}})
		return
	}
	defer client.Close()
	writeJSON(w, http.StatusOK, map[string]any{"steps": deployAgent(r.Context(), client, cfg.DataDir)})
}

// sshAccept : le membre a lu l'empreinte et l'accepte (TOFU explicite).
func (s *server) sshAccept(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "clé d'hôte requise"})
		return
	}
	s.mu.Lock()
	s.cfg.SSH.HostKey = in.Key
	err := saveConfig(s.configPath, s.cfg)
	s.mu.Unlock()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (s *server) browse(w http.ResponseWriter, r *http.Request) {
	src := s.source()
	path := r.URL.Query().Get("path")
	if path == "" {
		s.mu.Lock()
		path = s.cfg.LastDir
		s.mu.Unlock()
	}
	if path == "" {
		path = src.Home()
	}
	entries, err := src.List(r.Context(), path)
	if err != nil {
		fail(w, err)
		return
	}
	parent := filepath.Dir(path)
	if _, remote := src.(*remoteSource); remote {
		parent = pathDir(path)
	}
	s.mu.Lock()
	s.cfg.LastDir = path
	source := s.cfg.Source
	_ = saveConfig(s.configPath, s.cfg)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "parent": parent, "entries": entries, "source": source})
}

func pathDir(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i > 0 {
		return p[:i]
	}
	return "/"
}

// prepare : lance en arrière-plan hachage + mediainfo + première analyse.
func (s *server) prepare(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path     string `json:"path"`
		Category string `json:"category"`
		Hash     string `json:"hash"` // torrent déjà dans le client : export sans re-hachage
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chemin requis"})
		return
	}
	c, err := s.client()
	if err != nil {
		fail(w, err)
		return
	}
	src := s.source()
	sourceTag := "DRAUPNIRR"
	s.mu.Lock()
	if tag, ok := s.me["source_tag"].(string); ok && tag != "" {
		sourceTag = tag
	}
	s.nextID++
	j := &job{ID: fmt.Sprint(s.nextID), Path: in.Path, Step: "hachage"}
	s.jobs[j.ID] = j
	clientCfg := s.cfg.Client
	s.mu.Unlock()

	go func() {
		ctx := context.Background()
		var raw []byte
		var err error
		// Exporté depuis le client puis re-scellé : pas de re-hachage. Si le
		// client ne sait pas exporter, on hache les données comme d'habitude.
		if in.Hash != "" {
			if tc, cerr := newTorrentClient(clientCfg); cerr == nil && tc != nil {
				if orig, eerr := tc.Export(ctx, in.Hash); eerr == nil {
					raw, err = resealTorrent(orig, sourceTag)
					s.mu.Lock()
					j.Exported = err == nil
					s.mu.Unlock()
				}
			}
		}
		if raw == nil {
			var cached bool
			raw, cached, err = s.makeTorrentCached(ctx, src, in.Path, sourceTag, 0, func(done, total int64) {
				s.mu.Lock()
				j.Progress = [2]int64{done, total}
				s.mu.Unlock()
			})
			if cached {
				s.mu.Lock()
				j.Step = "torrent repris du cache"
				s.mu.Unlock()
			}
		}
		s.mu.Lock()
		if err != nil {
			j.Error, j.Done = err.Error(), true
			s.mu.Unlock()
			return
		}
		j.raw = raw
		j.Torrent, _ = parseTorrent(raw)
		j.MainFile = src.MainFile(in.Path, j.Torrent)
		// Le plus gros fichier est une piste : c'est un album. MediaInfo lit
		// alors tout le dossier (un objet par piste) et l'analyse se fait en
		// Musique — sauf si le membre a déjà choisi une catégorie Musique.
		j.Music = audioExt[extOf(j.MainFile)]
		target, category := j.MainFile, in.Category
		if j.Music {
			target = in.Path
			if !strings.HasPrefix(category, "musique") {
				category = "musique-album"
			}
		}
		j.Step = "mediainfo"
		s.mu.Unlock()

		mi, merr := src.MediaInfo(ctx, target)
		s.mu.Lock()
		if merr != nil {
			j.MediaErr = merr.Error()
		}
		j.MediaInfo = mi
		j.Step = "analyse"
		s.mu.Unlock()

		fields := map[string][]string{"category": {category}, "mediainfo": {mi}}
		out, aerr := c.Analyze(ctx, raw, fields)
		// Catégorie choisie par Bifröst : on suit celle que Draupnirr propose
		// d'après les pistes (FLAC, Album, OST).
		if aerr == nil && category != in.Category {
			var a analysis
			if json.Unmarshal(out, &a) == nil && a.Music != nil && a.Music.SuggestedCategory != "" && a.Music.SuggestedCategory != category {
				category = a.Music.SuggestedCategory
				fields["category"] = []string{category}
				out, aerr = c.Analyze(ctx, raw, fields)
			}
		}
		s.mu.Lock()
		if aerr != nil {
			j.Error = aerr.Error()
		}
		j.Category = category
		j.Analysis, j.Done, j.Step = out, true, "prêt"
		s.mu.Unlock()
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"job": j.ID})
}

func (s *server) jobStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	j, ok := s.jobs[r.PathValue("id")]
	var snapshot job
	if ok {
		snapshot = *j
	}
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "préparation inconnue"})
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

// formFields : la page envoie un objet plat {category, facets:{source:…}, …}
// qu'on aplatit en champs de formulaire Laravel (facets[source]).
func formFields(in map[string]any) map[string][]string {
	out := map[string][]string{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, vv := range x {
				key := k
				if prefix != "" {
					key = prefix + "[" + k + "]"
				}
				walk(key, vv)
			}
		case []any:
			for i, vv := range x {
				walk(fmt.Sprintf("%s[%d]", prefix, i), vv)
			}
		case nil:
		case bool:
			if x {
				out[prefix] = []string{"1"}
			}
		case float64:
			out[prefix] = []string{strings.TrimSuffix(fmt.Sprintf("%.0f", x), ".")}
		default:
			out[prefix] = []string{fmt.Sprint(x)}
		}
	}
	walk("", in)
	return out
}

// analyze : ré-analyse à chaque changement de la fiche (facettes, œuvre…).
func (s *server) analyze(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, err)
		return
	}
	jobID, _ := in["job"].(string)
	delete(in, "job")
	s.mu.Lock()
	j, ok := s.jobs[jobID]
	var raw []byte
	var mi string
	if ok {
		raw, mi = j.raw, j.MediaInfo
	}
	s.mu.Unlock()
	if !ok || raw == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "préparation inconnue"})
		return
	}
	c, err := s.client()
	if err != nil {
		fail(w, err)
		return
	}
	fields := formFields(in)
	fields["mediainfo"] = []string{mi}
	out, err := c.Analyze(r.Context(), raw, fields)
	if err != nil {
		fail(w, err)
		return
	}
	s.mu.Lock()
	j.Analysis = out
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(out)
}

func (s *server) proxyJSON(w http.ResponseWriter, out json.RawMessage, err error) {
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(out)
}

func (s *server) categories(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	if err != nil {
		fail(w, err)
		return
	}
	cats, err := c.Categories(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": cats})
}

func (s *server) tmdb(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	if err != nil {
		fail(w, err)
		return
	}
	out, err := c.TMDB(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("type"))
	s.proxyJSON(w, out, err)
}

func (s *server) tmdbImages(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	if err != nil {
		fail(w, err)
		return
	}
	q := r.URL.Query()
	out, err := c.TMDBImages(r.Context(), q.Get("id"), q.Get("type"), q.Get("episode"))
	s.proxyJSON(w, out, err)
}

// musicbrainz : éditions d'un album (artist, album, tracks) ou une édition (id).
func (s *server) musicbrainz(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	if err != nil {
		fail(w, err)
		return
	}
	q := url.Values{}
	for _, k := range []string{"artist", "album", "tracks", "id"} {
		if v := strings.TrimSpace(r.URL.Query().Get(k)); v != "" {
			q.Set(k, v)
		}
	}
	out, err := c.MusicBrainz(r.Context(), q)
	s.proxyJSON(w, out, err)
}

func (s *server) presentations(w http.ResponseWriter, r *http.Request) {
	c, err := s.client()
	if err != nil {
		fail(w, err)
		return
	}
	out, err := c.Presentations(r.Context())
	s.proxyJSON(w, out, err)
}

func (s *server) preview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content, Format string
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, err)
		return
	}
	c, err := s.client()
	if err != nil {
		fail(w, err)
		return
	}
	html, err := c.Preview(r.Context(), in.Content, in.Format)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"html": html})
}

// publish : POST /api/upload puis récupération du .torrent personnalisé,
// déposé dans OutDir (l'ajout au client torrent arrive en v0.2).
func (s *server) publish(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, err)
		return
	}
	jobID, _ := in["job"].(string)
	nfoText, _ := in["nfo_text"].(string)
	delete(in, "job")
	delete(in, "nfo_text")
	s.mu.Lock()
	j, ok := s.jobs[jobID]
	var raw []byte
	var mi, name string
	if ok && j.Torrent != nil {
		raw, mi, name = j.raw, j.MediaInfo, j.Torrent.Name
	}
	outDir := s.cfg.OutDir
	s.mu.Unlock()
	if raw == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "préparation inconnue"})
		return
	}
	c, err := s.client()
	if err != nil {
		fail(w, err)
		return
	}
	fields := formFields(in)
	fields["mediainfo"] = []string{mi}
	res, err := c.Upload(r.Context(), raw, []byte(nfoText), fields)
	if err != nil {
		fail(w, err)
		return
	}
	saved := ""
	personalized, derr := c.Download(context.Background(), res.ID)
	if derr == nil {
		_ = os.MkdirAll(outDir, 0o755)
		saved = filepath.Join(outDir, sanitize(name)+".torrent")
		if werr := os.WriteFile(saved, personalized, 0o644); werr != nil {
			derr, saved = werr, ""
		}
	}
	resp := map[string]any{"result": res, "saved": saved, "url": s.cfg.SiteURL + "/torrents/" + res.ID, "at": time.Now().Format(time.RFC3339)}
	if derr != nil {
		resp["download_error"] = derr.Error()
	}
	// Remise au client sur les mêmes données : save_path = le dossier qui
	// contient la release (son nom est celui du torrent).
	s.mu.Lock()
	clientCfg := s.cfg.Client
	remote := s.cfg.Source == "ssh"
	s.mu.Unlock()
	if tc, cerr := newTorrentClient(clientCfg); cerr == nil && tc != nil && derr == nil {
		savePath := filepath.Dir(j.Path)
		if remote {
			savePath = pathDir(j.Path)
		}
		if aerr := tc.Add(r.Context(), personalized, savePath, clientCfg.SkipCheck, clientCfg.Label); aerr != nil {
			resp["client_error"] = aerr.Error()
		} else {
			resp["client_added"] = clientCfg.Type
		}
	} else if cerr != nil {
		resp["client_error"] = cerr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

func sanitize(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
}

// ---- mot de passe local, clients, mise à jour ----

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	s.sess.throttle()
	s.mu.Lock()
	hash := s.cfg.UIPasswordHash
	s.mu.Unlock()
	if hash == "" || !checkPassword(hash, in.Password) {
		s.sess.failed()
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "mot de passe refusé"})
		return
	}
	s.sess.succeeded()
	s.sess.issue(w)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

// password : définit le mot de passe local (première fois), ou le change
// avec l'ancien.
func (s *server) password(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
		Current  string `json:"current"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if len(in.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": errWeakPassword.Error()})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.UIPasswordHash != "" && !checkPassword(s.cfg.UIPasswordHash, in.Current) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "mot de passe actuel refusé"})
		return
	}
	s.cfg.UIPasswordHash = hashPassword(in.Password)
	if err := saveConfig(s.configPath, s.cfg); err != nil {
		fail(w, err)
		return
	}
	s.sess.issue(w)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (s *server) clientTest(w http.ResponseWriter, r *http.Request) {
	var in ClientConfig
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, err)
		return
	}
	s.mu.Lock()
	if in.Password == "" {
		in.Password = s.cfg.Client.Password
	}
	s.cfg.Client = in
	_ = saveConfig(s.configPath, s.cfg)
	s.mu.Unlock()
	tc, err := newTorrentClient(in)
	if err != nil {
		fail(w, err)
		return
	}
	if tc == nil {
		writeJSON(w, http.StatusOK, map[string]string{"version": "aucun client : les .torrent iront dans le dossier de sortie"})
		return
	}
	v, err := tc.Test(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": v})
}

func (s *server) clientList(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg.Client
	s.mu.Unlock()
	tc, err := newTorrentClient(cfg)
	if err != nil {
		fail(w, err)
		return
	}
	if tc == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "aucun client torrent configuré"})
		return
	}
	list, err := tc.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	s.mu.Lock()
	site := s.cfg.SiteURL
	s.mu.Unlock()
	_, exportable := tc.(*qbitClient)
	writeJSON(w, http.StatusOK, map[string]any{"torrents": groupCrossSeeds(list, site), "exportable": exportable})
}

func (s *server) pendingUpdate(ctx context.Context) *release {
	s.mu.Lock()
	fresh := time.Since(s.updateChecked) < 24*time.Hour
	rel := s.update
	s.mu.Unlock()
	if fresh || Version == "dev" {
		return rel
	}
	rel, _ = checkUpdate(ctx)
	s.mu.Lock()
	s.update, s.updateChecked = rel, time.Now()
	s.mu.Unlock()
	return rel
}

func (s *server) doUpdate(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	busy := false
	for _, j := range s.jobs {
		if !j.Done {
			busy = true
		}
	}
	s.updateChecked = time.Time{}
	s.mu.Unlock()
	if busy {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "une préparation est en cours : réessaie après"})
		return
	}
	s.mu.Lock()
	if s.batch != nil && s.batch.Running {
		busy = true
	}
	s.mu.Unlock()
	if busy {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "un lot est en cours : réessaie après"})
		return
	}
	rel := s.pendingUpdate(r.Context())
	if rel == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "déjà à jour"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "mise à jour vers " + rel.Version + ", redémarrage…"})
	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := applyUpdate(context.Background(), rel); err != nil {
			fmt.Fprintln(os.Stderr, "mise à jour :", err)
		}
	}()
}

// clientEntry : une ligne de la liste « Depuis mon client ». Les cross-seeds
// (même chemin de données) sont fusionnés : une release, N trackers.
type clientEntry struct {
	ClientTorrent
	Copies      int  `json:"copies"`
	OnDraupnirr bool `json:"on_draupnirr"`
	// Tous les infohash des copies fusionnées : la copie Draupnirr en fait
	// partie même quand c'est une autre qui est affichée.
	Hashes []string `json:"hashes"`
}

func groupCrossSeeds(list []ClientTorrent, siteURL string) []clientEntry {
	host := ""
	if u, err := url.Parse(siteURL); err == nil {
		host = strings.ToLower(u.Hostname())
	}
	byPath := map[string]*clientEntry{}
	var order []string
	for _, t := range list {
		key := strings.TrimRight(t.Path, "/")
		if key == "" {
			key = t.Hash
		}
		onSite := false
		for _, h := range t.Trackers {
			if host != "" && h == host {
				onSite = true
			}
		}
		if e, ok := byPath[key]; ok {
			e.Copies++
			e.Hashes = append(e.Hashes, strings.ToLower(t.Hash))
			e.OnDraupnirr = e.OnDraupnirr || onSite
			// Pour l'export, préférer une copie qui n'est PAS celle de Draupnirr
			// (son infohash est déjà pris) et qui est complète.
			if (e.ClientTorrent.Progress < 1 && t.Progress >= 1) || (onSite && !e.OnDraupnirr) {
				e.ClientTorrent = t
			}
			continue
		}
		byPath[key] = &clientEntry{ClientTorrent: t, Copies: 1, OnDraupnirr: onSite, Hashes: []string{strings.ToLower(t.Hash)}}
		order = append(order, key)
	}
	out := make([]clientEntry, 0, len(order))
	for _, k := range order {
		out = append(out, *byPath[k])
	}
	return out
}

// ---- cross-seed ----

func (s *server) crossScan(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg, site := s.cfg.Client, s.cfg.SiteURL
	s.mu.Unlock()
	tc, err := newTorrentClient(cfg)
	if err != nil {
		fail(w, err)
		return
	}
	if tc == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "configure d'abord un client torrent dans les Réglages"})
		return
	}
	c, err := s.client()
	if err != nil {
		fail(w, err)
		return
	}
	list, err := tc.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	rows, err := crossSeedScan(r.Context(), c, groupCrossSeeds(list, site))
	if err != nil {
		fail(w, err)
		return
	}
	matched := 0
	for _, row := range rows {
		if row.Match != nil {
			matched++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows, "matched": matched})
}

// crossAdd : télécharge le .torrent Draupnirr de la correspondance, vérifie
// que l'arborescence est bien sur le disque, puis l'ajoute au client sur ces
// données. Rien n'est haché ; le client re-vérifie sauf skip check.
func (s *server) crossAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Path == "" || in.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chemin et id requis"})
		return
	}
	s.mu.Lock()
	cfg := s.cfg.Client
	s.mu.Unlock()
	tc, err := newTorrentClient(cfg)
	if err != nil || tc == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "aucun client torrent configuré"})
		return
	}
	c, err := s.client()
	if err != nil {
		fail(w, err)
		return
	}
	raw, err := c.Download(r.Context(), in.ID)
	if err != nil {
		fail(w, err)
		return
	}
	t, err := parseTorrent(raw)
	if err != nil {
		fail(w, err)
		return
	}
	if list, lerr := tc.List(r.Context()); lerr == nil {
		for _, ct := range list {
			if strings.EqualFold(ct.Hash, t.InfoHash) {
				writeJSON(w, http.StatusConflict, map[string]any{"error": "déjà dans le client : ce torrent Draupnirr y est sous le même infohash (" + ct.State + ")"})
				return
			}
		}
	}
	problems, savePath, err := layoutProblems(r.Context(), s.source(), in.Path, t)
	if err != nil {
		fail(w, err)
		return
	}
	if len(problems) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "arborescence différente, cross-seed refusé", "problems": problems})
		return
	}
	if err := tc.Add(r.Context(), raw, savePath, cfg.SkipCheck, cfg.Label); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": t.Name, "infohash": t.InfoHash, "save_path": savePath})
}

// ---- mode lot ----

func (s *server) batchStartHandler(w http.ResponseWriter, r *http.Request) {
	var in batchOpts
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, err)
		return
	}
	if _, err := s.client(); err != nil {
		fail(w, err)
		return
	}
	j, err := s.batchStart(in)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"root": j.Root, "dry_run": j.DryRun})
}

func (s *server) batchStopHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.batch != nil && s.batch.cancel != nil {
		s.batch.cancel()
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

func (s *server) batchStatusHandler(w http.ResponseWriter, r *http.Request) {
	j := s.batchSnapshot()
	if j == nil {
		writeJSON(w, http.StatusOK, map[string]any{"rows": []any{}, "done": true})
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func writeFileMkdir(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (s *server) clientDetect(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"clients": detectLocalClients(r.Context())})
}
