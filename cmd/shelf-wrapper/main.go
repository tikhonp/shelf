// Command shelf-wrapper is the entrypoint of the shelf-wrapper image. It
// runs wrapper-lite in service mode for amdl, starts it again when it
// exits, signs it in to an Apple ID when shelf asks, and serves the control
// API that shelf's Status page uses:
//
//	shelf-wrapper           supervise wrapper-lite and serve the control API
//	shelf-wrapper version
//
// It reads SHELF_WRAPPER_TOKEN, the control API's bearer token, and
// SHELF_WRAPPER_LITE_ARGS, more wrapper-lite flags split on spaces, where
// quotes keep a value with spaces whole. The protocol is in
// internal/wrapperapi; docs/sources/apple-music.md has the rest.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tikhonp/shelf/internal/wrapperapi"
)

// version is set by the image build.
var version = "dev"

// controlAddr is where the control API listens.
const controlAddr = ":12341"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println(version)
			return
		default:
			fmt.Fprintf(os.Stderr, "usage: shelf-wrapper [version]\n")
			os.Exit(2)
		}
	}
	os.Exit(run())
}

func run() int {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// Docker's SIGTERM starts the stop: SIGINT to wrapper-lite, then SIGKILL.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	token := strings.TrimSpace(os.Getenv("SHELF_WRAPPER_TOKEN"))
	if token == "" {
		log.Warn("SHELF_WRAPPER_TOKEN is empty: the control API refuses every request")
	}
	cfg := DefaultConfig()
	extra, err := splitArgs(os.Getenv("SHELF_WRAPPER_LITE_ARGS"))
	if err != nil {
		log.Error("SHELF_WRAPPER_LITE_ARGS", "error", err)
		return 1
	}
	cfg.Extra = extra
	r := newRing(keepLines, func(l wrapperapi.LogLine) { log.Info(l.Text, "from", "wrapper-lite") })
	sup := NewSupervisor(cfg, r)

	ln, err := net.Listen("tcp", controlAddr)
	if err != nil {
		log.Error("listen", "addr", controlAddr, "error", err)
		return 1
	}
	srv := &http.Server{Handler: control(sup, token, log), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	log.Info("shelf-wrapper started", "version", version, "control", controlAddr)

	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx) }()
	code := 0
	select {
	case err := <-served:
		log.Error("control API", "error", err)
		code = 1
		stop()
		if err := <-done; err != nil {
			log.Error("supervisor", "error", err)
		}
	case err := <-done:
		if err != nil {
			log.Error("supervisor", "error", err)
			code = 1
		}
	}
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Warn("control API shutdown", "error", err)
	}
	log.Info("shelf-wrapper stopped")
	return code
}

// splitArgs splits flags on spaces. Single or double quotes keep a value
// with spaces whole, such as --device-info's "Pixel 8"; there are no
// escapes.
func splitArgs(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	quote, inArg := rune(0), false
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inArg {
				out = append(out, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("a quote is not closed")
	}
	if inArg {
		out = append(out, cur.String())
	}
	return out, nil
}
