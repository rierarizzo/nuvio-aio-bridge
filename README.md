# nuvio-aio-bridge

Keeps [Nuvio](https://nuvio.tv) and [AIOStreams](https://github.com/Viren070/AIOStreams)
in sync in both directions: favorites, watched status and playback progress.

It is a custom AIOStreams addon implementing the `watch_state` resource. It runs
as a single Go binary in a Docker container, with no database and no UI, next to
AIOStreams on the same Docker network.

- **Nuvio → AIOStreams (pull):** Nuvio's library becomes the favorites, and its
  watched history and progress fill the library and Continue Watching.
- **AIOStreams → Nuvio (push):** favorites, watched marks and playback from the
  AIOStreams apps (browser, desktop, Jellyfin clients) are written to Nuvio.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the design and
[docs/nuvio-api.md](docs/nuvio-api.md) for the Nuvio API contract as verified
against a live account.

## Endpoints

All routes live under `/{BRIDGE_TOKEN}`, the addon base URL.

| Route | Caller | Purpose |
| --- | --- | --- |
| `GET /{token}/manifest.json` | AIOStreams | Addon manifest |
| `POST /{token}/watch_state/push/{type}/{id}.json` | AIOStreams | Receives an event |
| `GET /{token}/watch_state/pull.json?since=...` | AIOStreams | Returns Nuvio's state |

## Configuration

| Variable | Required | Purpose |
| --- | --- | --- |
| `NUVIO_EMAIL` | yes | Nuvio account |
| `NUVIO_PASSWORD` | yes | Account password |
| `NUVIO_PROFILE` | yes | Nuvio profile to sync, integer 1-6 |
| `BRIDGE_TOKEN` | yes | Random string used as part of the addon path |
| `NUVIO_API_URL` | no | Defaults to `https://api.nuvio.tv` |
| `NUVIO_ANON_KEY` | no | Defaults to Nuvio's public backend key |
| `PORT` | no | Defaults to `8080` |

Generate the token with `openssl rand -hex 32`.

## Deployment

The bridge joins the Docker network AIOStreams already uses, so AIOStreams
reaches it by container name and no ports are published.

1. On the VPS, find AIOStreams' network name:

   ```sh
   docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{"\n"}}{{end}}' <aiostreams_container>
   ```

2. Create a `.env` next to the compose file (see `.env.example`):

   ```dotenv
   NUVIO_EMAIL=...
   NUVIO_PASSWORD=...
   NUVIO_PROFILE=1
   BRIDGE_TOKEN=...
   AIO_NETWORK=aiostreams_default
   ```

3. Build and start:

   ```sh
   AIO_NETWORK=aiostreams_default docker compose up -d --build
   ```

4. In AIOStreams, add the addon with the internal URL:

   ```text
   http://nuvio-bridge:8080/<BRIDGE_TOKEN>
   ```

   Then enable **Watch State** for it. If AIOStreams rejects private URLs, check
   that `ALLOW_PRIVATE_URLS` is not disabled on the instance.

## Development

```sh
go test ./...
go vet ./...
gofmt -l .
go run ./cmd/bridge
```

`tools/` holds read-only and reversible probes used to verify the Nuvio API
(`nuvio_probe.py`, `nuvio_write_test.py`). They read credentials from `.env`.
