// Package obs holds the structured logger and the build version.
package obs

import (
	"io"
	"log"
	"log/slog"
	"strings"
)

// AppVersion is "<ref>-<sha7>" in a release build, set with
//
//	-X github.com/tikhonp/shelf/internal/platform/obs.AppVersion=${APP_VERSION}
//
// and "dev" otherwise.
var AppVersion = "dev"

// SetupLogging installs a JSON slog handler on w as the process-wide default
// and routes the stdlib log package through it, so Dozzle reads one
// machine-readable stream.
func SetupLogging(w io.Writer, level slog.Level) *slog.Logger {
	logger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	log.SetFlags(0)
	log.SetOutput(slogWriter{logger})
	return logger
}

type slogWriter struct{ logger *slog.Logger }

func (w slogWriter) Write(p []byte) (int, error) {
	w.logger.Info(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}
