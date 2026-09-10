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
        self._stage_deploy_bundle(self.root / ".incoming" / "test")
        self._write_mocks()

    def _stage_deploy_bundle(self, stage):
        (stage / "deploy").mkdir(parents=True, exist_ok=True)
        (stage / "scripts").mkdir(exist_ok=True)
        shutil.copy2(REPOSITORY / "docker-compose.yml", stage / "docker-compose.yml")
        shutil.copy2(REPOSITORY / "deploy" / "Caddyfile", stage / "deploy" / "Caddyfile")
        for name in ("deploy.sh", "rollback.sh", "job.sh", "db_snapshot.py", "db_restore.py",
                     "db_validate.py"):
            shutil.copy2(REPOSITORY / "scripts" / name, stage / "scripts" / name)

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
        self._write_executable("rmdir", """
            #!/usr/bin/env python3
            import os, pathlib, sys
            if os.environ.get("MOCK_CLEANUP_FAIL") and any(
                    pathlib.Path(argument).name.startswith(("transaction.", "rollback."))
                    for argument in sys.argv[1:]):
                raise SystemExit(1)
            os.execv("/bin/rmdir", ["rmdir", *sys.argv[1:]])
        """)
        self._write_executable("rm", """
            #!/usr/bin/env python3
            import os, pathlib, subprocess, sys
            result = subprocess.run(["/bin/rm", *sys.argv[1:]], check=False)
            if os.environ.get("MOCK_RM_CLEANUP_FAIL") and any(
                    pathlib.Path(argument).parent.name.startswith(("transaction.", "rollback."))
                    for argument in sys.argv[1:] if not argument.startswith("-")):
                raise SystemExit(1)
            raise SystemExit(result.returncode)
        """)
        self._write_executable("sudo", """
            #!/usr/bin/env python3
            import os, pathlib, sys
            arguments = sys.argv[1:]
            if arguments == ["-n", "true"]:
                raise SystemExit(0)
            if arguments and arguments[0] == "-n":
                arguments = arguments[1:]
            if any(argument.endswith("db_snapshot.py") for argument in arguments):
                root = pathlib.Path(os.environ["MOCK_ROOT"])
                counter = root / ".snapshot-count"
                current = int(counter.read_text()) + 1 if counter.exists() else 1
                counter.write_text(str(current))
                if str(current) == os.environ.get("MOCK_SNAPSHOT_FAIL_AT"):
                    raise SystemExit(1)
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
            elif args and args[0] == "compose" and "config" in args and "--services" in args:
                compose_path = pathlib.Path(args[args.index("-f") + 1])
                in_services = False
                for line in compose_path.read_text(encoding="utf-8").splitlines():
                    if line == "services:":
                        in_services = True
                        continue
                    if in_services and line and not line.startswith(" "):
                        break
                    if (in_services and line.startswith("  ")
                            and not line.startswith("    ") and line.endswith(":")):
                        print(line.strip()[:-1])
            elif args and args[0] == "compose" and "stop" in args:
                count = root / ".stop-count"
                count.write_text(str(int(count.read_text()) + 1) if count.exists() else "1")
                if not os.environ.get("MOCK_STOP_UNCERTAIN"):
                    (root / ".mock-running").write_text("false")
            elif args and args[0] == "compose" and "ps" in args and "--quiet" in args:
                service = args[-1] if args[-1] not in ("--quiet", "--all") else ""
                if service in ("app", "backend"):
                    print("cid-app")
                elif service:
                    print("cid-" + service)
                else:
                    print("cid-app")
                    print("cid-caddy")
                    if os.environ.get("MOCK_RUNNING_ORPHAN"):
                        print("cid-orphan")
                if service == "app" and os.environ.get("MOCK_TWO_WRITERS"):
                    print("cid-app-2")
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
                if current == 1 and os.environ.get("MOCK_MUTATE_ON_FIRST_UP"):
                    rows = json.loads((root / "db" / "db.json").read_text())
                    rows[0]["menus"][0] = "mutated"
                    (root / "db" / "db.json").write_text(json.dumps(rows), encoding="utf-8")
                if current == 1 and os.environ.get("MOCK_DELETE_ON_FIRST_UP"):
                    (root / "db" / "db.json").unlink()
                if current == 1 and os.environ.get("MOCK_INVALID_ON_FIRST_UP"):
                    (root / "db" / "db.json").write_text("{", encoding="utf-8")
                if current == 1 and os.environ.get("MOCK_DAMAGE_PERSISTENT_FILES_ON_FIRST_UP"):
                    (root / "db" / "db.backup.json").unlink()
                    (root / "db" / "metadata.json").write_text(
                        '{"lastImageHash":7}', encoding="utf-8")
                    (root / "db" / "future state.bin").write_bytes(b"damaged")
                if current == 1 and os.environ.get("MOCK_FAIL_FIRST_UP"):
                    raise SystemExit(1)
            elif args[:2] == ["inspect", "--format"]:
                template = args[2]
                if template == "{{.State.Running}}":
                    if args[-1] == "cid-orphan" and os.environ.get("MOCK_RUNNING_ORPHAN"):
                        print("true")
                    else:
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
                         "menus": row["menus"] if row["menus"] is not None else [],
                         "isValid": row["valid"]} for row in reversed(rows)]
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
        for name, value in flags.items():
            if value:
                environment[name] = "1" if value is True else str(value)
        return environment

    def _run_deploy(self, **flags):
        return subprocess.run(
            ["bash", str(REPOSITORY / "scripts" / "deploy.sh"), IMAGE, PUBLIC_URL,
             str(self.root / ".incoming" / "test")],
            env=self._environment(**flags), text=True, capture_output=True, timeout=30,
        )

    def _run_operation_with_root(self, script, root):
        environment = self._environment()
        environment["FOODBOX_ROOT"] = root
        arguments = ["bash", str(REPOSITORY / "scripts" / script)]
        if script == "deploy.sh":
            arguments.extend([IMAGE, PUBLIC_URL, str(self.root / ".incoming" / "test")])
        elif script == "rollback.sh":
            arguments.append(PUBLIC_URL)
        else:
            arguments.extend(["status", "path-validation-test"])
        return subprocess.run(
            arguments, env=environment, text=True, capture_output=True, timeout=30,
        )

    def _root_snapshot(self):
        snapshot = []
        for path in sorted(self.root.rglob("*")):
            relative = str(path.relative_to(self.root))
            mode = path.lstat().st_mode
            if path.is_symlink():
                contents = os.readlink(path)
            elif path.is_file():
                contents = path.read_bytes()
            else:
                contents = None
            snapshot.append((relative, mode, contents))
        return snapshot

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

    def test_unsafe_deployment_roots_fail_before_filesystem_or_docker_changes(self):
        unsafe_roots = (
            "/",
            "/srv",
            str(self.root.parent / "safe" / ".." / "foodbox"),
            str(self.root) + "\n/etc",
            str(self.root) + "//nested",
        )
        expected_error = (
            "FOODBOX_ROOT must be a canonical absolute path with at least two components."
        )

        for script in ("deploy.sh", "rollback.sh", "job.sh"):
            for unsafe_root in unsafe_roots:
                with self.subTest(script=script, root=repr(unsafe_root)):
                    before = self._root_snapshot()
                    result = self._run_operation_with_root(script, unsafe_root)
                    self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
                    self.assertIn(expected_error, result.stderr)
                    self.assertEqual(self._root_snapshot(), before)
                    self.assertFalse(self.log.exists())

    def test_canonical_multicomponent_deployment_root_passes_path_validation(self):
        result = self._run_deploy()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("FOODBOX_ROOT must be", result.stderr)

        before = self._root_snapshot()
        result = self._run_operation_with_root("job.sh", str(self.root))
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertEqual(result.stdout.strip(), "UNKNOWN")
        self.assertNotIn("FOODBOX_ROOT must be", result.stderr)
        self.assertEqual(self._root_snapshot(), before)

    def test_workflows_validate_deployment_root_before_filesystem_setup(self):
        path_guard = "if [[ ! $DEPLOY_PATH =~ ^/[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)+$ ]]"
        canonical_guard = '[[ $(realpath -m -- "$DEPLOY_PATH") != "$DEPLOY_PATH" ]]'

        for workflow in ("deploy.yml", "rollback.yml"):
            with self.subTest(workflow=workflow):
                contents = (REPOSITORY / ".github" / "workflows" / workflow).read_text(
                    encoding="utf-8"
                )
                self.assertIn("DEPLOY_PATH: ${{ vars.DEPLOY_PATH || '/home/ubuntu/foodbox' }}", contents)
                self.assertIn(path_guard, contents)
                self.assertIn(canonical_guard, contents)
                self.assertLess(contents.index(path_guard), contents.index('mkdir -p "$HOME/.ssh"'))

    def test_workflow_durable_job_ids_are_scoped_to_run_attempt(self):
        deploy = (REPOSITORY / ".github" / "workflows" / "deploy.yml").read_text(
            encoding="utf-8"
        )
        rollback = (REPOSITORY / ".github" / "workflows" / "rollback.yml").read_text(
            encoding="utf-8"
        )

        self.assertIn(
            "printf 'DEPLOY_JOB_ID=deploy-%s-attempt-%s\\n' \"$GITHUB_RUN_ID\" \"$GITHUB_RUN_ATTEMPT\"",
            deploy,
        )
        self.assertIn(
            "printf 'DEPLOY_STAGE_ID=deploy-%s-attempt-%s\\n' \"$GITHUB_RUN_ID\" \"$GITHUB_RUN_ATTEMPT\"",
            deploy,
        )
        self.assertIn(
            "printf 'ROLLBACK_JOB_ID=rollback-%s-attempt-%s\\n' \"$GITHUB_RUN_ID\" \"$GITHUB_RUN_ATTEMPT\"",
            rollback,
        )
        self.assertNotIn("DEPLOY_JOB_ID=deploy-%s\\n", deploy)
        self.assertNotIn("ROLLBACK_JOB_ID=rollback-%s\\n", rollback)

        self.assertNotEqual("deploy-123-attempt-1", "deploy-123-attempt-2")
        self.assertNotEqual("rollback-123-attempt-1", "rollback-123-attempt-2")

    def test_first_cutover_stops_writer_after_snapshot_and_succeeds(self):
        result = self._run_deploy()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        commands = self.log.read_text(encoding="utf-8") if self.log.exists() else ""
        self.assertLess(commands.index(" stop --timeout"), commands.index("docker run"))
        snapshots = list((self.root / "backups").glob("db-*/manifest.json"))
        self.assertEqual(len(snapshots), 1)
        self.assertEqual(json.loads((self.root / "db" / "db.json").read_text()), VALID_DATABASE)
        self.assertFalse((self.root / ".deploy-state" / "previous-release").exists())

    def test_deploy_preserves_legacy_null_menus_and_api_parity(self):
        legacy = [
            {"date": [2025, 5, 5], "menus": None, "valid": False},
            {"date": [2026, 7, 25], "menus": ["soup", "main", "side"], "valid": True},
        ]
        database = self.root / "db" / "db.json"
        database.write_text(json.dumps(legacy), encoding="utf-8")
        original = database.read_bytes()

        result = self._run_deploy()

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(database.read_bytes(), original)
        self.assertEqual(json.loads(database.read_text(encoding="utf-8")), legacy)

    def test_first_cutover_ignores_running_orphan_outside_starting_compose(self):
        result = self._run_deploy(MOCK_RUNNING_ORPHAN=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.root / ".mock-running").read_text(), "true")

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

    def test_manual_rollback_final_snapshot_failure_restarts_only_after_baseline_check(self):
        current_image, _ = self._configure_rollback_release()
        result = self._run_rollback(MOCK_SNAPSHOT_FAIL_AT=2)
        self.assertEqual(result.returncode, 10, result.stdout + result.stderr)
        self.assertEqual(json.loads((self.root / "db" / "db.json").read_text()), VALID_DATABASE)
        self.assertIn(f"FOODBOX_IMAGE={current_image}", (self.root / ".deploy.env").read_text())
        self.assertEqual((self.root / ".mock-running").read_text(), "true")
        self.assertEqual((self.root / ".snapshot-count").read_text(), "2")
        self.assertEqual(len(list((self.root / "backups").glob("db-*/manifest.json"))), 1)
        self.assertFalse(list((self.root / ".deploy-state").glob("rollback.*")))

    def _assert_final_snapshot_restart_damage_is_restored(self, damage_flag):
        backup_contents = b"preserved backup\n"
        metadata_contents = b'{"lastImageHash":"preserved"}\n'
        custom_contents = b"opaque future state\x00\xff"
        (self.root / "db" / "db.backup.json").write_bytes(backup_contents)
        (self.root / "db" / "metadata.json").write_bytes(metadata_contents)
        (self.root / "db" / "future state.bin").write_bytes(custom_contents)
        self._configure_rollback_release()
        result = self._run_rollback(
            MOCK_SNAPSHOT_FAIL_AT=2,
            MOCK_DAMAGE_PERSISTENT_FILES_ON_FIRST_UP=True,
            **{damage_flag: True},
        )
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)
        self.assertEqual(json.loads((self.root / "db" / "db.json").read_text()), VALID_DATABASE)
        self.assertEqual((self.root / "db" / "db.backup.json").read_bytes(), backup_contents)
        self.assertEqual((self.root / "db" / "metadata.json").read_bytes(), metadata_contents)
        self.assertEqual((self.root / "db" / "future state.bin").read_bytes(), custom_contents)
        self.assertEqual(
            sorted(path.name for path in (self.root / "db").iterdir()),
            ["db.backup.json", "db.json", "future state.bin", "metadata.json"],
        )
        self.assertEqual((self.root / ".mock-running").read_text(), "false")
        self.assertEqual((self.root / ".snapshot-count").read_text(), "2")
        self.assertEqual(len(list((self.root / "backups").glob("db-*/manifest.json"))), 1)
        self.assertTrue(list((self.root / ".deploy-state").glob("rollback.*")))
        self.assertIn("original database snapshot was restored exactly", result.stderr)

    def test_manual_rollback_final_snapshot_failure_restores_restart_mutation(self):
        self._assert_final_snapshot_restart_damage_is_restored("MOCK_MUTATE_ON_FIRST_UP")

    def test_manual_rollback_final_snapshot_failure_restores_restart_deletion(self):
        self._assert_final_snapshot_restart_damage_is_restored("MOCK_DELETE_ON_FIRST_UP")

    def test_manual_rollback_final_snapshot_failure_restores_restart_corruption(self):
        self._assert_final_snapshot_restart_damage_is_restored("MOCK_INVALID_ON_FIRST_UP")

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

    def test_rollback_preserves_legacy_null_menus_and_api_parity(self):
        current_image, previous_image = self._configure_rollback_release()
        legacy = [
            {"date": [2025, 5, 5], "menus": None, "valid": False},
            {"date": [2026, 7, 25], "menus": ["soup", "main", "side"], "valid": True},
        ]
        database = self.root / "db" / "db.json"
        database.write_text(json.dumps(legacy), encoding="utf-8")
        original = database.read_bytes()

        result = self._run_rollback()

        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(f"FOODBOX_IMAGE={previous_image}", (self.root / ".deploy.env").read_text())
        self.assertNotEqual(previous_image, current_image)
        self.assertEqual(database.read_bytes(), original)
        self.assertEqual(json.loads(database.read_text(encoding="utf-8")), legacy)

    def test_successful_deploy_cleanup_failure_exits_11(self):
        result = self._run_deploy(MOCK_CLEANUP_FAIL=True)
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)
        self.assertEqual((self.root / ".mock-running").read_text(), "true")
        self.assertTrue(list((self.root / ".deploy-state").glob("transaction.*")))

    def test_recovered_deploy_cleanup_failure_exits_11(self):
        self._configure_go_release()
        result = self._run_deploy(MOCK_FAIL_FIRST_UP=True, MOCK_CLEANUP_FAIL=True)
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)
        self.assertEqual((self.root / ".mock-running").read_text(), "true")
        self.assertTrue(list((self.root / ".deploy-state").glob("transaction.*")))

    def test_successful_rollback_cleanup_failure_exits_11(self):
        self._configure_rollback_release()
        result = self._run_rollback(MOCK_CLEANUP_FAIL=True)
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)
        self.assertEqual((self.root / ".mock-running").read_text(), "true")
        self.assertTrue(list((self.root / ".deploy-state").glob("rollback.*")))

    def test_recovered_rollback_cleanup_failure_exits_11(self):
        self._configure_rollback_release()
        result = self._run_rollback(MOCK_FAIL_FIRST_UP=True, MOCK_CLEANUP_FAIL=True)
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)
        self.assertEqual((self.root / ".mock-running").read_text(), "true")
        self.assertTrue(list((self.root / ".deploy-state").glob("rollback.*")))

    def test_deploy_rm_cleanup_failure_cannot_be_masked_by_successful_rmdir(self):
        result = self._run_deploy(MOCK_RM_CLEANUP_FAIL=True)
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)
        self.assertFalse(list((self.root / ".deploy-state").glob("transaction.*")))

    def test_early_deploy_cleanup_rm_failure_exits_11(self):
        self._configure_go_release()
        (self.root / "deploy" / "Caddyfile").unlink()
        result = self._run_deploy(MOCK_RM_CLEANUP_FAIL=True)
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)
        self.assertFalse((self.root / ".stop-count").exists())
        self.assertFalse(list((self.root / ".deploy-state").glob("transaction.*")))

    def test_rollback_rm_cleanup_failure_cannot_be_masked_by_successful_rmdir(self):
        self._configure_rollback_release()
        result = self._run_rollback(MOCK_RM_CLEANUP_FAIL=True)
        self.assertEqual(result.returncode, 11, result.stdout + result.stderr)
        self.assertFalse(list((self.root / ".deploy-state").glob("rollback.*")))

    def test_job_rejects_symlinked_stage_children_without_path_escape(self):
        scripts = self.root / "scripts"
        scripts.mkdir()
        shutil.copy2(REPOSITORY / "scripts" / "job.sh", scripts / "job.sh")
        stage = self.root / ".incoming" / "test"
        shutil.rmtree(stage / "deploy")
        victim = pathlib.Path(self.temporary.name) / "victim"
        victim.mkdir()
        victim_file = victim / "Caddyfile"
        victim_file.write_text("must remain\n", encoding="utf-8")
        (stage / "deploy").symlink_to(victim, target_is_directory=True)

        result = subprocess.run([
            "bash", str(scripts / "job.sh"), "start", "deploy-path-escape", "deploy",
            IMAGE, PUBLIC_URL, str(stage),
        ], env=self._environment(), text=True, capture_output=True, timeout=30)

        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertEqual(victim_file.read_text(encoding="utf-8"), "must remain\n")
        self.assertFalse((self.root / ".up-count").exists())

        (stage / "deploy").unlink()
        (stage / "deploy").mkdir()
        shutil.copy2(REPOSITORY / "deploy" / "Caddyfile", stage / "deploy" / "Caddyfile")
        staged_deploy = stage / "scripts" / "deploy.sh"
        staged_deploy.unlink()
        staged_deploy.symlink_to(victim_file)
        file_result = subprocess.run([
            "bash", str(scripts / "job.sh"), "start", "deploy-file-path-escape", "deploy",
            IMAGE, PUBLIC_URL, str(stage),
        ], env=self._environment(), text=True, capture_output=True, timeout=30)
        self.assertEqual(file_result.returncode, 2, file_result.stdout + file_result.stderr)
        self.assertEqual(victim_file.read_text(encoding="utf-8"), "must remain\n")
        self.assertFalse((self.root / ".up-count").exists())

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

    def test_snapshot_and_validation_handle_legacy_null_menus_and_reject_invalid_types(self):
        backups = self.root / "backups"
        backups.mkdir()
        database = self.root / "db" / "db.json"
        legacy = [
            {"date": [2025, 5, 5], "menus": None, "valid": False},
            {"date": [2025, 5, 6], "menus": ["soup", "main", "side"], "valid": True},
        ]
        original = json.dumps(legacy, separators=(",", ":")).encode()
        database.write_bytes(original)

        snapshot = pathlib.Path(subprocess.check_output([
            "python3", str(REPOSITORY / "scripts" / "db_snapshot.py"),
            str(self.root / "db"), str(backups), str(os.getuid()), str(os.getgid()),
        ], text=True).strip())
        self.assertEqual((snapshot / "data" / "db.json").read_bytes(), original)
        subprocess.run([
            "python3", str(REPOSITORY / "scripts" / "db_validate.py"), "--exact",
            str(snapshot), str(self.root / "db"),
        ], check=True)

        for menus in ({"unexpected": "object"}, ["soup", 7]):
            with self.subTest(menus=menus):
                database.write_text(json.dumps([{
                    "date": [2025, 5, 5], "menus": menus, "valid": False,
                }]), encoding="utf-8")
                snapshot_result = subprocess.run([
                    "python3", str(REPOSITORY / "scripts" / "db_snapshot.py"),
                    str(self.root / "db"), str(backups), str(os.getuid()), str(os.getgid()),
                ], text=True, capture_output=True)
                self.assertNotEqual(snapshot_result.returncode, 0)
                validate_result = subprocess.run([
                    "python3", str(REPOSITORY / "scripts" / "db_validate.py"),
                    str(snapshot), str(self.root / "db"),
                ], text=True, capture_output=True)
                self.assertNotEqual(validate_result.returncode, 0)

    def test_snapshot_and_restore_preserve_every_safe_regular_file_exactly(self):
        backups = self.root / "backups"
        backups.mkdir()
        custom = self.root / "db" / "future state.bin"
        custom.write_bytes(b"opaque persistent state\x00\xff")
        custom.chmod(0o640)
        snapshot = subprocess.check_output([
            "python3", str(REPOSITORY / "scripts" / "db_snapshot.py"),
            str(self.root / "db"), str(backups), str(os.getuid()), str(os.getgid()),
        ], text=True).strip()
        manifest = json.loads((pathlib.Path(snapshot) / "manifest.json").read_text())
        self.assertEqual(
            sorted(item["name"] for item in manifest["files"]),
            ["db.json", "future state.bin"],
        )

        custom.write_bytes(b"damaged")
        (self.root / "db" / "unexpected-new.dat").write_bytes(b"quarantine me")
        subprocess.run([
            "python3", str(REPOSITORY / "scripts" / "db_restore.py"), snapshot,
            str(self.root / "db"), str(backups),
        ], check=True)

        self.assertEqual(
            sorted(path.name for path in (self.root / "db").iterdir()),
            ["db.json", "future state.bin"],
        )
        self.assertEqual(custom.read_bytes(), b"opaque persistent state\x00\xff")
        self.assertEqual(custom.stat().st_mode & 0o777, 0o640)
        self.assertTrue(any(
            (path / "unexpected-new.dat").read_bytes() == b"quarantine me"
            for path in backups.glob("rejected-db-*")
            if (path / "unexpected-new.dat").is_file()
        ))
        subprocess.run([
            "python3", str(REPOSITORY / "scripts" / "db_validate.py"), "--exact",
            snapshot, str(self.root / "db"),
        ], check=True)

    def test_snapshot_rejects_unsafe_names_symlinks_hardlinks_and_special_files(self):
        backups = self.root / "backups"
        backups.mkdir()
        snapshot_command = [
            "python3", str(REPOSITORY / "scripts" / "db_snapshot.py"),
            str(self.root / "db"), str(backups), str(os.getuid()), str(os.getgid()),
        ]
        outside = pathlib.Path(self.temporary.name) / "outside"
        outside.write_text("outside", encoding="utf-8")

        unsafe_entries = (
            ("symlink", lambda path: path.symlink_to(outside)),
            ("hardlink", lambda path: os.link(self.root / "db" / "db.json", path)),
            ("fifo", lambda path: os.mkfifo(path)),
            ("unsafe-name", lambda path: path.write_text("unsafe", encoding="utf-8")),
        )
        for kind, create in unsafe_entries:
            with self.subTest(kind=kind):
                name = "unsafe\nname" if kind == "unsafe-name" else f"unsafe-{kind}"
                path = self.root / "db" / name
                create(path)
                result = subprocess.run(snapshot_command, text=True, capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                path.unlink()

    def test_restore_rejects_manifest_path_traversal_before_live_changes(self):
        backups = self.root / "backups"
        backups.mkdir()
        snapshot = pathlib.Path(subprocess.check_output([
            "python3", str(REPOSITORY / "scripts" / "db_snapshot.py"),
            str(self.root / "db"), str(backups), str(os.getuid()), str(os.getgid()),
        ], text=True).strip())
        manifest_path = snapshot / "manifest.json"
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        manifest["files"][0]["name"] = "../escape"
        manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
        original = (self.root / "db" / "db.json").read_bytes()

        result = subprocess.run([
            "python3", str(REPOSITORY / "scripts" / "db_restore.py"), str(snapshot),
            str(self.root / "db"), str(backups),
        ], text=True, capture_output=True)

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.root / "db" / "db.json").read_bytes(), original)
        self.assertFalse((self.root / "escape").exists())

    def test_restore_quarantines_unsafe_live_entries_without_following_them(self):
        backups = self.root / "backups"
        backups.mkdir()
        metadata = self.root / "db" / "metadata.json"
        metadata.write_text('{"lastImageHash":"original"}\n', encoding="utf-8")
        snapshot = subprocess.check_output([
            "python3", str(REPOSITORY / "scripts" / "db_snapshot.py"),
            str(self.root / "db"), str(backups), str(os.getuid()), str(os.getgid()),
        ], text=True).strip()
        outside = pathlib.Path(self.temporary.name) / "outside-live"
        outside.write_text("outside must remain", encoding="utf-8")

        corruptions = (
            ("symlink", lambda: (metadata.unlink(), metadata.symlink_to(outside))),
            ("hardlink", lambda: (metadata.unlink(), os.link(outside, metadata))),
            ("fifo", lambda: os.mkfifo(self.root / "db" / "rogue.fifo")),
        )
        for kind, corrupt in corruptions:
            with self.subTest(kind=kind):
                corrupt()
                result = subprocess.run([
                    "python3", str(REPOSITORY / "scripts" / "db_restore.py"), snapshot,
                    str(self.root / "db"), str(backups),
                ], text=True, capture_output=True)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(outside.read_text(encoding="utf-8"), "outside must remain")
                self.assertEqual(
                    sorted(path.name for path in (self.root / "db").iterdir()),
                    ["db.json", "metadata.json"],
                )
                subprocess.run([
                    "python3", str(REPOSITORY / "scripts" / "db_validate.py"), "--exact",
                    snapshot, str(self.root / "db"),
                ], check=True)

    @unittest.skipUnless(pathlib.Path("/proc/self/cmdline").is_file(), "durable jobs require Linux /proc")
    def test_deploy_job_clears_operation_owned_stage_on_start_and_reattach(self):
        scripts = self.root / "scripts"
        scripts.mkdir()
        shutil.copy2(REPOSITORY / "scripts" / "job.sh", scripts / "job.sh")
        stage = self.root / ".incoming" / "test"
        command = [
            "bash", str(scripts / "job.sh"), "start", "deploy-stage-cleanup", "deploy",
            IMAGE, PUBLIC_URL, str(stage),
        ]

        first = subprocess.run(command, env=self._environment(), text=True, capture_output=True)
        self.assertEqual(first.returncode, 0, first.stdout + first.stderr)
        self.assertFalse(stage.exists())

        status_command = [
            "bash", str(self.root / ".deploy-state" / "jobs" / "deploy-stage-cleanup" / "job.sh"),
            "status", "deploy-stage-cleanup",
        ]
        status = None
        for _ in range(200):
            status = subprocess.run(
                status_command, env=self._environment(), text=True, capture_output=True)
            if status.stdout.strip().startswith("EXIT:"):
                break
            time.sleep(0.05)
        self.assertEqual(status.stdout.strip(), "EXIT:0", status.stdout + status.stderr)

        self._stage_deploy_bundle(stage)
        repeated = subprocess.run(command, env=self._environment(), text=True, capture_output=True)
        self.assertEqual(repeated.returncode, 0, repeated.stdout + repeated.stderr)
        self.assertFalse(stage.exists())
        self.assertEqual((self.root / ".up-count").read_text(), "1")

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

    @unittest.skipUnless(pathlib.Path("/proc/self/cmdline").is_file(), "durable jobs require Linux /proc")
    def test_detached_status_waits_for_terminal_publication(self):
        scripts = self.root / "scripts"
        scripts.mkdir()
        for name in ("job.sh", "db_snapshot.py", "db_restore.py", "db_validate.py"):
            shutil.copy2(REPOSITORY / "scripts" / name, scripts / name)
        runner = scripts / "test-runner.sh"
        runner.write_text("#!/usr/bin/env bash\n", encoding="utf-8")
        runner.chmod(0o755)

        self._write_executable("flock", """
            #!/usr/bin/env python3
            import os
            import sys

            os.execv("/usr/bin/flock", ["flock", *sys.argv[1:]])
        """)
        self._write_executable("mv", """
            #!/usr/bin/env python3
            import os
            import pathlib
            import sys
            import time

            arguments = sys.argv[1:]
            destination = pathlib.Path(arguments[-1])
            if destination.name == "status" and os.environ.get("BLOCK_STATUS_MOVE"):
                blocked = pathlib.Path(os.environ["STATUS_MOVE_BLOCKED"])
                if not blocked.exists():
                    pathlib.Path(os.environ["STATUS_MOVE_READY"]).write_text("ready", encoding="utf-8")
                    blocked.write_text("blocked", encoding="utf-8")
                    while not pathlib.Path(os.environ["STATUS_MOVE_RELEASE"]).exists():
                        time.sleep(0.001)
            os.execv("/bin/mv", ["mv", *arguments])
        """)
        environment = self._environment()
        environment.update({
            "BLOCK_STATUS_MOVE": "1",
            "STATUS_MOVE_BLOCKED": str(self.root / ".status-move-blocked"),
            "STATUS_MOVE_READY": str(self.root / ".status-move-ready"),
            "STATUS_MOVE_RELEASE": str(self.root / ".status-move-release"),
        })
        job_id = "rollback-status-publication"
        command = ["bash", str(scripts / "job.sh"), "start", job_id, "rollback", PUBLIC_URL, str(runner)]
        first = subprocess.run(command, env=environment, text=True, capture_output=True)
        self.assertEqual(first.returncode, 0, first.stdout + first.stderr)
        ready = self.root / ".status-move-ready"
        for _ in range(200):
            if ready.exists():
                break
            time.sleep(0.01)
        self.assertTrue(ready.exists(), "the detached runner did not reach terminal publication")

        pid_file = self.root / ".deploy-state" / "jobs" / job_id / "pid"
        pid_file.write_text("99999999\n", encoding="utf-8")
        status_command = ["bash", str(self.root / ".deploy-state" / "jobs" / job_id / "job.sh"),
                          "status", job_id]
        status_process = subprocess.Popen(
            status_command, env=environment, text=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        )
        completed_before_release = False
        try:
            try:
                stdout, stderr = status_process.communicate(timeout=0.5)
                completed_before_release = True
            except subprocess.TimeoutExpired:
                (self.root / ".status-move-release").write_text("release", encoding="utf-8")
                stdout, stderr = status_process.communicate(timeout=5)
        finally:
            (self.root / ".status-move-release").write_text("release", encoding="utf-8")
            if status_process.poll() is None:
                status_process.kill()
                status_process.wait(timeout=5)

        self.assertFalse(completed_before_release, stdout + stderr)
        self.assertEqual(status_process.returncode, 0, stdout + stderr)
        self.assertEqual(stdout.strip(), "EXIT:0")

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
