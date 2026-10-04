#!/usr/bin/env python3
"""Read-only probe for the Nuvio cloud API.

Purpose: validate the endpoints and payload shapes the bridge will rely on,
before any of them is written into the Go implementation. It signs in with the
account from the local environment, lists profiles, and reads the library,
watched items and playback progress for one profile.

It never prints passwords or tokens: anything whose key looks sensitive is
redacted. It performs no writes unless `--writes` is passed explicitly.

Usage:
    cp .env.example .env   # then fill in NUVIO_EMAIL / NUVIO_PASSWORD
    python3 tools/nuvio_probe.py
    python3 tools/nuvio_probe.py --profile 2
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from typing import Any

DEFAULT_API_URL = "https://api.nuvio.tv"
DEFAULT_ANON_KEY = "sb_publishable_1Clq8rlTVACkdcZuqr6_AD__xUUC_EN"
USER_AGENT = "nuvio-aio-bridge-probe/0.1"

SENSITIVE = re.compile(r"(?i)(token|password|secret|apikey|anon_key)")


def load_dotenv(path: str) -> None:
    """Load KEY=VALUE pairs into os.environ without overriding real env vars."""
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


def redact(value: Any, key: str | None = None) -> Any:
    if key and SENSITIVE.search(key):
        return "***redacted***"
    if isinstance(value, dict):
        return {k: redact(v, k) for k, v in value.items()}
    if isinstance(value, list):
        return [redact(item, key) for item in value]
    if isinstance(value, str) and re.fullmatch(r"[\w.+-]+@[\w.-]+", value):
        local, _, domain = value.partition("@")
        return f"{local[:1]}***@{domain}"
    return value


def request(
    method: str,
    url: str,
    *,
    headers: dict[str, str],
    payload: dict[str, Any] | None = None,
    timeout: float = 30.0,
) -> tuple[int, Any]:
    data = None
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, method=method.upper())
    for name, value in headers.items():
        req.add_header(name, value)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read()
            return resp.status, _parse_json(body)
    except urllib.error.HTTPError as exc:
        body = exc.read()
        return exc.code, _parse_json(body)
    except urllib.error.URLError as exc:
        print(f"network error: {exc.reason}", file=sys.stderr)
        return 0, None


def _parse_json(body: bytes) -> Any:
    if not body:
        return None
    try:
        return json.loads(body.decode("utf-8"))
    except (ValueError, UnicodeDecodeError):
        return body.decode("utf-8", "replace")


class Nuvio:
    def __init__(self, base_url: str, anon_key: str) -> None:
        self.base_url = base_url.rstrip("/")
        self.anon_key = anon_key
        self.access_token = ""
        self.refresh_token = ""

    def _headers(self, *, auth: bool) -> dict[str, str]:
        headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
            "User-Agent": USER_AGENT,
            "apikey": self.anon_key,
        }
        if auth:
            headers["Authorization"] = f"Bearer {self.access_token}"
        return headers

    def sign_in(self, email: str, password: str) -> tuple[int, Any]:
        url = f"{self.base_url}/auth/v1/token?" + urllib.parse.urlencode(
            {"grant_type": "password"}
        )
        status, data = request(
            "POST", url, headers=self._headers(auth=False),
            payload={"email": email, "password": password},
        )
        if status == 200 and isinstance(data, dict):
            self.access_token = str(data.get("access_token") or "")
            self.refresh_token = str(data.get("refresh_token") or "")
        return status, data

    def rpc(self, name: str, payload: dict[str, Any]) -> tuple[int, Any]:
        url = f"{self.base_url}/rest/v1/rpc/{name}"
        return request("POST", url, headers=self._headers(auth=True), payload=payload)


def summarize_rows(rows: Any, limit: int = 3) -> None:
    if not isinstance(rows, list):
        print(f"    (not a list: {type(rows).__name__})")
        return
    print(f"    rows: {len(rows)}")
    types: dict[str, int] = {}
    for row in rows:
        if isinstance(row, dict):
            ctype = str(row.get("content_type") or "?")
            types[ctype] = types.get(ctype, 0) + 1
    if types:
        print(f"    content_type counts: {types}")
    for row in rows[:limit]:
        print("    " + json.dumps(redact(row), ensure_ascii=False, sort_keys=True))


def read_library(client: "Nuvio", profile_id: int, page_size: int = 500) -> list[Any]:
    """Paginate `sync_pull_library` by offset until a short page."""
    out: list[Any] = []
    offset = 0
    while len(out) < page_size * 100:
        status, page = client.rpc(
            "sync_pull_library",
            {"p_profile_id": profile_id, "p_limit": page_size, "p_offset": offset},
        )
        if status != 200 or not isinstance(page, list):
            print(f"    HTTP {status} page at offset {offset}: " + json.dumps(redact(page)))
            break
        out.extend(page)
        if len(page) < page_size:
            break
        offset += page_size
    return out


def read_watched(client: "Nuvio", profile_id: int, page_size: int = 500) -> list[Any]:
    """Paginate `sync_pull_watched_items` by page number until a short page."""
    out: list[Any] = []
    page_number = 1
    while page_number <= 100:
        status, page = client.rpc(
            "sync_pull_watched_items",
            {"p_profile_id": profile_id, "p_page": page_number, "p_page_size": page_size},
        )
        if status != 200 or not isinstance(page, list):
            print(f"    HTTP {status} page {page_number}: " + json.dumps(redact(page)))
            break
        out.extend(page)
        if len(page) < page_size:
            break
        page_number += 1
    return out


def shape_of(row: Any) -> str:
    if not isinstance(row, dict):
        return "?"
    ctype = str(row.get("content_type") or "?")
    if ctype == "series":
        if row.get("season") is None and row.get("episode") is None:
            return "series(show-level)"
        return "series(episode)"
    return ctype


def samples_by_shape(rows: list[Any], per_shape: int = 1) -> None:
    seen: dict[str, int] = {}
    for row in rows:
        shape = shape_of(row)
        if seen.get(shape, 0) >= per_shape:
            continue
        seen[shape] = seen.get(shape, 0) + 1
        print(f"    sample [{shape}]: " + json.dumps(redact(row), ensure_ascii=False, sort_keys=True))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", default=".env", help="path to .env file")
    parser.add_argument("--profile", type=int, help="profile id (1-6) to read")
    parser.add_argument(
        "--writes",
        action="store_true",
        help="also run controlled write tests (not implemented yet)",
    )
    args = parser.parse_args()

    load_dotenv(args.env)

    email = os.environ.get("NUVIO_EMAIL", "").strip()
    password = os.environ.get("NUVIO_PASSWORD", "").strip()
    base_url = os.environ.get("NUVIO_API_URL", DEFAULT_API_URL).strip() or DEFAULT_API_URL
    anon_key = os.environ.get("NUVIO_ANON_KEY", DEFAULT_ANON_KEY).strip() or DEFAULT_ANON_KEY
    profile_env = os.environ.get("NUVIO_PROFILE", "").strip()
    profile_id = args.profile or (int(profile_env) if profile_env.isdigit() else None)

    print(f"base_url   : {base_url}")
    print(f"anon_key   : {'set (default)' if anon_key == DEFAULT_ANON_KEY else 'set (custom)'}")
    print(f"email      : {'set' if email else 'MISSING'}")
    print(f"password   : {'set' if password else 'MISSING'}")
    print(f"profile    : {profile_id if profile_id else '(none; will list only)'}")
    print()

    if not email or not password:
        print("NUVIO_EMAIL and NUVIO_PASSWORD must be set (see .env.example).", file=sys.stderr)
        return 2

    client = Nuvio(base_url, anon_key)

    print("[1] sign in (password grant)")
    status, data = client.sign_in(email, password)
    print(f"    HTTP {status}")
    if status != 200 or not isinstance(data, dict):
        print("    response: " + json.dumps(redact(data), ensure_ascii=False))
        return 1
    print(f"    access_token: {'yes' if client.access_token else 'no'}")
    print(f"    refresh_token: {'yes' if client.refresh_token else 'no'}")
    print(f"    expires_in: {data.get('expires_in')}")

    print()
    print("[2] profiles (sync_pull_profiles)")
    status, profiles = client.rpc("sync_pull_profiles", {})
    print(f"    HTTP {status}")
    if status == 200 and isinstance(profiles, list):
        for prof in profiles:
            print("    " + json.dumps(redact(prof), ensure_ascii=False, sort_keys=True))
    else:
        print("    response: " + json.dumps(redact(profiles), ensure_ascii=False))
        return 1

    if profile_id is None:
        print()
        print("No profile selected; pass --profile N to read library/watched/progress.")
        return 0

    print()
    print(f"[3] library (sync_pull_library, paginated) profile={profile_id}")
    library = read_library(client, profile_id)
    print(f"    total rows: {len(library)}")
    summarize_rows(library, limit=2)

    print()
    print(f"[4] watched items (sync_pull_watched_items, paginated) profile={profile_id}")
    watched = read_watched(client, profile_id)
    print(f"    total rows: {len(watched)}")
    summarize_rows(watched, limit=0)
    samples_by_shape(watched)

    print()
    print(f"[5] playback progress (sync_pull_watch_progress) profile={profile_id}")
    status, progress = client.rpc(
        "sync_pull_watch_progress", {"p_profile_id": profile_id, "p_limit": 200}
    )
    print(f"    HTTP {status} (signature A: p_profile_id, p_limit)")
    if status != 200:
        print("    retrying with signature B: p_profile_id, p_since_last_watched, p_limit")
        status, progress = client.rpc(
            "sync_pull_watch_progress",
            {"p_profile_id": profile_id, "p_since_last_watched": 0, "p_limit": 200},
        )
        print(f"    HTTP {status} (signature B)")
    summarize_rows(progress)

    if args.writes:
        print()
        print("Controlled write tests are not implemented yet.")
        return 3

    print()
    print("Read-only probe complete.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
