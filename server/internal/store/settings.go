package store

// Setting keys an admin can change at runtime.
const (
	SettingServerName        = "server_name"
	SettingTMDBKey           = "tmdb_api_key"
	SettingMetadataLanguage  = "metadata_language"
	SettingHWEncoder         = "hw_encoder"
	SettingTranscodeDir      = "transcode_dir"
	SettingTonemap           = "tonemap_enabled"
	SettingAllowDownloads    = "allow_downloads"
	SettingScanIntervalHours = "scan_interval_hours"
)

var SettingDefaults = map[string]string{
	SettingServerName:        "BingeBandit",
	SettingTMDBKey:           "",
	SettingMetadataLanguage:  "en-US",
	SettingHWEncoder:         "none",
	SettingTranscodeDir:      "",
	SettingTonemap:           "true",
	SettingAllowDownloads:    "false",
	SettingScanIntervalHours: "6",
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
		out[k] = v
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
