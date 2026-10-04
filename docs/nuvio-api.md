# Nuvio API notes

Working notes for the bridge. Source: reading the reference adapters in
[Scrob](https://github.com/ellite/scrob) (`backend/core/nuvio.py`) and
[CrossWatch](https://github.com/cenodude/CrossWatch)
(`providers/auth/_auth_NUVIO.py`, `providers/sync/nuvio/*`). Nothing here is
copied code; it is a description of the observed HTTP contract.

Status legend: **observed in code** = both references agree or one is explicit;
**verified live** = confirmed against the account with `tools/nuvio_probe.py`;
**to verify** = still unconfirmed.

## Verified read path (live, profile_index 1)

Probe run on 2026-10-03 with `tools/nuvio_probe.py --profile 1`:

- Sign-in returns `200` with `expires_in: 604800` (7 days).
- `sync_pull_profiles` returns 3 profiles. The identifier is **`profile_index`**
  (1 = Keneth, 2 = Camila, 3 = Guest); `p_profile_id` consumes that value.
  CrossWatch's `profile_id` is an internal rename.
- `sync_pull_library`: **1023 rows** (982 movie, 41 series). It returns at most
  500 per page, so pagination by `p_offset` is required. `content_id` is a bare
  IMDb id (`tt...`).
- `sync_pull_watched_items`: **1899 rows** (181 movie, 1711 series, 7 sport).
  It returns at most the page size, so pagination is required. Two series row
  shapes coexist: a show-level row (`season` and `episode` null) and
  episode-level rows. `sport` rows use ids like `streamed:norway-vs-...`.
- `sync_pull_watch_progress`: 29 rows. **Signature A works**
  (`p_profile_id`, `p_limit`); no `p_offset`. Several rows have
  `position == duration`, i.e. finished but still listed.

Implications for the bridge:

- Always page library and watched to exhaustion; never trust a single call.
- Skip any `content_type` other than `movie` and `series` (the account has
  `sport`), since AIOStreams only handles movies and series.
- Decide how to treat show-level series watched rows: they carry no episode, so
  they cannot become `watched.episodes` entries.
- Treat `position == duration` (or above a threshold) as finished, not as an
  in-progress item, when building the pull's `items`.

## Transport

| Item | Value | Status |
| --- | --- | --- |
| Base URL | `https://api.nuvio.tv` (self-hosted: your Supabase project URL) | verified live |
| Backend model | Supabase (auth at `/auth/v1`, data at `/rest/v1`) | verified live |
| Public anon key | `sb_publishable_1Clq8rlTVACkdcZuqr6_AD__xUUC_EN` (default, overridable) | verified live |
| Headers | `apikey: <anon key>`, `Content-Type: application/json`, `Accept: application/json` | verified live |
| Auth header | `Authorization: Bearer <access_token>` on data calls | verified live |
| Profiles | integer `profile_index`, `1`-`6` | verified live |

## Auth

Sign in (password grant):

```http
POST {base}/auth/v1/token?grant_type=password
{ "email": "...", "password": "..." }
-> { "access_token", "refresh_token", "expires_in" }
```

Refresh:

```http
POST {base}/auth/v1/token?grant_type=refresh_token
{ "refresh_token": "..." }
-> { "access_token", "refresh_token", "expires_in" }
```

**The refresh token rotates on every use and is single-use.** The bridge holds
the current refresh token and the access token in memory only, and refreshes
before it expires. A stale token is rejected with
`refresh_token_already_used`. Status **observed in code** (Scrob documents this
explicitly).

## RPC calls

All data calls are `POST {base}/rest/v1/rpc/{function}` with the bearer token.

| Function | Payload | Purpose | Status |
| --- | --- | --- | --- |
| `sync_pull_profiles` | `{}` | List profiles | verified live |
| `sync_pull_library` | `{p_profile_id, p_limit, p_offset}` | Read library (watchlist) | verified live |
| `sync_push_library` | `{p_profile_id, p_items}` | Replace library snapshot with a merged list | to verify (write) |
| `sync_pull_watched_items` | `{p_profile_id, p_page, p_page_size}` | Read watched history | verified live |
| `sync_push_watched_items` | `{p_profile_id, p_items}` | Add/update watched entries | to verify (write) |
| `sync_delete_watched_items` | `{p_profile_id, p_keys}` | Remove watched entries | to verify (write) |
| `sync_pull_watch_progress` | `{p_profile_id, p_limit}` | Read in-progress items (signature A works) | verified live |
| `sync_push_watch_progress` | `{p_profile_id, p_entries}` | Upsert playback progress | to verify (write) |
| `sync_delete_watch_progress` | `{p_profile_id, p_keys}` / `{p_profile_id, p_progress_key}` | Remove progress | to verify (write) |

Scrob passes only `p_profile_id` and `p_limit` to `sync_pull_watch_progress`
and notes that adding `p_offset` makes PostgREST 404. CrossWatch passes
`p_since_last_watched` too. The probe tries both signatures.

## Payload shapes

### Library item (`sync_pull_library` / `sync_push_library`)

```jsonc
{
  "content_id":   "tt0137523" | "tmdb:550",   // IMDb bare id or tmdb:<id>
  "content_type": "movie" | "series",
  "name":         "...",
  "poster":       "https://...",              // optional
  "poster_shape": "poster",                   // optional
  "background":   "https://...",              // optional
  "description":  "...",                      // optional
  "release_info": "1999",                     // optional, year
  "imdb_rating":  8.4,                         // optional
  "genres":       ["Drama"],                   // optional
  "addon_base_url": "...",                     // set by Nuvio, preserve on write
  "added_at":     1784419200000                 // epoch ms
}
```

A write is a **full snapshot replace**: read the current library, merge local
changes over it, and push the whole list. This is the only way to remove an
item. Status **observed in code** (both references merge client-side).

### Watched item (`sync_pull_watched_items` / `sync_push_watched_items`)

Movie:

```json
{ "content_id": "tt0137523", "content_type": "movie",
  "title": "Fight Club", "watched_at": 1785000000000 }
```

Episode:

```json
{ "content_id": "tt0903747", "content_type": "series",
  "title": "Breaking Bad - S01E01", "season": 1, "episode": 1,
  "watched_at": 1785000000000 }
```

`content_id` is the **show/movie** id, never the episode id; season/episode are
separate fields. Delete uses keys `{content_id, season, episode}` (season and
episode omitted for a movie). Status **observed in code**.

### Progress item (`sync_pull_watch_progress` / `sync_push_watch_progress`)

```jsonc
{ "content_id": "tt0903747", "content_type": "series",
  "video_id": "tt0903747:1:1", "season": 1, "episode": 1,
  "position": 1800000,      // ms
  "duration": 7200000,      // ms
  "last_watched": 1785000000000 }
```

Movies use `content_type: "movie"` with `video_id` equal to `content_id` and no
season/episode. `position`/`duration` are milliseconds; `last_watched` is epoch
milliseconds. Status **observed in code**.

## IDs

- `content_id` is either a bare IMDb id (`tt...`) or `tmdb:<number>`.
- AIOStreams uses IMDb `tt...`, so the bridge should write bare IMDb ids where
  Nuvio accepts them (Scrob does exactly this).
- CrossWatch prefers `tmdb:` for matching, which is a CrossWatch internal
  choice, not a Nuvio requirement.
- A title without an IMDb id is skipped rather than guessed.
- Status **partly to verify**: confirm the live library returns `tt...`,
  `tmdb:...`, or both for the same title.

## Verified write path (live, reversible, profile 1)

`tools/nuvio_write_test.py --profile 1` snapshotted the profile, ran add/remove
for each kind, and restored it. 12/12 checks passed; the profile ended with the
same row counts it started with (library 1023, watched 1899, progress 29).

| Operation | Call | Result |
| --- | --- | --- |
| Add favorite | `sync_push_library` with the merged list | `204`, present on read |
| Remove favorite | `sync_push_library` without it | `204`, absent on read |
| Mark watched | `sync_push_watched_items` | `204`, present on read |
| Clear watched | `sync_delete_watched_items` keys `[{content_id}]` | `204`, absent on read |
| Set progress | `sync_push_watch_progress` entries `[{content_id, content_type, video_id, position, duration, last_watched}]` | `204`, present on read |
| Clear progress | `sync_delete_watch_progress` **`{p_progress_key: <key>}`** | `204`, absent on read |

Notes:
- `sync_push_library` is a full snapshot replace. Send back only the fields
  Scrob's `_LIBRARY_PUSH_FIELDS` keeps (`content_id`, `content_type`, `name`,
  `poster`, `poster_shape`, `background`, `description`, `release_info`,
  `imdb_rating`, `genres`, `addon_base_url`, `added_at`); the server fills
  `id`, `profile_id`, `user_id`, `created_at`, `updated_at`.
- A library item pushed with only `content_id`/`content_type`/`name`/`added_at`
  is accepted. Image and metadata fields are optional.
- `logo` exists on 29 rows but is not part of the fields Scrob sends; leave it
  out to avoid overwriting it.
- The progress delete key is the value from the read's `progress_key` field
  (`tt...` for a movie, `tt..._s<a>e<b>` for an episode).

## Progress upsert requirement (important)

A `sync_push_watch_progress` call returns `204` but only persists when the
series show / movie already exists in the profile (in the library or in watched
history). For content Nuvio does not know, it is silently dropped:

| Target | In profile? | Result |
| --- | --- | --- |
| `tt27486290` episode | yes (in watched) | persisted |
| `tt0903747` episode | no | dropped |
| `tt0137523` movie | no | dropped |
| `tmdb:550` movie | no | dropped |

So a `start`/`pause`/`stop` for a title the account has never seen will not
create a resume point. In practice the bridge pushes progress for titles the
user is playing, which are usually already known. If a first play of an unknown
title must keep a resume point, the bridge would have to add the title to the
library or watched history first; that is not done today.

`sync_push_watched_items` and `sync_push_library` accept bare `tt...` ids and do
not have this constraint.

## Verified / open

- [x] Exact profile field name: `profile_index`; `p_profile_id` takes its value.
- [x] `sync_pull_watch_progress` signature: A (`p_profile_id`, `p_limit`).
- [x] Real library item field set and id format (bare IMDb `tt...`).
- [x] Real watched/progress row shapes, including show-level series rows and `sport`.
- [x] Write behaviour for library, watched and progress (reversible test passed).
- [ ] Decide how the bridge handles `sport` and show-level series rows.
- [ ] Confirm large-write behaviour (1k+ item library snapshot) stays in limits.
