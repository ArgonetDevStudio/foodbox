#!/usr/bin/env python3
import json
import pathlib
import re
import subprocess
import time
import unittest
import urllib.error
import urllib.request
import uuid


REPOSITORY = pathlib.Path(__file__).resolve().parents[1]
CADDYFILE_DIRECTORY = REPOSITORY / "deploy"


def caddy_image():
    compose = (REPOSITORY / "docker-compose.yml").read_text(encoding="utf-8")
    match = re.search(r"(?ms)^  caddy:\n(?P<body>(?:^    .*(?:\n|$))*)", compose)
    if not match:
        raise RuntimeError("docker-compose.yml does not define the caddy service")
    image = re.search(r"(?m)^    image:\s*([^\s#]+)", match.group("body"))
    if not image:
        raise RuntimeError("the caddy service does not define an image")
    return image.group(1)


def run_docker(*arguments, check=True):
    return subprocess.run(
        ["docker", *arguments],
        check=check,
        text=True,
        capture_output=True,
        timeout=90,
    )


class CaddyTests(unittest.TestCase):
    def test_admin_token_is_absent_from_access_logs(self):
        image = caddy_image()
        mount = f"{CADDYFILE_DIRECTORY}:/etc/caddy:ro"
        validation = run_docker(
            "run", "--rm", "--env", "DOMAIN=foodbox.example.com",
            "--volume", mount, image,
            "caddy", "validate", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile",
        )
        self.assertIn("Valid configuration", validation.stdout + validation.stderr)

        container = f"foodbox-caddy-log-test-{uuid.uuid4().hex[:12]}"
        secret = f"admin-secret-{uuid.uuid4().hex}"
        marker = f"visible-marker-{uuid.uuid4().hex}"
        started = False
        try:
            run_docker(
                "run", "--detach", "--rm", "--name", container,
                "--publish", "127.0.0.1::80", "--env", "DOMAIN=:80",
                "--volume", mount, image,
                "caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile",
            )
            started = True

            deadline = time.monotonic() + 20
            status = None
            while time.monotonic() < deadline:
                published = run_docker("port", container, "80/tcp", check=False)
                if published.returncode == 0 and published.stdout.strip():
                    port = published.stdout.strip().rsplit(":", 1)[-1]
                    request = urllib.request.Request(
                        f"http://127.0.0.1:{port}/api/crawl",
                        headers={"X-Admin-Token": secret, "X-Caddy-Test": marker},
                    )
                    try:
                        with urllib.request.urlopen(request, timeout=2) as response:
                            status = response.status
                    except urllib.error.HTTPError as error:
                        status = error.code
                    except urllib.error.URLError:
                        time.sleep(0.1)
                        continue
                    break
                time.sleep(0.1)
            self.assertEqual(status, 404, "Caddy did not return the protected-route response")

            access_entry = None
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                logs = run_docker("logs", container, check=False)
                for line in (logs.stdout + logs.stderr).splitlines():
                    try:
                        entry = json.loads(line)
                    except json.JSONDecodeError:
                        continue
                    request_data = entry.get("request", {})
                    if request_data.get("uri") == "/api/crawl":
                        access_entry = entry
                if access_entry is not None:
                    break
                time.sleep(0.1)

            self.assertIsNotNone(access_entry, "Caddy did not emit the expected access log")
            headers = access_entry["request"].get("headers", {})
            normalized_headers = {name.lower(): values for name, values in headers.items()}
            self.assertIn(marker, normalized_headers.get("x-caddy-test", []))
            self.assertNotIn("x-admin-token", normalized_headers)
            self.assertNotIn(secret, json.dumps(access_entry, sort_keys=True))
        finally:
            if started:
                run_docker("rm", "--force", container, check=False)


if __name__ == "__main__":
    unittest.main()
