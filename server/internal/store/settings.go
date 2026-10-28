package store

import (
	"crypto/rand"
	"encoding/hex"
)

// Setting keys an admin can change at runtime.
const (
	SettingServerName        = "server_name"
	SettingTMDBKey           = "tmdb_api_key"
	SettingMetadataLanguage  = "metadata_language"
	SettingTVDBKey           = "tvdb_api_key"     // TheTVDB v4 key, for shows set to use TheTVDB
	SettingTVDBPin           = "tvdb_pin"         // subscriber PIN, needed with a user-supported key
	SettingRatingsProvider   = "ratings_provider" // "" (off) | mdblist | omdb: IMDb and Rotten Tomatoes scores
	SettingRatingsKey        = "ratings_api_key"
	SettingMDBListKey        = "mdblist_api_key" // for collections that follow MDBList lists
	SettingYamtrackURL       = "yamtrack_url"    // the Yamtrack server whose lists collections follow
	SettingYamtrackUser      = "yamtrack_username"
	SettingYamtrackPassword  = "yamtrack_password"
	SettingHWEncoder         = "hw_encoder"
	SettingTranscodeDir      = "transcode_dir"
	SettingTonemap           = "tonemap_enabled"
	SettingTonemapAlgorithm  = "tonemap_algorithm" // hable | mobius | reinhard | clip | linear
	SettingHWDecode          = "hw_decode"         // also decode on the GPU when converting video
	SettingHWDevice          = "hw_device"         // VAAPI/QSV render node (/dev/dri/renderD129) or NVIDIA GPU number; empty = default
	SettingTranscodeThrottle = "transcode_throttle"
	SettingAdaptiveStreaming = "adaptive_streaming" // convert to several qualities at once so players can switch
	SettingAllowDownloads    = "allow_downloads"
	SettingRecommendations   = "recommendations_enabled" // false: nobody can recommend titles to anyone
	SettingWatchedPercent    = "watched_percent"         // how far into a movie or episode it counts as watched
	SettingContinueDays      = "continue_watching_days"  // titles not played for this long leave Continue Watching; 0 = never
	SettingWatchFolders      = "watch_folders"
	SettingMaintenanceStart  = "maintenance_start" // hour 0-23
	SettingMaintenanceEnd    = "maintenance_end"   // hour 0-23; equal to start = any time
	SettingBackupDir         = "backup_dir"
	SettingPreRollPaths      = "preroll_paths"   // intro videos before movies: files or folders, one per line
	SettingPluginCatalogs    = "plugin_catalogs" // more plugin catalogue URLs, one per line (the official one is always used)
	SettingBackupKeep        = "backup_keep"
	// S3-compatible storage that each backup is also uploaded to.
	SettingBackupS3Endpoint  = "backup_s3_endpoint" // e.g. https://s3.us-west-002.backblazeb2.com; empty = off
	SettingBackupS3Region    = "backup_s3_region"
	SettingBackupS3Bucket    = "backup_s3_bucket"
	SettingBackupS3Prefix    = "backup_s3_prefix" // folder inside the bucket
	SettingBackupS3AccessKey = "backup_s3_access_key"
	SettingBackupS3SecretKey = "backup_s3_secret_key"
	SettingDVRFolder         = "dvr_folder"
	SettingDVRNaming         = "dvr_naming" // library (Show/Season 01/…) | folder (one flat folder)
	SettingDVRApproval       = "dvr_require_approval"
	SettingDVRAllowUsers     = "dvr_allow_users"
	SettingDVRPadBefore      = "dvr_padding_before" // minutes
	SettingDVRPadAfter       = "dvr_padding_after"  // minutes
	SettingDVRPriority       = "dvr_priority"       // true: a recording may take a provider connection from live viewers
	SettingPublicURL         = "public_url"         // how people reach the server from outside, e.g. https://tv.example.com
	SettingTrustedProxies    = "trusted_proxies"    // IPs/CIDRs whose X-Forwarded-* headers are believed
	SettingLANNetworks       = "lan_networks"       // extra CIDRs treated as local, beyond private ranges
	SettingRemoteAccess      = "remote_access"      // false = only local networks may connect
	SettingTraktClientID     = "trakt_client_id"    // the server's Trakt API app
	SettingTraktSecret       = "trakt_client_secret"
	SettingLiveTimeshift     = "live_timeshift_minutes" // how far back live TV can be paused or rewound; 0 = just a short buffer
	SettingLiveBufferSeconds = "live_buffer_seconds"    // how far behind live players sit
	SettingLiveStallSeconds  = "live_stall_seconds"     // no data this long → switch to the next stream
	SettingLiveSwitchSlow    = "live_switch_slow"       // false: only switch streams when one fails, not when it falls behind
	SettingLiveProxy         = "live_proxy"             // proxy for provider traffic; empty = HTTP(S)_PROXY environment
	// Poster overlays (Kometa's): badges drawn on movie and show posters as
	// they're served. The master switch also covers uploaded overlays.
	SettingOverlays          = "overlays_enabled"
	SettingOverlayResolution = "overlay_resolution" // 4K / 1080P / 720P / SD
	SettingOverlayHDR        = "overlay_hdr"        // Dolby Vision / HDR
	SettingOverlayAudio      = "overlay_audio"      // Atmos, DTS:X, TrueHD, DTS-HD, 7.1, 5.1
	SettingOverlayRatings    = "overlay_ratings"    // IMDb, Rotten Tomatoes and TMDB scores
	// Default collections (Kometa's defaults), built in every movie and show
	// library by the "Build default collections" task.
	SettingDefaultStudios = "default_collections_studios" // studios (networks for shows)
	SettingDefaultDecades = "default_collections_decades"
	SettingDefaultGenres  = "default_collections_genres"
	SettingDefaultAwards  = "default_collections_awards"  // award winners, from Wikidata
	SettingDefaultMinimum = "default_collections_minimum" // titles a studio, decade or genre needs for a collection
)

var SettingDefaults = map[string]string{
	SettingServerName:        "BingeBandit",
	SettingTMDBKey:           "",
	SettingMetadataLanguage:  "en-US",
	SettingTVDBKey:           "",
	SettingTVDBPin:           "",
	SettingRatingsProvider:   "",
	SettingRatingsKey:        "",
	SettingMDBListKey:        "",
	SettingYamtrackURL:       "",
	SettingYamtrackUser:      "",
	SettingYamtrackPassword:  "",
	SettingHWEncoder:         "none",
	SettingTranscodeDir:      "",
	SettingTonemap:           "true",
	SettingTonemapAlgorithm:  "hable",
	SettingHWDecode:          "false",
	SettingHWDevice:          "",
	SettingTranscodeThrottle: "true",
	SettingAdaptiveStreaming: "false",
	SettingAllowDownloads:    "false",
	SettingRecommendations:   "true",
	SettingWatchedPercent:    "90",
	SettingContinueDays:      "0",
	SettingWatchFolders:      "true",
	SettingMaintenanceStart:  "2",
	SettingMaintenanceEnd:    "5",
	SettingBackupDir:         "",
	SettingPreRollPaths:      "",
	SettingPluginCatalogs:    "",
	SettingBackupKeep:        "7",
	SettingBackupS3Endpoint:  "",
	SettingBackupS3Region:    "",
	SettingBackupS3Bucket:    "",
	SettingBackupS3Prefix:    "bingebandit",
	SettingBackupS3AccessKey: "",
	SettingBackupS3SecretKey: "",
	SettingDVRFolder:         "",
	SettingDVRNaming:         "library",
	SettingDVRApproval:       "false",
	SettingDVRAllowUsers:     "true",
	SettingDVRPadBefore:      "1",
	SettingDVRPadAfter:       "3",
	SettingDVRPriority:       "false",
	SettingPublicURL:         "",
	SettingTrustedProxies:    "",
	SettingLANNetworks:       "",
	SettingRemoteAccess:      "true",
	SettingTraktClientID:     "",
	SettingTraktSecret:       "",
	SettingLiveTimeshift:     "15",
	SettingLiveBufferSeconds: "10",
	SettingLiveStallSeconds:  "12",
	SettingLiveSwitchSlow:    "true",
	SettingLiveProxy:         "",
	SettingOverlays:          "false",
	SettingOverlayResolution: "true",
	SettingOverlayHDR:        "true",
	SettingOverlayAudio:      "true",
	SettingOverlayRatings:    "true",
	SettingDefaultStudios:    "false",
	SettingDefaultDecades:    "false",
	SettingDefaultGenres:     "false",
	SettingDefaultAwards:     "false",
	SettingDefaultMinimum:    "5",
}

func (s *Store) Setting(key string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return SettingDefaults[key]
	}
	return v
}

func (s *Store) Settings() (map[string]string, error) {
	out := make(map[string]string, len(SettingDefaults))
	for k, v := range SettingDefaults {
		out[k] = v
	}
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		// Retired keys may linger in old databases; never hand them out.
		if _, known := SettingDefaults[k]; known {
			out[k] = v
		}
	}
	return out, rows.Err()
}

func (s *Store) SetSettings(values map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range values {
		if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.loadOverlayTag()
	return nil
}

// settingServerID names this server for apps that know it by more than one
// address (its LAN address and its public URL): they check it before
// switching. It isn't an admin setting, so Settings leaves it out.
const settingServerID = "server_id"

// ServerID returns this server's id, making one the first time.
func (s *Store) ServerID() string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, settingServerID).Scan(&v); err == nil && v != "" {
		return v
	}
	b := make([]byte, 16)
	rand.Read(b)
	s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`, settingServerID, hex.EncodeToString(b))
	s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, settingServerID).Scan(&v)
	return v
}
