# TODO

Open issues found while testing the bridge against live accounts.

## Watched status: anime episodes do not sync Nuvio → AIOStreams

- **Seen:** marking an anime episode as watched in AIOStreams records it in
  Nuvio (push works).
- **Broken:** marking the same episode as watched in Nuvio does not reach
  AIOStreams (pull does not import it).
- **Works:** movies sync in both directions.

Likely cause: Nuvio stores the watched row under a `tt...` show id, but the
anime show's AIOStreams metadata uses a different id space (Kitsu/MAL/etc.), so
the imported `tt0903747:1:1` style episode id does not match the show AIOStreams
serves for that title. Needs investigation of how the anime metadata addon keys
the show versus what Nuvio returns.

Investigate:
- [ ] What `content_id` Nuvio returns for the watched anime episode.
- [ ] Which id/type AIOStreams uses for the same anime show in its catalogs.
- [ ] Whether the bridge should map ids, or whether AIOStreams accepts the
      IMDb-keyed video id for a Kitsu-keyed show.

## Deleting a Continue Watching entry does not propagate

- **Nuvio → AIOStreams:** removing a resume point in Nuvio may not remove it in
  AIOStreams. The pull's `items` is not version-gated, but AIOStreams keeps its
  own local progress and only clears imports it made itself. Unverified.
- **AIOStreams → Nuvio:** not possible. The `watch_state` protocol has no event
  for clearing a resume point (only `played`/`unplayed` for watched status).

Not planned unless the first half can be made reliable.

## Pull latency

The pull runs on AIOStreams' schedule (`WATCH_STATE_PULL_INTERVAL`, default
30 min) and on demand after `WATCH_STATE_PULL_TTL` (default 300 s). Not a bug,
but for testing, lower both on the AIOStreams instance.
