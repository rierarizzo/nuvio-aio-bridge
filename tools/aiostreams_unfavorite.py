#!/usr/bin/env python3
"""Remove every favourite from an AIOStreams media server.

Uses the Jellyfin-compatible API that AIOStreams exposes. Unlike the history
endpoint, favourites are reachable with an API key.

Fill an env file (default `.env.aiostreams`) with:

    AIO_URL=https://aiostreams.example.com
    AIO_API_KEY=...
    AIO_USER_ID=...           # optional; discovered from the API if omitted

Usage:
    python3 tools/aiostreams_unfavorite.py            # dry run, lists favourites
    python3 tools/aiostreams_unfavorite.py --yes      # remove them all
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request

USER_AGENT = (
    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)


def load_env(path: str) -> None:
    if not os.path.isfile(path):
        return
    with open(path, "r", encoding="utf-8") as handle:
        for line in handle:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, _, value = line.partition("=")
            key = key.strip()
            value = value.strip().strip('"').strip("'")
            if key and key not in os.environ:
                os.environ[key] = value


def request(method: str, url: str, key: str, payload: dict | None = None) -> tuple[int, object]:
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(url, data=data, method=method.upper())
    req.add_header("X-Emby-Token", key)
    req.add_header("Accept", "application/json")
    req.add_header("User-Agent", USER_AGENT)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            body = resp.read()
            return resp.status, (json.loads(body) if body else None)
    except urllib.error.HTTPError as exc:
        body = exc.read()
        try:
            return exc.code, json.loads(body) if body else None
        except ValueError:
            return exc.code, body.decode("utf-8", "replace")
    except urllib.error.URLError as exc:
        print(f"network error: {exc.reason}", file=sys.stderr)
        return 0, None


def discover_user(url: str, key: str) -> str | None:
    status, users = request("GET", f"{url}/jellyfin/Users", key)
    if status == 200 and isinstance(users, list) and users:
        return str(users[0].get("Id") or "")
    return None


def list_favourites(url: str, key: str, user_id: str) -> list[dict]:
    endpoint = (
        f"{url}/jellyfin/Users/{user_id}/Items"
        "?Filters=IsFavorite&Recursive=true&IncludeItemTypes=Movie,Series&Limit=10000"
    )
    status, data = request("GET", endpoint, key)
    if status != 200 or not isinstance(data, dict):
        print(f"failed to list favourites: HTTP {status} {data}", file=sys.stderr)
        return []
    return list(data.get("Items") or [])


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", default=".env.aiostreams")
    parser.add_argument("--yes", action="store_true", help="actually remove")
    args = parser.parse_args()

    load_env(args.env)
    url = (os.environ.get("AIO_URL") or "").rstrip("/")
    key = os.environ.get("AIO_API_KEY") or ""
    user_id = (os.environ.get("AIO_USER_ID") or "").strip()
    if not url or not key:
        print("AIO_URL and AIO_API_KEY must be set (see the module docstring).", file=sys.stderr)
        return 2
    if not user_id:
        user_id = discover_user(url, key) or ""
    if not user_id:
        print("could not determine the user id; set AIO_USER_ID.", file=sys.stderr)
        return 2

    favs = list_favourites(url, key, user_id)
    print(f"favourites: {len(favs)} (user {user_id})")
    for item in favs[:10]:
        print(f"  {item.get('Id')} | {item.get('Type')} | {item.get('Name')}")
    if len(favs) > 10:
        print(f"  ... and {len(favs) - 10} more")

    if not args.yes:
        print("dry run; pass --yes to remove them all.")
        return 0

    removed = 0
    for item in favs:
        item_id = item.get("Id")
        if not item_id:
            continue
        status, _ = request(
            "POST", f"{url}/jellyfin/UserFavoriteItems/{item_id}/delete", key, {}
        )
        if status in (200, 204):
            removed += 1
        else:
            print(f"  failed {item_id}: HTTP {status}", file=sys.stderr)
    print(f"removed: {removed}/{len(favs)}")

    remaining = list_favourites(url, key, user_id)
    print(f"favourites after: {len(remaining)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
