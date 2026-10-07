package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"strings"
	"time"
)

// ClientConfig : le client torrent qui seedera après publication (docs/23 §9.1).
// Le mot de passe est enregistré (fichier 0600), comme le jeton Draupnirr.
type ClientConfig struct {
	Type      string `json:"type"` // "" | qbittorrent | rtorrent | transmission | deluge
	URL       string `json:"url"`
	User      string `json:"user"`
	Password  string `json:"password"`
	Label     string `json:"label"`
	SkipCheck bool   `json:"skip_check"`
}

type ClientTorrent struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Path     string  `json:"path"` // chemin du contenu (fichier ou dossier)
	Progress float64 `json:"progress"`
	State    string  `json:"state"`
	// Hôtes des trackers : « déjà sur Draupnirr » = l'un d'eux est le site.
	Trackers []string `json:"trackers,omitempty"`
}

// trackerHosts extrait les hôtes d'une liste d'URL d'annonce (ou d'un magnet).
func trackerHosts(urls []string) []string {
	var hosts []string
	for _, raw := range urls {
		if u, err := url.Parse(strings.TrimSpace(raw)); err == nil && u.Hostname() != "" {
			hosts = append(hosts, strings.ToLower(u.Hostname()))
		}
	}
	return hosts
}

func magnetTrackers(magnet string) []string {
	u, err := url.Parse(magnet)
	if err != nil {
		return nil
	}
	return trackerHosts(u.Query()["tr"])
}

// errNoExport : le client ne sait pas rendre le .torrent d'origine ; on
// re-hache les données par la source (ce poste ou l'agent).
var errNoExport = errors.New("ce client n'exporte pas les .torrent : les données seront re-hachées")

// torrentClient : trois verbes (docs/23 §9.1) plus un test de connexion.
type torrentClient interface {
	Test(ctx context.Context) (string, error)
	List(ctx context.Context) ([]ClientTorrent, error)
	Export(ctx context.Context, hash string) ([]byte, error)
	Add(ctx context.Context, raw []byte, savePath string, skipCheck bool, label string) error
}

func newTorrentClient(c ClientConfig) (torrentClient, error) {
	base := strings.TrimRight(strings.TrimSpace(c.URL), "/")
	if c.Type == "" {
		return nil, nil
	}
	if !strings.HasPrefix(base, "http") {
		return nil, errors.New("URL du client requise (http:// ou https://)")
	}
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Timeout: 2 * time.Minute, Jar: jar}
	switch c.Type {
	case "qbittorrent":
		return &qbitClient{base: base, cfg: c, hc: hc}, nil
	case "transmission":
		return &transmissionClient{base: base, cfg: c, hc: hc}, nil
	case "rtorrent":
		return &rtorrentClient{base: base, cfg: c, hc: hc}, nil
	case "deluge":
		return &delugeClient{base: base, cfg: c, hc: hc}, nil
	}
	return nil, fmt.Errorf("type de client inconnu : %s", c.Type)
}

func readBody(res *http.Response) ([]byte, error) {
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 160 {
			msg = msg[:160]
		}
		return data, fmt.Errorf("HTTP %d %s", res.StatusCode, msg)
	}
	return data, nil
}

// ---------------- qBittorrent (API Web v2, 4.1+ et 5.x) ----------------

type qbitClient struct {
	base string
	cfg  ClientConfig
	hc   *http.Client
}

func (q *qbitClient) login(ctx context.Context) error {
	form := url.Values{"username": {q.cfg.User}, "password": {q.cfg.Password}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.base+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", q.base)
	res, err := q.hc.Do(req)
	if err != nil {
		return err
	}
	data, err := readBody(res)
	if err != nil {
		return err
	}
	if strings.HasPrefix(string(data), "Ok") {
		return nil
	}
	// Pas « Ok. » : soit les identifiants sont faux (« Fails. »), soit ce
	// n'est pas qBittorrent qui répond (proxy, page de connexion d'un panel…).
	// Avant de conclure, un accès libre (IP en liste blanche) se vérifie.
	if probe, err := q.hc.Get(q.base + "/api/v2/app/version"); err == nil {
		b, perr := readBody(probe)
		if perr == nil && strings.HasPrefix(strings.TrimSpace(string(b)), "v") {
			return nil // authentification contournée pour cette IP : on continue sans cookie
		}
	}
	body := strings.TrimSpace(string(data))
	if len(body) > 80 {
		body = body[:80] + "…"
	}
	if strings.HasPrefix(body, "Fails") {
		return errors.New("qBittorrent a répondu « Fails. » : identifiant ou mot de passe refusé (ceux de l'interface Web, Options › Interface Web ; après plusieurs échecs, qBittorrent bannit l'IP quelques minutes)")
	}
	return fmt.Errorf("ce n'est pas qBittorrent qui répond à %s/api/v2/auth/login (réponse : « %s ») : vérifie l'adresse, un proxy ou une page de connexion intermédiaire", q.base, body)
}

func (q *qbitClient) get(ctx context.Context, p string, query url.Values) ([]byte, error) {
	u := q.base + p
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	res, err := q.hc.Do(req)
	if err != nil {
		return nil, err
	}
	return readBody(res)
}

func (q *qbitClient) Test(ctx context.Context) (string, error) {
	if err := q.login(ctx); err != nil {
		return "", err
	}
	v, err := q.get(ctx, "/api/v2/app/version", nil)
	return "qBittorrent " + strings.TrimSpace(string(v)), err
}

func (q *qbitClient) List(ctx context.Context) ([]ClientTorrent, error) {
	if err := q.login(ctx); err != nil {
		return nil, err
	}
	data, err := q.get(ctx, "/api/v2/torrents/info", nil)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Hash        string  `json:"hash"`
		Name        string  `json:"name"`
		Size        int64   `json:"size"`
		ContentPath string  `json:"content_path"`
		SavePath    string  `json:"save_path"`
		Progress    float64 `json:"progress"`
		State       string  `json:"state"`
		Tracker     string  `json:"tracker"`
		MagnetURI   string  `json:"magnet_uri"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]ClientTorrent, 0, len(raw))
	for _, t := range raw {
		p := t.ContentPath
		if p == "" {
			p = path.Join(t.SavePath, t.Name)
		}
		trackers := magnetTrackers(t.MagnetURI)
		if len(trackers) == 0 && t.Tracker != "" {
			trackers = trackerHosts([]string{t.Tracker})
		}
		out = append(out, ClientTorrent{Hash: t.Hash, Name: t.Name, Size: t.Size, Path: p, Progress: t.Progress, State: t.State, Trackers: trackers})
	}
	return out, nil
}

func (q *qbitClient) Export(ctx context.Context, hash string) ([]byte, error) {
	if err := q.login(ctx); err != nil {
		return nil, err
	}
	data, err := q.get(ctx, "/api/v2/torrents/export", url.Values{"hash": {hash}})
	if err != nil {
		return nil, errNoExport // 404 sur < 4.5 : on re-hache
	}
	return data, nil
}

func (q *qbitClient) Add(ctx context.Context, raw []byte, savePath string, skipCheck bool, label string) error {
	if err := q.login(ctx); err != nil {
		return err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("torrents", "release.torrent")
	part.Write(raw)
	fields := map[string]string{"savepath": savePath, "autoTMM": "false", "paused": "false", "stopped": "false", "category": label}
	if skipCheck {
		fields["skip_checking"] = "true"
	}
	for k, v := range fields {
		if v != "" {
			_ = w.WriteField(k, v)
		}
	}
	w.Close()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.base+"/api/v2/torrents/add", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := q.hc.Do(req)
	if err != nil {
		return err
	}
	data, err := readBody(res)
	if err != nil {
		return err
	}
	if strings.HasPrefix(string(data), "Fails") {
		return errors.New("qBittorrent a refusé le .torrent : il est sans doute déjà dans le client (même infohash)")
	}
	return nil
}

// ---------------- Transmission (RPC JSON) ----------------

type transmissionClient struct {
	base      string
	cfg       ClientConfig
	hc        *http.Client
	sessionID string
}

func (t *transmissionClient) rpcURL() string {
	if strings.HasSuffix(t.base, "/rpc") {
		return t.base
	}
	return t.base + "/transmission/rpc"
}

func (t *transmissionClient) call(ctx context.Context, method string, args any, out any) error {
	body, _ := json.Marshal(map[string]any{"method": method, "arguments": args})
	for attempt := 0; attempt < 2; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, t.rpcURL(), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Transmission-Session-Id", t.sessionID)
		if t.cfg.User != "" {
			req.SetBasicAuth(t.cfg.User, t.cfg.Password)
		}
		res, err := t.hc.Do(req)
		if err != nil {
			return err
		}
		if res.StatusCode == http.StatusConflict {
			t.sessionID = res.Header.Get("X-Transmission-Session-Id")
			res.Body.Close()
			continue
		}
		data, err := readBody(res)
		if err != nil {
			return err
		}
		var env struct {
			Result    string          `json:"result"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(data, &env); err != nil {
			return err
		}
		if env.Result != "success" {
			return errors.New("Transmission : " + env.Result)
		}
		if out != nil {
			return json.Unmarshal(env.Arguments, out)
		}
		return nil
	}
	return errors.New("Transmission : session-id non négocié")
}

func (t *transmissionClient) Test(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	err := t.call(ctx, "session-get", map[string]any{"fields": []string{"version"}}, &out)
	return "Transmission " + out.Version, err
}

func (t *transmissionClient) List(ctx context.Context) ([]ClientTorrent, error) {
	var out struct {
		Torrents []struct {
			Hash        string  `json:"hashString"`
			Name        string  `json:"name"`
			Size        int64   `json:"totalSize"`
			DownloadDir string  `json:"downloadDir"`
			PercentDone float64 `json:"percentDone"`
			Status      int     `json:"status"`
			Trackers    []struct {
				Announce string `json:"announce"`
			} `json:"trackers"`
		} `json:"torrents"`
	}
	if err := t.call(ctx, "torrent-get", map[string]any{"fields": []string{"hashString", "name", "totalSize", "downloadDir", "percentDone", "status", "trackers"}}, &out); err != nil {
		return nil, err
	}
	states := map[int]string{0: "stopped", 4: "downloading", 6: "seeding"}
	res := make([]ClientTorrent, 0, len(out.Torrents))
	for _, x := range out.Torrents {
		var urls []string
		for _, tr := range x.Trackers {
			urls = append(urls, tr.Announce)
		}
		res = append(res, ClientTorrent{Hash: x.Hash, Name: x.Name, Size: x.Size, Path: path.Join(x.DownloadDir, x.Name), Progress: x.PercentDone, State: states[x.Status], Trackers: trackerHosts(urls)})
	}
	return res, nil
}

func (t *transmissionClient) Export(context.Context, string) ([]byte, error) { return nil, errNoExport }

func (t *transmissionClient) Add(ctx context.Context, raw []byte, savePath string, _ bool, label string) error {
	args := map[string]any{"metainfo": base64.StdEncoding.EncodeToString(raw), "download-dir": savePath, "paused": false}
	if label != "" {
		args["labels"] = []string{label}
	}
	// ponytail: Transmission re-vérifie toujours les données à l'ajout ; pas de skip_check côté RPC.
	return t.call(ctx, "torrent-add", args, nil)
}

// ---------------- Deluge 2 (JSON-RPC de l'interface web) ----------------

type delugeClient struct {
	base string
	cfg  ClientConfig
	hc   *http.Client
	id   int
}

func (d *delugeClient) call(ctx context.Context, method string, params []any, out any) error {
	d.id++
	body, _ := json.Marshal(map[string]any{"method": method, "params": params, "id": d.id})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, d.base+"/json", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := d.hc.Do(req)
	if err != nil {
		return err
	}
	data, err := readBody(res)
	if err != nil {
		return err
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	if env.Error != nil {
		return errors.New("Deluge : " + env.Error.Message)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

func (d *delugeClient) login(ctx context.Context) error {
	var ok bool
	if err := d.call(ctx, "auth.login", []any{d.cfg.Password}, &ok); err != nil {
		return err
	}
	if !ok {
		return errors.New("Deluge : mot de passe refusé")
	}
	var connected bool
	_ = d.call(ctx, "web.connected", nil, &connected)
	if !connected {
		var hosts [][]any
		if err := d.call(ctx, "web.get_hosts", nil, &hosts); err == nil && len(hosts) > 0 {
			_ = d.call(ctx, "web.connect", []any{hosts[0][0]}, nil)
		}
	}
	return nil
}

func (d *delugeClient) Test(ctx context.Context) (string, error) {
	if err := d.login(ctx); err != nil {
		return "", err
	}
	var v string
	err := d.call(ctx, "daemon.info", nil, &v)
	return "Deluge " + v, err
}

func (d *delugeClient) List(ctx context.Context) ([]ClientTorrent, error) {
	if err := d.login(ctx); err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Name     string  `json:"name"`
		Size     int64   `json:"total_size"`
		SavePath string  `json:"save_path"`
		Progress float64 `json:"progress"`
		State    string  `json:"state"`
		Host     string  `json:"tracker_host"`
	}
	if err := d.call(ctx, "core.get_torrents_status", []any{map[string]any{}, []string{"name", "total_size", "save_path", "progress", "state", "tracker_host"}}, &raw); err != nil {
		return nil, err
	}
	out := make([]ClientTorrent, 0, len(raw))
	for hash, t := range raw {
		var trackers []string
		if t.Host != "" {
			trackers = []string{strings.ToLower(t.Host)}
		}
		out = append(out, ClientTorrent{Hash: hash, Name: t.Name, Size: t.Size, Path: path.Join(t.SavePath, t.Name), Progress: t.Progress / 100, State: strings.ToLower(t.State), Trackers: trackers})
	}
	return out, nil
}

func (d *delugeClient) Export(context.Context, string) ([]byte, error) { return nil, errNoExport }

func (d *delugeClient) Add(ctx context.Context, raw []byte, savePath string, skipCheck bool, label string) error {
	if err := d.login(ctx); err != nil {
		return err
	}
	opts := map[string]any{"download_location": savePath, "add_paused": false, "seed_mode": skipCheck}
	if err := d.call(ctx, "core.add_torrent_file", []any{"release.torrent", base64.StdEncoding.EncodeToString(raw), opts}, nil); err != nil {
		return err
	}
	// ponytail: le label Deluge demande le plugin Label activé ; ignoré sinon.
	return nil
}

// ---------------- rTorrent / ruTorrent (XML-RPC) ----------------

type rtorrentClient struct {
	base string
	cfg  ClientConfig
	hc   *http.Client
	ep   string
}

// endpoints : l'URL saisie est celle de ruTorrent (…/rutorrent/) ou le
// montage XML-RPC direct (…/RPC2) ; on essaie dans cet ordre.
func (r *rtorrentClient) endpoints() []string {
	if r.ep != "" {
		return []string{r.ep}
	}
	if strings.HasSuffix(r.base, "/RPC2") || strings.HasSuffix(r.base, "action.php") {
		return []string{r.base}
	}
	return []string{r.base + "/plugins/httprpc/action.php", r.base + "/RPC2"}
}

func (r *rtorrentClient) call(ctx context.Context, method string, params ...any) (any, error) {
	body := xmlrpcEncode(method, params)
	var lastErr error
	for _, ep := range r.endpoints() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ep, bytes.NewReader(body))
		req.Header.Set("Content-Type", "text/xml")
		if r.cfg.User != "" {
			req.SetBasicAuth(r.cfg.User, r.cfg.Password)
		}
		res, err := r.hc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, err := readBody(res)
		if err != nil {
			lastErr = err
			continue
		}
		v, err := xmlrpcDecode(data)
		if err != nil {
			lastErr = err
			continue
		}
		r.ep = ep
		return v, nil
	}
	return nil, fmt.Errorf("rTorrent : %w", lastErr)
}

func (r *rtorrentClient) Test(ctx context.Context) (string, error) {
	v, err := r.call(ctx, "system.client_version")
	if err != nil {
		return "", err
	}
	return "rTorrent " + fmt.Sprint(v), nil
}

func (r *rtorrentClient) List(ctx context.Context) ([]ClientTorrent, error) {
	v, err := r.call(ctx, "d.multicall2", "", "main", "d.hash=", "d.name=", "d.size_bytes=", "d.base_path=", "d.complete=", "d.state=")
	if err != nil {
		return nil, err
	}
	rows, _ := v.([]any)
	out := make([]ClientTorrent, 0, len(rows))
	for _, row := range rows {
		f, _ := row.([]any)
		if len(f) < 6 {
			continue
		}
		state := "stopped"
		if toInt(f[5]) == 1 {
			state = "seeding"
		}
		progress := 0.0
		if toInt(f[4]) == 1 {
			progress = 1
		}
		out = append(out, ClientTorrent{Hash: strings.ToLower(fmt.Sprint(f[0])), Name: fmt.Sprint(f[1]), Size: toInt(f[2]), Path: fmt.Sprint(f[3]), Progress: progress, State: state})
	}
	return out, nil
}

func (r *rtorrentClient) Export(context.Context, string) ([]byte, error) { return nil, errNoExport }

func (r *rtorrentClient) Add(ctx context.Context, raw []byte, savePath string, _ bool, label string) error {
	// load.raw_start charge et démarre ; d.directory_base.set pointe sur les
	// données existantes. rTorrent re-vérifie le hachage au premier démarrage.
	// ponytail: pas de fast-resume (rtorrent_fast_resume) ; à ajouter si les
	// gros packs mettent trop longtemps à repartir.
	params := []any{"", xmlrpcBase64(raw), "d.directory_base.set=" + shellQuote(savePath)}
	if label != "" {
		params = append(params, "d.custom1.set="+label)
	}
	_, err := r.call(ctx, "load.raw_start", params...)
	return err
}

func toInt(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case string:
		var n int64
		fmt.Sscan(x, &n)
		return n
	}
	return 0
}

// detectLocalClients : les clients de bureau écoutent sur des ports connus.
// Chaque sonde exige la SIGNATURE du client (un simple « ça répond » prenait
// n'importe quel serveur web local pour un client).
func detectLocalClients(ctx context.Context) []ClientConfig {
	hc := &http.Client{Timeout: 2 * time.Second}
	get := func(u string) (*http.Response, []byte) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		res, err := hc.Do(req)
		if err != nil {
			return nil, nil
		}
		b, _ := readBody(res)
		return res, b
	}
	post := func(u, ct string, body string) (*http.Response, []byte) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(body))
		req.Header.Set("Content-Type", ct)
		res, err := hc.Do(req)
		if err != nil {
			return nil, nil
		}
		b, _ := readBody(res)
		return res, b
	}
	var found []ClientConfig
	// qBittorrent : /app/version répond « v5.0.2 » (accès libre sur localhost) ou 403 « Forbidden ».
	if res, b := get("http://127.0.0.1:8080/api/v2/app/version"); res != nil {
		if (res.StatusCode == 200 && strings.HasPrefix(strings.TrimSpace(string(b)), "v")) || (res.StatusCode == 403 && strings.Contains(string(b), "Forbidden")) {
			found = append(found, ClientConfig{Type: "qbittorrent", URL: "http://127.0.0.1:8080"})
		}
	}
	// Transmission : 409 avec l'en-tête de session.
	if res, _ := post("http://127.0.0.1:9091/transmission/rpc", "application/json", `{"method":"session-get"}`); res != nil && res.Header.Get("X-Transmission-Session-Id") != "" {
		found = append(found, ClientConfig{Type: "transmission", URL: "http://127.0.0.1:9091"})
	}
	// Deluge : JSON-RPC qui répond au moins { "id": 1 }.
	if res, b := post("http://127.0.0.1:8112/json", "application/json", `{"method":"auth.check_session","params":[],"id":1}`); res != nil && res.StatusCode == 200 && strings.Contains(string(b), `"id"`) && strings.Contains(string(b), `"result"`) {
		found = append(found, ClientConfig{Type: "deluge", URL: "http://127.0.0.1:8112"})
	}
	// ruTorrent : XML-RPC (ou 401 si protégé).
	if res, b := post("http://127.0.0.1/rutorrent/plugins/httprpc/action.php", "text/xml", string(xmlrpcEncode("system.client_version", nil))); res != nil && (res.StatusCode == 401 || strings.Contains(string(b), "methodResponse")) {
		found = append(found, ClientConfig{Type: "rtorrent", URL: "http://127.0.0.1/rutorrent"})
	}
	return found
}
