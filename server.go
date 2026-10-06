package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
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
	MediaErr  string          `json:"mediainfo_error,omitempty"`
	Analysis  json.RawMessage `json:"analysis,omitempty"`
	raw       []byte
}

func newServer(cfg *Config, configPath string) *server {
	s := &server{mux: http.NewServeMux(), cfg: cfg, configPath: configPath, jobs: map[string]*job{}}
	sub, _ := fs.Sub(uiFS, "ui")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))
	s.mux.HandleFunc("GET /ui/state", s.state)
	s.mux.HandleFunc("POST /ui/connect", s.connect)
	s.mux.HandleFunc("POST /ui/settings", s.settings)
	s.mux.HandleFunc("POST /ui/ssh/test", s.sshTest)
	s.mux.HandleFunc("POST /ui/ssh/accept", s.sshAccept)
	s.mux.HandleFunc("GET /ui/browse", s.browse)
	s.mux.HandleFunc("POST /ui/prepare", s.prepare)
	s.mux.HandleFunc("GET /ui/job/{id}", s.jobStatus)
	s.mux.HandleFunc("POST /ui/analyze", s.analyze)
	s.mux.HandleFunc("GET /ui/categories", s.categories)
	s.mux.HandleFunc("GET /ui/tmdb", s.tmdb)
	s.mux.HandleFunc("GET /ui/tmdb-images", s.tmdbImages)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"version":          Version,
		"config":           cfg,
		"connected":        me != nil,
		"me":               me,
		"mediainfo":        mediaInfoAvailable(),
		"mediainfo_hint":   mediaInfoInstallHint(),
		"has_token":        s.cfg.APIToken != "",
		"ssh_password_set": s.sshPassword != "",
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
		OutDir   string     `json:"out_dir"`
		Source   string     `json:"source"`
		SSH      *SSHConfig `json:"ssh"`
		Password string     `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, err)
		return
	}
	s.mu.Lock()
	s.applySettings(in.OutDir, in.Source, in.SSH, in.Password)
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
	s.mu.Unlock()

	go func() {
		ctx := context.Background()
		raw, err := src.MakeTorrent(ctx, in.Path, sourceTag, func(done, total int64) {
			s.mu.Lock()
			j.Progress = [2]int64{done, total}
			s.mu.Unlock()
		})
		s.mu.Lock()
		if err != nil {
			j.Error, j.Done = err.Error(), true
			s.mu.Unlock()
			return
		}
		j.raw = raw
		j.Torrent, _ = parseTorrent(raw)
		j.MainFile = src.MainFile(in.Path, j.Torrent)
		j.Step = "mediainfo"
		s.mu.Unlock()

		mi, merr := src.MediaInfo(ctx, j.MainFile)
		s.mu.Lock()
		if merr != nil {
			j.MediaErr = merr.Error()
		}
		j.MediaInfo = mi
		j.Step = "analyse"
		s.mu.Unlock()

		analysis, aerr := c.Analyze(ctx, raw, map[string][]string{"category": {in.Category}, "mediainfo": {mi}})
		s.mu.Lock()
		if aerr != nil {
			j.Error = aerr.Error()
		}
		j.Analysis, j.Done, j.Step = analysis, true, "prêt"
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
