// Command grc-render typesets documents for a GRC installation on another host.
//
// GRC sends a prepared workspace -- the template sources with the brand
// applied, the generated entry file and the payload -- and grc-render runs
// Typst or Pandoc on it with the same fixed commands GRC would run itself, and
// sends back the PDF or the .docx. It holds no templates and no database,
// keeps nothing after a request, and can do nothing else. It exists so the
// host that serves GRC needs no typesetter: the work goes to a machine that
// has one, such as a Wintermute rig node.
//
// It is trusted with documents in full, so the token is not optional and the
// port belongs behind a firewall rule, as for wintermute-ocr. See
// DOCUMENT_TEMPLATES.md, "Rendering on another host".
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"grc/internal/doctemplate"
)

// minTokenLength refuses a guessable token: `openssl rand -hex 32` gives 64.
const minTokenLength = 32

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "grc-render: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		addr = flag.String("addr", envOr("GRC_RENDER_ADDR", "127.0.0.1:8892"),
			"address to listen on; bind wider than loopback only behind a firewall rule")
		token = flag.String("token", envOr("GRC_RENDER_TOKEN", ""),
			"bearer token GRC must present (RENDER_TOKEN there); required, at least 32 characters")
		certFile = flag.String("tls-cert", envOr("GRC_RENDER_TLS_CERT", ""),
			"certificate chain (PEM) to serve HTTPS with; with -tls-key")
		keyFile = flag.String("tls-key", envOr("GRC_RENDER_TLS_KEY", ""),
			"private key (PEM) for -tls-cert")
		concurrency = flag.Int("concurrency", envInt("GRC_RENDER_CONCURRENCY", 2),
			"how many documents to typeset at once")
	)
	flag.Parse()

	if len(strings.TrimSpace(*token)) < minTokenLength {
		return fmt.Errorf("a token of at least %d characters is required (-token or GRC_RENDER_TOKEN)", minTokenLength)
	}
	if (*certFile == "") != (*keyFile == "") {
		return errors.New("-tls-cert and -tls-key go together")
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	for engine, st := range doctemplate.EngineStatuses() {
		if engine != doctemplate.EngineTypst && engine != doctemplate.EnginePandoc {
			continue
		}
		if st.Available {
			log.Info("engine", "engine", engine, "path", st.Path, "version", st.Version)
		} else {
			log.Warn("engine missing; jobs for it are refused", "engine", engine, "detail", st.Detail)
		}
	}

	server := &http.Server{
		Addr:              *addr,
		Handler:           doctemplate.NewRemoteServer(strings.TrimSpace(*token), *concurrency, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      doctemplate.RenderTimeout + time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}

	errs := make(chan error, 1)
	go func() {
		scheme := "http"
		if *certFile != "" {
			scheme = "https"
		}
		log.Info("listening", "addr", *addr, "scheme", scheme)
		if *certFile != "" {
			errs <- server.ListenAndServeTLS(*certFile, *keyFile)
		} else {
			errs <- server.ListenAndServe()
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case sig := <-stop:
		log.Info("stopping", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), doctemplate.RenderTimeout)
		defer cancel()
		return server.Shutdown(ctx)
	}
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func envInt(name string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return fallback
	}
	return n
}
