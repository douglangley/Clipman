import argparse
import hashlib
import io
import json
import os
import tempfile
import tarfile
import unittest
import zipfile
from pathlib import Path
from unittest import mock

import clipman_server_updater as updater


class ClipmanServerUpdaterTests(unittest.TestCase):
    def write_native_archive(self, path: Path, version: str = "3.0.0", binary: bytes = b"native-go-server") -> None:
        manifest = json.dumps({
            "format_version": 2,
            "name": "Clipman Server",
            "version": version,
            "artifacts": [{
                "os": "linux",
                "architecture": updater.native_linux_architecture(),
                "path": "bin/clipman-server",
                "sha256": hashlib.sha256(binary).hexdigest(),
            }],
        }).encode("utf-8")
        with tarfile.open(path, "w:gz") as output:
            for name, data, mode in (
                ("package/manifest-v2.json", manifest, 0o644),
                ("package/bin/clipman-server", binary, 0o755),
            ):
                info = tarfile.TarInfo(name)
                info.size = len(data)
                info.mode = mode
                output.addfile(info, io.BytesIO(data))

    def test_runit_service_backup_excludes_live_supervision_state(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            service = root / "service" / "clipman-server"
            backup = root / "backup" / "clipman-server"
            supervise = service / "supervise"
            supervise.mkdir(parents=True)
            (service / "run").write_text("persistent service definition", encoding="utf-8")
            (supervise / "status").write_text("live runtime state", encoding="utf-8")
            if hasattr(os, "mkfifo"):
                os.mkfifo(supervise / "ok")
                os.mkfifo(supervise / "control")

            updater.copy_service_artifact(service, backup, "runit")

            self.assertEqual(
                "persistent service definition",
                (backup / "run").read_text(encoding="utf-8"),
            )
            self.assertFalse((backup / "supervise").exists())

    def test_restore_program_files_supports_runit_service_directories(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "app"
            helper = root / "bin" / "clipmanserver"
            launcher = root / "bin" / "clipman-server"
            service = root / "service" / "clipman-server"
            backup = root / "backup"
            update_service = service.parent / "clipman-server-update"
            for path in (
                app,
                service,
                update_service,
                backup / "app",
                backup / "services" / "clipman-server",
                backup / "services" / "clipman-server-update",
            ):
                path.mkdir(parents=True)
            helper.parent.mkdir(parents=True, exist_ok=True)
            (app / "clipman_server.py").write_text("changed", encoding="utf-8")
            helper.write_text("changed helper", encoding="utf-8")
            launcher.write_text("changed launcher", encoding="utf-8")
            (service / "run").write_text("changed run", encoding="utf-8")
            (service / "stale").write_text("remove me", encoding="utf-8")
            (service / "supervise").mkdir()
            (service / "supervise" / "status").write_text("live server state", encoding="utf-8")
            (update_service / "run").write_text("changed update run", encoding="utf-8")
            (update_service / "supervise").mkdir()
            (update_service / "supervise" / "status").write_text("live updater state", encoding="utf-8")
            (backup / "app" / "clipman_server.py").write_text("original", encoding="utf-8")
            (backup / "clipmanserver").write_text("original helper", encoding="utf-8")
            (backup / "clipman-server").write_text("original launcher", encoding="utf-8")
            (backup / "services" / "clipman-server" / "run").write_text("original run", encoding="utf-8")
            (backup / "services" / "clipman-server" / "down").touch()
            (backup / "services" / "clipman-server-update" / "run").write_text(
                "original update run", encoding="utf-8"
            )
            (backup / "services" / "clipman-server-update" / "down").touch()

            updater.restore_program_files(app, helper, launcher, service, backup, "runit")

            self.assertEqual("original", (app / "clipman_server.py").read_text(encoding="utf-8"))
            self.assertEqual("original helper", helper.read_text(encoding="utf-8"))
            self.assertEqual("original launcher", launcher.read_text(encoding="utf-8"))
            self.assertEqual("original run", (service / "run").read_text(encoding="utf-8"))
            self.assertTrue((service / "down").is_file())
            self.assertFalse((service / "stale").exists())
            self.assertEqual(
                "live server state",
                (service / "supervise" / "status").read_text(encoding="utf-8"),
            )
            self.assertEqual("original update run", (update_service / "run").read_text(encoding="utf-8"))
            self.assertTrue((update_service / "down").is_file())
            self.assertEqual(
                "live updater state",
                (update_service / "supervise" / "status").read_text(encoding="utf-8"),
            )

    def test_versions_and_release_asset_are_selected_numerically(self):
        release = {
            "tag_name": "server-v2.10.0",
            "assets": [
                {
                    "name": "ClipmanServer-2.10.0.zip",
                    "browser_download_url": "https://example.test/ClipmanServer-2.10.0.zip",
                }
            ],
        }
        version, asset = updater.find_update([release], "2.9.9")
        self.assertEqual("2.10.0", version)
        self.assertEqual("ClipmanServer-2.10.0.zip", asset["name"])
        self.assertIsNone(updater.find_update([release], "2.10.0"))

    def test_bridge_selects_same_version_native_asset(self):
        release = {
            "tag_name": "server-v2.10.0",
            "assets": [
                {"name": "ClipmanServer-2.10.0.zip", "browser_download_url": "https://example.test/combined.zip"},
                {"name": "ClipmanServer-Linux-amd64-2.10.0.tar.gz", "browser_download_url": "https://example.test/native.tar.gz"},
            ],
        }
        version, asset = updater.find_update(
            [release], "2.10.0", prefer_native=True, allow_same_version_native=True, machine="x86_64"
        )
        self.assertEqual("2.10.0", version)
        self.assertEqual("ClipmanServer-Linux-amd64-2.10.0.tar.gz", asset["name"])
        self.assertTrue(asset["_clipman_native"])

    def test_bridge_falls_back_to_transition_asset_for_newer_release(self):
        release = {
            "tag_name": "server-v2.11.0",
            "assets": [{"name": "ClipmanServer-2.11.0.zip", "browser_download_url": "https://example.test/combined.zip"}],
        }
        version, asset = updater.find_update([release], "2.10.0", prefer_native=True, machine="x86_64")
        self.assertEqual("2.11.0", version)
        self.assertFalse(asset["_clipman_native"])

    def test_historical_two_cycle_bridge_selects_transition_then_same_version_native(self):
        release = {
            "tag_name": "server-v2.4.3",
            "assets": [
                {"name": "ClipmanServer-2.4.3.zip", "browser_download_url": "https://example.test/transition.zip"},
                {
                    "name": f"ClipmanServer-Linux-{updater.native_linux_architecture()}-2.4.3.tar.gz",
                    "browser_download_url": "https://example.test/native.tar.gz",
                },
            ],
        }
        bridge_version, transition = updater.find_update([release], "2.4.0")
        self.assertEqual("2.4.3", bridge_version)
        self.assertEqual("ClipmanServer-2.4.3.zip", transition["name"])
        native_version, native = updater.find_update(
            [release], bridge_version, prefer_native=True, allow_same_version_native=True
        )
        self.assertEqual(bridge_version, native_version)
        self.assertTrue(native["_clipman_native"])

    def test_native_tar_manifest_and_digest_are_validated(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root / "native.tar.gz"
            binary = b"native-go-server"
            manifest = json.dumps({
                "format_version": 2,
                "name": "Clipman Server",
                "version": "3.0.0",
                "artifacts": [{"os": "linux", "architecture": "amd64", "path": "bin/clipman-server", "sha256": hashlib.sha256(binary).hexdigest()}],
            }).encode("utf-8")
            with tarfile.open(archive, "w:gz") as output:
                for name, data in (("package/manifest-v2.json", manifest), ("package/bin/clipman-server", binary)):
                    info = tarfile.TarInfo(name)
                    info.size = len(data)
                    info.mode = 0o755
                    output.addfile(info, io.BytesIO(data))
            extracted = root / "extracted"
            updater.safe_extract(archive, extracted)
            package_root = updater.locate_native_package_root(extracted, "3.0.0", machine="x86_64")
            self.assertEqual(binary, updater.native_server_artifact(package_root, "x86_64").read_bytes())

    def test_native_tar_symlink_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            archive = Path(temporary) / "unsafe.tar.gz"
            with tarfile.open(archive, "w:gz") as output:
                info = tarfile.TarInfo("package/link")
                info.type = tarfile.SYMTYPE
                info.linkname = "../../outside"
                output.addfile(info)
            with self.assertRaisesRegex(RuntimeError, "unsafe entry type"):
                updater.safe_extract(archive, Path(temporary) / "output")

    def test_managed_native_bridge_can_restore_python_launcher_without_touching_settings(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            package = root / "package"
            app = root / "app"
            launcher = root / "bin" / "clipman-server"
            config = root / "config" / "settings.json"
            package.mkdir()
            app.mkdir()
            launcher.parent.mkdir()
            config.parent.mkdir()
            binary = b"native-go-server"
            native = package / "bin" / "clipman-server"
            native.parent.mkdir()
            native.write_bytes(binary)
            (package / "manifest-v2.json").write_text(json.dumps({
                "format_version": 2, "name": "Clipman Server", "version": "3.0.0",
                "artifacts": [{"os": "linux", "architecture": "amd64", "path": "bin/clipman-server", "sha256": hashlib.sha256(binary).hexdigest()}],
            }), encoding="utf-8")
            python_server = app / "clipman_server.py"
            python_server.write_text("legacy python server", encoding="utf-8")
            launcher.write_text("#!/bin/sh\nexec python3 legacy.py\n", encoding="utf-8")
            settings = b'{"AuthToken":"preserved","Port":61234}'
            config.write_bytes(settings)
            snapshots = updater.snapshot_program_paths([app / "clipman-server", launcher])
            updater.install_managed_native(package, app, launcher, config)
            self.assertEqual(binary, (app / "clipman-server").read_bytes())
            self.assertNotIn("python3", launcher.read_text(encoding="utf-8"))
            updater.restore_managed_program_files(snapshots)
            self.assertFalse((app / "clipman-server").exists())
            self.assertIn("python3", launcher.read_text(encoding="utf-8"))
            self.assertEqual(settings, config.read_bytes())
            self.assertEqual("legacy python server", python_server.read_text(encoding="utf-8"))

    def test_historical_standard_install_transitions_to_native_without_touching_persistent_state(self):
        with tempfile.TemporaryDirectory(prefix="clipman historical 'standard' ") as temporary:
            root = Path(temporary)
            app = root / "app"
            bin_dir = root / "bin"
            state = root / "state"
            service = root / "systemd" / "clipman-server.service"
            for directory in (app, bin_dir, state / "tls", state / "databases", service.parent):
                directory.mkdir(parents=True, exist_ok=True)
            config = state / "clipman-server-settings.json"
            persistent = {
                config: b'{"Host":"127.0.0.1","Port":61234,"Token":"dummy-token"}',
                state / "databases" / "dummy.clipdb": b"opaque-encrypted-dummy-history",
                state / "tls" / "clipman-server-ca.crt": b"dummy-public-ca",
                state / "tls" / "clipman-server-ca.key": b"dummy-private-ca-key",
                state / "clipman-server-connection.clpconf": b'{"Version":1,"Token":"dummy-token"}\n',
            }
            for path, data in persistent.items():
                path.write_bytes(data)
            (app / "clipman_server.py").write_bytes(b"historical-python-server")
            (app / "clipman_server_updater.py").write_bytes(b"bridge-python-updater")
            helper = bin_dir / "clipmanserver"
            launcher = bin_dir / "clipman-server"
            helper.write_bytes(b"historical-helper")
            launcher.write_text("#!/bin/sh\nexec python3 historical.py \"$@\"\n", encoding="utf-8")
            service.write_bytes(b"historical-systemd-unit")
            archive = root / "ClipmanServer-Linux-native-3.0.0.tar.gz"
            self.write_native_archive(archive)
            archive_bytes = archive.read_bytes()
            commands = []

            def fake_run(command, **_kwargs):
                commands.append(list(command))
                return mock.Mock(returncode=0)

            args = argparse.Namespace(
                yes=True, current_version="2.4.0", app_dir=str(app), bin_dir=str(bin_dir),
                config=str(config), service_file=str(service), helper_path=str(helper), managed_program_only=False,
            )
            asset = {"name": archive.name, "browser_download_url": "https://example.test/native.tar.gz", "_clipman_native": True}
            with mock.patch.object(updater, "download_asset", side_effect=lambda _asset, path: path.write_bytes(archive_bytes)), \
                 mock.patch.object(updater, "run", side_effect=fake_run), \
                 mock.patch.object(updater, "wait_for_health"):
                updater.install_update(args, "3.0.0", asset)

            self.assertEqual(b"native-go-server", (app / "clipman-server").read_bytes())
            launcher_text = launcher.read_text(encoding="utf-8")
            self.assertNotIn("python3", launcher_text)
            self.assertIn("--config", launcher_text)
            self.assertEqual(b"historical-python-server", (app / "clipman_server.py").read_bytes())
            self.assertEqual(b"bridge-python-updater", (app / "clipman_server_updater.py").read_bytes())
            self.assertEqual(b"historical-helper", helper.read_bytes())
            self.assertEqual(b"historical-systemd-unit", service.read_bytes())
            for path, data in persistent.items():
                self.assertEqual(data, path.read_bytes(), str(path))
            self.assertNotIn("sh", [command[0] for command in commands])

    def test_historical_standard_install_failed_native_health_restores_every_program_file(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "app"
            bin_dir = root / "bin"
            state = root / "state"
            service = root / "systemd" / "clipman-server.service"
            for directory in (app, bin_dir, state / "tls", state / "databases", service.parent):
                directory.mkdir(parents=True, exist_ok=True)
            config = state / "settings.json"
            original = {
                app / "clipman_server.py": b"historical-python-server",
                app / "clipman_server_updater.py": b"bridge-python-updater",
                bin_dir / "clipmanserver": b"historical-helper",
                bin_dir / "clipman-server": b"historical-python-launcher",
                service: b"historical-service",
                config: b'{"Host":"127.0.0.1","Port":61234,"Token":"dummy-token"}',
                state / "databases" / "dummy.clipdb": b"opaque-encrypted-dummy-history",
                state / "tls" / "clipman-server-ca.key": b"dummy-private-ca-key",
            }
            for path, data in original.items():
                path.write_bytes(data)
            archive = root / "native.tar.gz"
            self.write_native_archive(archive)
            archive_bytes = archive.read_bytes()
            args = argparse.Namespace(
                yes=True, current_version="2.4.0", app_dir=str(app), bin_dir=str(bin_dir),
                config=str(config), service_file=str(service), helper_path=str(bin_dir / "clipmanserver"),
                managed_program_only=False,
            )
            asset = {"name": archive.name, "browser_download_url": "https://example.test/native.tar.gz", "_clipman_native": True}
            with mock.patch.object(updater, "download_asset", side_effect=lambda _asset, path: path.write_bytes(archive_bytes)), \
                 mock.patch.object(updater, "run", return_value=mock.Mock(returncode=0)), \
                 mock.patch.object(updater, "wait_for_health", side_effect=RuntimeError("native process did not become healthy")), \
                 mock.patch.object(updater, "service_diagnostics", return_value="dummy service diagnostics"):
                with self.assertRaisesRegex(RuntimeError, "native process did not become healthy"):
                    updater.install_update(args, "3.0.0", asset)

            self.assertFalse((app / "clipman-server").exists())
            for path, data in original.items():
                self.assertEqual(data, path.read_bytes(), str(path))

    def test_client_releases_and_prereleases_are_ignored(self):
        releases = [
            {
                "tag_name": "v9.0.0",
                "assets": [{"name": "ClipmanServer-9.0.0.zip", "browser_download_url": "https://example.test/client.zip"}],
            },
            {
                "tag_name": "server-v2.1.1",
                "assets": [{"name": "ClipmanServer-2.1.1.zip", "browser_download_url": "https://example.test/server.zip"}],
            },
            {
                "tag_name": "server-v2.2.0",
                "prerelease": True,
                "assets": [{"name": "ClipmanServer-2.2.0.zip", "browser_download_url": "https://example.test/preview.zip"}],
            },
        ]

        self.assertIsNone(updater.find_update(releases, "2.1.1"))

    def test_unsafe_zip_path_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root / "unsafe.zip"
            with zipfile.ZipFile(archive, "w") as output:
                output.writestr("../outside.txt", "unsafe")
            with self.assertRaisesRegex(RuntimeError, "unsafe path"):
                updater.safe_extract(archive, root / "output")

    def test_missing_or_wrong_release_digest_is_rejected(self):
        digest = "a" * 64
        with self.assertRaisesRegex(RuntimeError, "did not provide"):
            updater.verify_sha256_digest("", digest)
        with self.assertRaisesRegex(RuntimeError, "failed its SHA-256"):
            updater.verify_sha256_digest("sha256:" + "b" * 64, digest)
        updater.verify_sha256_digest("sha256:" + digest, digest)

    def test_https_health_uses_local_listener_and_advertised_certificate_identity(self):
        settings = {
            "Host": "0.0.0.0",
            "AdvertiseHost": "server.example.test",
            "Port": 61234,
            "CertFile": "/tls/server.pem",
            "KeyFile": "/tls/server.key",
        }
        self.assertEqual(
            ("https", "127.0.0.1", "server.example.test", 61234),
            updater.health_connection(settings),
        )
        self.assertEqual("https://127.0.0.1:61234/api/v1/health", updater.health_url(settings))

    def test_ipv6_wildcard_health_uses_ipv6_loopback(self):
        settings = {
            "Host": "::",
            "AdvertiseHost": "2001:db8::1",
            "Port": 61234,
            "CertFile": "/tls/server.pem",
            "KeyFile": "/tls/server.key",
        }
        self.assertEqual(
            ("https", "::1", "2001:db8::1", 61234),
            updater.health_connection(settings),
        )
        self.assertEqual("https://[::1]:61234/api/v1/health", updater.health_url(settings))

    def test_https_health_connects_locally_but_validates_advertised_name(self):
        settings = {
            "Host": "0.0.0.0",
            "AdvertiseHost": "server.example.test",
            "Port": 61234,
            "CertFile": "/tls/server.pem",
            "KeyFile": "/tls/server.key",
            "CaFile": "/tls/authority.pem",
        }
        plain_socket = mock.Mock()
        tls_socket = mock.MagicMock()
        context = mock.Mock()
        context.wrap_socket.return_value = tls_socket
        response = mock.Mock(status=200)
        response.read.return_value = b'{"Status":"ok"}'

        with mock.patch.object(updater.ssl, "create_default_context", return_value=context) as create_context, \
             mock.patch.object(updater.socket, "create_connection", return_value=plain_socket) as create_connection, \
             mock.patch.object(updater.http.client, "HTTPResponse", return_value=response):
            payload = updater.read_health(settings)

        self.assertEqual({"Status": "ok"}, payload)
        create_context.assert_called_once_with(cafile="/tls/authority.pem")
        create_connection.assert_called_once_with(("127.0.0.1", 61234), timeout=3)
        context.wrap_socket.assert_called_once_with(plain_socket, server_hostname="server.example.test")
        request = tls_socket.__enter__.return_value.sendall.call_args.args[0].decode("ascii")
        self.assertIn("Host: server.example.test:61234", request)

    def test_health_url_uses_local_http_backend_behind_reverse_proxy(self):
        settings = {
            "Host": "127.0.0.1",
            "AdvertiseHost": "clipboard.example.test",
            "Port": 25767,
            "CertFile": "",
            "KeyFile": "",
        }
        self.assertEqual("http://127.0.0.1:25767/api/v1/health", updater.health_url(settings))

    def test_listen_host_validation_accepts_ipv6_brackets_and_rejects_urls(self):
        self.assertEqual("fd7a:115c:a1e0::1", updater.normalize_listen_host("[fd7a:115c:a1e0::1]"))
        with self.assertRaisesRegex(ValueError, "without a scheme"):
            updater.normalize_listen_host("https://100.64.0.10")
        with self.assertRaisesRegex(ValueError, "without a scheme"):
            updater.normalize_listen_host("100.64.0.10:62673/path")

    def test_wildcard_listen_host_requires_a_client_address(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "settings.json"
            config.write_text(json.dumps({"Host": "127.0.0.1", "AdvertiseHost": "", "Port": 61234}), encoding="utf-8")
            args = argparse.Namespace(config=str(config), bin_dir=str(root / "bin"))
            with self.assertRaisesRegex(ValueError, "wildcard listener requires"):
                updater.change_listen_host(args, "0.0.0.0")

    def test_listen_host_change_restarts_and_refreshes_connection_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "config" / "settings.json"
            bin_dir = root / "bin"
            config.parent.mkdir(parents=True)
            bin_dir.mkdir()
            settings = {
                "Host": "127.0.0.1",
                "AdvertiseHost": "127.0.0.1",
                "Port": 61234,
                "CertFile": "",
                "KeyFile": "",
            }
            config.write_text(json.dumps(settings), encoding="utf-8")
            connection_text = config.parent / "clipman-server-connection.txt"
            connection_config = config.parent / "clipman-server-connection.clpconf"
            connection_text.write_text("old text", encoding="utf-8")
            connection_config.write_text("old config", encoding="utf-8")
            commands = []

            def fake_run(command, **_kwargs):
                commands.append(command)
                if command[0] == str(bin_dir / "clipman-server"):
                    updated = json.loads(config.read_text(encoding="utf-8"))
                    updated["Host"] = command[command.index("--host") + 1]
                    if "--advertise-host" in command:
                        updated["AdvertiseHost"] = command[command.index("--advertise-host") + 1]
                    config.write_text(json.dumps(updated), encoding="utf-8")
                    connection_text.write_text("new text", encoding="utf-8")
                    connection_config.write_text("new config", encoding="utf-8")
                return mock.Mock(returncode=0)

            args = argparse.Namespace(config=str(config), bin_dir=str(bin_dir))
            with mock.patch.object(updater, "run", side_effect=fake_run), \
                 mock.patch.object(updater, "wait_for_health"):
                updater.change_listen_host(args, "100.64.0.10")

            updated = json.loads(config.read_text(encoding="utf-8"))
            self.assertEqual("100.64.0.10", updated["Host"])
            self.assertEqual("100.64.0.10", updated["AdvertiseHost"])
            self.assertEqual("new text", connection_text.read_text(encoding="utf-8"))
            self.assertEqual([str(bin_dir / "clipmanserver"), "stop"], commands[0])
            self.assertIn("--write-connection-info", commands[1])
            self.assertEqual([str(bin_dir / "clipmanserver"), "start"], commands[2])

    def test_failed_listen_host_change_restores_settings_and_connection_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = root / "config" / "settings.json"
            bin_dir = root / "bin"
            config.parent.mkdir(parents=True)
            bin_dir.mkdir()
            original = json.dumps({
                "Host": "127.0.0.1",
                "AdvertiseHost": "clipboard.example.test",
                "Port": 61234,
                "CertFile": "",
                "KeyFile": "",
            }).encode("utf-8")
            config.write_bytes(original)
            connection_text = config.parent / "clipman-server-connection.txt"
            connection_config = config.parent / "clipman-server-connection.clpconf"
            connection_text.write_bytes(b"old text")
            connection_config.write_bytes(b"old config")
            config.chmod(0o600)
            connection_text.chmod(0o600)
            connection_config.chmod(0o600)
            commands = []

            def fake_run(command, **_kwargs):
                commands.append(command)
                if command[0] == str(bin_dir / "clipman-server"):
                    updated = json.loads(config.read_text(encoding="utf-8"))
                    updated["Host"] = command[command.index("--host") + 1]
                    config.write_text(json.dumps(updated), encoding="utf-8")
                    connection_text.write_bytes(b"new text")
                    connection_config.write_bytes(b"new config")
                return mock.Mock(returncode=0)

            args = argparse.Namespace(config=str(config), bin_dir=str(bin_dir))
            with mock.patch.object(updater, "run", side_effect=fake_run), \
                 mock.patch.object(updater, "wait_for_health", side_effect=[RuntimeError("new listener failed"), None]), \
                 mock.patch.object(updater, "service_diagnostics", return_value="service failed"):
                with self.assertRaisesRegex(RuntimeError, "(?s)new listener failed.*Service status before restoration"):
                    updater.change_listen_host(args, "100.64.0.99")

            self.assertEqual(original, config.read_bytes())
            self.assertEqual(b"old text", connection_text.read_bytes())
            self.assertEqual(b"old config", connection_config.read_bytes())
            if os.name != "nt":
                self.assertEqual(0o600, config.stat().st_mode & 0o777)
                self.assertEqual(0o600, connection_text.stat().st_mode & 0o777)
                self.assertEqual(0o600, connection_config.stat().st_mode & 0o777)
            self.assertEqual(2, commands.count([str(bin_dir / "clipmanserver"), "stop"]))
            self.assertEqual(2, commands.count([str(bin_dir / "clipmanserver"), "start"]))

    def test_failed_health_check_restores_previous_program(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "app"
            bin_dir = root / "bin"
            config = root / "config" / "settings.json"
            service = root / "systemd" / "clipman-server.service"
            app.mkdir(parents=True)
            bin_dir.mkdir(parents=True)
            config.parent.mkdir(parents=True)
            service.parent.mkdir(parents=True)
            (app / "old.txt").write_text("old", encoding="utf-8")
            helper = bin_dir / "clipmanserver"
            launcher = bin_dir / "clipman-server"
            helper.write_text("old helper", encoding="utf-8")
            launcher.write_text("old launcher", encoding="utf-8")
            service.write_text("old service", encoding="utf-8")
            config.write_text(json.dumps({"Host": "127.0.0.1", "Port": 60000}), encoding="utf-8")

            def fake_extract(_archive, destination):
                package = destination / "ClipmanServer"
                (package / "Linux").mkdir(parents=True)
                (package / "manifest.json").write_text(
                    json.dumps({"Name": "Clipman Server", "Version": "2.1.1"}), encoding="utf-8"
                )
                (package / "clipman_server.py").write_text("new", encoding="utf-8")
                (package / "clipman_server_updater.py").write_text("new", encoding="utf-8")
                (package / "Linux" / "install-clipman-server.sh").write_text("installer", encoding="utf-8")

            def fake_run(command, **_kwargs):
                if command[0] == "sh":
                    (app / "new.txt").write_text("new", encoding="utf-8")
                    (app / "old.txt").unlink()
                    helper.write_text("new helper", encoding="utf-8")
                    launcher.write_text("new launcher", encoding="utf-8")
                    service.write_text("new service", encoding="utf-8")
                return mock.Mock(returncode=0)

            args = argparse.Namespace(
                yes=True,
                current_version="2.1.0",
                app_dir=str(app),
                bin_dir=str(bin_dir),
                config=str(config),
                service_file=str(service),
            )
            asset = {"browser_download_url": "https://example.test/server.zip"}
            with mock.patch.object(updater, "download_asset", side_effect=lambda _asset, path: path.write_bytes(b"zip")), \
                 mock.patch.object(updater, "safe_extract", side_effect=fake_extract), \
                 mock.patch.object(updater, "run", side_effect=fake_run), \
                 mock.patch.object(updater, "wait_for_health", side_effect=RuntimeError("not healthy")):
                with mock.patch.object(updater, "service_diagnostics", return_value="service failed"):
                    with self.assertRaisesRegex(RuntimeError, "(?s)not healthy.*Service status before rollback"):
                        updater.install_update(args, "2.1.1", asset)

            self.assertEqual("old", (app / "old.txt").read_text(encoding="utf-8"))
            self.assertFalse((app / "new.txt").exists())
            self.assertEqual("old helper", helper.read_text(encoding="utf-8"))
            self.assertEqual("old launcher", launcher.read_text(encoding="utf-8"))
            self.assertEqual("old service", service.read_text(encoding="utf-8"))

    def test_successful_health_check_keeps_updated_program(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "app"
            bin_dir = root / "bin"
            config = root / "config" / "settings.json"
            service = root / "systemd" / "clipman-server.service"
            app.mkdir(parents=True)
            bin_dir.mkdir(parents=True)
            config.parent.mkdir(parents=True)
            service.parent.mkdir(parents=True)
            (app / "program.txt").write_text("old", encoding="utf-8")
            helper = bin_dir / "clipmanserver"
            launcher = bin_dir / "clipman-server"
            helper.write_text("old helper", encoding="utf-8")
            launcher.write_text("old launcher", encoding="utf-8")
            service.write_text("old service", encoding="utf-8")
            config.write_text(json.dumps({"Host": "127.0.0.1", "Port": 60000}), encoding="utf-8")

            def fake_extract(_archive, destination):
                package = destination / "ClipmanServer"
                (package / "Linux").mkdir(parents=True)
                (package / "manifest.json").write_text(
                    json.dumps({"Name": "Clipman Server", "Version": "2.1.1"}), encoding="utf-8"
                )
                (package / "clipman_server.py").write_text("new", encoding="utf-8")
                (package / "clipman_server_updater.py").write_text("new", encoding="utf-8")
                (package / "Linux" / "install-clipman-server.sh").write_text("installer", encoding="utf-8")

            def fake_run(command, **_kwargs):
                if command[0] == "sh":
                    (app / "program.txt").write_text("new", encoding="utf-8")
                    helper.write_text("new helper", encoding="utf-8")
                    launcher.write_text("new launcher", encoding="utf-8")
                    service.write_text("new service", encoding="utf-8")
                return mock.Mock(returncode=0)

            args = argparse.Namespace(
                yes=True,
                current_version="2.1.0",
                app_dir=str(app),
                bin_dir=str(bin_dir),
                config=str(config),
                service_file=str(service),
            )
            with mock.patch.object(updater, "download_asset", side_effect=lambda _asset, path: path.write_bytes(b"zip")), \
                 mock.patch.object(updater, "safe_extract", side_effect=fake_extract), \
                 mock.patch.object(updater, "run", side_effect=fake_run), \
                 mock.patch.object(updater, "wait_for_health"):
                updater.install_update(args, "2.1.1", {"browser_download_url": "https://example.test/server.zip"})

            self.assertEqual("new", (app / "program.txt").read_text(encoding="utf-8"))
            self.assertEqual("new helper", helper.read_text(encoding="utf-8"))
            self.assertEqual("new launcher", launcher.read_text(encoding="utf-8"))
            self.assertEqual("new service", service.read_text(encoding="utf-8"))

    def test_system_managed_update_replaces_only_program_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "app"
            bin_dir = root / "bin"
            config = root / "config" / "settings.json"
            service = root / "systemd" / "clipman-public.service"
            app.mkdir(parents=True)
            bin_dir.mkdir(parents=True)
            config.parent.mkdir(parents=True)
            service.parent.mkdir(parents=True)
            (app / "clipman_server.py").write_text("old server", encoding="utf-8")
            (app / "clipman_server_updater.py").write_text("old updater", encoding="utf-8")
            helper = bin_dir / "clipmanserver"
            helper.write_text("managed helper", encoding="utf-8")
            service.write_text("hardened service", encoding="utf-8")
            config.write_text('{"Host":"127.0.0.1","Port":60000,"Token":"keep-me"}', encoding="utf-8")

            def fake_extract(_archive, destination):
                package = destination / "ClipmanServer"
                (package / "Linux").mkdir(parents=True)
                (package / "manifest.json").write_text(
                    json.dumps({"Name": "Clipman Server", "Version": "2.4.3"}), encoding="utf-8"
                )
                (package / "clipman_server.py").write_text("new server", encoding="utf-8")
                (package / "clipman_server_updater.py").write_text("new updater", encoding="utf-8")
                (package / "Manual.html").write_text("new manual", encoding="utf-8")
                (package / "Linux" / "install-clipman-server.sh").write_text("unused installer", encoding="utf-8")

            args = argparse.Namespace(
                yes=True,
                current_version="2.4.0",
                app_dir=str(app),
                bin_dir=str(bin_dir),
                config=str(config),
                service_file=str(service),
                helper_path=str(helper),
                managed_program_only=True,
            )
            before_config = config.read_bytes()
            with mock.patch.object(updater, "download_asset", side_effect=lambda _asset, path: path.write_bytes(b"zip")), \
                 mock.patch.object(updater, "safe_extract", side_effect=fake_extract), \
                 mock.patch.object(updater, "run", return_value=mock.Mock(returncode=0)) as run_command, \
                 mock.patch.object(updater, "wait_for_health"):
                updater.install_update(args, "2.4.3", {"browser_download_url": "https://example.test/server.zip"})

            self.assertEqual("new server", (app / "clipman_server.py").read_text(encoding="utf-8"))
            self.assertEqual("new updater", (app / "clipman_server_updater.py").read_text(encoding="utf-8"))
            self.assertEqual("managed helper", helper.read_text(encoding="utf-8"))
            self.assertEqual("hardened service", service.read_text(encoding="utf-8"))
            self.assertEqual(before_config, config.read_bytes())
            self.assertNotIn("install-clipman-server.sh", " ".join(str(call) for call in run_command.call_args_list))

    def test_failed_system_managed_update_restores_program_without_touching_state(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            app = root / "app"
            bin_dir = root / "bin"
            config = root / "config" / "settings.json"
            service = root / "systemd" / "clipman-public.service"
            app.mkdir(parents=True)
            bin_dir.mkdir(parents=True)
            config.parent.mkdir(parents=True)
            service.parent.mkdir(parents=True)
            server = app / "clipman_server.py"
            server.write_text("old server", encoding="utf-8")
            updater_file = app / "clipman_server_updater.py"
            updater_file.write_text("old updater", encoding="utf-8")
            helper = bin_dir / "clipmanserver"
            helper.write_text("managed helper", encoding="utf-8")
            service.write_text("hardened service", encoding="utf-8")
            config.write_text('{"Host":"127.0.0.1","Port":60000,"Token":"keep-me"}', encoding="utf-8")
            if os.name != "nt":
                os.chmod(server, 0o750)
            before_config = config.read_bytes()

            def fake_extract(_archive, destination):
                package = destination / "ClipmanServer"
                (package / "Linux").mkdir(parents=True)
                (package / "manifest.json").write_text(
                    json.dumps({"Name": "Clipman Server", "Version": "2.4.3"}), encoding="utf-8"
                )
                (package / "clipman_server.py").write_text("broken server", encoding="utf-8")
                (package / "clipman_server_updater.py").write_text("new updater", encoding="utf-8")
                (package / "Linux" / "install-clipman-server.sh").write_text("unused installer", encoding="utf-8")

            args = argparse.Namespace(
                yes=True,
                current_version="2.4.0",
                app_dir=str(app),
                bin_dir=str(bin_dir),
                config=str(config),
                service_file=str(service),
                helper_path=str(helper),
                managed_program_only=True,
            )
            with mock.patch.object(updater, "download_asset", side_effect=lambda _asset, path: path.write_bytes(b"zip")), \
                 mock.patch.object(updater, "safe_extract", side_effect=fake_extract), \
                 mock.patch.object(updater, "run", return_value=mock.Mock(returncode=0)), \
                 mock.patch.object(updater, "wait_for_health", side_effect=RuntimeError("not healthy")), \
                 mock.patch.object(updater, "service_diagnostics", return_value="service failed"):
                with self.assertRaisesRegex(RuntimeError, "not healthy"):
                    updater.install_update(args, "2.4.3", {"browser_download_url": "https://example.test/server.zip"})

            self.assertEqual("old server", server.read_text(encoding="utf-8"))
            self.assertEqual("old updater", updater_file.read_text(encoding="utf-8"))
            self.assertEqual("managed helper", helper.read_text(encoding="utf-8"))
            self.assertEqual("hardened service", service.read_text(encoding="utf-8"))
            self.assertEqual(before_config, config.read_bytes())
            if os.name != "nt":
                self.assertEqual(0o750, server.stat().st_mode & 0o777)


if __name__ == "__main__":
    unittest.main()
