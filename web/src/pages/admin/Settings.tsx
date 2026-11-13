import { useEffect, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../../api'
import { useAuth } from '../../auth'
import { Icon } from '../../components/Icon'
import { Modal } from '../../components/Modal'
import { useApi, useDocumentTitle } from '../../hooks'
import type { OIDCProvider, SportsLeague, Webhook } from '../../types'

type Settings = Record<string, string>

const encoders = [
  ['none', 'Software (CPU, libx264)'],
  ['nvenc', 'NVIDIA NVENC'],
  ['qsv', 'Intel Quick Sync'],
  ['vaapi', 'VAAPI (Linux / Docker / Unraid)'],
  ['amf', 'AMD AMF (Windows)'],
  ['videotoolbox', 'Apple VideoToolbox'],
]

const tonemapAlgorithms: [string, string][] = [
  ['hable', 'Hable (default: filmic, keeps highlight detail)'],
  ['mobius', 'Möbius (brighter, closer to the original colours)'],
  ['reinhard', 'Reinhard (simple and soft)'],
  ['clip', 'Clip (accurate midtones, blown-out highlights)'],
  ['linear', 'Linear (darkest, no curve)'],
]

/** The remote quality limit's choices, in kbit/s. */
const remoteQualities: [string, string][] = [
  ['0', 'No limit'],
  ['40000', '40 Mbps (4K)'],
  ['20000', '20 Mbps (4K, lighter)'],
  ['12000', '12 Mbps (1440p)'],
  ['8000', '8 Mbps (1080p)'],
  ['4000', '4 Mbps (720p)'],
  ['2000', '2 Mbps (480p)'],
  ['1000', '1 Mbps (480p, low)'],
]

export default function AdminSettings() {
  useDocumentTitle('Settings')
  const { refresh } = useAuth()
  const { data } = useApi<Settings>('/api/admin/settings')
  const [s, setS] = useState<Settings>({})
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [hwTest, setHwTest] = useState<{ ok?: boolean; text: string } | null>(null)

  const testEncoder = async () => {
    const enc = s.hw_encoder ?? 'none'
    setHwTest({ text: 'Testing…' })
    try {
      const r = await api.post<{ ok: boolean; error?: string }>(`/api/admin/live/test-encoder?encoder=${encodeURIComponent(enc)}`)
      setHwTest(r.ok ? { ok: true, text: 'Works: a 1080i test picture was converted with this encoder.' } : { ok: false, text: r.error ?? 'Failed.' })
    } catch (err) {
      setHwTest({ ok: false, text: (err as Error).message })
    }
  }

  useEffect(() => {
    if (data) setS(data)
  }, [data])

  const set = (k: string) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
    const v = e.target instanceof HTMLInputElement && e.target.type === 'checkbox' ? String(e.target.checked) : e.target.value
    setS({ ...s, [k]: v })
  }

  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      setS(await api.put<Settings>('/api/admin/settings', s))
      setMsg({ ok: true, text: 'Settings saved.' })
      refresh()
    } catch (err) {
      setMsg({ ok: false, text: (err as Error).message })
    }
  }

  if (!data) return <div className="page-loading" />

  return (
    <div className="page narrow">
      <h1>Settings</h1>
      <form className="form" onSubmit={save}>
        <section className="panel form">
          <h3>General</h3>
          <label>
            Server name
            <input value={s.server_name ?? ''} onChange={set('server_name')} />
          </label>
        </section>

        <section className="panel form">
          <h3>Metadata</h3>
          <label>
            TMDB API key or read access token
            <input value={s.tmdb_api_key ?? ''} onChange={set('tmdb_api_key')} placeholder="Paste your key" autoComplete="off" spellCheck={false} />
          </label>
          <p className="muted small">
            Free at{' '}
            <a href="https://www.themoviedb.org/settings/api" target="_blank" rel="noreferrer">
              themoviedb.org/settings/api
            </a>
            . After adding it, use “Refresh metadata” on each library.
          </p>
          <label>
            Metadata language
            <input value={s.metadata_language ?? ''} onChange={set('metadata_language')} placeholder="en-US" />
          </label>
          <label>
            TheTVDB API key (optional)
            <input value={s.tvdb_api_key ?? ''} onChange={set('tvdb_api_key')} placeholder="Paste your key" autoComplete="off" spellCheck={false} />
          </label>
          {s.tvdb_api_key && (
            <label>
              TheTVDB subscriber PIN
              <input value={s.tvdb_pin ?? ''} onChange={set('tvdb_pin')} placeholder="Only for a user-supported key" autoComplete="off" spellCheck={false} />
            </label>
          )}
          <p className="muted small">
            From{' '}
            <a href="https://thetvdb.com/api-information" target="_blank" rel="noreferrer">
              thetvdb.com/api-information
            </a>
            . Lets a show use TheTVDB instead of TMDB (Fix match on the show), for aired, DVD or absolute episode numbering.
          </p>
          <label>
            MDBList API key for list collections (optional)
            <input value={s.mdblist_api_key ?? ''} onChange={set('mdblist_api_key')} placeholder="Paste your key" autoComplete="off" spellCheck={false} />
          </label>
          <p className="muted small">
            From{' '}
            <a href="https://mdblist.com/preferences/" target="_blank" rel="noreferrer">
              mdblist.com/preferences
            </a>
            . Collections can follow an MDBList list (Edit collection → Follow a list). Public lists work without a key; with MDBList as the ratings
            provider its key is used too.
          </p>
          <p className="muted small">
            Collections can also follow TMDB charts (with the TMDB key above), IMDb charts and lists (no key), and Trakt charts and lists (with the
            client ID under Trakt below).
          </p>
          <label>
            Yamtrack server for list collections (optional)
            <input className="mono" value={s.yamtrack_url ?? ''} onChange={set('yamtrack_url')} placeholder="https://yamtrack.example.com" spellCheck={false} />
          </label>
          <div className="field-grid">
            <label>
              Yamtrack username
              <input value={s.yamtrack_username ?? ''} onChange={set('yamtrack_username')} autoComplete="off" spellCheck={false} />
            </label>
            <label>
              Yamtrack password
              <input type="password" value={s.yamtrack_password ?? ''} onChange={set('yamtrack_password')} autoComplete="new-password" />
            </label>
          </div>
          <p className="muted small">
            Lets a collection follow a Yamtrack list (https://yamtrack…/list/…), in the list's own order. The sign-in is only needed for lists
            that aren't public.
          </p>
          <label>
            IMDb and Rotten Tomatoes ratings
            <select value={s.ratings_provider ?? ''} onChange={set('ratings_provider')}>
              <option value="">Off</option>
              <option value="mdblist">MDBList (IMDb, Rotten Tomatoes critics and audience, Metacritic)</option>
              <option value="omdb">OMDb (IMDb, Rotten Tomatoes critics, Metacritic; needs the TMDB key)</option>
            </select>
          </label>
          {s.ratings_provider && (
            <>
              <label>
                {s.ratings_provider === 'omdb' ? 'OMDb' : 'MDBList'} API key
                <input value={s.ratings_api_key ?? ''} onChange={set('ratings_api_key')} placeholder="Paste your key" autoComplete="off" spellCheck={false} />
              </label>
              <p className="muted small">
                Free at{' '}
                {s.ratings_provider === 'omdb' ? (
                  <a href="https://www.omdbapi.com/apikey.aspx" target="_blank" rel="noreferrer">omdbapi.com</a>
                ) : (
                  <a href="https://mdblist.com/preferences/" target="_blank" rel="noreferrer">mdblist.com/preferences</a>
                )}
                {' '}(about 1,000 lookups a day). Scores fill in when a title is opened, and the “Refresh IMDb and Rotten Tomatoes ratings” task
                keeps them current.
              </p>
            </>
          )}
        </section>

        <section className="panel form">
          <h3>Transcoding</h3>
          <label>
            Hardware acceleration
            <select value={s.hw_encoder ?? 'none'} onChange={set('hw_encoder')}>
              {encoders.map(([v, label]) => (
                <option key={v} value={v}>{label}</option>
              ))}
            </select>
          </label>
          <div>
            <button type="button" className="btn btn-glass btn-sm" onClick={testEncoder}>Test encoder</button>
          </div>
          {hwTest && <pre className={`hw-test ${hwTest.ok === false ? 'bad' : hwTest.ok ? 'ok' : ''}`}>{hwTest.text}</pre>}
          {['vaapi', 'qsv', 'nvenc'].includes(s.hw_encoder ?? '') && (
            <label>
              Hardware device (for servers with more than one GPU)
              <input
                className="mono"
                value={s.hw_device ?? ''}
                onChange={set('hw_device')}
                placeholder={s.hw_encoder === 'nvenc' ? 'GPU number, e.g. 1 (blank = first GPU)' : '/dev/dri/renderD129 (blank = renderD128)'}
              />
            </label>
          )}
          {(s.hw_encoder ?? 'none') !== 'none' && (
            <label className="check">
              <input type="checkbox" checked={s.hw_decode === 'true'} onChange={set('hw_decode')} />
              Decode on the GPU too (less CPU when converting; falls back to the CPU for codecs the GPU can't decode)
            </label>
          )}
          <p className="muted small">
            If the hardware encoder fails during Live TV, channels are converted on the CPU for the next 30 minutes and Live TV → Activity says why.
            Channels that are already H.264 are passed through without converting, so they never use the encoder.
          </p>
          <label>
            Temporary transcode folder
            <input value={s.transcode_dir ?? ''} onChange={set('transcode_dir')} placeholder="Default: inside the data folder" className="mono" />
          </label>
          <label className="check">
            <input type="checkbox" checked={s.tonemap_enabled === 'true'} onChange={set('tonemap_enabled')} />
            Tone map HDR to SDR when transcoding
          </label>
          {s.tonemap_enabled === 'true' && (
            <label>
              Tone mapping algorithm
              <select value={s.tonemap_algorithm ?? 'hable'} onChange={set('tonemap_algorithm')}>
                {tonemapAlgorithms.map(([v, label]) => (
                  <option key={v} value={v}>{label}</option>
                ))}
              </select>
            </label>
          )}
          <label className="check">
            <input type="checkbox" checked={s.transcode_throttle === 'true'} onChange={set('transcode_throttle')} />
            Throttle transcodes (stay about 2× ahead of playback instead of converting the whole file at full speed)
          </label>
          <label>
            Videos converted at once
            <input
              type="number"
              min={0}
              value={s.max_video_conversions ?? '0'}
              onChange={set('max_video_conversions')}
              placeholder="0"
            />
          </label>
          <p className="muted small">
            0 picks for you: a quarter of this computer's CPU threads (at least 2) when converting on the CPU, no limit on a graphics card. Past
            the limit, a new viewer who needs a conversion is asked to wait instead of everyone stuttering. Seeking and files played as they
            are don't count.
          </p>
          <label className="check">
            <input type="checkbox" checked={s.adaptive_streaming === 'true'} onChange={set('adaptive_streaming')} />
            Adaptive streaming: when a video has to be converted, make up to three qualities at once so the player can switch on slow
            connections (uses more CPU or GPU per viewer)
          </label>
        </section>

        <section className="panel form">
          <h3>Scrub previews and subtitles</h3>
          <label className="check">
            <input type="checkbox" checked={s.trickplay_enabled !== 'false'} onChange={set('trickplay_enabled')} />
            Show thumbnails while scrubbing through movies and episodes
          </label>
          <p className="muted small">
            The “Make scrub previews” task under <Link to="/admin/tasks">Scheduled Tasks</Link> makes them from each file's keyframes during
            maintenance hours, and a file played before then gets them in the background. They take a few MB per hour of video.
          </p>
          <label>
            OpenSubtitles API key (for “Find subtitles online” in the players)
            <input value={s.opensubtitles_api_key ?? ''} onChange={set('opensubtitles_api_key')} placeholder="Paste your key" autoComplete="off" spellCheck={false} />
          </label>
          <div className="field-grid">
            <label>
              OpenSubtitles username
              <input value={s.opensubtitles_username ?? ''} onChange={set('opensubtitles_username')} autoComplete="off" spellCheck={false} />
            </label>
            <label>
              OpenSubtitles password
              <input type="password" value={s.opensubtitles_password ?? ''} onChange={set('opensubtitles_password')} autoComplete="new-password" />
            </label>
          </div>
          <p className="muted small">
            Make a free consumer key at{' '}
            <a href="https://www.opensubtitles.com/en/consumers" target="_blank" rel="noreferrer">
              opensubtitles.com/consumers
            </a>
            . The account is optional, but OpenSubtitles allows more downloads a day with one. Downloaded subtitles are saved
            beside the video as .srt files, so the media folder must be writable. Kid profiles can't download.
          </p>
          <label>
            Whisper model (for “Make subtitles from the sound”)
            <select
              value={['tiny', 'base', 'small', 'medium', 'large-v3-turbo'].includes(s.whisper_model ?? 'base') ? s.whisper_model ?? 'base' : 'path'}
              onChange={(e) => setS({ ...s, whisper_model: e.target.value === 'path' ? '' : e.target.value })}
            >
              <option value="tiny">Tiny (75 MB, fastest)</option>
              <option value="base">Base (140 MB)</option>
              <option value="small">Small (460 MB)</option>
              <option value="medium">Medium (1.5 GB)</option>
              <option value="large-v3-turbo">Large v3 Turbo (1.6 GB, best)</option>
              <option value="path">A model file on the server…</option>
            </select>
          </label>
          {!['tiny', 'base', 'small', 'medium', 'large-v3-turbo'].includes(s.whisper_model ?? 'base') && (
            <label>
              Model file
              <input value={s.whisper_model ?? ''} onChange={set('whisper_model')} placeholder="/models/ggml-large-v3.bin" spellCheck={false} />
            </label>
          )}
          <p className="muted small">
            Admins can make subtitles from a video's own sound in the players' Find subtitles, with{' '}
            <a href="https://github.com/ggml-org/whisper.cpp" target="_blank" rel="noreferrer">
              whisper.cpp
            </a>{' '}
            on this server (install it so whisper-cli is on the PATH, or set BB_WHISPER). The model downloads once. Bigger models are more
            accurate and slower; on a CPU, Base takes a few minutes for a film.
          </p>
        </section>

        <section className="panel form">
          <h3>Library</h3>
          <label className="check">
            <input type="checkbox" checked={s.watch_folders === 'true'} onChange={set('watch_folders')} />
            Watch library folders and add new files automatically
          </label>
          <p className="muted small">
            Full rescans, backups and cleanup are under <Link to="/admin/tasks">Scheduled Tasks</Link>. Some network shares don't report changes;
            the scheduled scan still finds those.
          </p>
          <label className="check">
            <input type="checkbox" checked={s.allow_downloads === 'true'} onChange={set('allow_downloads')} />
            Let users download files (original files on the web, offline downloads in the phone app)
          </label>
          <label className="check">
            <input type="checkbox" checked={s.recommendations_enabled !== 'false'} onChange={set('recommendations_enabled')} />
            Let people recommend movies and shows to each other
          </label>
          <p className="muted small">Off hides the Recommend buttons, the opt-in under Account and the “Recommended for You” row on Home, on the web and in the TV app.</p>
          <label className="check">
            <input type="checkbox" checked={s.because_rows_enabled !== 'false'} onChange={set('because_rows_enabled')} />
            Show “Because you watched” and “Because you liked” rows on Home
          </label>
          <p className="muted small">
            Picked from TMDB’s recommendations for what each person finished or gave a thumbs up lately, keeping only what the server has. Titles
            someone marked “Not interested” stay out of them.
          </p>
          <label className="check">
            <input type="checkbox" checked={s.requests_enabled !== 'false'} onChange={set('requests_enabled')} />
            Let people request movies and shows the server doesn’t have
          </label>
          <p className="muted small">
            Requests wait for an admin on the <Link to="/requests">Requests</Link> page (or are approved at once for people you pick under
            People & limits there). Approved ones go to Sonarr and Radarr, or Cinemask, through their plugin.
          </p>
          {s.requests_enabled !== 'false' && (
            <>
              <label className="check">
                <input type="checkbox" checked={s.request_kids === 'true'} onChange={set('request_kids')} />
                Let kid profiles request too (a grown-up in their household says yes first)
              </label>
              <label className="check">
                <input type="checkbox" checked={s.request_4k === 'true'} onChange={set('request_4k')} />
                Let people ask for the 4K copy
              </label>
              <p className="muted small">4K requests need a 4K Radarr/Sonarr, or a 4K quality profile in Cinemask, set in the plugin’s settings.</p>
              <label>
                Movie requests per person per week (0 is no limit)
                <input type="number" min={0} max={1000} value={s.request_movie_limit ?? '0'} onChange={set('request_movie_limit')} />
              </label>
              <label>
                Show requests per person per week (0 is no limit)
                <input type="number" min={0} max={1000} value={s.request_show_limit ?? '0'} onChange={set('request_show_limit')} />
              </label>
              <label>
                List requested titles nobody watched after (days; 0 is off)
                <input type="number" min={0} max={3650} value={s.request_cleanup_days ?? '0'} onChange={set('request_cleanup_days')} />
              </label>
              <p className="muted small">
                Limits can be changed per person on the Requests page. Titles from people’s Trakt watchlists don’t count. Never watched lists
                requested titles none of the people who asked have played, so you can free up space.
              </p>
            </>
          )}
          <label>
            Count as watched after (% of the runtime)
            <input type="number" min={50} max={100} value={s.watched_percent ?? '90'} onChange={set('watched_percent')} />
          </label>
          <p className="muted small">
            Once someone plays this far, a movie or episode is marked watched and leaves Continue Watching. Skipping the credits counts too.
          </p>
          <label>
            Keep titles in Continue Watching for (days)
            <input type="number" min={0} max={3650} value={s.continue_watching_days ?? '0'} onChange={set('continue_watching_days')} />
          </label>
          <p className="muted small">
            Movies and shows nobody has played for this long drop out of Continue Watching and On Deck. 0 keeps them until they're finished.
          </p>
          <label>
            Recently Added goes back (days)
            <input type="number" min={0} max={3650} value={s.recently_added_days ?? '100'} onChange={set('recently_added_days')} />
          </label>
          <p className="muted small">
            Recently Added rows on Home and in each library only show titles added this many days ago or later. 0 shows everything, newest first.
          </p>
          <label>
            Intro videos before movies
            <textarea
              className="mono"
              rows={2}
              value={s.preroll_paths ?? ''}
              onChange={(e) => setS({ ...s, preroll_paths: e.target.value })}
              placeholder="Files or folders, one per line (e.g. /media/prerolls)"
              spellCheck={false}
            />
          </label>
          <p className="muted small">
            One is picked at random each time, like Plex's movie pre-roll. Each person turns it off, or adds trailers, in their playback
            settings. Use MP4 (H.264 and AAC) so every browser can play it.
          </p>
        </section>

        <section className="panel form">
          <h3>Live TV playback</h3>
          <label>
            Pause and rewind window (minutes)
            <input type="number" min={0} max={240} value={s.live_timeshift_minutes ?? '15'} onChange={set('live_timeshift_minutes')} />
          </label>
          <p className="muted small">
            How far back viewers can pause or rewind a channel. Each channel being watched keeps this much video in the transcode folder
            (about 40 MB per minute for HD), so keep it modest if that folder is in RAM. 0 keeps only a short buffer.
          </p>
          <div className="field-grid">
            <label>
              Buffer (seconds behind live)
              <input type="number" min={2} max={60} value={s.live_buffer_seconds ?? '10'} onChange={set('live_buffer_seconds')} />
            </label>
            <label>
              Switch streams after buffering for (seconds)
              <input type="number" min={3} max={120} value={s.live_stall_seconds ?? '12'} onChange={set('live_stall_seconds')} />
            </label>
          </div>
          <label className="check">
            <input type="checkbox" checked={s.live_switch_slow !== 'false'} onChange={set('live_switch_slow')} />
            Also switch when a stream falls behind (not only when it stops)
          </label>
          <p className="muted small">
            A bigger buffer rides out provider hiccups but starts each channel further behind live. A channel with fallback streams
            moves to the next one when its stream sends nothing for the switch time. With the box ticked, it also moves when the
            stream falls so far behind real time that viewers would have been buffering that long, but it won't go back to a stream
            it left for that within 10 minutes. Fallback streams are often a few seconds apart, so each switch can repeat or skip a
            moment; untick it if switches happen more than the buffering did.
          </p>
          <label>
            Proxy for IPTV providers
            <input className="mono" value={s.live_proxy ?? ''} onChange={set('live_proxy')} placeholder="None (http://host:port or socks5://user:pass@host:port)" autoComplete="off" spellCheck={false} />
          </label>
          <p className="muted small">
            Streams, playlists, guides and logos are fetched through this, for example a VPN container. Leave it empty to connect
            directly (or use the container's HTTP_PROXY). HLS provider streams can only use an http:// proxy.
          </p>
        </section>

        <section className="panel form">
          <h3>Live TV recording (DVR)</h3>
          <label className="check">
            <input type="checkbox" checked={s.dvr_allow_users === 'true'} onChange={set('dvr_allow_users')} />
            Let users (not just admins) record
          </label>
          <label className="check">
            <input type="checkbox" checked={s.dvr_require_approval === 'true'} onChange={set('dvr_require_approval')} />
            Ask an admin to approve users' recordings first
          </label>
          <label className="check">
            <input type="checkbox" checked={s.dvr_priority === 'true'} onChange={set('dvr_priority')} />
            Recordings come first: when a provider is at its connection limit, stop a live channel someone is watching to record
          </label>
          <label className="check">
            <input type="checkbox" checked={s.dvr_detect_ads !== 'false'} onChange={set('dvr_detect_ads')} />
            Find the ad breaks in recordings, for a Skip Ad button
          </label>
          <p className="muted small">
            Uses Comskip when it's installed on the server, otherwise the black, silent moments broadcasters cut to ads on. Runs as the “Find ad breaks
            in recordings” task, hourly. Each person chooses under Account whether ads show a button or are skipped by themselves.
          </p>
          <label>
            Recordings folder
            <input className="mono" value={s.dvr_folder ?? ''} onChange={set('dvr_folder')} placeholder="Default: recordings inside the data folder" />
          </label>
          <label>
            Save recordings
            <select value={s.dvr_naming ?? 'library'} onChange={set('dvr_naming')}>
              <option value="library">Named like a TV library (Show/Season 01/Show - S01E02.ts)</option>
              <option value="folder">All in one folder (Show - date.ts)</option>
            </select>
          </label>
          <div className="field-grid">
            <label>
              Start early (minutes)
              <input type="number" min={0} max={60} value={s.dvr_padding_before ?? '1'} onChange={set('dvr_padding_before')} />
            </label>
            <label>
              Keep going after (minutes)
              <input type="number" min={0} max={120} value={s.dvr_padding_after ?? '3'} onChange={set('dvr_padding_after')} />
            </label>
          </div>
        </section>

        <section className="panel form">
          <h3>Trakt</h3>
          <p className="muted small">
            Lets people sync their watch history with Trakt from their Account page, and collections follow Trakt charts and lists (that only needs the
            client ID). Create an app at{' '}
            <a href="https://trakt.tv/oauth/applications/new" target="_blank" rel="noreferrer">trakt.tv/oauth/applications</a> with the redirect URI{' '}
            <code>urn:ietf:wg:oauth:2.0:oob</code>, then paste its keys here.
          </p>
          <div className="field-grid">
            <label>
              Client ID
              <input className="mono" value={s.trakt_client_id ?? ''} onChange={set('trakt_client_id')} autoComplete="off" spellCheck={false} />
            </label>
            <label>
              Client secret
              <input className="mono" type="password" value={s.trakt_client_secret ?? ''} onChange={set('trakt_client_secret')} autoComplete="new-password" />
            </label>
          </div>
        </section>

        <section className="panel form">
          <h3>Network</h3>
          <label>
            Public URL
            <input className="mono" value={s.public_url ?? ''} onChange={set('public_url')} placeholder="https://tv.example.com (optional)" spellCheck={false} />
            <span className="muted small">How people reach this server from outside. Used for single sign-on redirects. Leave empty to use whatever address the browser used.</span>
          </label>
          <label className="check">
            <input type="checkbox" checked={s.remote_access !== 'false'} onChange={set('remote_access')} />
            Allow connections from outside the home network
          </label>
          {s.remote_access !== 'false' && (
            <div className="field-grid">
              <label>
                Streams at once from outside, per account
                <input type="number" min={0} max={50} value={s.remote_max_streams ?? '0'} onChange={set('remote_max_streams')} />
                <span className="muted small">0 = no limit. An account and its profiles share it, live TV included. Admin accounts have no limit.</span>
              </label>
              <label>
                Quality limit outside the home network
                <select value={s.remote_max_kbps ?? '0'} onChange={set('remote_max_kbps')}>
                  {remoteQualities.map(([v, label]) => (
                    <option key={v} value={v}>{label}</option>
                  ))}
                  {!remoteQualities.some(([v]) => v === (s.remote_max_kbps ?? '0')) && <option value={s.remote_max_kbps}>{s.remote_max_kbps} kbps</option>}
                </select>
                <span className="muted small">Videos above it are converted down for viewers away from home. Live TV isn't limited.</span>
              </label>
            </div>
          )}
          <RemoteTunnel s={s} set={set} />
          <label>
            Extra local networks
            <input className="mono" value={s.lan_networks ?? ''} onChange={set('lan_networks')} placeholder="e.g. 100.64.0.0/10 for Tailscale" spellCheck={false} />
            <span className="muted small">Private ranges (192.168.x, 10.x, 172.16–31.x) always count as local. Add others, comma-separated.</span>
          </label>
          <label>
            Trusted reverse proxies
            <input className="mono" value={s.trusted_proxies ?? ''} onChange={set('trusted_proxies')} placeholder="e.g. 172.17.0.1 or 10.0.0.0/8" spellCheck={false} />
            <span className="muted small">
              If you use Nginx, Caddy, Traefik or a Cloudflare tunnel, list its address here so BingeBandit sees the real client address and https.
              Only list proxies you run: their X-Forwarded-For headers are believed.
            </span>
          </label>
        </section>

        <section className="panel form">
          <h3>Casting</h3>
          <p className="muted small">
            People can play titles on Chromecasts, Google TVs and DLNA smart TVs from the web and phone apps. The server finds them on its own network
            and sends them the video. In Docker, that needs host networking; otherwise list the TVs here and set the address they reach the server at.
          </p>
          <label>
            Address TVs reach this server at
            <input className="mono" value={s.cast_url ?? ''} onChange={set('cast_url')} placeholder="e.g. http://192.168.1.10:8484 (optional)" spellCheck={false} />
            <span className="muted small">Leave empty to use this machine's own address on the TV's network.</span>
          </label>
          <label>
            TVs to look for by address
            <textarea className="mono" rows={3} value={s.cast_devices ?? ''} onChange={(e) => setS({ ...s, cast_devices: e.target.value })} placeholder={'192.168.1.50\n192.168.1.51'} spellCheck={false} />
            <span className="muted small">One IP address per line, for TVs the server can't find by itself. Giving TVs fixed addresses in your router helps.</span>
          </label>
        </section>

        <section className="panel form">
          <h3>Sports data</h3>
          <p className="muted small">
            The Sports pages read scores, schedules and box scores from ESPN. If you run CFB Tracker, point this at it and both apps share one copy of
            ESPN's data instead of each asking ESPN. If the tracker stops answering, the server reads ESPN directly until it's back.
          </p>
          <label>
            CFB Tracker address
            <input className="mono" value={s.sports_relay_url ?? ''} onChange={set('sports_relay_url')} placeholder="e.g. http://192.168.1.10:3000 (optional)" spellCheck={false} />
            <span className="muted small">Leave empty to read ESPN directly.</span>
          </label>
          <label>
            CFB Tracker key
            <input type="password" className="mono" value={s.sports_relay_key ?? ''} onChange={set('sports_relay_key')} autoComplete="new-password" spellCheck={false} />
            <span className="muted small">
              The tracker's RELAY_API_KEY (its relay stays off until that's set). The tracker can also use it to ask which channel shows a network, so its game
              pages link straight to your channel (“Watch on BingeBandit”).
            </span>
          </label>
          <SportsLeagues value={s.sports_hidden_leagues ?? ''} onChange={(v) => setS({ ...s, sports_hidden_leagues: v })} />
        </section>

        <AdminAlerts s={s} set={set} />

        {msg && <div className={msg.ok ? 'form-ok' : 'form-error'}>{msg.text}</div>}
        <div className="actions">
          <button className="btn btn-primary">Save settings</button>
        </div>
      </form>
      <SingleSignOn />
      <Webhooks />
    </div>
  )
}

const alertKinds: [string, string][] = [
  ['alert_updates', 'A new BingeBandit version is out'],
  ['alert_backups', 'A database backup failed'],
  ['alert_drives', 'A library or storage folder goes offline (or comes back)'],
  ['alert_disk_space', 'A drive is nearly full'],
  ['alert_recordings', 'A recording failed'],
]

const channelLabels: Record<string, string> = { email: 'Email', discord: 'Discord', ntfy: 'ntfy' }

/** Where admin alerts go besides admins' notifications, and which ones are sent. Saved with the rest of the form. */
function AdminAlerts({ s, set }: { s: Settings; set: (k: string) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => void }) {
  const [test, setTest] = useState<{ ok?: boolean; text: string } | null>(null)

  const sendTest = async () => {
    setTest({ text: 'Sending…' })
    const keys = ['alert_email_to', 'alert_smtp_host', 'alert_smtp_port', 'alert_smtp_user', 'alert_smtp_password', 'alert_smtp_from', 'alert_discord_url', 'alert_ntfy_url', 'alert_ntfy_token']
    try {
      const r = await api.post<{ results: Record<string, string> }>('/api/admin/alerts/test', Object.fromEntries(keys.map((k) => [k, s[k] ?? ''])))
      const entries = Object.entries(r.results)
      setTest({
        ok: entries.every(([, v]) => v === 'sent'),
        text: entries.map(([ch, v]) => `${channelLabels[ch] ?? ch}: ${v === 'sent' ? 'sent' : v}`).join(' · '),
      })
    } catch (err) {
      setTest({ ok: false, text: (err as Error).message })
    }
  }

  return (
    <section className="panel form">
      <h3>Admin alerts</h3>
      <p className="muted small">
        Admins always see these in their notifications. Fill in any of email, Discord or ntfy to also get them on your phone. The{' '}
        <Link to="/admin/health">Health</Link> page shows drives, free space and the alerts sent lately.
      </p>
      {alertKinds.map(([key, label]) => (
        <label key={key} className="check">
          <input type="checkbox" checked={s[key] !== 'false'} onChange={set(key)} />
          {label}
        </label>
      ))}
      <div className="field-grid">
        <label>
          Nearly full below (% free)
          <input type="number" min={1} max={50} value={s.disk_low_percent ?? '5'} onChange={set('disk_low_percent')} />
        </label>
        <label className="check" style={{ alignSelf: 'end' }}>
          <input type="checkbox" checked={s.update_check !== 'false'} onChange={set('update_check')} />
          Check GitHub for new versions daily
        </label>
      </div>

      <h4>Discord</h4>
      <label>
        Channel webhook URL
        <input className="mono" value={s.alert_discord_url ?? ''} onChange={set('alert_discord_url')} placeholder="https://discord.com/api/webhooks/…" autoComplete="off" spellCheck={false} />
        <span className="muted small">In Discord: channel settings → Integrations → Webhooks → New Webhook → Copy Webhook URL.</span>
      </label>

      <h4>ntfy</h4>
      <div className="field-grid">
        <label>
          Topic URL
          <input className="mono" value={s.alert_ntfy_url ?? ''} onChange={set('alert_ntfy_url')} placeholder="https://ntfy.sh/my-bingebandit" autoComplete="off" spellCheck={false} />
        </label>
        <label>
          Access token or user:password (optional)
          <input className="mono" type="password" value={s.alert_ntfy_token ?? ''} onChange={set('alert_ntfy_token')} autoComplete="new-password" />
        </label>
      </div>
      <p className="muted small">Subscribe to the same topic in the ntfy app. On ntfy.sh, pick a topic name nobody would guess.</p>

      <h4>Email</h4>
      <label>
        Send to
        <input value={s.alert_email_to ?? ''} onChange={set('alert_email_to')} placeholder="me@example.com, other@example.com" autoComplete="off" spellCheck={false} />
      </label>
      <div className="field-grid">
        <label>
          SMTP server
          <input className="mono" value={s.alert_smtp_host ?? ''} onChange={set('alert_smtp_host')} placeholder="smtp.gmail.com" autoComplete="off" spellCheck={false} />
        </label>
        <label>
          Port
          <input type="number" min={1} max={65535} value={s.alert_smtp_port ?? '587'} onChange={set('alert_smtp_port')} />
        </label>
        <label>
          Username
          <input value={s.alert_smtp_user ?? ''} onChange={set('alert_smtp_user')} autoComplete="off" spellCheck={false} />
        </label>
        <label>
          Password
          <input type="password" value={s.alert_smtp_password ?? ''} onChange={set('alert_smtp_password')} autoComplete="new-password" />
        </label>
      </div>
      <label>
        From address
        <input value={s.alert_smtp_from ?? ''} onChange={set('alert_smtp_from')} placeholder="Blank = the username" autoComplete="off" spellCheck={false} />
      </label>
      <p className="muted small">Port 587 uses STARTTLS, 465 uses TLS. For Gmail, use an app password.</p>

      <div className="actions">
        <button type="button" className="btn btn-glass btn-sm" onClick={sendTest}>Send a test alert</button>
        {test && <span className={test.ok === undefined ? 'muted small' : test.ok ? 'form-ok' : 'form-error'}>{test.text}</span>}
      </div>
    </section>
  )
}

/** OpenID Connect providers people can sign in with. */
function SingleSignOn() {
  const { data, reload } = useApi<{ providers: OIDCProvider[]; redirectUrl: string }>('/api/admin/oidc')
  const [editing, setEditing] = useState<Partial<OIDCProvider> | null>(null)
  const remove = async (p: OIDCProvider) => {
    if (!confirm(`Remove ${p.name}? Accounts linked to it are unlinked, and people whose account was made by ${p.name} can only sign in again with a password an admin sets.`)) return
    await api.del(`/api/admin/oidc/${p.id}`).catch((e) => alert(e.message))
    reload()
  }
  return (
    <section className="panel" style={{ marginTop: 20 }}>
      <header className="panel-head">
        <h3>Single sign-on</h3>
        <button className="btn btn-glass btn-sm" onClick={() => setEditing({ enabled: true, autoCreate: false, scopes: '' })}>
          <Icon name="plus" size={16} /> Add provider
        </button>
      </header>
      <p className="muted small">
        Let people sign in with Google, TinyAuth, Authelia, Authentik, Keycloak or any OpenID Connect provider. Register BingeBandit with the
        provider using this redirect URL:
      </p>
      {data && <code className="totp-secret">{data.redirectUrl}</code>}
      <div className="list">
        {data?.providers.map((p) => (
          <div key={p.id} className="list-row">
            <div className="grow">
              <div>
                {p.name} {!p.enabled && <span className="pill">Off</span>}
              </div>
              <div className="muted small mono" style={{ wordBreak: 'break-all' }}>{p.issuer}</div>
              <div className="muted small">{p.autoCreate ? 'Creates accounts for new people' : 'Only for people who linked it under Account'}</div>
            </div>
            <button className="btn btn-glass btn-sm" onClick={() => setEditing(p)}>Edit</button>
            <button className="btn btn-glass btn-sm danger" onClick={() => remove(p)} aria-label={`Remove ${p.name}`}>
              <Icon name="trash" size={16} />
            </button>
          </div>
        ))}
      </div>
      {editing && (
        <ProviderEditor
          provider={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
    </section>
  )
}

function ProviderEditor({ provider, onClose, onSaved }: { provider: Partial<OIDCProvider>; onClose: () => void; onSaved: () => void }) {
  const [p, setP] = useState<Partial<OIDCProvider>>({ ...provider, clientSecret: '' })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const save = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      if (p.id) await api.put(`/api/admin/oidc/${p.id}`, p)
      else await api.post('/api/admin/oidc', p)
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const presetGoogle = () => setP({ ...p, name: p.name || 'Google', issuer: 'https://accounts.google.com' })
  return (
    <Modal title={p.id ? `Edit ${provider.name}` : 'Add sign-in provider'} onClose={onClose}>
      <form className="form" onSubmit={save}>
        <label>
          Name on the sign-in button
          <input value={p.name ?? ''} onChange={(e) => setP({ ...p, name: e.target.value })} required autoFocus placeholder="Google" />
        </label>
        <label>
          <span className="field-head">
            Issuer URL
            {!p.id && (
              <button type="button" className="link-btn small" onClick={presetGoogle}>Use Google</button>
            )}
          </span>
          <input className="mono" value={p.issuer ?? ''} onChange={(e) => setP({ ...p, issuer: e.target.value })} required placeholder="https://auth.example.com" spellCheck={false} />
        </label>
        <label>
          Client ID
          <input className="mono" value={p.clientId ?? ''} onChange={(e) => setP({ ...p, clientId: e.target.value })} required spellCheck={false} autoComplete="off" />
        </label>
        <label>
          Client secret
          <input
            className="mono"
            type="password"
            value={p.clientSecret ?? ''}
            onChange={(e) => setP({ ...p, clientSecret: e.target.value })}
            placeholder={p.hasSecret ? 'Saved; leave empty to keep it' : ''}
            autoComplete="new-password"
          />
        </label>
        <label>
          Extra scopes (optional)
          <input className="mono" value={p.scopes ?? ''} onChange={(e) => setP({ ...p, scopes: e.target.value })} placeholder="openid, profile and email are always asked for" spellCheck={false} />
        </label>
        <label className="check">
          <input type="checkbox" checked={p.autoCreate ?? false} onChange={(e) => setP({ ...p, autoCreate: e.target.checked })} />
          Create an account for anyone who signs in with it
        </label>
        <p className="muted small" style={{ marginTop: -6 }}>
          Only turn this on for a provider that just lets in people you trust (your own TinyAuth or Authelia). With Google it would let anyone with a Google account in.
          New accounts can see all libraries; adjust them under Users.
        </p>
        <label className="check">
          <input type="checkbox" checked={p.enabled ?? true} onChange={(e) => setP({ ...p, enabled: e.target.checked })} />
          Show on the sign-in page
        </label>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={busy}>{busy ? 'Checking…' : 'Save'}</button>
        </div>
      </form>
    </Modal>
  )
}

const eventLabels: Record<string, string> = {
  'playback.start': 'Playback started',
  'playback.stop': 'Playback stopped',
  'playback.finished': 'Finished watching',
  'player.play': 'Player opened (at once)',
  'player.pause': 'Paused',
  'player.resume': 'Resumed',
  'player.stop': 'Player closed',
  'recording.started': 'Recording started',
  'recording.completed': 'Recording completed',
  'recording.failed': 'Recording failed',
}

/** Webhooks POST signed JSON to other services (Home Assistant, Discord bots, n8n…) when things happen. */
function Webhooks() {
  const { data, reload } = useApi<{ webhooks: Webhook[]; events: string[] }>('/api/admin/webhooks')
  const [editing, setEditing] = useState<Partial<Webhook> | null>(null)
  const [results, setResults] = useState<Record<number, string>>({})

  const test = async (h: Webhook) => {
    setResults((r) => ({ ...r, [h.id]: 'Sending…' }))
    try {
      const res = await api.post<{ status: string }>(`/api/admin/webhooks/${h.id}/test`)
      setResults((r) => ({ ...r, [h.id]: res.status }))
    } catch (err) {
      setResults((r) => ({ ...r, [h.id]: (err as Error).message }))
    }
    reload()
  }
  const remove = async (h: Webhook) => {
    if (!confirm(`Delete the webhook "${h.name}"?`)) return
    await api.del(`/api/admin/webhooks/${h.id}`).catch((e) => alert(e.message))
    reload()
  }
  const toggle = async (h: Webhook, enabled: boolean) => {
    await api.put(`/api/admin/webhooks/${h.id}`, { ...h, enabled }).catch((e) => alert(e.message))
    reload()
  }

  return (
    <section className="panel" style={{ marginTop: 20 }}>
      <header className="panel-head">
        <h3>Webhooks</h3>
        <button className="btn btn-glass btn-sm" onClick={() => setEditing({ enabled: true, events: [] })}>
          <Icon name="plus" size={16} /> Add webhook
        </button>
      </header>
      <p className="muted small">
        BingeBandit sends a JSON POST to each URL when a chosen event happens. With a secret set, the body is signed in the{' '}
        <code>X-BingeBandit-Signature</code> header (HMAC-SHA256).
      </p>
      <div className="list">
        {data?.webhooks.map((h) => (
          <div key={h.id} className="list-row">
            <label className="switch" title={h.enabled ? 'On' : 'Off'}>
              <input type="checkbox" checked={h.enabled} onChange={(e) => toggle(h, e.target.checked)} aria-label={`${h.name} enabled`} />
              <span />
            </label>
            <div className="grow">
              <div>{h.name}</div>
              <div className="muted small mono" style={{ wordBreak: 'break-all' }}>{h.url}</div>
              <div className="muted small">
                {h.format === 'plex' && 'Plex-style · '}
                {h.events.length ? h.events.map((e) => eventLabels[e] ?? e).join(', ') : 'No events chosen'}
                {(results[h.id] || h.lastStatus) && <> · last response: {results[h.id] || h.lastStatus}</>}
              </div>
            </div>
            <button className="btn btn-glass btn-sm" onClick={() => test(h)}>Send test</button>
            <button className="btn btn-glass btn-sm" onClick={() => setEditing(h)}>Edit</button>
            <button className="btn btn-glass btn-sm danger" onClick={() => remove(h)} aria-label={`Delete ${h.name}`}>
              <Icon name="trash" size={16} />
            </button>
          </div>
        ))}
      </div>
      {editing && data && (
        <WebhookEditor
          hook={editing}
          events={data.events}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
    </section>
  )
}

function WebhookEditor({ hook, events, onClose, onSaved }: { hook: Partial<Webhook>; events: string[]; onClose: () => void; onSaved: () => void }) {
  const [h, setH] = useState<Partial<Webhook>>(hook)
  const [error, setError] = useState('')
  const toggle = (e: string) => {
    const cur = new Set(h.events ?? [])
    if (cur.has(e)) cur.delete(e)
    else cur.add(e)
    setH({ ...h, events: events.filter((x) => cur.has(x)) })
  }
  const save = async (e: FormEvent) => {
    e.preventDefault()
    try {
      if (h.id) await api.put(`/api/admin/webhooks/${h.id}`, h)
      else await api.post('/api/admin/webhooks', h)
      onSaved()
    } catch (err) {
      setError((err as Error).message)
    }
  }
  return (
    <Modal title={h.id ? 'Edit webhook' : 'Add webhook'} onClose={onClose}>
      <form className="form" onSubmit={save}>
        <label>
          Name
          <input value={h.name ?? ''} onChange={(e) => setH({ ...h, name: e.target.value })} required autoFocus placeholder="Home Assistant" />
        </label>
        <label>
          URL
          <input className="mono" type="url" value={h.url ?? ''} onChange={(e) => setH({ ...h, url: e.target.value })} required placeholder="https://…" />
        </label>
        <label>
          Format
          <select value={h.format ?? ''} onChange={(e) => setH({ ...h, format: e.target.value })}>
            <option value="">BingeBandit JSON</option>
            <option value="plex">Plex-style (for apps that accept Plex webhooks, like Yamtrack's Plex URL; playback events only)</option>
          </select>
          {h.format === 'plex' && /\/webhook\/bingebandit\//i.test(h.url ?? '') && (
            <span className="muted small">This is a BingeBandit webhook URL, which expects BingeBandit JSON. Plex-style will be refused.</span>
          )}
        </label>
        <label>
          Secret (optional, for signing)
          <input className="mono" value={h.secret ?? ''} onChange={(e) => setH({ ...h, secret: e.target.value })} autoComplete="off" />
        </label>
        <fieldset className="webhook-events">
          {events.map((ev) => (
            <label key={ev} className="check">
              <input type="checkbox" checked={h.events?.includes(ev) ?? false} onChange={() => toggle(ev)} />
              {eventLabels[ev] ?? ev}
            </label>
          ))}
        </fieldset>
        {error && <div className="form-error">{error}</div>}
        <div className="actions end">
          <button type="button" className="btn btn-glass" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary">Save</button>
        </div>
      </form>
    </Modal>
  )
}

/**
 * Which leagues the Sports section shows on every TV, Roku and the web.
 * Unticked leagues are left out for everyone; each profile can still turn
 * off or reorder the rest (Sports → Rows on the web, the hub's gear on a TV).
 */
function SportsLeagues({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { data: leagues } = useApi<SportsLeague[]>('/api/sports/leagues')
  if (!leagues) return null
  const hidden = new Set(value.split(',').map((x) => x.trim()).filter(Boolean))
  const toggle = (id: string, show: boolean) => {
    if (show) hidden.delete(id)
    else hidden.add(id)
    onChange(leagues.filter((l) => hidden.has(l.id)).map((l) => l.id).join(','))
  }
  return (
    <fieldset className="sports-league-picks">
      <legend>Leagues in the Sports section</legend>
      <span className="muted small">
        Untick a league to take it out of Sports for everyone: its row, its scores and following its teams. Each profile can turn off or reorder the
        rest for itself.
      </span>
      <div className="sports-league-grid">
        {leagues.map((l) => (
          <label key={l.id} className="check">
            <input type="checkbox" checked={!hidden.has(l.id)} onChange={(e) => toggle(l.id, e.target.checked)} /> {l.name}
          </label>
        ))}
      </div>
    </fieldset>
  )
}

interface TunnelStatus {
  mode?: string
  state: 'off' | 'starting' | 'running' | 'failed'
  url?: string
  error?: string
  port?: number
}

/** Remote access without opening a port on the router: a Cloudflare tunnel
 *  (a quick one at a random address, or the admin's own) or Tailscale Funnel. */
function RemoteTunnel({ s, set }: { s: Settings; set: (k: string) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => void }) {
  const { data: status, reload } = useApi<TunnelStatus>('/api/admin/tunnel')
  // Starting takes a few seconds: look again until it settles.
  useEffect(() => {
    if (status?.state !== 'starting' && (status?.mode ?? '') === (s.remote_tunnel ?? '')) return
    const t = window.setTimeout(reload, 2000)
    return () => window.clearTimeout(t)
  }, [status, s.remote_tunnel, reload])
  const mode = s.remote_tunnel ?? ''
  return (
    <div className="form">
      <label>
        Remote access without port forwarding
        <select value={mode} onChange={set('remote_tunnel')}>
          <option value="">Off (use port forwarding, a reverse proxy or a VPN)</option>
          <option value="cloudflare-quick">Cloudflare quick tunnel (no account; the address changes on each restart)</option>
          <option value="cloudflare">My Cloudflare tunnel (a fixed address on my domain)</option>
          <option value="tailscale">Tailscale Funnel (a fixed *.ts.net address)</option>
        </select>
        <span className="muted small">
          The server connects out to Cloudflare or Tailscale, which hand it visitors over HTTPS, so nothing on your router changes. People who come in this
          way always count as outside the home network. cloudflared is downloaded the first time if it isn't installed.
        </span>
      </label>
      {mode === 'cloudflare' && (
        <div className="field-grid">
          <label>
            Tunnel token
            <input className="mono" type="password" value={s.remote_tunnel_token ?? ''} onChange={set('remote_tunnel_token')} autoComplete="new-password" spellCheck={false} />
            <span className="muted small">In Cloudflare: Zero Trust → Networks → Tunnels → Create a tunnel → copy the token from the install command.</span>
          </label>
          <label>
            Public hostname
            <input className="mono" value={s.remote_tunnel_hostname ?? ''} onChange={set('remote_tunnel_hostname')} placeholder="tv.example.com" spellCheck={false} />
            <span className="muted small">
              Add it under the tunnel's Public hostnames with the service <code>http://localhost:{s.remote_tunnel_port || '8485'}</code>.
            </span>
          </label>
        </div>
      )}
      {mode === 'tailscale' && (
        <p className="muted small">
          Install Tailscale on this machine and sign in, then allow Funnel for it in the tailnet's access controls (the Tailscale admin page offers a button).
        </p>
      )}
      {mode !== '' && (
        <label>
          Port the tunnel reaches the server on (on this machine only)
          <input type="number" min={1024} max={65535} value={s.remote_tunnel_port ?? '8485'} onChange={set('remote_tunnel_port')} />
        </label>
      )}
      {status && status.state !== 'off' && (
        <p className={status.state === 'failed' ? 'form-error' : 'muted small'}>
          {status.state === 'running' && status.url ? (
            <>
              Running at{' '}
              <a href={status.url} target="_blank" rel="noreferrer" className="mono">{status.url}</a>
              {!s.public_url && '. Until you set a Public URL above, apps and links use this address.'}
            </>
          ) : status.state === 'running' ? (
            'Running.'
          ) : status.state === 'starting' ? (
            'Starting…'
          ) : (
            `Not working: ${status.error ?? 'unknown error'}. Trying again shortly.`
          )}
        </p>
      )}
    </div>
  )
}
