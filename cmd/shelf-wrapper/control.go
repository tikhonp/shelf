package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/tikhonp/shelf/internal/wrapperapi"
)

// maxBody bounds a request body; a sign-in is a few hundred bytes.
const maxBody = 64 << 10

// control is the supervisor's HTTP API. Every request needs the bearer
// token, compared in constant time. An empty token refuses every request,
// so a missing secret never opens the API.
func control(s *Supervisor, token string, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+wrapperapi.PathState, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.Status())
	})
	mux.HandleFunc("POST "+wrapperapi.PathLogin, func(w http.ResponseWriter, r *http.Request) {
		var req wrapperapi.LoginRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, `send {"username", "password"}`)
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" || strings.Contains(req.Username, ":") {
			writeError(w, http.StatusBadRequest, "the Apple ID and the password are both needed")
			return
		}
		if err := s.Login(req); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		log.Info("sign-in requested", "user", req.Username)
		writeJSON(w, http.StatusAccepted, s.Status())
	})
	mux.HandleFunc("POST "+wrapperapi.PathCode, func(w http.ResponseWriter, r *http.Request) {
		var req wrapperapi.CodeRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, `send {"code"}`)
			return
		}
		switch err := s.SubmitCode(strings.TrimSpace(req.Code)); {
		case errors.Is(err, errBadCode):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, errNotAwaiting):
			writeError(w, http.StatusConflict, err.Error())
		case err != nil:
			log.Error("write the 2FA code", "error", err)
			writeError(w, http.StatusInternalServerError, "could not write the code file")
		default:
			writeJSON(w, http.StatusOK, s.Status())
		}
	})
	mux.HandleFunc("POST "+wrapperapi.PathRestart, func(w http.ResponseWriter, _ *http.Request) {
		if err := s.Restart(); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, s.Status())
	})
	mux.HandleFunc("GET "+wrapperapi.PathLog, func(w http.ResponseWriter, r *http.Request) {
		var after int64
		if v := r.URL.Query().Get("after"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, "after must be a line number")
				return
			}
			after = n
		}
		writeJSON(w, http.StatusOK, wrapperapi.LogPage{Lines: s.ring.after(after)})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "no such route")
	})

	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if token == "" || subtle.ConstantTimeCompare(got, want) != 1 {
			writeError(w, http.StatusUnauthorized, "wrong or missing token")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, wrapperapi.ErrorBody{Error: msg})
}
