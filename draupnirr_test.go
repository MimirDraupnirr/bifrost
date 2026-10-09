package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoRetriesOn429(t *testing.T) {
	old := retryBase
	retryBase = time.Millisecond
	defer func() { retryBase = old }()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "content=x&format=bbcode" {
			t.Errorf("corps renvoyé = %q", body)
		}
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"message":"Too Many Attempts."}`))
			return
		}
		w.Write([]byte(`{"html":"ok"}`))
	}))
	defer srv.Close()
	html, err := newClient(srv.URL, "t").Preview(withRetry(context.Background(), nil), "x", "bbcode")
	if err != nil || html != "ok" || calls.Load() != 3 {
		t.Fatalf("html=%q err=%v appels=%d", html, err, calls.Load())
	}
}

func TestDoFailsFastOutsideBatch(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := newClient(srv.URL, "t").Me(context.Background())
	if ae, ok := err.(*apiError); !ok || ae.Status != 429 || calls.Load() != 1 {
		t.Fatalf("hors lot, le 429 remonte tout de suite : err=%v appels=%d", err, calls.Load())
	}
}

func TestDoGivesUpWhenWaitExceedsCap(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := newClient(srv.URL, "t").Me(withRetry(context.Background(), nil))
	if ae, ok := err.(*apiError); !ok || ae.Status != 429 || calls.Load() != 1 {
		t.Fatalf("une heure demandée : pas d'essai inutile : err=%v appels=%d", err, calls.Load())
	}
}

func TestDoGivesUpAfterMaxAttempts(t *testing.T) {
	old := retryBase
	retryBase = time.Millisecond
	defer func() { retryBase = old }()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"message":"Too Many Attempts."}`))
	}))
	defer srv.Close()
	_, err := newClient(srv.URL, "t").Me(withRetry(context.Background(), nil))
	ae, ok := err.(*apiError)
	if !ok || ae.Status != 429 || calls.Load() != maxAttempts {
		t.Fatalf("err=%v appels=%d", err, calls.Load())
	}
}

func TestPauseIsShared(t *testing.T) {
	g := gateFor("https://shared.example", "/api/tmdb")
	g.pause(time.Hour)
	if gateFor("https://shared.example", "/api/upload/analyze") != g {
		t.Fatal("TMDB et l'analyse partagent le quota api-analyze du site")
	}
	if gateFor("https://shared.example", "/api/upload") == g || gateFor("https://shared.example", "/api/upload/preview") == g {
		t.Fatal("api-write et api-read ont leurs propres quotas")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := g.wait(ctx, nil); err == nil {
		t.Fatal("wait doit respecter l'annulation")
	}
}

func TestRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "17")
	if d := retryAfter(h, 0); d != 18*time.Second {
		t.Fatalf("secondes, plus une de marge : %v", d)
	}
	h.Set("Retry-After", "0")
	if d := retryAfter(h, 0); d != time.Second {
		t.Fatalf("« 0 » = dernière seconde d'une fenêtre encore fermée : %v", d)
	}
	h = http.Header{}
	h.Set("Retry-After", time.Now().Add(30*time.Second).UTC().Format(http.TimeFormat))
	if d := retryAfter(h, 0); d < 29*time.Second || d > 31*time.Second {
		t.Fatalf("date HTTP : %v", d)
	}
	h = http.Header{}
	h.Set("Retry-After", "3600")
	if d := retryAfter(h, 0); d <= retryCap {
		t.Fatalf("longue pause rendue telle quelle (do() renonce au-delà de retryCap) : %v", d)
	}
	if d := retryAfter(http.Header{}, 2); d != retryBase<<2 {
		t.Fatalf("backoff : %v", d)
	}
}

func TestGateWaitNotice(t *testing.T) {
	g := &gate{}
	g.pause(30 * time.Millisecond)
	var got []time.Duration
	notice := func(d time.Duration) { got = append(got, d) }
	if err := g.wait(context.Background(), notice); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] <= 0 || got[0] > 30*time.Millisecond || got[1] != 0 {
		t.Fatalf("pause annoncée puis reprise : %v", got)
	}
	got = nil
	_ = g.wait(context.Background(), notice)
	if got != nil {
		t.Fatal("aucune pause en cours : rien à annoncer")
	}
}

func TestPauseNoticeKeepsStep(t *testing.T) {
	s := &server{}
	detail := "analyse"
	f := s.pauseNotice(&detail)
	f(12 * time.Second)
	f(300 * time.Millisecond)
	if detail != "analyse"+pauseMark+"1 s" {
		t.Fatalf("détail : %q", detail)
	}
	if f(0); detail != "analyse" {
		t.Fatalf("la mention part à la reprise : %q", detail)
	}
}
