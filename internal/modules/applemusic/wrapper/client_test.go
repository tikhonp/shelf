package wrapper_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/modules/applemusic/wrapper"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/wrapperapi"
)

func TestClient(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/status":
			_, _ = w.Write([]byte(`{"code":0,"msg":"SUCCESS","data":{"regions":["ru"]}}`))
		case wrapperapi.PathLogin:
			var req wrapperapi.LoginRequest
			if json.NewDecoder(r.Body).Decode(&req) != nil || req.Password != "pw" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"a sign-in is running"}`))
		case wrapperapi.PathLog:
			_, _ = w.Write([]byte(`{"lines":[{"n":5,"text":"x"}]}`))
		default:
			_ = json.NewEncoder(w).Encode(wrapperapi.Status{State: wrapperapi.Running, Storefront: "ru"})
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c := wrapper.New(u, u, config.Secret("tok"), &http.Client{Timeout: time.Second})
	ctx := context.Background()

	if st, err := c.State(ctx); err != nil || st.State != wrapperapi.Running {
		t.Fatalf("%+v %v", st, err)
	}
	_, err := c.Login(ctx, "me", "pw")
	var ae *wrapper.APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict || ae.Message != "a sign-in is running" {
		t.Fatalf("%v", err)
	}
	if p, err := c.Log(ctx, 4); err != nil || len(p.Lines) != 1 || p.Lines[0].N != 5 {
		t.Fatalf("%+v %v", p, err)
	}
	if r, err := c.Regions(ctx); err != nil || !slices.Equal(r, []string{"ru"}) {
		t.Fatalf("%v %v", r, err)
	}
	want := []string{
		"GET /v1/state Bearer tok", "POST /v1/login Bearer tok", "GET /v1/log?after=4 Bearer tok", "GET /status ",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}

	srv.Close()
	if _, err := c.Restart(ctx); !errors.Is(err, wrapper.ErrUnreachable) {
		t.Fatalf("%v", err)
	}
	if _, err := c.Regions(ctx); !errors.Is(err, wrapper.ErrUnreachable) {
		t.Fatalf("%v", err)
	}
}
