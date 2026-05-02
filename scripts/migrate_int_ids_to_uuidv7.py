#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.12"
# ///
"""One-time migration from old integer SQLite IDs to UUIDv7 BLOB IDs.

Run from the repository root while lurker is stopped:

    uv run scripts/migrate_int_ids_to_uuidv7.py --data-dir ./data --apply

The script creates replacement control/log DBs, backs up old DB files into a
.timestamped directory under the data dir, then swaps the new DBs into place.
It intentionally does not migrate across arbitrary historical schemas; it is a
throwaway helper for the pre-UUIDv7 greenfield database shape.
"""

from __future__ import annotations

import argparse
import os
import random
import re
import shutil
import sqlite3
import sys
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

NETWORK_RE = re.compile(r"^[A-Za-z0-9_-]+$")


def now_storage() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%f")[:23] + "Z"


def parse_storage_ms(value: str | None) -> int:
    if not value:
        return int(time.time() * 1000)
    raw = value.strip()
    if raw.endswith("Z"):
        raw = raw[:-1] + "+00:00"
    try:
        return int(datetime.fromisoformat(raw).timestamp() * 1000)
    except ValueError:
        return int(time.time() * 1000)


def uuidv7_bytes(ms: int | None = None) -> bytes:
    """Generate RFC 9562 UUIDv7 bytes."""
    if ms is None:
        ms = int(time.time() * 1000)
    ms &= (1 << 48) - 1
    rand_a = random.getrandbits(12)
    rand_b = random.getrandbits(62)
    value = (ms << 80) | (0x7 << 76) | (rand_a << 64) | (0b10 << 62) | rand_b
    return value.to_bytes(16, "big")


class OrderedUUIDv7:
    """Generate UUIDv7 bytes that sort in caller order."""

    def __init__(self) -> None:
        self.last_ms = 0

    def next(self, preferred_ms: int | None = None) -> bytes:
        ms = preferred_ms if preferred_ms is not None else int(time.time() * 1000)
        if ms <= self.last_ms:
            ms = self.last_ms + 1
        self.last_ms = ms
        return uuidv7_bytes(ms)


def uuid_text(raw: bytes | None) -> str:
    if raw is None:
        return "<null>"
    h = raw.hex()
    return f"{h[:8]}-{h[8:12]}-{h[12:16]}-{h[16:20]}-{h[20:]}"


def normalize_network_name(name: str) -> str:
    if not NETWORK_RE.match(name):
        raise SystemExit(f"invalid network name {name!r}; cannot derive log DB filename")
    return name.lower()


def table_exists(conn: sqlite3.Connection, name: str) -> bool:
    row = conn.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (name,)).fetchone()
    return row is not None


def column_exists(conn: sqlite3.Connection, table: str, column: str) -> bool:
    return any(row[1] == column for row in conn.execute(f"PRAGMA table_info({table})"))


def id_column_decl(conn: sqlite3.Connection, table: str) -> str:
    for row in conn.execute(f"PRAGMA table_info({table})"):
        if row[1] == "id":
            return str(row[2]).upper()
    return ""


def connect(path: Path) -> sqlite3.Connection:
    conn = sqlite3.connect(path)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA foreign_keys=ON")
    return conn


def checkpoint(path: Path) -> None:
    if not path.exists():
        return
    conn = sqlite3.connect(path)
    try:
        conn.execute("PRAGMA wal_checkpoint(FULL)")
    finally:
        conn.close()


def apply_migrations(conn: sqlite3.Connection, migration_dir: Path) -> None:
    conn.execute(
        """CREATE TABLE IF NOT EXISTS schema_migrations (
            version TEXT PRIMARY KEY,
            applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
        )"""
    )
    for path in sorted(migration_dir.glob("*.sql")):
        conn.executescript(path.read_text())
        conn.execute("INSERT INTO schema_migrations(version) VALUES (?)", (path.name,))
    conn.commit()


@dataclass
class NetworkRow:
    old_id: int
    new_id: bytes
    name: str
    log_path: Path


@dataclass
class MigrationState:
    data_dir: Path
    repo_root: Path
    temp_dir: Path
    network_ids: dict[int, bytes]
    buffer_ids: dict[tuple[int, str], bytes]
    network_rows: list[NetworkRow]
    new_control_path: Path
    new_log_paths: dict[Path, Path]


def migrate_control(data_dir: Path, repo_root: Path, temp_dir: Path) -> MigrationState:
    old_path = data_dir / "control.db"
    if not old_path.exists():
        raise SystemExit(f"missing {old_path}")
    checkpoint(old_path)
    old = connect(old_path)
    try:
        decl = id_column_decl(old, "networks")
        if "BLOB" in decl:
            raise SystemExit("control.db already appears to use UUID/BLOB IDs; aborting")
        new_path = temp_dir / "control.db"
        new = connect(new_path)
        try:
            apply_migrations(new, repo_root / "db" / "control_migrations")

            network_ids: dict[int, bytes] = {}
            network_rows: list[NetworkRow] = []
            has_sort = column_exists(old, "networks", "sort_order")
            has_disabled = column_exists(old, "networks", "disabled")
            cols = "id, name, name_ci, host, port, tls, nick, realname, sasl_user, sasl_pass, autoconnect, created_at"
            for r in old.execute(f"SELECT {cols} FROM networks ORDER BY id"):
                old_id = int(r["id"])
                new_id = uuidv7_bytes()
                network_ids[old_id] = new_id
                sort_order = old.execute("SELECT sort_order FROM networks WHERE id=?", (old_id,)).fetchone()[0] if has_sort else 0
                disabled = old.execute("SELECT disabled FROM networks WHERE id=?", (old_id,)).fetchone()[0] if has_disabled else 0
                new.execute(
                    """INSERT INTO networks(id, name, name_ci, host, port, tls, nick, realname,
                       sasl_user, sasl_pass, autoconnect, created_at, sort_order, disabled)
                       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                    (
                        new_id,
                        r["name"],
                        r["name_ci"],
                        r["host"],
                        r["port"],
                        r["tls"],
                        r["nick"],
                        r["realname"],
                        r["sasl_user"],
                        r["sasl_pass"],
                        r["autoconnect"],
                        r["created_at"],
                        sort_order,
                        disabled,
                    ),
                )
                network_rows.append(
                    NetworkRow(old_id, new_id, r["name"], data_dir / f"{normalize_network_name(r['name'])}.db")
                )

            buffer_ids: dict[tuple[int, str], bytes] = {}
            if table_exists(old, "buffer_registry"):
                for r in old.execute("SELECT id, network_id, name, kind, created_at FROM buffer_registry ORDER BY id"):
                    old_network_id = int(r["network_id"])
                    new_network_id = network_ids[old_network_id]
                    new_id = uuidv7_bytes()
                    buffer_ids[(old_network_id, r["name"])] = new_id
                    new.execute(
                        "INSERT INTO buffer_registry(id, network_id, name, kind, created_at) VALUES (?, ?, ?, ?, ?)",
                        (new_id, new_network_id, r["name"], r["kind"], r["created_at"]),
                    )

            if table_exists(old, "ignores"):
                for r in old.execute("SELECT network_id, mask, created_at FROM ignores ORDER BY id"):
                    new.execute(
                        "INSERT INTO ignores(id, network_id, mask, created_at) VALUES (?, ?, ?, ?)",
                        (uuidv7_bytes(), network_ids[int(r["network_id"])], r["mask"], r["created_at"]),
                    )

            if table_exists(old, "buffer_settings"):
                for r in old.execute(
                    """SELECT buffer_id, show_embeds, show_presence_events,
                       collapse_presence_events, pinned, updated_at FROM buffer_settings"""
                ):
                    old_buffer_id = int(r["buffer_id"])
                    match = old.execute(
                        "SELECT network_id, name FROM buffer_registry WHERE id=?", (old_buffer_id,)
                    ).fetchone()
                    if match is None:
                        continue
                    new_buffer_id = buffer_ids[(int(match["network_id"]), match["name"])]
                    new.execute(
                        """INSERT INTO buffer_settings(buffer_id, show_embeds, show_presence_events,
                           collapse_presence_events, pinned, updated_at) VALUES (?, ?, ?, ?, ?, ?)""",
                        (
                            new_buffer_id,
                            r["show_embeds"],
                            r["show_presence_events"],
                            r["collapse_presence_events"],
                            r["pinned"],
                            r["updated_at"],
                        ),
                    )

            new.commit()
        finally:
            new.close()
    finally:
        old.close()

    return MigrationState(data_dir, repo_root, temp_dir, network_ids, buffer_ids, network_rows, new_path, {})


def ensure_registry_buffer(state: MigrationState, conn: sqlite3.Connection, old_network_id: int, name: str, kind: str, created_at: str) -> bytes:
    key = (old_network_id, name)
    existing = state.buffer_ids.get(key)
    if existing is not None:
        return existing
    new_id = uuidv7_bytes()
    state.buffer_ids[key] = new_id
    conn.execute(
        "INSERT INTO buffer_registry(id, network_id, name, kind, created_at) VALUES (?, ?, ?, ?, ?)",
        (new_id, state.network_ids[old_network_id], name, kind, created_at),
    )
    return new_id


def migrate_log(state: MigrationState, network: NetworkRow) -> None:
    old_path = network.log_path
    if not old_path.exists():
        print(f"warning: missing log DB for {network.name}: {old_path}", file=sys.stderr)
        return
    checkpoint(old_path)
    old = connect(old_path)
    try:
        decl = id_column_decl(old, "buffers")
        if "BLOB" in decl:
            raise SystemExit(f"{old_path} already appears to use UUID/BLOB IDs; aborting")
        new_path = state.temp_dir / old_path.name
        new = connect(new_path)
        try:
            apply_migrations(new, state.repo_root / "db" / "log_migrations")
            control = connect(state.new_control_path)
            try:
                old_buffer_to_new: dict[int, bytes] = {}
                old_last_seen: dict[int, int | None] = {}
                buffer_cols = "id, name, kind, topic, last_seen_id, created_at"
                for r in old.execute(f"SELECT {buffer_cols} FROM buffers ORDER BY id"):
                    created_at = r["created_at"] or now_storage()
                    new_buffer_id = ensure_registry_buffer(state, control, network.old_id, r["name"], r["kind"], created_at)
                    old_buffer_to_new[int(r["id"])] = new_buffer_id
                    old_last_seen[int(r["id"])] = r["last_seen_id"]
                    new.execute(
                        "INSERT INTO buffers(id, name, kind, topic, last_seen_id, created_at) VALUES (?, ?, ?, ?, NULL, ?)",
                        (new_buffer_id, r["name"], r["kind"], r["topic"], created_at),
                    )
                control.commit()
            finally:
                control.close()

            old_message_to_new: dict[int, bytes] = {}
            skipped_orphan_messages = 0
            ordered = OrderedUUIDv7()
            msg_cols = "id, buffer_id, msgid, ts, sender, account, kind, target, content, raw"
            for r in old.execute(f"SELECT {msg_cols} FROM messages ORDER BY id"):
                old_buffer_id = int(r["buffer_id"])
                new_buffer_id = old_buffer_to_new.get(old_buffer_id)
                if new_buffer_id is None:
                    skipped_orphan_messages += 1
                    continue
                old_msg_id = int(r["id"])
                new_msg_id = ordered.next(parse_storage_ms(r["ts"]))
                old_message_to_new[old_msg_id] = new_msg_id
                new.execute(
                    """INSERT INTO messages(id, buffer_id, msgid, ts, sender, account, kind, target, content, raw)
                       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                    (
                        new_msg_id,
                        new_buffer_id,
                        r["msgid"],
                        r["ts"],
                        r["sender"],
                        r["account"],
                        r["kind"],
                        r["target"],
                        r["content"],
                        r["raw"],
                    ),
                )

            for old_buffer_id, old_seen in old_last_seen.items():
                if old_seen is None:
                    continue
                new_seen = old_message_to_new.get(int(old_seen))
                if new_seen is not None:
                    new.execute(
                        "UPDATE buffers SET last_seen_id=? WHERE id=?",
                        (new_seen, old_buffer_to_new[old_buffer_id]),
                    )

            if table_exists(old, "message_previews"):
                for r in old.execute("SELECT message_id, url, position FROM message_previews ORDER BY message_id, position"):
                    new_msg_id = old_message_to_new.get(int(r["message_id"]))
                    if new_msg_id is None:
                        continue
                    new.execute(
                        "INSERT OR IGNORE INTO message_previews(message_id, url, position) VALUES (?, ?, ?)",
                        (new_msg_id, r["url"], r["position"]),
                    )

            if skipped_orphan_messages:
                print(
                    f"warning: skipped {skipped_orphan_messages} messages in {old_path.name} whose buffer_id has no buffers row",
                    file=sys.stderr,
                )

            new.commit()
            state.new_log_paths[old_path] = new_path
        finally:
            new.close()
    finally:
        old.close()


def move_with_sidecars(src: Path, dst_dir: Path) -> None:
    for p in [src, Path(str(src) + "-wal"), Path(str(src) + "-shm")]:
        if p.exists():
            shutil.move(str(p), str(dst_dir / p.name))


def apply_swap(state: MigrationState) -> Path:
    backup_dir = state.data_dir / ("pre-uuidv7-backup-" + datetime.now().strftime("%Y%m%d-%H%M%S"))
    backup_dir.mkdir()
    move_with_sidecars(state.data_dir / "control.db", backup_dir)
    for old_path in state.new_log_paths:
        move_with_sidecars(old_path, backup_dir)
    shutil.move(str(state.new_control_path), str(state.data_dir / "control.db"))
    for old_path, new_path in state.new_log_paths.items():
        shutil.move(str(new_path), str(old_path))
    return backup_dir


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--data-dir", default="data", type=Path)
    parser.add_argument("--repo-root", default=Path(__file__).resolve().parents[1], type=Path)
    parser.add_argument("--apply", action="store_true", help="swap migrated DBs into place; otherwise dry-run only")
    args = parser.parse_args()

    data_dir = args.data_dir.resolve()
    repo_root = args.repo_root.resolve()
    temp_dir = data_dir / (".uuidv7-migration-tmp-" + datetime.now().strftime("%Y%m%d-%H%M%S"))
    if temp_dir.exists():
        raise SystemExit(f"temp dir already exists: {temp_dir}")
    temp_dir.mkdir(parents=True)

    try:
        state = migrate_control(data_dir, repo_root, temp_dir)
        for network in state.network_rows:
            migrate_log(state, network)

        print(f"migrated control DB: {state.new_control_path}")
        print(f"migrated log DBs: {len(state.new_log_paths)}")
        print(f"networks: {len(state.network_ids)}; buffers: {len(state.buffer_ids)}")
        if not args.apply:
            print("dry-run complete; rerun with --apply to replace DB files")
            print(f"temporary migrated files left at: {temp_dir}")
            return 0

        backup_dir = apply_swap(state)
        try:
            temp_dir.rmdir()
        except OSError:
            print(f"temporary directory not empty, left at: {temp_dir}", file=sys.stderr)
        print(f"migration applied; old DBs backed up at: {backup_dir}")
        print("previews.db was left in place; stop lurker before running this script on real data.")
        return 0
    except Exception:
        print(f"migration failed; temporary files left at: {temp_dir}", file=sys.stderr)
        raise


if __name__ == "__main__":
    raise SystemExit(main())
