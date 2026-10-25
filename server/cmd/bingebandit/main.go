// Command bingebandit is the BingeBandit media server.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"bingebandit/internal/api"
	"bingebandit/internal/config"
	"bingebandit/internal/importer"
	"bingebandit/internal/library"
	"bingebandit/internal/livetv"
	"bingebandit/internal/logs"
	"bingebandit/internal/markers"
	"bingebandit/internal/metadata"
	"bingebandit/internal/plugins"
	"bingebandit/internal/store"
	"bingebandit/internal/stream"
	"bingebandit/internal/tasks"
	"bingebandit/internal/trakt"
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
	detector := markers.NewDetector(st, cfg.FFmpeg, filepath.Join(cfg.DataDir, "cache", "fingerprints"))
	scheduler := tasks.New(st, tasks.Builtin(tasks.Deps{
		Store: st, Metadata: meta, Scanner: scanner, Markers: detector, DataDir: cfg.DataDir, FFprobe: cfg.FFprobe,
	})...)
	go scheduler.Start(ctx)
	go scanner.Watch(ctx)
	live := livetv.NewManager(st, cfg.FFmpeg, cfg.FFprobe, cfg.DataDir)
	recorder := livetv.NewRecorder(st, live, cfg.DataDir)
	go recorder.Run(ctx)

	artwork := metadata.NewArtwork(filepath.Join(cfg.DataDir, "artwork"))
	srv := &api.Server{
		Store:      st,
		Scanner:    scanner,
		Metadata:   meta,
		Images:     metadata.NewImageCache(filepath.Join(cfg.DataDir, "cache", "images")),
		Artwork:    artwork,
		TPDb:       metadata.NewTPDb(cfg.TPDbURL, filepath.Join(cfg.DataDir, "cache", "tpdb")),
		Transcoder: transcoder,
		Activity:   stream.NewActivity(),
		Tasks:      scheduler,
		Markers:    detector,
		Live:       live,
		Logos:      livetv.NewLogoCache(filepath.Join(cfg.DataDir, "cache", "logos")),
		LogoLib:    livetv.NewLogoLibrary(filepath.Join(cfg.DataDir, "logos")),
		DVR:        recorder,
		Importer:   importer.NewRunner(st, artwork),
		Trakt:      trakt.NewSyncer(st, cfg.TraktAPI),
		Logs:       logBuf,
		Version:    version,
		DataDir:    cfg.DataDir,
		FFmpeg:     cfg.FFmpeg,
		FFprobe:    cfg.FFprobe,
		UpdateRepo: cfg.UpdateRepo,
	}
	srv.Plugins = plugins.NewHost(st, []string{cfg.PluginsDir, filepath.Join(cfg.DataDir, "plugins")},
		filepath.Join(cfg.DataDir, "plugin-data"), loopbackURL(cfg.Addr))
	srv.Plugins.InstallDir = filepath.Join(cfg.DataDir, "plugins")
	srv.Plugins.Env = []string{"BINGEBANDIT_FFMPEG=" + cfg.FFmpeg, "BINGEBANDIT_FFPROBE=" + cfg.FFprobe}
	srv.Plugins.ServerName = func() string { return st.Setting(store.SettingServerName) }
	go srv.Plugins.Start(ctx)
	go srv.RunNotifications(ctx)
	recorder.OnEvent = func(event string, r *store.Recording) { srv.Emit(event, r) }
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
	srv.Plugins.Wait(5 * time.Second)
}

// loopbackURL is how plugins this server starts reach it: the listen port on 127.0.0.1.
func loopbackURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:8484"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
