package store

// Setting keys an admin can change at runtime.
const (
	SettingServerName        = "server_name"
	SettingTMDBKey           = "tmdb_api_key"
	SettingMetadataLanguage  = "metadata_language"
	SettingHWEncoder         = "hw_encoder"
	SettingTranscodeDir      = "transcode_dir"
	SettingTonemap           = "tonemap_enabled"
	SettingTranscodeThrottle = "transcode_throttle"
	SettingAdaptiveStreaming = "adaptive_streaming" // convert to several qualities at once so players can switch
	SettingAllowDownloads    = "allow_downloads"
	SettingWatchFolders      = "watch_folders"
	SettingMaintenanceStart  = "maintenance_start" // hour 0-23
	SettingMaintenanceEnd    = "maintenance_end"   // hour 0-23; equal to start = any time
	SettingBackupDir         = "backup_dir"
	SettingBackupKeep        = "backup_keep"
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
	SettingHWEncoder:         "none",
	SettingTranscodeDir:      "",
	SettingTonemap:           "true",
	SettingTranscodeThrottle: "true",
	SettingAdaptiveStreaming: "false",
	SettingAllowDownloads:    "false",
	SettingWatchFolders:      "true",
	SettingMaintenanceStart:  "2",
	SettingMaintenanceEnd:    "5",
	SettingBackupDir:         "",
	SettingBackupKeep:        "7",
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
