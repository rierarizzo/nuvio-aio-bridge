#!/usr/bin/env python3
"""Wipe a Nuvio profile: watchlist, watched history and playback progress.

It snapshots everything to a JSON file first, so the profile can be restored.
For a one-off destructive action, not part of normal operation.

Usage:
    python3 tools/nuvio_wipe.py --profile 1 --yes
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from typing import Any

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from nuvio_probe import (  # noqa: E402
    DEFAULT_ANON_KEY,
    DEFAULT_API_URL,
    Nuvio,
    load_dotenv,
    read_library,
    read_watched,
)

SNAPSHOT_DIR = "/tmp/opencode/nuvio-snapshots"
PAGE = 500


def chunks(rows: list[Any], size: int) -> list[list[Any]]:
    return [rows[i : i + size] for i in range(0, len(rows), size)]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", default=".env")
    parser.add_argument("--profile", type=int, required=True)
    parser.add_argument("--yes", action="store_true", help="confirm the wipe")
    args = parser.parse_args()

    if not args.yes:
        print("Refusing to wipe without --yes.", file=sys.stderr)
        return 2

    load_dotenv(args.env)
    email = os.environ.get("NUVIO_EMAIL", "").strip()
    password = os.environ.get("NUVIO_PASSWORD", "").strip()
    base_url = os.environ.get("NUVIO_API_URL", DEFAULT_API_URL).strip() or DEFAULT_API_URL
    anon_key = os.environ.get("NUVIO_ANON_KEY", DEFAULT_ANON_KEY).strip() or DEFAULT_ANON_KEY
    if not email or not password:
        print("NUVIO_EMAIL and NUVIO_PASSWORD must be set (see .env.example).", file=sys.stderr)
        return 2

    client = Nuvio(base_url, anon_key)
    status, _ = client.sign_in(email, password)
    if status != 200:
        print(f"sign-in failed: HTTP {status}", file=sys.stderr)
        return 1
    pid = args.profile

    library = read_library(client, pid)
    watched = read_watched(client, pid)
    status, progress = client.rpc(
        "sync_pull_watch_progress", {"p_profile_id": pid, "p_limit": 1000}
    )
    progress = progress if isinstance(progress, list) else []

    os.makedirs(SNAPSHOT_DIR, exist_ok=True)
    snapshot_path = os.path.join(SNAPSHOT_DIR, f"profile{pid}-wipe-{int(time.time())}.json")
    with open(snapshot_path, "w", encoding="utf-8") as handle:
        json.dump(
            {"profile": pid, "library": library, "watched": watched, "progress": progress},
            handle,
        )
    print(f"snapshot: {snapshot_path}")
    print(f"before: library={len(library)} watched={len(watched)} progress={len(progress)}")

    print("[1] clearing library (watchlist)")
    status, body = client.rpc("sync_push_library", {"p_profile_id": pid, "p_items": []})
    print(f"    sync_push_library -> HTTP {status}")

    print("[2] clearing watched history")
    keys = []
    for row in watched:
        if not isinstance(row, dict) or not row.get("content_id"):
            continue
        key: dict[str, Any] = {"content_id": row["content_id"]}
        if row.get("season") is not None:
            key["season"] = row["season"]
        if row.get("episode") is not None:
            key["episode"] = row["episode"]
        keys.append(key)
    # De-duplicate keys: the API is keyed on content/season/episode.
    seen = set()
    unique_keys = []
    for key in keys:
        token = (key["content_id"], key.get("season"), key.get("episode"))
        if token in seen:
            continue
        seen.add(token)
        unique_keys.append(key)
    print(f"    watched keys: {len(unique_keys)} (from {len(watched)} rows)")
    for batch in chunks(unique_keys, PAGE):
        status, body = client.rpc(
            "sync_delete_watched_items", {"p_profile_id": pid, "p_keys": batch}
        )
        print(f"    sync_delete_watched_items batch={len(batch)} -> HTTP {status}")

    print("[3] clearing playback progress")
    progress_keys = []
    for row in progress:
        if not isinstance(row, dict):
            continue
        key = str(row.get("progress_key") or "").strip()
        if key:
            progress_keys.append(key)
    print(f"    progress keys: {len(progress_keys)}")
    for key in progress_keys:
        status, body = client.rpc(
            "sync_delete_watch_progress", {"p_profile_id": pid, "p_progress_key": key}
        )
        if status not in (200, 201, 204):
            print(f"    WARN {key} -> HTTP {status} {body}")

    print()
    print("[verify]")
    after_library = read_library(client, pid)
    after_watched = read_watched(client, pid)
    status, after_progress = client.rpc(
        "sync_pull_watch_progress", {"p_profile_id": pid, "p_limit": 1000}
    )
    after_progress = after_progress if isinstance(after_progress, list) else []
    print(
        f"after: library={len(after_library)} watched={len(after_watched)} "
        f"progress={len(after_progress)}"
    )
    print(f"restore from: {snapshot_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
