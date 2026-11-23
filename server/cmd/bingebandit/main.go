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
	"bingebandit/internal/opensubs"
	"bingebandit/internal/plugins"
	"bingebandit/internal/store"
	"bingebandit/internal/stream"
	"bingebandit/internal/tasks"
	"bingebandit/internal/trakt"
	"bingebandit/internal/trickplay"
	"bingebandit/internal/tunnel"
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
	if api.OnFUSE(cfg.DataDir) {
		log.Printf("WARNING: the data directory is on a FUSE filesystem (such as Unraid's /mnt/user). " +
			"SQLite databases get damaged there over time; map /config to the pool directly, e.g. /mnt/cache/appdata/bingebandit")
	}

	st, err := store.Open(filepath.Join(cfg.DataDir, "bingebandit.db"))
	if err != nil {
		log.Fatalf("database: %v", err)
	}

	artwork := metadata.NewArtwork(filepath.Join(cfg.DataDir, "artwork"))
	meta := metadata.NewService(st)
	meta.Artwork = artwork
	scanner := library.NewScanner(st, meta, cfg.FFprobe)
	transcoder := stream.NewManager(st, cfg.FFmpeg, cfg.DataDir)
	defer transcoder.Shutdown()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	detector := markers.NewDetector(st, cfg.FFmpeg, filepath.Join(cfg.DataDir, "cache", "fingerprints"))
	thumbs := trickplay.New(st, cfg.FFmpeg, cfg.DataDir)
	images := metadata.NewImageCache(filepath.Join(cfg.DataDir, "cache", "images"))
	scheduler := tasks.New(st, tasks.Builtin(tasks.Deps{
		Store: st, Metadata: meta, Scanner: scanner, Markers: detector, Trickplay: thumbs, Images: images, DataDir: cfg.DataDir, FFprobe: cfg.FFprobe,
	})...)
	go scanner.Watch(ctx)
	live := livetv.NewManager(st, cfg.FFmpeg, cfg.FFprobe, cfg.DataDir)
	recorder := livetv.NewRecorder(st, live, cfg.DataDir)
	go recorder.Run(ctx)

	importRunner := importer.NewRunner(st)
	importRunner.Images = artwork

	srv := &api.Server{
		Store:      st,
		Scanner:    scanner,
		Metadata:   meta,
		Images:     images,
		Artwork:    artwork,
		TPDb:       metadata.NewTPDb(cfg.TPDbURL, filepath.Join(cfg.DataDir, "cache", "tpdb")),
		Transcoder: transcoder,
		Activity:   stream.NewActivity(),
		Tasks:      scheduler,
		Markers:    detector,
		Trickplay:  thumbs,
		OpenSubs: &opensubs.Client{
			UserAgent: "BingeBandit v" + version,
			Key:       func() string { return st.Setting(store.SettingOpenSubsKey) },
			Login: func() (string, string) {
				return st.Setting(store.SettingOpenSubsUser), st.Setting(store.SettingOpenSubsPassword)
			},
		},
		Live:       live,
		Logos:      livetv.NewLogoCache(filepath.Join(cfg.DataDir, "cache", "logos")),
		LogoLib:    livetv.NewLogoLibrary(filepath.Join(cfg.DataDir, "logos")),
		DVR:        recorder,
		Importer:   importRunner,
		Trakt:      trakt.NewSyncer(st, cfg.TraktAPI),
		Logs:       logBuf,
		Version:    version,
		DataDir:    cfg.DataDir,
		FFmpeg:     cfg.FFmpeg,
		FFprobe:    cfg.FFprobe,
		UpdateRepo: cfg.UpdateRepo,
		ListenAddr: cfg.Addr,
	}
	srv.Plugins = plugins.NewHost(st, []string{cfg.PluginsDir, filepath.Join(cfg.DataDir, "plugins")},
		filepath.Join(cfg.DataDir, "plugin-data"), loopbackURL(cfg.Addr))
	srv.Plugins.InstallDir = filepath.Join(cfg.DataDir, "plugins")
	srv.Plugins.Env = []string{"BINGEBANDIT_FFMPEG=" + cfg.FFmpeg, "BINGEBANDIT_FFPROBE=" + cfg.FFprobe}
	srv.Plugins.ServerName = func() string { return st.Setting(store.SettingServerName) }
	go srv.Plugins.Start(ctx)
	srv.WatchPlaybackFailures()
	go srv.RunNotifications(ctx)
	go srv.RunSports(ctx)
	go srv.RunHealth(ctx)
	scheduler.OnDone = srv.TaskDone
	go scheduler.Start(ctx)
	recorder.OnEvent = func(event string, r *store.Recording) { srv.Emit(event, r) }
	router := srv.Router(ctx)
	httpSrv := &http.Server{Addr: cfg.Addr, Handler: router, ReadHeaderTimeout: 10 * time.Second}
	// Remote access without port forwarding: the tunnel reaches the same
	// routes through a listener of its own on 127.0.0.1.
	srv.Tunnel = tunnel.New(st, cfg.DataDir, srv.TunnelHandler(router))
	srv.Tunnel.OnURL = func(string) { srv.ReloadNetwork() }
	go srv.Tunnel.Run(ctx)

	go func() {
		log.Printf("listening on %s", cfg.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	// Docker sends SIGKILL 10 seconds after SIGTERM (docker stop, Unraid,
	// auto-updaters), so everything here has to finish well inside that,
	// or the database is never closed. Open streams are cut after 4s.
	log.Print("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	httpSrv.Shutdown(shutdownCtx)
	srv.Plugins.Wait(2 * time.Second)
	if err := st.Close(); err != nil {
		log.Printf("database: close: %v", err)
	} else {
		log.Print("database closed")
	}
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
