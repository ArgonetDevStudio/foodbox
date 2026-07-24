#!/usr/bin/env python3
import datetime
import hashlib
import json
import os
import shutil
import sys
import tempfile


ALLOWED_FILES = {"db.json", "db.backup.json", "metadata.json"}


def digest(path):
    value = hashlib.sha256()
    with open(path, "rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def fsync_directory(path):
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def restore(snapshot, destination, backups):
    with open(os.path.join(snapshot, "manifest.json"), encoding="utf-8") as source:
        manifest = json.load(source)
    if not isinstance(manifest, dict) or set(manifest) != {"schema", "records", "directory", "files"} or \
            manifest.get("schema") != "foodbox-db-snapshot-v1":
        raise RuntimeError("snapshot manifest schema is invalid")
    if type(manifest["records"]) is not int or manifest["records"] < 0:
        raise RuntimeError("snapshot record count is invalid")
    directory = manifest["directory"]
    if not isinstance(directory, dict) or set(directory) != {"uid", "gid", "mode"} or \
            any(type(directory[field]) is not int or directory[field] < 0 for field in directory):
        raise RuntimeError("snapshot directory metadata is invalid")
    items = manifest["files"]
    if not isinstance(items, list):
        raise RuntimeError("snapshot manifest file list is invalid")
    expected = {}
    for item in items:
        if not isinstance(item, dict) or set(item) != {"name", "sha256", "bytes", "uid", "gid", "mode"}:
            raise RuntimeError("snapshot manifest item is invalid")
        name = item["name"]
        if name not in ALLOWED_FILES or name in expected:
            raise RuntimeError("snapshot manifest filename is invalid")
        if not isinstance(item["sha256"], str) or len(item["sha256"]) != 64 or \
                any(character not in "0123456789abcdef" for character in item["sha256"]):
            raise RuntimeError("snapshot manifest checksum is invalid")
        if any(type(item[field]) is not int or item[field] < 0
               for field in ("bytes", "uid", "gid", "mode")):
            raise RuntimeError("snapshot manifest file metadata is invalid")
        expected[name] = item
    if "db.json" not in expected:
        raise RuntimeError("snapshot manifest file set is invalid")

    snapshot_data = os.path.join(snapshot, "data")
    actual = {}
    for entry in os.scandir(snapshot_data):
        if not entry.is_file(follow_symlinks=False):
            raise RuntimeError("snapshot contains an unsafe entry")
        actual[entry.name] = entry.path
    if set(actual) != set(expected):
        raise RuntimeError("snapshot data file set differs from manifest")
    for name, item in expected.items():
        source_path = actual[name]
        if os.path.getsize(source_path) != item["bytes"] or digest(source_path) != item["sha256"]:
            raise RuntimeError("snapshot checksum is invalid")

    current = {}
    for entry in os.scandir(destination):
        if not entry.is_file(follow_symlinks=False) and not entry.is_symlink():
            raise RuntimeError(f"live database contains unsafe entry: {entry.name}")
        current[entry.name] = (entry.path, entry.is_symlink())

    prepared = {}
    try:
        for name, item in expected.items():
            temporary = tempfile.NamedTemporaryFile(prefix=f".{name}.", dir=destination, delete=False)
            prepared[name] = temporary.name
            with open(os.path.join(snapshot, "data", name), "rb") as reader, temporary:
                shutil.copyfileobj(reader, temporary)
                temporary.flush()
                os.fsync(temporary.fileno())
            os.chmod(prepared[name], item["mode"])
            os.chown(prepared[name], item["uid"], item["gid"])

        timestamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        quarantine = tempfile.mkdtemp(prefix=f"rejected-db-{timestamp}.", dir=backups)
        for name, (path, is_symlink) in current.items():
            target = os.path.join(quarantine, name)
            if name in expected and not is_symlink:
                descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
                with os.fdopen(descriptor, "rb") as reader, open(target, "xb") as writer:
                    shutil.copyfileobj(reader, writer)
                    writer.flush()
                    os.fsync(writer.fileno())
            else:
                os.replace(path, target)
        fsync_directory(quarantine)

        order = sorted(expected, key=lambda name: (name == "db.json", name))
        for name in order:
            os.replace(prepared.pop(name), os.path.join(destination, name))
        os.chmod(destination, directory["mode"])
        os.chown(destination, directory["uid"], directory["gid"])
        fsync_directory(destination)
        fsync_directory(backups)

        restored = sorted(entry.name for entry in os.scandir(destination))
        if restored != sorted(expected):
            raise RuntimeError("restored database file set differs from snapshot")
        for name, item in expected.items():
            if digest(os.path.join(destination, name)) != item["sha256"]:
                raise RuntimeError("restored database checksum differs from snapshot")
    finally:
        for path in prepared.values():
            try:
                os.unlink(path)
            except FileNotFoundError:
                pass


def main():
    if len(sys.argv) != 4:
        raise SystemExit("usage: db_restore.py SNAPSHOT LIVE_DB_DIR BACKUP_DIR")
    for path in sys.argv[1:]:
        if os.path.islink(path) or not os.path.isdir(path):
            raise SystemExit("snapshot, live database, and backup paths must be real directories")
    restore(*(os.path.realpath(path) for path in sys.argv[1:]))


if __name__ == "__main__":
    main()
