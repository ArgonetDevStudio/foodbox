#!/usr/bin/env python3
import datetime
import hashlib
import json
import os
import re
import stat
import sys


def safe_filename(name):
    return isinstance(name, str) and name not in {"", ".", ".."} and \
        os.path.basename(name) == name and len(os.fsencode(name)) <= 255 and \
        all(character.isprintable() and character not in "\\\r\n" for character in name)


def digest(path):
    value = hashlib.sha256()
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    details = os.fstat(descriptor)
    if not stat.S_ISREG(details.st_mode) or details.st_nlink != 1:
        os.close(descriptor)
        raise RuntimeError(f"unsafe regular file: {os.path.basename(path)}")
    with os.fdopen(descriptor, "rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def load_database(path):
    with open(path, encoding="utf-8") as database:
        rows = json.load(database)
    if not isinstance(rows, list):
        raise RuntimeError("database root is not an array")
    result = {}
    previous = None
    for row in rows:
        if not isinstance(row, dict) or set(row) != {"date", "menus", "valid"}:
            raise RuntimeError("database record shape is invalid")
        date = row["date"]
        if not isinstance(date, list) or len(date) != 3 or any(type(value) is not int for value in date):
            raise RuntimeError("database date shape is invalid")
        parsed = datetime.date(*date)
        if parsed in result or (previous is not None and parsed < previous):
            raise RuntimeError("database dates must be unique and oldest first")
        if not isinstance(row["menus"], list) or any(not isinstance(item, str) for item in row["menus"]):
            raise RuntimeError("database menus are invalid")
        if type(row["valid"]) is not bool:
            raise RuntimeError("database validity is invalid")
        result[parsed] = row
        previous = parsed
    return result


def validate_metadata(path):
    with open(path, encoding="utf-8") as metadata_file:
        content = metadata_file.read()
    if not content.strip():
        return
    with open(path, encoding="utf-8") as metadata_file:
        metadata = json.load(metadata_file)
        if metadata_file.read(1) != "":
            raise RuntimeError("metadata contains trailing content")
    if not isinstance(metadata, dict) or set(metadata) != {"lastImageHash"} or \
            not isinstance(metadata["lastImageHash"], str):
        raise RuntimeError("metadata shape is invalid")


def validate(snapshot, current, exact=False):
    with open(os.path.join(snapshot, "manifest.json"), encoding="utf-8") as source:
        manifest = json.load(source)
    if manifest.get("schema") != "foodbox-db-snapshot-v1":
        raise RuntimeError("snapshot manifest schema is invalid")
    items = manifest.get("files")
    if not isinstance(items, list):
        raise RuntimeError("snapshot manifest file list is invalid")
    expected = {}
    for item in items:
        if not isinstance(item, dict) or set(item) != {"name", "sha256", "bytes", "uid", "gid", "mode"}:
            raise RuntimeError("snapshot manifest item is invalid")
        name = item["name"]
        if not safe_filename(name) or name in expected:
            raise RuntimeError("snapshot manifest filename is invalid")
        if not isinstance(item["sha256"], str) or not re.fullmatch(r"[a-f0-9]{64}", item["sha256"]):
            raise RuntimeError("snapshot manifest checksum is invalid")
        if any(type(item[field]) is not int or item[field] < 0
               for field in ("bytes", "uid", "gid", "mode")) or item["mode"] > 0o7777:
            raise RuntimeError("snapshot manifest metadata is invalid")
        expected[name] = item
    if "db.json" not in expected:
        raise RuntimeError("snapshot manifest file set is invalid")

    snapshot_data = os.path.join(snapshot, "data")
    snapshot_files = {}
    for entry in os.scandir(snapshot_data):
        if not safe_filename(entry.name):
            raise RuntimeError("snapshot contains an unsafe filename")
        details = entry.stat(follow_symlinks=False)
        if not stat.S_ISREG(details.st_mode) or details.st_nlink != 1:
            raise RuntimeError(f"snapshot contains unsafe entry: {entry.name}")
        snapshot_files[entry.name] = entry.path
    if set(snapshot_files) != set(expected):
        raise RuntimeError("snapshot data file set differs from manifest")
    for name, item in expected.items():
        if os.path.getsize(snapshot_files[name]) != item["bytes"] or digest(snapshot_files[name]) != item["sha256"]:
            raise RuntimeError(f"snapshot data differs from manifest: {name}")

    live = {}
    transient_prefixes = (".db.json.tmp-", ".metadata.json.tmp-", ".foodbox-writable-check-")
    transient_found = False
    for entry in os.scandir(current):
        if not safe_filename(entry.name):
            raise RuntimeError("live database contains an unsafe filename")
        details = entry.stat(follow_symlinks=False)
        if not stat.S_ISREG(details.st_mode) or details.st_nlink != 1:
            raise RuntimeError(f"live database contains unsafe entry: {entry.name}")
        if entry.name.startswith(transient_prefixes):
            transient_found = True
            continue
        live[entry.name] = entry.path
    allowed = set(expected) | {"metadata.json"}
    if set(live) - allowed or set(expected) - set(live):
        raise RuntimeError("live database file set is invalid")

    for name, item in expected.items():
        if name not in {"db.json", "metadata.json"} and digest(live[name]) != item["sha256"]:
            raise RuntimeError(f"persistent database file changed: {name}")
    snapshot_rows = load_database(snapshot_files["db.json"])
    if "metadata.json" in snapshot_files:
        validate_metadata(snapshot_files["metadata.json"])
    current_rows = load_database(live["db.json"])
    if any(current_rows.get(date) != row for date, row in snapshot_rows.items()):
        raise RuntimeError("preserved menu record changed or disappeared")

    if "metadata.json" in live:
        validate_metadata(live["metadata.json"])
    if exact:
        if transient_found:
            raise RuntimeError("live database contains transient files")
        if set(live) != set(expected):
            raise RuntimeError("live database file set differs from snapshot")
        for name, item in expected.items():
            if os.path.getsize(live[name]) != item["bytes"] or digest(live[name]) != item["sha256"]:
                raise RuntimeError(f"live database file differs from snapshot: {name}")


def main():
    if len(sys.argv) not in (3, 4) or (len(sys.argv) == 4 and sys.argv[1] != "--exact"):
        raise SystemExit("usage: db_validate.py [--exact] SNAPSHOT LIVE_DB_DIR")
    exact = len(sys.argv) == 4
    arguments = sys.argv[2:] if exact else sys.argv[1:]
    if any(os.path.islink(path) or not os.path.isdir(path) for path in arguments):
        raise SystemExit("snapshot and live database paths must be real directories")
    validate(*(os.path.realpath(path) for path in arguments), exact=exact)


if __name__ == "__main__":
    main()
