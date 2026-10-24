// Command bingebandit is the BingeBandit media server.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"bingebandit/internal/api"
	"bingebandit/internal/config"
	"bingebandit/internal/library"
	"bingebandit/internal/logs"
	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
	"bingebandit/internal/stream"
	"bingebandit/internal/tasks"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0"
var version = "dev"

func main() {
	cfg := config.Load()
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("data dir: %v", err)
	}
	logBuf, err := logs.New(filepath.Join(cfg.DataDir, "logs"), os.Stderr)
	if err != nil {
		log.Fatalf("log file: %v", err)
	}
	log.SetOutput(logBuf)
	log.Printf("BingeBandit %s", version)
	log.Printf("data directory: %s", cfg.DataDir)

	st, err := store.Open(filepath.Join(cfg.DataDir, "bingebandit.db"))
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer st.Close()

	meta := metadata.NewService(st)
	scanner := library.NewScanner(st, meta, cfg.FFprobe)
	transcoder := stream.NewManager(st, cfg.FFmpeg, cfg.DataDir)
	defer transcoder.Shutdown()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	scheduler := tasks.New(st, tasks.Builtin(tasks.Deps{Store: st, Scanner: scanner, DataDir: cfg.DataDir, FFprobe: cfg.FFprobe})...)
	go scheduler.Start(ctx)
	go scanner.Watch(ctx)

	srv := &api.Server{
		Store:      st,
		Scanner:    scanner,
		Metadata:   meta,
		Images:     metadata.NewImageCache(filepath.Join(cfg.DataDir, "cache", "images")),
		Artwork:    metadata.NewArtwork(filepath.Join(cfg.DataDir, "artwork")),
		Transcoder: transcoder,
		Activity:   stream.NewActivity(),
		Tasks:      scheduler,
		Logs:       logBuf,
		Version:    version,
		DataDir:    cfg.DataDir,
		FFmpeg:     cfg.FFmpeg,
		FFprobe:    cfg.FFprobe,
	}
	httpSrv := &http.Server{Addr: cfg.Addr, Handler: srv.Router(ctx), ReadHeaderTimeout: 10 * time.Second}

	go func() {
		log.Printf("listening on %s", cfg.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpSrv.Shutdown(shutdownCtx)
}
