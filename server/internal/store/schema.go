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
	// 5: play history, watchlist, playlists, shareable profiles.
	`
CREATE TABLE plays (
	id          INTEGER PRIMARY KEY,
	user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	item_id     INTEGER REFERENCES items(id) ON DELETE SET NULL,
	title       TEXT NOT NULL,              -- kept so history survives library removals
	subtitle    TEXT NOT NULL DEFAULT '',
	kind        TEXT NOT NULL,
	library_id  INTEGER NOT NULL DEFAULT 0,
	show_id     INTEGER NOT NULL DEFAULT 0,
	started_at  INTEGER NOT NULL,
	stopped_at  INTEGER NOT NULL,
	watched     INTEGER NOT NULL DEFAULT 0, -- seconds actually played
	completed   INTEGER NOT NULL DEFAULT 0,
	mode        TEXT NOT NULL DEFAULT '',
	client      TEXT NOT NULL DEFAULT '',
	ip          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX plays_user ON plays(user_id, started_at);
CREATE INDEX plays_started ON plays(started_at);

CREATE TABLE watchlist (
	user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	item_id  INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	added_at INTEGER NOT NULL,
	PRIMARY KEY (user_id, item_id)
);

CREATE TABLE playlists (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX playlists_user ON playlists(user_id);
CREATE TABLE playlist_items (
	playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
	item_id     INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	position    INTEGER NOT NULL,
	PRIMARY KEY (playlist_id, item_id)
);

ALTER TABLE users ADD COLUMN profile_public INTEGER NOT NULL DEFAULT 0;
`,
	// 6: intro/credits markers. markers_at records when detection last ran on an episode.
	`
CREATE TABLE markers (
	item_id  INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	kind     TEXT NOT NULL CHECK (kind IN ('intro','credits')),
	start_at REAL NOT NULL,
	end_at   REAL NOT NULL,
	source   TEXT NOT NULL DEFAULT 'detected' CHECK (source IN ('detected','manual')),
	PRIMARY KEY (item_id, kind)
);
ALTER TABLE items ADD COLUMN markers_at INTEGER NOT NULL DEFAULT 0;
`,
	// 7: Live TV — M3U/EPG sources, the raw streams they list, channels built
	// from those streams (with fallbacks), guide data and favourites.
	`
CREATE TABLE live_sources (
	id            INTEGER PRIMARY KEY,
	kind          TEXT NOT NULL CHECK (kind IN ('m3u','epg')),
	name          TEXT NOT NULL,
	url           TEXT NOT NULL,          -- http(s) URL or a local file path
	user_agent    TEXT NOT NULL DEFAULT '',
	max_streams   INTEGER NOT NULL DEFAULT 0, -- provider connection limit, 0 = unlimited
	refresh_hours INTEGER NOT NULL DEFAULT 24,
	enabled       INTEGER NOT NULL DEFAULT 1,
	last_refresh  INTEGER NOT NULL DEFAULT 0,
	last_error    TEXT NOT NULL DEFAULT '',
	entry_count   INTEGER NOT NULL DEFAULT 0,
	created_at    INTEGER NOT NULL
);

CREATE TABLE live_streams (
	id        INTEGER PRIMARY KEY,
	source_id INTEGER NOT NULL REFERENCES live_sources(id) ON DELETE CASCADE,
	name      TEXT NOT NULL,
	url       TEXT NOT NULL,
	logo      TEXT NOT NULL DEFAULT '',
	grp       TEXT NOT NULL DEFAULT '',
	tvg_id    TEXT NOT NULL DEFAULT '',
	tvg_chno  TEXT NOT NULL DEFAULT '',
	seen_gen  INTEGER NOT NULL DEFAULT 0,
	UNIQUE (source_id, url)
);
CREATE INDEX live_streams_group ON live_streams(source_id, grp);

CREATE TABLE channels (
	id         INTEGER PRIMARY KEY,
	number     REAL NOT NULL,
	name       TEXT NOT NULL,
	logo       TEXT NOT NULL DEFAULT '',
	grp        TEXT NOT NULL DEFAULT '',
	epg_id     TEXT NOT NULL DEFAULT '',    -- XMLTV channel id
	enabled    INTEGER NOT NULL DEFAULT 1,
	created_at INTEGER NOT NULL
);
CREATE INDEX channels_number ON channels(number);

CREATE TABLE channel_streams (
	channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	stream_id  INTEGER NOT NULL REFERENCES live_streams(id) ON DELETE CASCADE,
	priority   INTEGER NOT NULL,
	PRIMARY KEY (channel_id, stream_id)
);

CREATE TABLE epg_channels (
	source_id    INTEGER NOT NULL REFERENCES live_sources(id) ON DELETE CASCADE,
	xmltv_id     TEXT NOT NULL,
	display_name TEXT NOT NULL,
	icon         TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (source_id, xmltv_id)
);

CREATE TABLE epg_programs (
	id          INTEGER PRIMARY KEY,
	source_id   INTEGER NOT NULL REFERENCES live_sources(id) ON DELETE CASCADE,
	xmltv_id    TEXT NOT NULL,
	start_at    INTEGER NOT NULL,
	stop_at     INTEGER NOT NULL,
	title       TEXT NOT NULL,
	subtitle    TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	category    TEXT NOT NULL DEFAULT '',
	icon        TEXT NOT NULL DEFAULT '',
	episode     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX epg_programs_channel ON epg_programs(xmltv_id, start_at);

CREATE TABLE channel_favorites (
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	PRIMARY KEY (user_id, channel_id)
);
`,
	// 8: DVR — series rules and individual recordings.
	`
CREATE TABLE recording_rules (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE, -- NULL = any channel
	title      TEXT NOT NULL,
	status     TEXT NOT NULL CHECK (status IN ('pending','active')),
	created_at INTEGER NOT NULL
);

CREATE TABLE recordings (
	id            INTEGER PRIMARY KEY,
	user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	rule_id       INTEGER REFERENCES recording_rules(id) ON DELETE SET NULL,
	channel_id    INTEGER REFERENCES channels(id) ON DELETE SET NULL,
	channel_name  TEXT NOT NULL,
	epg_id        TEXT NOT NULL DEFAULT '',
	title         TEXT NOT NULL,
	subtitle      TEXT NOT NULL DEFAULT '',
	description   TEXT NOT NULL DEFAULT '',
	episode       TEXT NOT NULL DEFAULT '',
	program_start INTEGER NOT NULL,
	program_stop  INTEGER NOT NULL,
	start_at      INTEGER NOT NULL, -- program times plus padding
	stop_at       INTEGER NOT NULL,
	status        TEXT NOT NULL CHECK (status IN ('pending','scheduled','recording','completed','failed','cancelled')),
	path          TEXT NOT NULL DEFAULT '',
	size          INTEGER NOT NULL DEFAULT 0,
	error         TEXT NOT NULL DEFAULT '',
	created_at    INTEGER NOT NULL,
	UNIQUE (channel_id, program_start)
);
CREATE INDEX recordings_due ON recordings(status, start_at);
`,
	// 9: custom channels (ErsatzTV-style) built from library content.
	`
ALTER TABLE channels ADD COLUMN custom INTEGER NOT NULL DEFAULT 0;

CREATE TABLE custom_blocks (
	id           INTEGER PRIMARY KEY,
	channel_id   INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	position     INTEGER NOT NULL DEFAULT 0,
	start_minute INTEGER NOT NULL DEFAULT -1, -- minutes after midnight; -1 = all day (the default block)
	end_minute   INTEGER NOT NULL DEFAULT -1,
	mode         TEXT NOT NULL DEFAULT 'shuffle' CHECK (mode IN ('shuffle','order')),
	sources      TEXT NOT NULL DEFAULT '[]',
	cursor       INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE playout_items (
	id          INTEGER PRIMARY KEY,
	channel_id  INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	item_id     INTEGER REFERENCES items(id) ON DELETE SET NULL,
	file_id     INTEGER NOT NULL,
	start_at    INTEGER NOT NULL,
	stop_at     INTEGER NOT NULL,
	title       TEXT NOT NULL,
	subtitle    TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	category    TEXT NOT NULL DEFAULT '',
	icon        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX playout_channel ON playout_items(channel_id, start_at);
`,
	// 10: security — TOTP 2FA, API keys, webhooks, opt-in activity sharing.
	`
ALTER TABLE users ADD COLUMN totp_secret TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN recovery_codes TEXT NOT NULL DEFAULT '[]';
ALTER TABLE users ADD COLUMN share_activity INTEGER NOT NULL DEFAULT 0;

CREATE TABLE api_keys (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	key_hash   TEXT NOT NULL UNIQUE,
	prefix     TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	last_used  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE webhooks (
	id          INTEGER PRIMARY KEY,
	name        TEXT NOT NULL,
	url         TEXT NOT NULL,
	events      TEXT NOT NULL DEFAULT '[]',
	secret      TEXT NOT NULL DEFAULT '',
	enabled     INTEGER NOT NULL DEFAULT 1,
	last_status TEXT NOT NULL DEFAULT '',
	last_at     INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL
);
`,
	// 11: single sign-on through OpenID Connect providers, and the accounts linked to them.
	`
CREATE TABLE oidc_providers (
	id            INTEGER PRIMARY KEY,
	name          TEXT NOT NULL,
	issuer        TEXT NOT NULL,
	client_id     TEXT NOT NULL,
	client_secret TEXT NOT NULL DEFAULT '',
	scopes        TEXT NOT NULL DEFAULT '',
	auto_create   INTEGER NOT NULL DEFAULT 0,
	enabled       INTEGER NOT NULL DEFAULT 1,
	created_at    INTEGER NOT NULL
);

CREATE TABLE user_identities (
	id          INTEGER PRIMARY KEY,
	user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	provider_id INTEGER NOT NULL REFERENCES oidc_providers(id) ON DELETE CASCADE,
	subject     TEXT NOT NULL,
	email       TEXT NOT NULL DEFAULT '',
	created_at  INTEGER NOT NULL,
	last_used   INTEGER NOT NULL DEFAULT 0,
	UNIQUE (provider_id, subject)
);
CREATE INDEX user_identities_user ON user_identities(user_id);
`,
	// 12: imported plays (Plex, Tautulli) carry a source key so re-running an import adds nothing twice.
	`
ALTER TABLE plays ADD COLUMN import_key TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX plays_import ON plays(user_id, import_key) WHERE import_key != '';
`,
	// 13: Trakt accounts, and which plays have been sent to Trakt.
	`
CREATE TABLE trakt_accounts (
	user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	username      TEXT NOT NULL DEFAULT '',
	access_token  TEXT NOT NULL,
	refresh_token TEXT NOT NULL,
	expires_at    INTEGER NOT NULL,
	auto_sync     INTEGER NOT NULL DEFAULT 1,
	connected_at  INTEGER NOT NULL,
	last_sync     INTEGER NOT NULL DEFAULT 0,
	last_error    TEXT NOT NULL DEFAULT ''
);
ALTER TABLE plays ADD COLUMN trakt_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE webhooks ADD COLUMN format TEXT NOT NULL DEFAULT ''; -- '' (BingeBandit JSON) or 'plex'
`,
	// 14: per-playlist group filter. Streams in a disabled group aren't imported
	// (unless a channel already uses them); new_groups_enabled decides groups a
	// later refresh discovers. Existing groups start enabled.
	`
ALTER TABLE live_sources ADD COLUMN new_groups_enabled INTEGER NOT NULL DEFAULT 1;
CREATE TABLE live_source_groups (
	source_id INTEGER NOT NULL REFERENCES live_sources(id) ON DELETE CASCADE,
	name      TEXT NOT NULL,
	enabled   INTEGER NOT NULL DEFAULT 1,
	streams   INTEGER NOT NULL DEFAULT 0, -- in the playlist, imported or not
	PRIMARY KEY (source_id, name)
);
INSERT INTO live_source_groups (source_id, name, enabled, streams)
	SELECT source_id, grp, 1, COUNT(*) FROM live_streams GROUP BY source_id, grp;
`,
	// 15: a per-user passcode that guards settings (Account and Admin).
	`
ALTER TABLE users ADD COLUMN passcode_hash TEXT NOT NULL DEFAULT '';
`,
	// 16: profiles. A profile is a user under an account (owner_id): one
	// sign-in, separate history, watchlist and preferences. Profiles have no
	// password; they're reached by switching from another member.
	`
ALTER TABLE users ADD COLUMN owner_id INTEGER REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE users ADD COLUMN profile_name TEXT NOT NULL DEFAULT '';
CREATE INDEX users_owner ON users(owner_id);
`,
	// 17: plugins (M9). Each one is a separate process or service that speaks
	// HTTP; see docs/PLUGINS.md. The key authenticates both directions.
	`
CREATE TABLE plugins (
	id         TEXT PRIMARY KEY,           -- the manifest id, e.g. "watchlist-rss"
	kind       TEXT NOT NULL,              -- 'managed' (BingeBandit runs it) or 'remote' (its own service)
	url        TEXT NOT NULL DEFAULT '',   -- remote: the plugin's base URL
	server_url TEXT NOT NULL DEFAULT '',   -- remote: BingeBandit's address as the plugin reaches it
	enabled    INTEGER NOT NULL DEFAULT 0,
	plugin_key TEXT NOT NULL UNIQUE,
	settings   TEXT NOT NULL DEFAULT '{}', -- JSON object of the admin's values
	manifest   TEXT NOT NULL DEFAULT '{}', -- last manifest the plugin served
	added_at   INTEGER NOT NULL
);
`,
	// 18: the channel logo library (admin uploads; a channel uses one with logo
	// "library:<file>") and viewers' own channel categories.
	`
CREATE TABLE channel_logos (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL,
	file       TEXT NOT NULL UNIQUE,
	created_at INTEGER NOT NULL
);

CREATE TABLE channel_categories (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	position   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE INDEX channel_categories_user ON channel_categories(user_id, position);

CREATE TABLE channel_category_members (
	category_id INTEGER NOT NULL REFERENCES channel_categories(id) ON DELETE CASCADE,
	channel_id  INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	PRIMARY KEY (category_id, channel_id)
);
`,
	// 19: smart collections by genre, studio or year range, alongside labels.
	`
ALTER TABLE collections ADD COLUMN genre_rule TEXT NOT NULL DEFAULT '';
ALTER TABLE collections ADD COLUMN studio_rule TEXT NOT NULL DEFAULT '';
ALTER TABLE collections ADD COLUMN year_from INTEGER NOT NULL DEFAULT 0;
ALTER TABLE collections ADD COLUMN year_to INTEGER NOT NULL DEFAULT 0;
`,
	// 20: DVR series rules can keep only the newest N recordings (0 = all).
	`
ALTER TABLE recording_rules ADD COLUMN keep_last INTEGER NOT NULL DEFAULT 0;
`,
	// 21: IMDb, Rotten Tomatoes and Metacritic scores for movies and shows,
	// from MDBList or OMDb. A row with no scores records a lookup that found
	// nothing, so it isn't retried every run.
	`
CREATE TABLE external_ratings (
	item_id     INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
	imdb_id     TEXT NOT NULL DEFAULT '',
	imdb        REAL NOT NULL DEFAULT 0,
	imdb_votes  INTEGER NOT NULL DEFAULT 0,
	rt          INTEGER NOT NULL DEFAULT 0,
	rt_audience INTEGER NOT NULL DEFAULT 0,
	metacritic  INTEGER NOT NULL DEFAULT 0,
	fetched_at  INTEGER NOT NULL
);
`,
	// 22: people recommend movies and shows to each other. Only users who
	// opted in (recommendation_optins) can receive them.
	`
CREATE TABLE recommendation_optins (
	user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE
);
CREATE TABLE recommendations (
	from_user  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	to_user    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	item_id    INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	note       TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	dismissed  INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (from_user, to_user, item_id)
);
CREATE INDEX recommendations_to ON recommendations(to_user, dismissed, created_at);
`,
	// 23: passkeys (WebAuthn). credential is the library's JSON for the
	// credential (public key, sign count, flags); user handles are the user id.
	`
CREATE TABLE IF NOT EXISTS passkeys (
	id            INTEGER PRIMARY KEY,
	user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	credential_id BLOB NOT NULL UNIQUE,
	credential    TEXT NOT NULL,
	name          TEXT NOT NULL,
	created_at    INTEGER NOT NULL,
	last_used_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS passkeys_user ON passkeys(user_id);
`,
	// 24: a show can take its metadata and episode numbering from TheTVDB
	// (tvdb_id set) in aired, DVD or absolute order. Libraries can turn off
	// intro and credits detection.
	`
ALTER TABLE items ADD COLUMN tvdb_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE items ADD COLUMN tvdb_order TEXT NOT NULL DEFAULT '';
ALTER TABLE libraries ADD COLUMN skip_markers INTEGER NOT NULL DEFAULT 0;
`,
	// 25: notifications in the app (new episodes of shows you follow,
	// finished recordings). link is an app path such as /item/12.
	`
CREATE TABLE notifications (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind       TEXT NOT NULL,
	title      TEXT NOT NULL,
	body       TEXT NOT NULL DEFAULT '',
	link       TEXT NOT NULL DEFAULT '',
	image      TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	read_at    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX notifications_user ON notifications(user_id, created_at);
`,
	// 26: Discover: YouTube playlists and channels an admin picked, and their
	// newest videos.
	`
CREATE TABLE discover_sources (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL,
	kind       TEXT NOT NULL, -- playlist | channel
	youtube_id TEXT NOT NULL UNIQUE,
	position   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	fetched_at INTEGER NOT NULL DEFAULT 0,
	error      TEXT NOT NULL DEFAULT ''
);
CREATE TABLE discover_videos (
	source_id    INTEGER NOT NULL REFERENCES discover_sources(id) ON DELETE CASCADE,
	video_id     TEXT NOT NULL,
	title        TEXT NOT NULL,
	description  TEXT NOT NULL DEFAULT '',
	published_at INTEGER NOT NULL,
	PRIMARY KEY (source_id, video_id)
);
`,
	// 27: profile pictures, and the member an account opens as without asking
	// who's watching (0 asks).
	`
ALTER TABLE users ADD COLUMN avatar TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN auto_profile_id INTEGER NOT NULL DEFAULT 0;
`,
	// 28: which libraries each user pinned, and in what order (Plex's pinned
	// sidebar). A library without a row is pinned, so new libraries show up;
	// pins_chosen is set once the user has been asked.
	`
CREATE TABLE library_pins (
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
	pinned     INTEGER NOT NULL DEFAULT 1,
	position   INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (user_id, library_id)
);
ALTER TABLE users ADD COLUMN pins_chosen INTEGER NOT NULL DEFAULT 0;
`,
	// 29: an admin can turn Live TV (and DVR) off for a user.
	`ALTER TABLE users ADD COLUMN live_tv INTEGER NOT NULL DEFAULT 1;`,
	// 30: each library's rows (built-in ones like Recently Added, and its
	// collections), their order, and where each shows: the library's
	// Recommended tab, Home, and Home for people outside the admin's household
	// (Plex's "Manage Recommendations"). Rows without one use the defaults.
	`
CREATE TABLE library_rows (
	library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
	row_key    TEXT NOT NULL, -- built-in name, or collection:<id>
	position   INTEGER NOT NULL,
	on_library INTEGER NOT NULL,
	on_home    INTEGER NOT NULL,
	on_friends INTEGER NOT NULL,
	PRIMARY KEY (library_id, row_key)
);
`,
	// 31: how a library's Library tab shows collections (Plex's setting):
	// show (collections and their items), hide (items only) or collapse
	// (collections instead of the items in them).
	`ALTER TABLE libraries ADD COLUMN collection_mode TEXT NOT NULL DEFAULT 'show';`,
	// 32: shows found in more than one folder ("The Office (US)" and
	// "The Office (2005)", or the same show on two drives) are merged into one
	// once they match the same TMDB or TVDB entry. The folder's match key
	// then points at the show that was kept, so later scans add to it.
	`
CREATE TABLE show_aliases (
	library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
	match_key  TEXT NOT NULL,
	show_id    INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	PRIMARY KEY (library_id, match_key)
);
`,
	// 33: a collection's own Collection Mode (Plex's Advanced tab), which
	// overrides the library's: '' (library default), show, hide or collapse.
	`ALTER TABLE collections ADD COLUMN mode TEXT NOT NULL DEFAULT '';`,
	// 34: collections kept in step with an MDBList list or an RSS feed
	// (Kometa's list builders). Members the list added are marked, so a sync
	// replaces them and leaves hand-picked ones alone.
	`
ALTER TABLE collections ADD COLUMN list_url TEXT NOT NULL DEFAULT '';
ALTER TABLE collections ADD COLUMN list_synced_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE collections ADD COLUMN list_error TEXT NOT NULL DEFAULT '';
ALTER TABLE collection_items ADD COLUMN from_list INTEGER NOT NULL DEFAULT 0;
`,
	// 35: matching across sources. Movies and shows keep their IMDb id (and
	// shows their TheTVDB id) from whichever source matched them, so titles
	// that share any of them are merged. metadata_source says where a title's
	// details come from: "" (TMDB, or TheTVDB when TMDB has no match), "tvdb"
	// (chosen in Fix Match) or "none" (left unmatched on purpose). Merged
	// folders of movies now get aliases too.
	`
ALTER TABLE items ADD COLUMN imdb_id TEXT NOT NULL DEFAULT '';
ALTER TABLE items ADD COLUMN metadata_source TEXT NOT NULL DEFAULT '';
UPDATE items SET metadata_source = 'tvdb' WHERE kind = 'show' AND tvdb_id > 0;
UPDATE items SET imdb_id = COALESCE((SELECT r.imdb_id FROM external_ratings r WHERE r.item_id = items.id), '')
	WHERE kind IN ('movie', 'show');
CREATE INDEX items_imdb ON items(library_id, imdb_id) WHERE imdb_id != '';
ALTER TABLE show_aliases RENAME TO item_aliases;
ALTER TABLE item_aliases RENAME COLUMN show_id TO item_id;
`,
	// 36: how a collection's titles are ordered (Plex's Collection Sort:
	// '' release date, release_desc, title, added or rating), and the
	// collection's own sort title, which orders it among the library's
	// collections (Plex's "+_1A…" trick to put one first).
	`
ALTER TABLE collections ADD COLUMN sort TEXT NOT NULL DEFAULT '';
ALTER TABLE collections ADD COLUMN sort_title TEXT NOT NULL DEFAULT '';
`,
	// 37: playlists shared with other people, who can play them but not
	// change them.
	`
CREATE TABLE playlist_shares (
	playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
	user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	PRIMARY KEY (playlist_id, user_id)
);
CREATE INDEX playlist_shares_user ON playlist_shares(user_id);
`,
	// 38: poster overlays and Kometa-style default collections. Overlays an
	// admin uploads (their own badges and frames) are drawn on posters when
	// they're served, with the built-in ones (resolution, HDR, audio,
	// ratings); the stored artwork is never changed. builtin marks the
	// collections BingeBandit made itself (studio:<name>, decade:1980,
	// genre:<name>, award:<id>), so turning a group off removes only those.
	`
CREATE TABLE poster_overlays (
	id         INTEGER PRIMARY KEY,
	name       TEXT NOT NULL,
	image      TEXT NOT NULL,                  -- local:/<file>
	position   TEXT NOT NULL DEFAULT 'top-left',
	size       INTEGER NOT NULL DEFAULT 25,    -- width, % of the poster's
	margin     INTEGER NOT NULL DEFAULT 3,     -- % of the poster's width
	condition  TEXT NOT NULL DEFAULT 'always', -- see overlay.Conditions
	kind       TEXT NOT NULL DEFAULT '',       -- '' (both), movie or show
	library_id INTEGER NOT NULL DEFAULT 0,     -- 0 = every library
	enabled    INTEGER NOT NULL DEFAULT 1,
	sort       INTEGER NOT NULL DEFAULT 0,
	updated_at INTEGER NOT NULL
);
ALTER TABLE collections ADD COLUMN builtin TEXT NOT NULL DEFAULT '';
CREATE INDEX collections_builtin ON collections(library_id, builtin) WHERE builtin != '';
`,
	// 39: the sports teams each person follows (copied from ESPN when
	// picked, in their order), and the game alerts already sent so a
	// restart doesn't send them again.
	`
CREATE TABLE sports_teams (
	user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	league    TEXT NOT NULL,
	team_id   TEXT NOT NULL,
	name      TEXT NOT NULL,
	short     TEXT NOT NULL DEFAULT '',
	abbr      TEXT NOT NULL DEFAULT '',
	logo      TEXT NOT NULL DEFAULT '',
	color     TEXT NOT NULL DEFAULT '',
	alt_color TEXT NOT NULL DEFAULT '',
	position  INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (user_id, league, team_id)
);
CREATE TABLE sports_alerts (
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	game_id    TEXT NOT NULL,
	kind       TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (user_id, game_id, kind)
);
`,
	// 40: kid mode for Live TV. A kid only sees the channels an admin
	// approved for kids, one by one (channels.kids) or a whole group at a
	// time (kid_channel_groups, by the channel's group name).
	`
ALTER TABLE users ADD COLUMN kid INTEGER NOT NULL DEFAULT 0;
ALTER TABLE channels ADD COLUMN kids INTEGER NOT NULL DEFAULT 0;
CREATE TABLE kid_channel_groups (grp TEXT PRIMARY KEY);
`,
}
