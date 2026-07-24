#!/usr/bin/env python3
import datetime
import hashlib
import json
import os
import shutil
import stat
import sys
import tempfile
import time


TRANSIENT_PREFIXES = (".db.json.tmp-", ".metadata.json.tmp-", ".foodbox-writable-check-")


def safe_filename(name):
    return isinstance(name, str) and name not in {"", ".", ".."} and \
        os.path.basename(name) == name and len(os.fsencode(name)) <= 255 and \
        all(character.isprintable() and character not in "\\\r\n" for character in name)


def regular_files(source):
    entries = sorted(os.scandir(source), key=lambda entry: entry.name)
    persistent = []
    for entry in entries:
        if not safe_filename(entry.name):
            raise RuntimeError("database directory contains an unsafe filename")
        if entry.name.startswith(TRANSIENT_PREFIXES):
            details = entry.stat(follow_symlinks=False)
            if not stat.S_ISREG(details.st_mode) or details.st_nlink != 1:
                raise RuntimeError(f"unsafe database entry: {entry.name}")
            continue
        persistent.append(entry)
    entries = persistent
    if not entries:
        raise RuntimeError("database directory is empty")
    for entry in entries:
        details = entry.stat(follow_symlinks=False)
        if not stat.S_ISREG(details.st_mode) or details.st_nlink != 1:
            raise RuntimeError(f"unsafe database entry: {entry.name}")
    if "db.json" not in {entry.name for entry in entries}:
        raise RuntimeError("db.json is missing")
    return entries


def digest(path):
    value = hashlib.sha256()
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    details = os.fstat(descriptor)
    if not stat.S_ISREG(details.st_mode) or details.st_nlink != 1:
        os.close(descriptor)
        raise RuntimeError(f"unsafe database entry: {os.path.basename(path)}")
    with os.fdopen(descriptor, "rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def capture(entries):
    result = {}
    for entry in entries:
        details = entry.stat(follow_symlinks=False)
        if not stat.S_ISREG(details.st_mode) or details.st_nlink != 1:
            raise RuntimeError(f"unsafe database entry: {entry.name}")
        result[entry.name] = (
            details.st_dev, details.st_ino, details.st_nlink, details.st_size,
            details.st_mtime_ns, digest(entry.path),
        )
    return result


def validate_database(path):
    with open(path, encoding="utf-8") as database:
        rows = json.load(database)
    if not isinstance(rows, list):
        raise RuntimeError("database root is not an array")
    previous = None
    seen = set()
    for row in rows:
        if not isinstance(row, dict) or set(row) != {"date", "menus", "valid"}:
            raise RuntimeError("database record shape is invalid")
        date = row["date"]
        if not isinstance(date, list) or len(date) != 3 or any(type(value) is not int for value in date):
            raise RuntimeError("database date shape is invalid")
        parsed = datetime.date(*date)
        if parsed in seen or (previous is not None and parsed < previous):
            raise RuntimeError("database dates must be unique and oldest first")
        seen.add(parsed)
        previous = parsed
        if not isinstance(row["menus"], list) or any(not isinstance(item, str) for item in row["menus"]):
            raise RuntimeError("database menus are invalid")
        if type(row["valid"]) is not bool:
            raise RuntimeError("database validity is invalid")
    return len(rows)


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


def snapshot(source, backups, output_uid, output_gid):
    temporary = tempfile.mkdtemp(prefix=".snapshot.", dir=backups)
    try:
        data = os.path.join(temporary, "data")
        os.mkdir(data, 0o700)
        completed = False
        for _ in range(3):
            shutil.rmtree(data)
            os.mkdir(data, 0o700)
            entries = regular_files(source)
            before = capture(entries)
            for entry in entries:
                target = os.path.join(data, entry.name)
                flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
                descriptor = os.open(entry.path, flags)
                try:
                    with os.fdopen(descriptor, "rb") as reader, open(target, "xb") as writer:
                        shutil.copyfileobj(reader, writer)
                        writer.flush()
                        os.fsync(writer.fileno())
                except BaseException:
                    try:
                        os.close(descriptor)
                    except OSError:
                        pass
                    raise
            entries_after = regular_files(source)
            after = capture(entries_after)
            copied = {entry.name: digest(os.path.join(data, entry.name)) for entry in entries_after}
            if before == after and all(copied[name] == values[5] for name, values in before.items()):
                completed = True
                break
            time.sleep(1)
        if not completed:
            raise RuntimeError("database changed during every snapshot attempt")

        record_count = validate_database(os.path.join(data, "db.json"))
        metadata_path = os.path.join(data, "metadata.json")
        if os.path.exists(metadata_path):
            validate_metadata(metadata_path)
        manifest = []
        for entry in entries_after:
            details = entry.stat(follow_symlinks=False)
            manifest.append({
                "name": entry.name,
                "sha256": copied[entry.name],
                "bytes": details.st_size,
                "uid": details.st_uid,
                "gid": details.st_gid,
                "mode": stat.S_IMODE(details.st_mode),
            })
        source_details = os.stat(source, follow_symlinks=False)
        with open(os.path.join(temporary, "manifest.json"), "x", encoding="utf-8") as output:
            json.dump({
                "schema": "foodbox-db-snapshot-v1",
                "records": record_count,
                "directory": {
                    "uid": source_details.st_uid,
                    "gid": source_details.st_gid,
                    "mode": stat.S_IMODE(source_details.st_mode),
                },
                "files": manifest,
            }, output, separators=(",", ":"))
            output.write("\n")
            output.flush()
            os.fsync(output.fileno())
        with open(os.path.join(temporary, "SHA256SUMS"), "x", encoding="utf-8") as output:
            for item in manifest:
                output.write(f'{item["sha256"]}  data/{item["name"]}\n')
            output.flush()
            os.fsync(output.fileno())
        os.chmod(os.path.join(temporary, "manifest.json"), 0o600)
        os.chmod(os.path.join(temporary, "SHA256SUMS"), 0o600)
        descriptor = os.open(data, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        timestamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        final = os.path.join(backups, f"db-{timestamp}-{os.getpid()}")
        os.rename(temporary, final)
        for directory, directories, files in os.walk(final):
            os.chown(directory, output_uid, output_gid)
            for name in directories + files:
                os.chown(os.path.join(directory, name), output_uid, output_gid)
        descriptor = os.open(backups, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        return final
    except BaseException:
        shutil.rmtree(temporary, ignore_errors=True)
        raise


def main():
    if len(sys.argv) != 5:
        raise SystemExit("usage: db_snapshot.py SOURCE_DB_DIR BACKUP_DIR OUTPUT_UID OUTPUT_GID")
    if os.path.islink(sys.argv[1]) or os.path.islink(sys.argv[2]):
        raise SystemExit("source and backup paths must not be symlinks")
    source = os.path.realpath(sys.argv[1])
    backups = os.path.realpath(sys.argv[2])
    output_uid = int(sys.argv[3])
    output_gid = int(sys.argv[4])
    if output_uid < 0 or output_gid < 0:
        raise SystemExit("output uid and gid must be non-negative")
    if not os.path.isdir(source) or not os.path.isdir(backups):
        raise SystemExit("source and backup directories must already exist")
    if source == backups or source.startswith(backups + os.sep) or backups.startswith(source + os.sep):
        raise SystemExit("source and backup directories must be separate")
    print(snapshot(source, backups, output_uid, output_gid))


if __name__ == "__main__":
    main()
