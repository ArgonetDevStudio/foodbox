#!/usr/bin/env python3
import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import textwrap
import time
import unittest


REPOSITORY = pathlib.Path(__file__).resolve().parents[1]
IMAGE = "ghcr.io/argonetdevstudio/foodbox@sha256:" + "a" * 64
PUBLIC_URL = "https://foodbox.example.com"
VALID_DATABASE = [{"date": [2026, 7, 25], "menus": ["soup", "main", "side"], "valid": True}]


class OperationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.temporary.name) / "foodbox"
        self.bin = pathlib.Path(self.temporary.name) / "bin"
        self.log = pathlib.Path(self.temporary.name) / "commands.log"
        self.root.mkdir()
        self.root = self.root.resolve()
        self.bin.mkdir()
        (self.root / "db").mkdir()
        (self.root / "deploy").mkdir()
        (self.root / ".incoming" / "test" / "deploy").mkdir(parents=True)
        (self.root / ".incoming" / "test" / "scripts").mkdir()
        (self.root / ".env").write_text("CLOVA_URL=https://example.com\n", encoding="utf-8")
        (self.root / "docker-compose.yml").write_text("services:\n  backend:\n    image: legacy:local\n", encoding="utf-8")
        (self.root / "db" / "db.json").write_text(json.dumps(VALID_DATABASE), encoding="utf-8")
        (self.root / ".mock-running").write_text("true", encoding="utf-8")
        stage = self.root / ".incoming" / "test"
        shutil.copy2(REPOSITORY / "docker-compose.yml", stage / "docker-compose.yml")
        shutil.copy2(REPOSITORY / "deploy" / "Caddyfile", stage / "deploy" / "Caddyfile")
        for name in ("deploy.sh", "rollback.sh", "job.sh", "db_snapshot.py", "db_restore.py",
                     "db_validate.py"):
            shutil.copy2(REPOSITORY / "scripts" / name, stage / "scripts" / name)
        self._write_mocks()

    def tearDown(self):
        self.temporary.cleanup()

    def _write_executable(self, name, source):
        path = self.bin / name
        path.write_text(textwrap.dedent(source).lstrip(), encoding="utf-8")
        path.chmod(0o755)

    def _write_mocks(self):
        self._write_executable("realpath", """
            #!/usr/bin/env python3
            import os, sys
            arguments = [argument for argument in sys.argv[1:] if argument != "-m"]
            print(os.path.realpath(arguments[-1]))
        """)
        self._write_executable("flock", """
            #!/usr/bin/env sh
            exit 0
        """)
        self._write_executable("mv", """
            #!/usr/bin/env python3
            import os, sys
            arguments = [argument for argument in sys.argv[1:] if argument != "-T"]
            os.execv("/bin/mv", ["mv", *arguments])
        """)
        self._write_executable("sudo", """
            #!/usr/bin/env python3
            import os, sys
            arguments = sys.argv[1:]
            if arguments == ["-n", "true"]:
                raise SystemExit(0)
            if arguments and arguments[0] == "-n":
                arguments = arguments[1:]
            if os.environ.get("MOCK_RESTORE_FAIL") and any(
                    argument.endswith("db_restore.py") for argument in arguments):
                raise SystemExit(1)
            os.execvp(arguments[0], arguments)
        """)
        self._write_executable("docker", """
            #!/usr/bin/env python3
            import json, os, pathlib, sys
            args = sys.argv[1:]
            root = pathlib.Path(os.environ["MOCK_ROOT"])
            with open(os.environ["MOCK_LOG"], "a", encoding="utf-8") as log:
                log.write("docker " + " ".join(args) + "\\n")
            if args and args[0] == "compose" and "config" in args and "--images" in args:
                print("ghcr.io/argonetdevstudio/foodbox@sha256:" + "a" * 64)
                print("caddy:2-alpine@sha256:" + "b" * 64)
            elif args and args[0] == "compose" and "stop" in args:
                count = root / ".stop-count"
                count.write_text(str(int(count.read_text()) + 1) if count.exists() else "1")
                if not os.environ.get("MOCK_STOP_UNCERTAIN"):
                    (root / ".mock-running").write_text("false")
            elif args and args[0] == "compose" and "ps" in args and "--quiet" in args:
                app_only = args[-1] == "app"
                print("cid-app")
                if app_only and os.environ.get("MOCK_TWO_WRITERS"):
                    print("cid-app-2")
                elif not app_only:
                    print("cid-caddy")
            elif args and args[0] == "compose" and "up" in args:
                count = root / ".up-count"
                current = int(count.read_text()) + 1 if count.exists() else 1
                count.write_text(str(current))
                (root / ".mock-running").write_text("true")
                if current == 1 and os.environ.get("MOCK_ADD_ON_FIRST_UP"):
                    rows = json.loads((root / "db" / "db.json").read_text())
                    rows.append({"date": [2026, 7, 26], "menus": ["new", "menu", "row"], "valid": True})
                    (root / "db" / "db.json").write_text(json.dumps(rows))
                if current == 1 and os.environ.get("MOCK_CORRUPT_ONCE"):
                    (root / "db" / "db.json").write_text("[]", encoding="utf-8")
                    (root / "db" / "metadata.json").write_text('{"lastImageHash":"bad"}', encoding="utf-8")
                if current == 1 and os.environ.get("MOCK_FAIL_FIRST_UP"):
                    raise SystemExit(1)
            elif args[:2] == ["inspect", "--format"]:
                template = args[2]
                if template == "{{.State.Running}}":
                    print((root / ".mock-running").read_text())
            raise SystemExit(0)
        """)
        self._write_executable("curl", """
            #!/usr/bin/env python3
            import json, os, pathlib, sys
            args = sys.argv[1:]
            output = pathlib.Path(args[args.index("--output") + 1])
            url = args[-1]
            root = pathlib.Path(os.environ["MOCK_ROOT"])
            if url.endswith("/healthz"):
                body = {"status": 200, "error": None, "data": {"ready": True}}
                output.write_text(json.dumps(body, separators=(",", ":")), encoding="utf-8")
            elif url.endswith("/api/menu"):
                rows = json.loads((root / "db" / "db.json").read_text(encoding="utf-8"))
                data = [{"date": f'{row["date"][0]:04d}-{row["date"][1]:02d}-{row["date"][2]:02d}',
                         "menus": row["menus"], "isValid": row["valid"]} for row in reversed(rows)]
                output.write_text(json.dumps({"status": 200, "error": None, "data": data},
                                             separators=(",", ":")), encoding="utf-8")
            else:
                output.write_text('<div id="app"></div>', encoding="utf-8")
            print("200", end="")
        """)

    def _environment(self, **flags):
        environment = os.environ.copy()
        environment.update({
            "FOODBOX_ROOT": str(self.root),
            "MOCK_ROOT": str(self.root),
            "MOCK_LOG": str(self.log),
            "PATH": str(self.bin) + os.pathsep + environment["PATH"],
        })
        for name, enabled in flags.items():
            if enabled:
                environment[name] = "1"
        return environment

    def _run_deploy(self, **flags):
        return subprocess.run(
            ["bash", str(REPOSITORY / "scripts" / "deploy.sh"), IMAGE, PUBLIC_URL,
             str(self.root / ".incoming" / "test")],
            env=self._environment(**flags), text=True, capture_output=True, timeout=30,
        )

    def _configure_go_release(self):
        old_image = "ghcr.io/argonetdevstudio/foodbox@sha256:" + "c" * 64
        shutil.copy2(REPOSITORY / "docker-compose.yml", self.root / "docker-compose.yml")
        shutil.copy2(REPOSITORY / "deploy" / "Caddyfile", self.root / "deploy" / "Caddyfile")
        (self.root / ".deploy.env").write_text(
            f"FOODBOX_IMAGE={old_image}\nDOMAIN=foodbox.example.com\n", encoding="utf-8")
        return old_image

    def _configure_rollback_release(self):
        current_image = self._configure_go_release()
        state = self.root / ".deploy-state"
        previous = state / "releases" / "release-previous"
        previous.mkdir(parents=True)
        shutil.copy2(REPOSITORY / "docker-compose.yml", previous / "docker-compose.yml")
        shutil.copy2(REPOSITORY / "deploy" / "Caddyfile", previous / "Caddyfile")
        previous_image = "ghcr.io/argonetdevstudio/foodbox@sha256:" + "d" * 64
        (previous / "deploy.env").write_text(
            f"FOODBOX_IMAGE={previous_image}\nDOMAIN=foodbox.example.com\n", encoding="utf-8")
        (state / "previous-release").write_text("release-previous\n", encoding="utf-8")
        scripts = self.root / "scripts"
        scripts.mkdir()
        for name in ("rollback.sh", "db_snapshot.py", "db_restore.py", "db_validate.py", "job.sh"):
            shutil.copy2(REPOSITORY / "scripts" / name, scripts / name)
        return current_image, previous_image

    def _run_rollback(self, **flags):
        return subprocess.run(
            ["bash", str(self.root / "scripts" / "rollback.sh"), PUBLIC_URL],
            env=self._environment(**flags), text=True, capture_output=True, timeout=30,
        )

    def test_first_cutover_stops_writer_after_snapshot_and_succeeds(self):
        result = self._run_deploy()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        commands = self.log.read_text(encoding="utf-8") if self.log.exists() else ""
        self.assertLess(commands.index(" stop --timeout"), commands.index("docker run"))
        snapshots = list((self.root / "backups").glob("db-*/manifest.json"))
        self.assertEqual(len(snapshots), 1)
        self.assertEqual(json.loads((self.root / "db" / "db.json").read_text()), VALID_DATABASE)
        self.assertFalse((self.root / ".deploy-state" / "previous-release").exists())

    def test_invalid_database_fails_before_stop_or_uid_change(self):
        (self.root / "db" / "db.json").write_text("{}", encoding="utf-8")
        result = self._run_deploy()
        self.assertEqual(result.returncode, 20, result.stdout + result.stderr)
        commands = self.log.read_text(encoding="utf-8") if self.log.exists() else ""
        self.assertNotIn("compose stop", commands)
        self.assertNotIn("docker run", commands)
        self.assertFalse(list((self.root / ".deploy-state").glob("transaction.*")))

    def test_deploy_blocks_unresolved_transactions_before_any_changes(self):
        state = self.root / ".deploy-state"
        state.mkdir()
        self.root.joinpath(".env").chmod(0o640)
        original_config = {
            path: path.read_bytes() if path.exists() else None
            for path in (
                self.root / ".env",
                self.root / "docker-compose.yml",
                self.root / "deploy" / "Caddyfile",
                self.root / ".deploy.env",
            )
        }
        original_database = (self.root / "db" / "db.json").read_bytes()

        for marker_name in ("transaction.crashed", "rollback.crashed"):
            with self.subTest(marker=marker_name):
                marker = state / marker_name
                marker.symlink_to(state / "missing-transaction-state")
                existing_markers = sorted(
                    path.name for pattern in ("transaction.*", "rollback.*")
                    for path in state.glob(pattern)
                )

                result = self._run_deploy()

                self.assertEqual(result.returncode, 20, result.stdout + result.stderr)
                self.assertIn("An unresolved deployment or rollback transaction exists", result.stderr)
                self.assertFalse(self.log.exists())
                self.assertFalse(list((self.root / "backups").glob("db-*")))
                self.assertFalse(list((state / "preflight-backups").glob("db-*")))
                self.assertEqual((self.root / "db" / "db.json").read_bytes(), original_database)
                for path, content in original_config.items():
                    self.assertEqual(path.read_bytes() if path.exists() else None, content)
                self.assertEqual(self.root.joinpath(".env").stat().st_mode & 0o777, 0o640)
                self.assertEqual(sorted(
                    path.name for pattern in ("transaction.*", "rollback.*")
                    for path in state.glob(pattern)
                ), existing_markers)
                marker.unlink()

    def test_first_cutover_corruption_restores_snapshot_and_stays_stopped(self):
        result = self._run_deploy(MOCK_CORRUPT_ONCE=True)
        self.assertEqual(result.returncode, 12, result.stdout + result.stderr)
        self.assertEqual(json.loads((self.root / "db" / "db.json").read_text()), VALID_DATABASE)
        self.assertFalse((self.root / "db" / "metadata.json").exists())
        rejected = list((self.root / "backups").glob("rejected-db-*/*"))
        self.assertTrue(any(path.name == "metadata.json" for path in rejected))
        self.assertEqual((self.root / ".mock-running").read_text(), "false")

    def test_first_cutover_failure_restores_config_existence_and_starts_no_runtime(self):
        original_compose = (self.root / "docker-compose.yml").read_bytes()
        original_caddy = b"legacy caddy configuration\n"
        (self.root / "deploy" / "Caddyfile").write_bytes(original_caddy)
        result = self._run_deploy(MOCK_FAIL_FIRST_UP=True)
        self.assertEqual(result.returncode, 12, result.stdout + result.stderr)
        self.assertEqual((self.root / "docker-compose.yml").read_bytes(), original_compose)
        self.assertEqual((self.root / "deploy" / "Caddyfile").read_bytes(), original_caddy)
        self.assertFalse((self.root / ".deploy.env").exists())
        self.assertEqual(json.loads((self.root / "db" / "db.json").read_text()), VALID_DATABASE)
        self.assertEqual((self.root / ".mock-running").read_text(), "false")
        self.assertFalse(list((self.root / ".deploy-state").glob("transaction.*")))

    def test_deploy_stop_uncertainty_exits_11_without_starting_target(self):
        result = self._run_deploy(MOCK_STOP_UNCERTAIN=True)
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)
        self.assertFalse((self.root / ".up-count").exists())
        self.assertEqual((self.root / ".stop-count").read_text(), "2")

    def test_deploy_never_reports_recovered_when_writer_count_is_uncertain(self):
        self._configure_go_release()
        result = self._run_deploy(MOCK_FAIL_FIRST_UP=True, MOCK_TWO_WRITERS=True)
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)

    def test_go_to_go_failure_preserves_added_rows_and_recovers_exact_release(self):
        old_image = self._configure_go_release()
        result = self._run_deploy(MOCK_ADD_ON_FIRST_UP=True, MOCK_FAIL_FIRST_UP=True)
        self.assertEqual(result.returncode, 10, result.stdout + result.stderr)
        rows = json.loads((self.root / "db" / "db.json").read_text())
        self.assertEqual(len(rows), 2)
        self.assertEqual(rows[1]["date"], [2026, 7, 26])
        self.assertIn(f"FOODBOX_IMAGE={old_image}", (self.root / ".deploy.env").read_text())
        self.assertEqual((self.root / ".mock-running").read_text(), "true")
        self.assertFalse(list((self.root / ".deploy-state").glob("transaction.*")))

    def test_go_to_go_corruption_restores_snapshot_before_recovery(self):
        old_image = self._configure_go_release()
        result = self._run_deploy(MOCK_CORRUPT_ONCE=True)
        self.assertEqual(result.returncode, 10, result.stdout + result.stderr)
        self.assertEqual(json.loads((self.root / "db" / "db.json").read_text()), VALID_DATABASE)
        self.assertFalse((self.root / "db" / "metadata.json").exists())
        self.assertIn(f"FOODBOX_IMAGE={old_image}", (self.root / ".deploy.env").read_text())

    def test_manual_rollback_failure_restores_starting_release(self):
        current_image, _ = self._configure_rollback_release()
        result = self._run_rollback(MOCK_FAIL_FIRST_UP=True)
        self.assertEqual(result.returncode, 10, result.stdout + result.stderr)
        self.assertIn(f"FOODBOX_IMAGE={current_image}", (self.root / ".deploy.env").read_text())
        self.assertEqual((self.root / ".mock-running").read_text(), "true")
        self.assertFalse(list((self.root / ".deploy-state").glob("rollback.*")))

    def test_manual_rollback_corruption_restores_snapshot_and_starting_release(self):
        current_image, _ = self._configure_rollback_release()
        result = self._run_rollback(MOCK_CORRUPT_ONCE=True)
        self.assertEqual(result.returncode, 10, result.stdout + result.stderr)
        self.assertEqual(json.loads((self.root / "db" / "db.json").read_text()), VALID_DATABASE)
        self.assertIn(f"FOODBOX_IMAGE={current_image}", (self.root / ".deploy.env").read_text())

    def test_manual_rollback_restore_failure_exits_11_and_leaves_transaction(self):
        self._configure_rollback_release()
        result = self._run_rollback(MOCK_CORRUPT_ONCE=True, MOCK_RESTORE_FAIL=True)
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)
        self.assertEqual((self.root / ".mock-running").read_text(), "false")
        self.assertTrue(list((self.root / ".deploy-state").glob("rollback.*")))

    def test_manual_rollback_blocks_unresolved_transaction(self):
        self._configure_rollback_release()
        (self.root / ".deploy-state" / "transaction.crashed").mkdir()
        result = self._run_rollback()
        self.assertEqual(result.returncode, 20, result.stdout + result.stderr)
        self.assertFalse((self.root / ".stop-count").exists())

    def test_manual_rollback_blocks_unresolved_rollback(self):
        self._configure_rollback_release()
        (self.root / ".deploy-state" / "rollback.crashed").mkdir()
        result = self._run_rollback()
        self.assertEqual(result.returncode, 20, result.stdout + result.stderr)
        self.assertFalse((self.root / ".stop-count").exists())

    def test_manual_rollback_stop_uncertainty_exits_11_without_starting_target(self):
        self._configure_rollback_release()
        result = self._run_rollback(MOCK_STOP_UNCERTAIN=True)
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)
        self.assertFalse((self.root / ".up-count").exists())
        self.assertEqual((self.root / ".stop-count").read_text(), "2")

    def test_manual_rollback_success_updates_reciprocal_pointer(self):
        _, previous_image = self._configure_rollback_release()
        result = self._run_rollback()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(f"FOODBOX_IMAGE={previous_image}", (self.root / ".deploy.env").read_text())
        pointer = (self.root / ".deploy-state" / "previous-release").read_text().strip()
        self.assertRegex(pointer, r"^release-[A-Za-z0-9]+$")
        self.assertNotEqual(pointer, "release-previous")
        reciprocal = self.root / ".deploy-state" / "releases" / pointer / "deploy.env"
        self.assertTrue(reciprocal.is_file())

    def test_snapshot_rejects_malformed_and_symlinked_metadata(self):
        backups = self.root / "backups"
        backups.mkdir()
        metadata = self.root / "db" / "metadata.json"
        metadata.write_text('{"lastImageHash":', encoding="utf-8")
        malformed = subprocess.run([
            "python3", str(REPOSITORY / "scripts" / "db_snapshot.py"),
            str(self.root / "db"), str(backups), str(os.getuid()), str(os.getgid()),
        ], capture_output=True)
        self.assertNotEqual(malformed.returncode, 0)
        metadata.unlink()
        metadata.symlink_to(self.root / "db" / "db.json")
        symlinked = subprocess.run([
            "python3", str(REPOSITORY / "scripts" / "db_snapshot.py"),
            str(self.root / "db"), str(backups), str(os.getuid()), str(os.getgid()),
        ], capture_output=True)
        self.assertNotEqual(symlinked.returncode, 0)

    @unittest.skipUnless(pathlib.Path("/proc/self/cmdline").is_file(), "durable jobs require Linux /proc")
    def test_detached_job_is_idempotent_and_crash_state_is_terminal(self):
        scripts = self.root / "scripts"
        scripts.mkdir()
        for name in ("job.sh", "db_snapshot.py", "db_restore.py", "db_validate.py"):
            shutil.copy2(REPOSITORY / "scripts" / name, scripts / name)
        runner = scripts / "test-runner.sh"
        runner.write_text("#!/usr/bin/env bash\necho run >>\"$FOODBOX_ROOT/job-runs\"\n", encoding="utf-8")
        runner.chmod(0o755)
        command = ["bash", str(scripts / "job.sh"), "start", "rollback-idempotent",
                   "rollback", PUBLIC_URL, str(runner)]
        first = subprocess.run(command, env=self._environment(), text=True, capture_output=True)
        self.assertEqual(first.returncode, 0, first.stdout + first.stderr)
        status_command = ["bash", str(self.root / ".deploy-state" / "jobs" /
                                      "rollback-idempotent" / "job.sh"),
                          "status", "rollback-idempotent"]
        status = None
        for _ in range(100):
            status = subprocess.run(status_command, env=self._environment(), text=True,
                                    capture_output=True)
            if status.stdout.strip().startswith("EXIT:"):
                break
            time.sleep(0.05)
        self.assertEqual(status.stdout.strip(), "EXIT:0", status.stdout + status.stderr)
        repeated = subprocess.run(command, env=self._environment(), text=True, capture_output=True)
        self.assertEqual(repeated.returncode, 0, repeated.stdout + repeated.stderr)
        self.assertEqual((self.root / "job-runs").read_text().splitlines(), ["run"])

        crash = self.root / ".deploy-state" / "jobs" / "deploy-crashed"
        crash.mkdir()
        shutil.copy2(REPOSITORY / "scripts" / "job.sh", crash / "job.sh")
        (crash / "pid").write_text("99999999\n", encoding="utf-8")
        crashed = subprocess.run(
            ["bash", str(crash / "job.sh"), "status", "deploy-crashed"],
            env=self._environment(), text=True, capture_output=True)
        self.assertEqual(crashed.returncode, 0, crashed.stdout + crashed.stderr)
        self.assertEqual(crashed.stdout.strip(), "EXIT:21")
        self.assertEqual((crash / "status").read_text().strip(), "21")

        launch_crash = self.root / ".deploy-state" / "jobs" / "deploy-launch-crashed"
        launch_crash.mkdir()
        shutil.copy2(REPOSITORY / "scripts" / "job.sh", launch_crash / "job.sh")
        (launch_crash / "launching").write_text("launching\n", encoding="utf-8")
        launch_status = subprocess.run(
            ["bash", str(launch_crash / "job.sh"), "status", "deploy-launch-crashed"],
            env=self._environment(), text=True, capture_output=True)
        self.assertEqual(launch_status.returncode, 0, launch_status.stdout + launch_status.stderr)
        self.assertEqual(launch_status.stdout.strip(), "EXIT:21")

    def test_blank_metadata_and_known_transient_file_are_valid(self):
        backups = self.root / "backups"
        backups.mkdir()
        snapshot = subprocess.check_output([
            "python3", str(REPOSITORY / "scripts" / "db_snapshot.py"),
            str(self.root / "db"), str(backups), str(os.getuid()), str(os.getgid()),
        ], text=True).strip()
        (self.root / "db" / "metadata.json").write_text("  \n", encoding="utf-8")
        (self.root / "db" / ".foodbox-writable-check-test").write_text("check", encoding="utf-8")
        subprocess.run([
            "python3", str(REPOSITORY / "scripts" / "db_validate.py"), snapshot,
            str(self.root / "db"),
        ], check=True)

    def test_malformed_or_symlinked_metadata_is_rejected(self):
        backups = self.root / "backups"
        backups.mkdir()
        snapshot = subprocess.check_output([
            "python3", str(REPOSITORY / "scripts" / "db_snapshot.py"),
            str(self.root / "db"), str(backups), str(os.getuid()), str(os.getgid()),
        ], text=True).strip()
        metadata = self.root / "db" / "metadata.json"
        metadata.write_text('{"lastImageHash":', encoding="utf-8")
        malformed = subprocess.run([
            "python3", str(REPOSITORY / "scripts" / "db_validate.py"), snapshot,
            str(self.root / "db"),
        ], capture_output=True)
        self.assertNotEqual(malformed.returncode, 0)
        metadata.unlink()
        metadata.symlink_to(self.root / "db" / "db.json")
        symlinked = subprocess.run([
            "python3", str(REPOSITORY / "scripts" / "db_validate.py"), snapshot,
            str(self.root / "db"),
        ], capture_output=True)
        self.assertNotEqual(symlinked.returncode, 0)


if __name__ == "__main__":
    unittest.main()
