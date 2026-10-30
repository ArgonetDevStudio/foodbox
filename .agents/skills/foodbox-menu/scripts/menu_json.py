#!/usr/bin/env python3
"""Build, validate and merge reviewed menu transcriptions without overwriting input files."""

import argparse
import datetime as dt
import hashlib
import json
from pathlib import Path
import re
import sys


def load(path):
    with Path(path).expanduser().open(encoding="utf-8") as stream:
        return json.load(stream)


def month(value):
    if not re.fullmatch(r"[0-9]{4}-[0-9]{2}", value):
        raise ValueError("Expected --month YYYY-MM.")
    year, number = map(int, value.split("-"))
    dt.date(year, number, 1)
    return year, number


def in_scope(date, target, following):
    if target is None:
        return True
    year, number = target
    if (date.year, date.month) == target:
        return True
    next_month = (year + 1, 1) if number == 12 else (year, number + 1)
    return following and (date.year, date.month) == next_month and date.day <= 7


def date_value(value):
    if not isinstance(value, list) or len(value) != 3 or any(type(x) is not int for x in value):
        raise ValueError("date must be [year, month, day] with integer values.")
    return dt.date(*value)


def check_rows(rows, target=None, following=False, legacy=False):
    if not isinstance(rows, list) or (not rows and not legacy):
        raise ValueError("Expected a non-empty array of menu records.")
    previous = None
    for row in rows:
        if not isinstance(row, dict) or set(row) != {"date", "menus", "valid"}:
            raise ValueError("Each operating record must contain exactly date, menus and valid.")
        date = date_value(row["date"])
        if previous is not None and date <= previous:
            raise ValueError("Dates must be unique and oldest first.")
        previous = date
        if not in_scope(date, target, following):
            raise ValueError(f"Date {date} is outside the requested source month/scope.")
        items = row["menus"]
        if legacy and items is None:
            pass
        elif not isinstance(items, list) or any(not isinstance(x, str) for x in items):
            raise ValueError(f"Menus for {date} must be a string array.")
        elif not legacy and (not items or any(not x.strip() or x != x.strip() for x in items)):
            raise ValueError(f"Menus for {date} must contain non-blank, trimmed source items.")
        if type(row["valid"]) is not bool:
            raise ValueError(f"Validity for {date} must be boolean.")
        if not legacy and row["valid"] != (len(items) >= 3):
            raise ValueError(f"New validity for {date} must equal menu count >= 3.")
    return rows


def build(transcription, target, following=False):
    if not isinstance(transcription, list) or not transcription:
        raise ValueError("Transcription must be a non-empty array.")
    rows = []
    for item in transcription:
        if not isinstance(item, dict) or set(item) != {"date", "menus"}:
            raise ValueError("Each transcription cell must contain exactly ISO date and menus.")
        raw_date = item["date"]
        if not isinstance(raw_date, str) or not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}", raw_date):
            raise ValueError("Transcription dates must be ISO YYYY-MM-DD strings.")
        date = dt.date.fromisoformat(raw_date)
        items = item["menus"]
        if not isinstance(items, list):
            raise ValueError("Transcription menus must be arrays.")
        rows.append({"date": [date.year, date.month, date.day], "menus": list(items), "valid": len(items) >= 3})
    rows.sort(key=lambda row: row["date"])
    return check_rows(rows, target, following)


def merge(base, patch):
    check_rows(base, legacy=True)
    check_rows(patch)
    result = {tuple(row["date"]): row for row in base}
    result.update({tuple(row["date"]): row for row in patch})
    return check_rows([result[key] for key in sorted(result)], legacy=True)


def write_new(path, rows, inputs=()):
    output = Path(path).expanduser()
    if output.resolve() in {Path(item).expanduser().resolve() for item in inputs}:
        raise ValueError("Output must differ from all source files.")
    with output.open("x", encoding="utf-8") as stream:
        json.dump(rows, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    return output.resolve()


def summary(rows):
    return {"records": len(rows), "valid_records": sum(row["valid"] for row in rows),
            "first_date": rows[0]["date"] if rows else None,
            "last_date": rows[-1]["date"] if rows else None}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    create = commands.add_parser("build", help="Convert visually reviewed ISO-date cells to operating JSON.")
    create.add_argument("--input", required=True)
    create.add_argument("--output", required=True)
    create.add_argument("--month", required=True)
    create.add_argument("--include-following-month", action="store_true")
    create.add_argument("--source-image")
    validate = commands.add_parser("validate", help="Validate new operating records, or a historical database.")
    validate.add_argument("--input", required=True)
    validate.add_argument("--month")
    validate.add_argument("--include-following-month", action="store_true")
    validate.add_argument("--legacy", action="store_true")
    combine = commands.add_parser("merge", help="Create a last-write-wins database candidate without modifying the base.")
    combine.add_argument("--base", required=True)
    combine.add_argument("--patch", required=True)
    combine.add_argument("--output", required=True)
    args = parser.parse_args()
    try:
        if args.command == "build":
            rows = build(load(args.input), month(args.month), args.include_following_month)
            result = summary(rows)
            if args.source_image:
                result["source_sha256"] = hashlib.sha256(Path(args.source_image).expanduser().read_bytes()).hexdigest()
            result["file"] = str(write_new(args.output, rows, [args.input] + ([args.source_image] if args.source_image else [])))
        elif args.command == "merge":
            rows = merge(load(args.base), load(args.patch))
            result = {**summary(rows), "file": str(write_new(args.output, rows, [args.base, args.patch]))}
        else:
            if args.include_following_month and not args.month:
                raise ValueError("--include-following-month requires --month.")
            rows = check_rows(load(args.input), month(args.month) if args.month else None,
                              args.include_following_month, args.legacy)
            result = {**summary(rows), "file": str(Path(args.input).expanduser().resolve()), "validated": True}
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 0
    except (ValueError, OSError, TypeError) as error:
        print("Error: " + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
