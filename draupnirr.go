package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client HTTP vers Draupnirr : jeton API (drp_…) ou clé d'annonce dans
// l'en-tête X-Api-Key. Les réponses d'erreur de l'API sont {"error": "…"}.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

func newClient(base, token string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Token: token, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string { return fmt.Sprintf("Draupnirr %d : %s", e.Status, e.Msg) }

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string, out any) error {
	u := c.Base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Bifrost/"+Version)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if res.StatusCode >= 400 {
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		msg := e.Error
		if msg == "" {
			msg = e.Message
		}
		if msg == "" {
			msg = strings.TrimSpace(string(data))
			if len(msg) > 200 {
				msg = msg[:200]
			}
		}
		return &apiError{Status: res.StatusCode, Msg: msg}
	}
	if out == nil {
		return nil
	}
	if raw, ok := out.(*[]byte); ok {
		*raw = data
		return nil
	}
	return json.Unmarshal(data, out)
}

// Me : GET /api/me — valide le jeton et renvoie le compte (+ source_tag).
func (c *Client) Me(ctx context.Context) (map[string]any, error) {
	var me map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/me", nil, nil, "", &me); err != nil {
		return nil, err
	}
	return me, nil
}

func (c *Client) Categories(ctx context.Context) ([]map[string]any, error) {
	var out struct {
		Categories []map[string]any `json:"categories"`
	}
	raw := []map[string]any{}
	var data []byte
	if err := c.do(ctx, http.MethodGet, "/api/categories", nil, nil, "", &data); err != nil {
		return nil, err
	}
	// Tolère les deux formes : {"categories":[…]} ou […].
	if err := json.Unmarshal(data, &out); err == nil && out.Categories != nil {
		return out.Categories, nil
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *Client) TMDB(ctx context.Context, q, kind string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/tmdb", url.Values{"q": {q}, "type": {kind}}, nil, "", &out)
	return out, err
}

func (c *Client) TMDBImages(ctx context.Context, id, kind, episode string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/tmdb-images", url.Values{"id": {id}, "type": {kind}, "episode": {episode}}, nil, "", &out)
	return out, err
}

// MusicBrainz : GET /api/musicbrainz — éditions d'un album (artist, album,
// tracks) ou une édition et ses pistes (id).
func (c *Client) MusicBrainz(ctx context.Context, q url.Values) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/musicbrainz", q, nil, "", &out)
	return out, err
}

func (c *Client) Presentations(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/me/presentations", nil, nil, "", &out)
	return out, err
}

func (c *Client) Preview(ctx context.Context, content, format string) (string, error) {
	var out struct {
		HTML string `json:"html"`
	}
	form := url.Values{"content": {content}, "format": {format}}
	err := c.do(ctx, http.MethodPost, "/api/upload/preview", nil, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", &out)
	return out.HTML, err
}

// multipartForm : champs texte (clé → valeurs, `facets[source]` compris) + fichiers.
type fileField struct {
	Field, Name string
	Data        []byte
}

func multipartForm(fields map[string][]string, files []fileField) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, vs := range fields {
		for _, v := range vs {
			// Vide = absent, sauf le type d'un album : vide exprès, il retire
			// les types que l'édition MusicBrainz aurait posés (docs/25 §6.3).
			if v == "" && !strings.HasSuffix(k, "[type]") {
				continue
			}
			if err := w.WriteField(k, v); err != nil {
				return nil, "", err
			}
		}
	}
	for _, f := range files {
		if len(f.Data) == 0 {
			continue
		}
		part, err := w.CreateFormFile(f.Field, f.Name)
		if err != nil {
			return nil, "", err
		}
		part.Write(f.Data)
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}

// Analyze : POST /api/upload/analyze — la fiche calculée par Draupnirr.
func (c *Client) Analyze(ctx context.Context, torrent []byte, fields map[string][]string) (json.RawMessage, error) {
	body, ct, err := multipartForm(fields, []fileField{{Field: "torrent", Name: "release.torrent", Data: torrent}})
	if err != nil {
		return nil, err
	}
	var out json.RawMessage
	err = c.do(ctx, http.MethodPost, "/api/upload/analyze", nil, body, ct, &out)
	return out, err
}

type UploadResult struct {
	ID                 string `json:"id"`
	InfoHash           string `json:"infohash"`
	Status             string `json:"status"`
	AwaitingValidation bool   `json:"awaiting_validation"`
}

// Upload : POST /api/upload. `fields` porte category, description, meta[…], mediainfo.
func (c *Client) Upload(ctx context.Context, torrent, nfo []byte, fields map[string][]string) (*UploadResult, error) {
	body, ct, err := multipartForm(fields, []fileField{
		{Field: "torrent", Name: "release.torrent", Data: torrent},
		{Field: "nfo", Name: "release.nfo", Data: nfo},
	})
	if err != nil {
		return nil, err
	}
	var out UploadResult
	if err := c.do(ctx, http.MethodPost, "/api/upload", nil, body, ct, &out); err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, errors.New("réponse d'upload sans id")
	}
	return &out, nil
}

// Download : le .torrent personnalisé (announce du membre), à remettre au client.
func (c *Client) Download(ctx context.Context, id string) ([]byte, error) {
	var data []byte
	err := c.do(ctx, http.MethodGet, "/api/torrents/"+url.PathEscape(id)+"/download", nil, nil, "", &data)
	return data, err
}
