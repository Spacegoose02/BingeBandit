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
}
