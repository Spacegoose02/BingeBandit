package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/s3"
	"bingebandit/internal/store"
	"bingebandit/internal/tasks"
)

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Tasks.List())
}

func (s *Server) configureTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled       bool `json:"enabled"`
		IntervalHours int  `json:"intervalHours"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Tasks.Configure(chi.URLParam(r, "id"), req.Enabled, req.IntervalHours); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runTask(w http.ResponseWriter, r *http.Request) {
	// Detach from the request so the task outlives it.
	err := s.Tasks.RunNow(s.ctx, chi.URLParam(r, "id"))
	switch {
	case errors.Is(err, tasks.ErrRunning):
		writeErr(w, http.StatusConflict, err.Error())
	case err != nil:
		writeErr(w, http.StatusNotFound, err.Error())
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}

// logLines returns buffered log lines newer than ?after=<seq>.
func (s *Server) logLines(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	writeJSON(w, http.StatusOK, s.Logs.Since(after))
}

func (s *Server) downloadLog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="bingebandit.log"`)
	http.ServeFile(w, r, s.Logs.Path())
}

type backupFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

var backupName = tasks.BackupFileName

// listBackups lists the database backups in the backup folder, newest first.
func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	dir := tasks.BackupDir(s.Store, s.DataDir)
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []backupFile{}
	for _, e := range entries {
		if e.IsDir() || !backupName.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, backupFile{e.Name(), info.Size(), info.ModTime().Unix()})
		}
	}
	// Names carry the time, so they sort newest first in reverse.
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	res := map[string]any{"dir": dir, "backups": out}
	if remote := tasks.RemoteBackups(s.Store); remote.Configured() {
		res["bucket"] = remote.Bucket
		res["remote"], res["remoteError"] = remoteBackups(r.Context(), remote)
	}
	writeJSON(w, http.StatusOK, res)
}

// remoteBackups lists the backups in the bucket, newest first, or why it
// couldn't.
func remoteBackups(ctx context.Context, remote *s3.Client) ([]backupFile, string) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	objs, err := remote.List(ctx)
	if err != nil {
		return []backupFile{}, err.Error()
	}
	out := []backupFile{}
	for i := len(objs) - 1; i >= 0; i-- {
		if backupName.MatchString(objs[i].Name) {
			out = append(out, backupFile{objs[i].Name, objs[i].Size, objs[i].Modified.Unix()})
		}
	}
	return out, ""
}

// testRemoteBackups checks the storage settings in the body (not yet saved)
// by writing, listing and deleting a small file.
func (s *Server) testRemoteBackups(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	remote := &s3.Client{
		Endpoint: strings.TrimSpace(in[store.SettingBackupS3Endpoint]), Region: strings.TrimSpace(in[store.SettingBackupS3Region]),
		Bucket: strings.TrimSpace(in[store.SettingBackupS3Bucket]), Prefix: strings.TrimSpace(in[store.SettingBackupS3Prefix]),
		AccessKey: strings.TrimSpace(in[store.SettingBackupS3AccessKey]), SecretKey: strings.TrimSpace(in[store.SettingBackupS3SecretKey]),
	}
	if !remote.Configured() {
		writeErr(w, http.StatusBadRequest, "fill in the endpoint, bucket, access key and secret key")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	const probe = "bingebandit-connection-test.txt"
	step := func(what string, err error) bool {
		if err != nil {
			writeErr(w, http.StatusBadRequest, what+": "+err.Error())
		}
		return err == nil
	}
	if !step("could not upload", remote.PutBytes(ctx, probe, []byte("BingeBandit can write backups here.\n"))) {
		return
	}
	objs, err := remote.List(ctx)
	if !step("could not list the bucket", err) || !step("could not delete the test file", remote.Delete(ctx, probe)) {
		return
	}
	n := 0
	for _, o := range objs {
		if backupName.MatchString(o.Name) {
			n++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"backups": n})
}

// restoreRemoteBackup downloads a backup from the bucket and restores it.
func (s *Server) restoreRemoteBackup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	remote := tasks.RemoteBackups(s.Store)
	if !backupName.MatchString(name) || !remote.Configured() {
		writeErr(w, http.StatusBadRequest, "bad backup name")
		return
	}
	f, err := os.CreateTemp(s.DataDir, "remote-*.db")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	path := f.Name()
	defer os.Remove(path)
	err = remote.Get(r.Context(), name, f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, "could not download the backup: "+err.Error())
		return
	}
	s.restoreFrom(w, r, path, name+" from "+remote.Bucket)
}

// restoreBackup backs up the current database, then replaces its contents
// with the named backup.
func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !backupName.MatchString(name) {
		writeErr(w, http.StatusBadRequest, "bad backup name")
		return
	}
	path := filepath.Join(tasks.BackupDir(s.Store, s.DataDir), name)
	if _, err := os.Stat(path); err != nil {
		writeErr(w, http.StatusNotFound, "backup not found")
		return
	}
	s.restoreFrom(w, r, path, name)
}

// downloadBackup sends a backup file, so it can be kept elsewhere or
// uploaded to a new server.
func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !backupName.MatchString(name) {
		writeErr(w, http.StatusBadRequest, "bad backup name")
		return
	}
	path := filepath.Join(tasks.BackupDir(s.Store, s.DataDir), name)
	if _, err := os.Stat(path); err != nil {
		writeErr(w, http.StatusNotFound, "backup not found")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, path)
}

// MaxBackupUpload caps an uploaded backup. Databases are mostly metadata, so
// even very large libraries stay far below it.
const MaxBackupUpload = 8 << 30

// uploadRestore restores from a backup file sent as the request body (an
// admin moving to this server, or restoring a copy kept elsewhere).
func (s *Server) uploadRestore(w http.ResponseWriter, r *http.Request) {
	path, err := s.receiveBackup(w, r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	defer os.Remove(path)
	s.restoreFrom(w, r, path, "the uploaded file")
}

// setupRestore lets a brand-new server start from a backup instead of a new
// admin account. Like setup, it only works while no users exist; afterwards
// people sign in with the accounts from the backup.
func (s *Server) setupRestore(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Requested-With") != "BingeBandit" {
		writeErr(w, http.StatusForbidden, "missing X-Requested-With header")
		return
	}
	if n, err := s.Store.CountUsers(); err != nil || n > 0 {
		writeErr(w, http.StatusConflict, "server is already set up")
		return
	}
	path, err := s.receiveBackup(w, r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	defer os.Remove(path)
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	// Checked again under the lock so two uploads can't both get in.
	if n, err := s.Store.CountUsers(); err != nil || n > 0 {
		writeErr(w, http.StatusConflict, "server is already set up")
		return
	}
	if err := s.Store.Restore(r.Context(), path, ""); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.reloadNetwork()
	log.Printf("new server set up from an uploaded backup")
	writeJSON(w, http.StatusOK, map[string]string{"restored": "the uploaded file"})
}

var sqliteMagic = []byte("SQLite format 3\x00")

// receiveBackup saves the request body to a temporary file in the data
// folder (the system temp folder can be small in containers) and returns its
// path. The caller removes it.
func (s *Server) receiveBackup(w http.ResponseWriter, r *http.Request) (string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBackupUpload)
	f, err := os.CreateTemp(s.DataDir, "upload-*.db")
	if err != nil {
		return "", err
	}
	path := f.Name()
	_, err = io.Copy(f, r.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return "", errors.New("the file is too large to be a BingeBandit backup")
		}
		return "", fmt.Errorf("upload failed: %w", err)
	}
	head := make([]byte, len(sqliteMagic))
	if f, err := os.Open(path); err == nil {
		_, err = io.ReadFull(f, head)
		f.Close()
	}
	if !bytes.Equal(head, sqliteMagic) {
		os.Remove(path)
		return "", errors.New("not a BingeBandit backup (expected a .db file from Scheduled Tasks)")
	}
	return path, nil
}

// restoreFrom backs up the current database, then replaces its contents with
// the backup at path. label names the backup in messages.
func (s *Server) restoreFrom(w http.ResponseWriter, r *http.Request, path, label string) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	safety, err := tasks.SaveBackup(s.Store, tasks.BackupDir(s.Store, s.DataDir))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not back up the current database first: "+err.Error())
		return
	}
	if err := s.Store.Restore(r.Context(), path, requestToken(r)); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.reloadNetwork()
	log.Printf("database restored from %s (previous data saved as %s)", label, filepath.Base(safety))
	writeJSON(w, http.StatusOK, map[string]string{"restored": label, "previous": filepath.Base(safety)})
}
