#!/usr/bin/env python3
"""Find one local profile background original and download a selected discovery candidate."""

import argparse
import hashlib
import http.client
import ipaddress
import json
import os
from pathlib import Path
import re
import shutil
import socket
import struct
import subprocess
import sys
import tempfile
import urllib.parse

MAX_IMAGE_BYTES = 10 * 1024 * 1024
RESOURCE_PATH = re.compile(r"/profile_resource/[0-9a-f]{64}/f\.(?:jpg|jpeg|png)\Z")
DEFAULT_MANIFEST = Path.home() / ".cache" / "foodbox" / "menu-original" / "candidates.json"
DECODER = json.JSONDecoder(strict=False)


def validate_url(url):
    parsed = urllib.parse.urlsplit(url)
    if (parsed.scheme != "https" or not parsed.hostname or parsed.username or parsed.password
            or parsed.query or parsed.fragment or parsed.port is not None
            or not RESOURCE_PATH.fullmatch(parsed.path)
            or not re.fullmatch(r"[a-z0-9]+(?:[.-][a-z0-9]+)*", parsed.hostname)
            or parsed.hostname in {"localhost", "localhost.localdomain"}
            or parsed.hostname.endswith((".localhost", ".local", ".internal"))):
        raise ValueError("Expected a public HTTPS profile_resource original URL without credentials, port, query or fragment.")
    try:
        address = ipaddress.ip_address(parsed.hostname)
    except ValueError:
        address = None
    if address is not None and not address.is_global:
        raise ValueError("Non-public image addresses are refused.")
    return url


def valid_url(url):
    try:
        validate_url(url)
        return True
    except (ValueError, TypeError):
        return False


def background_urls(record):
    """Walk a possibly reused JSON buffer; never search arbitrary neighboring URLs."""
    stack = []
    found = []
    pending_key = None
    identity_keys = set()
    position = 0
    while position < len(record):
        char = record[position]
        if char == '"':
            try:
                value, length = DECODER.raw_decode(record[position:])
            except ValueError:
                break
            # Once the intact URL is established, reused string storage in a
            # later field does not invalidate that prefix's advisory evidence.
            if found and any(ord(character) < 32 or character == "\ufffd" for character in value):
                return found
            end = position + length
            after = end
            while after < len(record) and record[after].isspace():
                after += 1
            if after < len(record) and record[after] == ":":
                if not stack or stack[-1][0] != "{":
                    return []
                pending_key = value
                if stack == [("{", None)] and value in {"userId", "nickname", "suspended"}:
                    if value in identity_keys:
                        return []
                    identity_keys.add(value)
                position = after + 1
            else:
                if stack == [("{", None), ("{", "backgroundImage")] and pending_key == "originalUrl":
                    try:
                        found.append(validate_url(value))
                    except ValueError:
                        pass
                pending_key = None
                position = end
        elif char in "{[":
            stack.append((char, pending_key))
            pending_key = None
            position += 1
        elif char in "}]":
            if not stack or stack[-1][0] != {"}": "{", "]": "["}[char]:
                return []
            stack.pop()
            if not stack:
                return found
            pending_key = None
            position += 1
        elif char == ",":
            pending_key = None
            position += 1
        else:
            position += 1
    return found


def extract_candidates(contexts, user_id, nickname):
    # The exact target header is essential when the rest of a response was reused.
    header = re.compile(
        r'\{"userId"\s*:\s*' + str(user_id)
        + r'\s*,\s*"suspended"\s*:\s*(?:false|true)\s*,\s*"nickname"\s*:\s*'
        + re.escape(json.dumps(nickname, ensure_ascii=False)) + r"\s*,"
    )
    candidates = {}
    for context in contexts:
        text = context.decode("utf-8", errors="replace")
        for match in header.finditer(text):
            record = text[match.start():match.start() + 8192]
            # Do not cross into another profile response, even if this one is truncated.
            next_record = re.search(r'\{"userId"\s*:', record[1:])
            if next_record:
                record = record[:next_record.start() + 1]
            # Reused buffers must not mix a target header with a later identity.
            # Conservatively reject repeated identity keys even in nested data.
            if any(len(re.findall(r'(?<!\\)"' + key + r'"\s*:', record)) != 1
                   for key in ("userId", "nickname", "suspended")):
                continue
            if len(re.findall(r'(?<!\\)"backgroundImage"\s*:', record)) != 1:
                continue
            source = "partial-response"
            try:
                obj, _ = json.JSONDecoder().raw_decode(record)
                if obj.get("userId") == user_id and obj.get("nickname") == nickname:
                    image = obj.get("backgroundImage")
                    if isinstance(image, dict):
                        url = image.get("originalUrl", "")
                        if isinstance(url, str) and valid_url(url):
                            candidates[url] = "complete-response"
                    continue
            except ValueError:
                pass
            for url in background_urls(record):
                candidates.setdefault(url, source)
    return [{"url": url, "evidence": evidence} for url, evidence in sorted(candidates.items())]


def decode_contexts(data):
    position = 0
    contexts = []
    while position < len(data):
        if len(data) - position < 4:
            raise ValueError("Truncated scanner output.")
        length = struct.unpack_from("<I", data, position)[0]
        position += 4
        if not 0 < length <= 32768 or position + length > len(data) or len(contexts) >= 128:
            raise ValueError("Invalid scanner output bounds.")
        contexts.append(data[position:position + length])
        position += length
    return contexts


def discover(user_id, nickname, wine_lib_dir, window_title=None):
    compiler = shutil.which("winegcc")
    if not compiler:
        raise ValueError("winegcc is required; this command is for the existing Linux/Wine desktop.")
    source = Path(__file__).with_name("profile_scan.c")
    environment = dict(os.environ, WINEDEBUG="-all")
    with tempfile.TemporaryDirectory(prefix="foodbox-profile-") as temporary:
        executable = Path(temporary) / "profile_scan"
        command = [compiler, "-m64", "-Wall", "-Wextra"]
        if wine_lib_dir:
            command.append("-L" + wine_lib_dir)
        command += ["-o", str(executable), str(source), "-luser32", "-lkernel32"]
        build = subprocess.run(command, capture_output=True, env=environment, timeout=30)
        if build.returncode:
            raise ValueError("Wine helper compilation failed; check winegcc and --wine-lib-dir (see runbook).")
        scan = subprocess.run(
            [str(executable) + ".exe", nickname.encode("utf-8").hex(),
             (window_title or nickname).encode("utf-8").hex()],
            capture_output=True, env=environment, timeout=45,
        )
        if scan.returncode:
            raise ValueError("Read-only profile scan failed; open the target photo viewer or pass --window-title and retry (see runbook).")
        candidates = extract_candidates(decode_contexts(scan.stdout), user_id, nickname)
    if not candidates:
        raise ValueError("No target-linked background original URL found. Reopen the target profile and retry; see runbook.")
    return {"user_id": user_id, "nickname": nickname, "candidates": candidates}


def save_manifest(result, path):
    path = Path(path).expanduser()
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    payload = json.dumps({"version": 1, **result}, ensure_ascii=False, indent=2).encode()
    flags = os.O_WRONLY | os.O_CREAT | os.O_TRUNC | getattr(os, "O_NOFOLLOW", 0)
    with os.fdopen(os.open(path, flags, 0o600), "wb") as file:
        os.fchmod(file.fileno(), 0o600)
        file.write(payload)
    return str(path.resolve())


def selected_candidate(path, number):
    with Path(path).expanduser().open("rb") as file:
        data = file.read(32769)
    if len(data) > 32768:
        raise ValueError("Discovery manifest exceeds its size limit.")
    manifest = json.loads(data)
    if (not isinstance(manifest, dict) or manifest.get("version") != 1
            or not isinstance(manifest.get("user_id"), int) or manifest["user_id"] <= 0
            or not isinstance(manifest.get("nickname"), str) or not manifest["nickname"]
            or not isinstance(manifest.get("candidates"), list)
            or not 1 <= number <= len(manifest["candidates"])):
        raise ValueError("Invalid discovery manifest or candidate number.")
    candidate = manifest["candidates"][number - 1]
    if (not isinstance(candidate, dict) or candidate.get("evidence") not in
            {"complete-response", "partial-response"} or not isinstance(candidate.get("url"), str)):
        raise ValueError("Invalid discovery candidate.")
    return validate_url(candidate["url"])


class PublicHTTPSConnection(http.client.HTTPSConnection):
    def connect(self):
        addresses = socket.getaddrinfo(self.host, 443, type=socket.SOCK_STREAM)
        if not addresses or any(not ipaddress.ip_address(item[4][0]).is_global for item in addresses):
            raise ValueError("Non-public image addresses are refused.")
        # Pin the checked address while retaining TLS hostname verification.
        raw = socket.create_connection((addresses[0][4][0], 443), self.timeout)
        try:
            self.sock = self._context.wrap_socket(raw, server_hostname=self.host)
        except BaseException:
            raw.close()
            raise


def image_dimensions(data):
    if data.startswith(b"\x89PNG\r\n\x1a\n") and len(data) >= 24 and data[12:16] == b"IHDR":
        width, height = struct.unpack_from(">II", data, 16)
        if width and height:
            return width, height
        raise ValueError("PNG dimensions must be positive.")
    if not data.startswith(b"\xff\xd8"):
        raise ValueError("Response is not a supported JPEG or PNG image.")
    position = 2
    while position + 4 <= len(data):
        if data[position] != 0xff:
            break
        while position < len(data) and data[position] == 0xff:
            position += 1
        if position >= len(data):
            break
        marker = data[position]
        position += 1
        length = int.from_bytes(data[position:position + 2], "big")
        if length < 2 or position + length > len(data):
            break
        if marker in {0xc0, 0xc1, 0xc2, 0xc3, 0xc5, 0xc6, 0xc7, 0xc9, 0xca, 0xcb, 0xcd, 0xce, 0xcf} and length >= 8:
            height, width = struct.unpack_from(">HH", data, position + 3)
            if width and height:
                return width, height
        position += length
    raise ValueError("JPEG dimensions were not found; no file was written.")


def download(url, output, connection_factory=None):
    validate_url(url)
    parsed = urllib.parse.urlsplit(url)
    connection = (connection_factory or PublicHTTPSConnection)(parsed.hostname, timeout=20)
    try:
        connection.request("GET", parsed.path, headers={"User-Agent": "foodbox-local-menu/1"})
        response = connection.getresponse()
        if response.status != 200:
            raise ValueError(f"Image returned HTTP {response.status}; redirects are refused, rediscover if expired.")
        data = response.read(MAX_IMAGE_BYTES + 1)
    finally:
        connection.close()
    if len(data) > MAX_IMAGE_BYTES:
        raise ValueError("Image exceeds the 10 MiB limit.")
    width, height = image_dimensions(data)
    output = Path(output).expanduser()
    with output.open("xb") as file:
        file.write(data)
    return {"file": str(output.resolve()), "bytes": len(data), "width": width, "height": height,
            "sha256": hashlib.sha256(data).hexdigest()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    scan = commands.add_parser("discover", help="Read one local profile's background URL; does not download.")
    scan.add_argument("--user-id", type=int, default=282181357)
    scan.add_argument("--nickname", default="이소도시락")
    scan.add_argument("--window-title", help="Exact runtime title; default is the target nickname photo viewer.")
    scan.add_argument("--manifest", default=str(DEFAULT_MANIFEST))
    scan.add_argument("--wine-lib-dir", default="/opt/wine-stable/lib/wine/x86_64-unix")
    fetch = commands.add_parser("download", help="Download an explicitly selected original URL.")
    fetch.add_argument("--manifest", default=str(DEFAULT_MANIFEST))
    fetch.add_argument("--candidate", type=int, required=True)
    fetch.add_argument("--output", required=True)
    arguments = parser.parse_args()
    try:
        if arguments.command == "discover":
            if arguments.user_id <= 0 or not arguments.nickname or len(arguments.nickname.encode()) > 256:
                raise ValueError("Expected a positive user ID and a non-empty nickname of at most 256 UTF-8 bytes.")
            if arguments.window_title and len(arguments.window_title.encode("utf-8")) > 256:
                raise ValueError("Window title exceeds 256 UTF-8 bytes.")
            result = discover(arguments.user_id, arguments.nickname, arguments.wine_lib_dir, arguments.window_title)
            result["manifest"] = save_manifest(result, arguments.manifest)
        else:
            result = download(selected_candidate(arguments.manifest, arguments.candidate), arguments.output)
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return 0
    except (ValueError, OSError, http.client.HTTPException, subprocess.SubprocessError) as error:
        if isinstance(error, FileExistsError):
            message = "Output already exists; choose another filename."
        elif isinstance(error, (http.client.HTTPException, ConnectionError, TimeoutError)):
            message = "Image request failed; check connectivity and rediscover the current URL."
        else:
            message = str(error)
        print("Error: " + message, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
