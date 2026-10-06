package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// Mot de passe local (docs/23 §9.2) : exigé dès que la page n'écoute pas sur
// loopback (Docker, --listen 0.0.0.0). Sinon n'importe qui sur le réseau
// publierait avec le jeton du membre. Haché argon2id dans la config.

const argonParams = "argon2id$t=2,m=65536,p=1"

func hashPassword(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, 2, 64*1024, 1, 32)
	return argonParams + "$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)
}

func checkPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0]+"$"+parts[1] != argonParams {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 2, 64*1024, 1, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// sessions : jetons de session en mémoire ; un redémarrage déconnecte tout
// le monde, ce qui est très bien pour un outil local.
type sessions struct {
	mu       sync.Mutex
	tokens   map[string]time.Time
	failures int
	lastFail time.Time
}

func newSessions() *sessions { return &sessions{tokens: map[string]time.Time{}} }

const sessionCookie = "bifrost_session"

func (s *sessions) issue(w http.ResponseWriter) {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	tok := base64.RawURLEncoding.EncodeToString(buf)
	s.mu.Lock()
	s.tokens[tok] = time.Now().Add(12 * time.Hour)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 3600})
}

func (s *sessions) valid(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tokens[c.Value]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.tokens, c.Value)
		return false
	}
	return true
}

// throttle : un essai raté coûte une pause croissante, plafonnée à 10 s.
// ponytail: compteur global ; par IP si l'outil est un jour exposé plus largement.
func (s *sessions) throttle() {
	s.mu.Lock()
	f := s.failures
	s.mu.Unlock()
	if f > 0 {
		d := time.Duration(f) * 500 * time.Millisecond
		if d > 10*time.Second {
			d = 10 * time.Second
		}
		time.Sleep(d)
	}
}

func (s *sessions) failed() {
	s.mu.Lock()
	s.failures++
	s.lastFail = time.Now()
	s.mu.Unlock()
}

func (s *sessions) succeeded() {
	s.mu.Lock()
	s.failures = 0
	s.mu.Unlock()
}

var errWeakPassword = errors.New("mot de passe trop court : 8 caractères minimum")
