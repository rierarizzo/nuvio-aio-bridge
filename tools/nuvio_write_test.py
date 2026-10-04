#!/usr/bin/env python3
"""Controlled, reversible write test for the Nuvio API.

It snapshots the whole profile first (library, watched, progress) to a JSON file
under /tmp, then:

  1. adds one movie to the library (favorites) and removes it again
  2. marks one movie watched and clears it again
  3. sets playback progress for one movie and removes it again

After every write it reads back and asserts the expected change. In a `finally`
block it pushes the original library snapshot back and deletes the test watched
and progress entries, so the profile ends where it started.

Credentials come from the local `.env` (same as `nuvio_probe.py`). Nothing
sensitive is printed.

Usage:
    python3 tools/nuvio_write_test.py --profile 1
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
    redact,
)

SNAPSHOT_DIR = "/tmp/opencode/nuvio-snapshots"

# Titles the test may borrow. The first candidate not already present is used.
CANDIDATES = [
    ("tt0137523", "movie", "Fight Club"),
    ("tt0108778", "series", "Friends"),
    ("tt0111161", "movie", "The Shawshank Redemption"),
]
LIBRARY_FIELDS = (
    "content_id",
    "content_type",
    "name",
    "poster",
    "poster_shape",
    "background",
    "description",
    "release_info",
    "imdb_rating",
    "genres",
    "addon_base_url",
    "added_at",
)


def now_ms() -> int:
    return int(time.time() * 1000)


def library_payload(rows: list[Any]) -> list[dict[str, Any]]:
    """Keep only the fields Scrob sends; drop server-managed fields."""
    out: list[dict[str, Any]] = []
    for row in rows:
        if not isinstance(row, dict):
            continue
        item = {k: row[k] for k in LIBRARY_FIELDS if row.get(k) not in (None, "", [])}
        if item.get("content_id"):
            out.append(item)
    return out


def find_in_library(rows: list[Any], content_id: str) -> dict[str, Any] | None:
    for row in rows:
        if isinstance(row, dict) and str(row.get("content_id")) == content_id:
            return row
    return None


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", default=".env", help="path to .env file")
    parser.add_argument("--profile", type=int, required=True)
    args = parser.parse_args()

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

    original_library = read_library(client, pid)
    original_watched = read_watched(client, pid)
    status, original_progress = client.rpc(
        "sync_pull_watch_progress", {"p_profile_id": pid, "p_limit": 1000}
    )
    original_progress = original_progress if isinstance(original_progress, list) else []

    os.makedirs(SNAPSHOT_DIR, exist_ok=True)
    snapshot_path = os.path.join(SNAPSHOT_DIR, f"profile{pid}-{now_ms()}.json")
    with open(snapshot_path, "w", encoding="utf-8") as handle:
        json.dump(
            {
                "profile": pid,
                "library": original_library,
                "watched": original_watched,
                "progress": original_progress,
            },
            handle,
        )
    print(f"snapshot written: {snapshot_path}")
    print(
        f"baseline: library={len(original_library)} watched={len(original_watched)} "
        f"progress={len(original_progress)}"
    )

    non_null_logo = sum(1 for r in original_library if isinstance(r, dict) and r.get("logo"))
    non_null_addon = sum(
        1 for r in original_library if isinstance(r, dict) and r.get("addon_base_url")
    )
    print(f"library rows with logo={non_null_logo}, addon_base_url={non_null_addon}")

    present_ids = {
        str(r.get("content_id"))
        for r in original_library
        if isinstance(r, dict)
    }
    watched_content_ids = {
        str(r.get("content_id"))
        for r in original_watched
        if isinstance(r, dict)
    }
    candidate = next(
        (c for c in CANDIDATES if c[0] not in present_ids and c[0] not in watched_content_ids),
        None,
    )
    if candidate is None:
        print("no unused candidate id available", file=sys.stderr)
        return 1
    test_id, test_type, test_name = candidate
    print(f"test title: {test_id} ({test_name})")
    print(f"already in library: {'yes' if test_id in present_ids else 'no'}")
    print(f"already watched: {'yes' if test_id in watched_content_ids else 'no'}")

    results: list[tuple[str, bool, str]] = []

    def record(name: str, ok: bool, detail: str) -> None:
        results.append((name, ok, detail))
        print(f"  [{'ok' if ok else 'FAIL'}] {name}: {detail}")

    try:
        print()
        print("[A] library add + remove")
        merged = library_payload(original_library)
        item = {
            "content_id": test_id,
            "content_type": test_type,
            "name": test_name,
            "added_at": now_ms(),
        }
        status, body = client.rpc(
            "sync_push_library", {"p_profile_id": pid, "p_items": merged + [item]}
        )
        record("push add", status in (200, 201, 204), f"HTTP {status} {json.dumps(redact(body))[:200]}")
        after_add = read_library(client, pid)
        added = find_in_library(after_add, test_id)
        record(
            "verify add",
            added is not None and len(after_add) == len(original_library) + 1,
            f"count={len(after_add)} present={added is not None}",
        )

        status, body = client.rpc(
            "sync_push_library", {"p_profile_id": pid, "p_items": library_payload(original_library)}
        )
        record("push restore", status in (200, 201, 204), f"HTTP {status} {json.dumps(redact(body))[:200]}")
        after_restore = read_library(client, pid)
        record(
            "verify restore",
            find_in_library(after_restore, test_id) is None
            and len(after_restore) == len(original_library),
            f"count={len(after_restore)}",
        )

        print()
        print("[B] watched add + delete")
        status, body = client.rpc(
            "sync_push_watched_items",
            {
                "p_profile_id": pid,
                "p_items": [
                    {
                        "content_id": test_id,
                        "content_type": test_type,
                        "title": test_name,
                        "watched_at": now_ms(),
                    }
                ],
            },
        )
        record("push watched", status in (200, 201, 204), f"HTTP {status} {json.dumps(redact(body))[:200]}")
        after_watched = read_watched(client, pid)
        present = any(
            isinstance(r, dict) and str(r.get("content_id")) == test_id for r in after_watched
        )
        record("verify watched add", present, f"count={len(after_watched)} present={present}")

        status, body = client.rpc(
            "sync_delete_watched_items",
            {"p_profile_id": pid, "p_keys": [{"content_id": test_id}]},
        )
        record("delete watched", status in (200, 201, 204), f"HTTP {status} {json.dumps(redact(body))[:200]}")
        after_delete = read_watched(client, pid)
        gone = not any(
            isinstance(r, dict) and str(r.get("content_id")) == test_id for r in after_delete
        )
        record("verify watched delete", gone, f"count={len(after_delete)} absent={gone}")

        print()
        print("[C] progress upsert + delete")
        status, body = client.rpc(
            "sync_push_watch_progress",
            {
                "p_profile_id": pid,
                "p_entries": [
                    {
                        "content_id": test_id,
                        "content_type": test_type,
                        "video_id": test_id,
                        "position": 120000,
                        "duration": 600000,
                        "last_watched": now_ms(),
                    }
                ],
            },
        )
        record("push progress", status in (200, 201, 204), f"HTTP {status} {json.dumps(redact(body))[:200]}")
        status, after_progress = client.rpc(
            "sync_pull_watch_progress", {"p_profile_id": pid, "p_limit": 1000}
        )
        after_progress = after_progress if isinstance(after_progress, list) else []
        present = any(
            isinstance(r, dict) and str(r.get("content_id")) == test_id for r in after_progress
        )
        record("verify progress set", present, f"count={len(after_progress)} present={present}")

        progress_key = test_id
        status, body = client.rpc(
            "sync_delete_watch_progress",
            {"p_profile_id": pid, "p_progress_key": progress_key},
        )
        record("delete progress (p_progress_key)", status in (200, 201, 204), f"HTTP {status} {json.dumps(redact(body))[:200]}")
        if status not in (200, 201, 204):
            status, body = client.rpc(
                "sync_delete_watch_progress",
                {"p_profile_id": pid, "p_keys": [progress_key]},
            )
            record("delete progress (p_keys)", status in (200, 201, 204), f"HTTP {status} {json.dumps(redact(body))[:200]}")
        status, after_progress = client.rpc(
            "sync_pull_watch_progress", {"p_profile_id": pid, "p_limit": 1000}
        )
        after_progress = after_progress if isinstance(after_progress, list) else []
        gone = not any(
            isinstance(r, dict) and str(r.get("content_id")) == test_id for r in after_progress
        )
        record("verify progress delete", gone, f"count={len(after_progress)} absent={gone}")

    finally:
        print()
        print("[R] final restore check")
        try:
            client.rpc(
                "sync_push_library",
                {"p_profile_id": pid, "p_items": library_payload(original_library)},
            )
            final_library = read_library(client, pid)
            client.rpc(
                "sync_delete_watched_items",
                {"p_profile_id": pid, "p_keys": [{"content_id": test_id}]},
            )
            final_watched = read_watched(client, pid)
            client.rpc(
                "sync_delete_watch_progress",
                {"p_profile_id": pid, "p_progress_key": test_id},
            )
            print(
                f"final: library={len(final_library)} (baseline {len(original_library)}), "
                f"watched={len(final_watched)} (baseline {len(original_watched)})"
            )
        except Exception as exc:  # noqa: BLE001
            print(f"RESTORE ERROR: {exc!r}", file=sys.stderr)

    print()
    failures = [r for r in results if not r[1]]
    print(f"{len(results) - len(failures)}/{len(results)} checks passed")
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
