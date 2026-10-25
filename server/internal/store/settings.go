package store

// Setting keys an admin can change at runtime.
const (
	SettingServerName        = "server_name"
	SettingTMDBKey           = "tmdb_api_key"
	SettingMetadataLanguage  = "metadata_language"
	SettingTVDBKey           = "tvdb_api_key"     // TheTVDB v4 key, for shows set to use TheTVDB
	SettingTVDBPin           = "tvdb_pin"         // subscriber PIN, needed with a user-supported key
	SettingRatingsProvider   = "ratings_provider" // "" (off) | mdblist | omdb: IMDb and Rotten Tomatoes scores
	SettingRatingsKey        = "ratings_api_key"
	SettingHWEncoder         = "hw_encoder"
	SettingTranscodeDir      = "transcode_dir"
	SettingTonemap           = "tonemap_enabled"
	SettingTonemapAlgorithm  = "tonemap_algorithm" // hable | mobius | reinhard | clip | linear
	SettingHWDecode          = "hw_decode"         // also decode on the GPU when converting video
	SettingHWDevice          = "hw_device"         // VAAPI/QSV render node (/dev/dri/renderD129) or NVIDIA GPU number; empty = default
	SettingTranscodeThrottle = "transcode_throttle"
	SettingAdaptiveStreaming = "adaptive_streaming" // convert to several qualities at once so players can switch
	SettingAllowDownloads    = "allow_downloads"
	SettingWatchFolders      = "watch_folders"
	SettingMaintenanceStart  = "maintenance_start" // hour 0-23
	SettingMaintenanceEnd    = "maintenance_end"   // hour 0-23; equal to start = any time
	SettingBackupDir         = "backup_dir"
	SettingPreRollPaths      = "preroll_paths" // intro videos before movies: files or folders, one per line
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
	SettingLiveProxy         = "live_proxy"             // proxy for provider traffic; empty = HTTP(S)_PROXY environment
)

var SettingDefaults = map[string]string{
	SettingServerName:        "BingeBandit",
	SettingTMDBKey:           "",
	SettingMetadataLanguage:  "en-US",
	SettingTVDBKey:           "",
	SettingTVDBPin:           "",
	SettingRatingsProvider:   "",
	SettingRatingsKey:        "",
	SettingHWEncoder:         "none",
	SettingTranscodeDir:      "",
	SettingTonemap:           "true",
	SettingTonemapAlgorithm:  "hable",
	SettingHWDecode:          "false",
	SettingHWDevice:          "",
	SettingTranscodeThrottle: "true",
	SettingAdaptiveStreaming: "false",
	SettingAllowDownloads:    "false",
	SettingWatchFolders:      "true",
	SettingMaintenanceStart:  "2",
	SettingMaintenanceEnd:    "5",
	SettingBackupDir:         "",
	SettingPreRollPaths:      "",
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
	SettingLiveProxy:         "",
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
	return tx.Commit()
}
