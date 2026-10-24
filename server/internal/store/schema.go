package store

// migrations run in order; the index+1 is stored in PRAGMA user_version.
// Never edit a shipped migration — append a new one.
var migrations = []string{
	`
CREATE TABLE users (
	id            INTEGER PRIMARY KEY,
	username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
	password_hash TEXT NOT NULL,
	is_admin      INTEGER NOT NULL DEFAULT 0,
	created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	device     TEXT NOT NULL DEFAULT '',
	ip         TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	last_seen  INTEGER NOT NULL
);

CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE libraries (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL,
	kind       TEXT NOT NULL CHECK (kind IN ('movies','shows')),
	paths      TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL,
	last_scan  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE items (
	id              INTEGER PRIMARY KEY,
	library_id      INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
	kind            TEXT NOT NULL CHECK (kind IN ('movie','show','season','episode')),
	parent_id       INTEGER REFERENCES items(id) ON DELETE CASCADE,
	show_id         INTEGER REFERENCES items(id) ON DELETE CASCADE,
	match_key       TEXT NOT NULL,
	title           TEXT NOT NULL,
	sort_title      TEXT NOT NULL,
	original_title  TEXT NOT NULL DEFAULT '',
	year            INTEGER NOT NULL DEFAULT 0,
	overview        TEXT NOT NULL DEFAULT '',
	tagline         TEXT NOT NULL DEFAULT '',
	poster          TEXT NOT NULL DEFAULT '',
	backdrop        TEXT NOT NULL DEFAULT '',
	logo            TEXT NOT NULL DEFAULT '',
	rating          REAL NOT NULL DEFAULT 0,
	content_rating  TEXT NOT NULL DEFAULT '',
	genres          TEXT NOT NULL DEFAULT '[]',
	studio          TEXT NOT NULL DEFAULT '',
	runtime         INTEGER NOT NULL DEFAULT 0,
	release_date    TEXT NOT NULL DEFAULT '',
	tmdb_id         INTEGER NOT NULL DEFAULT 0,
	collection_id   INTEGER NOT NULL DEFAULT 0,
	collection_name TEXT NOT NULL DEFAULT '',
	trailer         TEXT NOT NULL DEFAULT '',
	credits         TEXT NOT NULL DEFAULT '[]',
	season_number   INTEGER,
	episode_number  INTEGER,
	added_at        INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL,
	metadata_at     INTEGER NOT NULL DEFAULT 0,
	UNIQUE (library_id, match_key)
);
CREATE INDEX items_parent ON items(parent_id);
CREATE INDEX items_show ON items(show_id);
CREATE INDEX items_library_kind ON items(library_id, kind, added_at);
CREATE INDEX items_collection ON items(collection_id) WHERE collection_id > 0;

CREATE TABLE media_files (
	id             INTEGER PRIMARY KEY,
	item_id        INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	path           TEXT NOT NULL UNIQUE,
	size           INTEGER NOT NULL DEFAULT 0,
	mtime          INTEGER NOT NULL DEFAULT 0,
	container      TEXT NOT NULL DEFAULT '',
	duration       REAL NOT NULL DEFAULT 0,
	bitrate        INTEGER NOT NULL DEFAULT 0,
	video_codec    TEXT NOT NULL DEFAULT '',
	width          INTEGER NOT NULL DEFAULT 0,
	height         INTEGER NOT NULL DEFAULT 0,
	bit_depth      INTEGER NOT NULL DEFAULT 0,
	hdr            INTEGER NOT NULL DEFAULT 0,
	audio_codec    TEXT NOT NULL DEFAULT '',
	audio_channels INTEGER NOT NULL DEFAULT 0,
	streams        TEXT NOT NULL DEFAULT '[]',
	probed_at      INTEGER NOT NULL DEFAULT 0,
	scan_gen       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX media_files_item ON media_files(item_id);

CREATE TABLE watch_state (
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	item_id    INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	position   REAL NOT NULL DEFAULT 0,
	duration   REAL NOT NULL DEFAULT 0,
	watched    INTEGER NOT NULL DEFAULT 0,
	updated_at INTEGER NOT NULL,
	PRIMARY KEY (user_id, item_id)
);
CREATE INDEX watch_state_recent ON watch_state(user_id, updated_at);
`,
	// 2: admin editing (locked fields, labels, edition, episode order),
	// collections, and per-user library access.
	`
ALTER TABLE items ADD COLUMN locked TEXT NOT NULL DEFAULT '[]';
ALTER TABLE items ADD COLUMN labels TEXT NOT NULL DEFAULT '[]';
ALTER TABLE items ADD COLUMN edition TEXT NOT NULL DEFAULT '';
ALTER TABLE items ADD COLUMN episode_order TEXT NOT NULL DEFAULT '';

ALTER TABLE users ADD COLUMN all_libraries INTEGER NOT NULL DEFAULT 1;
CREATE TABLE user_libraries (
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
	PRIMARY KEY (user_id, library_id)
);

CREATE TABLE collections (
	id         INTEGER PRIMARY KEY,
	library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	overview   TEXT NOT NULL DEFAULT '',
	poster     TEXT NOT NULL DEFAULT '',
	backdrop   TEXT NOT NULL DEFAULT '',
	tmdb_id    INTEGER NOT NULL DEFAULT 0,
	label_rule TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX collections_name ON collections(library_id, name COLLATE NOCASE);
CREATE TABLE collection_items (
	collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
	item_id       INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	PRIMARY KEY (collection_id, item_id)
);
CREATE INDEX collection_items_item ON collection_items(item_id);

INSERT OR IGNORE INTO collections (library_id, name, tmdb_id, created_at)
	SELECT library_id, collection_name, collection_id, CAST(strftime('%s', 'now') AS INTEGER)
	FROM items WHERE collection_id > 0 AND collection_name != '' GROUP BY library_id, collection_id;
INSERT OR IGNORE INTO collection_items (collection_id, item_id)
	SELECT c.id, i.id FROM items i JOIN collections c ON c.library_id = i.library_id AND c.tmdb_id = i.collection_id
	WHERE i.collection_id > 0;
`,
	// 3: scheduled tasks. The old scan_interval_hours setting seeds the scan task.
	`
CREATE TABLE tasks (
	id               TEXT PRIMARY KEY,
	enabled          INTEGER NOT NULL,
	interval_hours   INTEGER NOT NULL,
	last_run         INTEGER NOT NULL DEFAULT 0,
	last_duration_ms INTEGER NOT NULL DEFAULT 0,
	last_result      TEXT NOT NULL DEFAULT '',
	last_error       TEXT NOT NULL DEFAULT ''
);
INSERT INTO tasks (id, enabled, interval_hours)
	SELECT 'scan_libraries', CAST(value AS INTEGER) > 0, MAX(CAST(value AS INTEGER), 1)
	FROM settings WHERE key = 'scan_interval_hours';
DELETE FROM settings WHERE key = 'scan_interval_hours';
`,
	// 4: per-user playback preferences (JSON, see store.Prefs).
	`ALTER TABLE users ADD COLUMN prefs TEXT NOT NULL DEFAULT '{}';`,
}
