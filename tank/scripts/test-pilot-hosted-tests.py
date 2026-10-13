#!/usr/bin/env python3
"""Focused schema/security tests; never contact or mutate any host."""
import contextlib
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location("hosted", Path(__file__).with_name("test-pilot-hosted.py"))
hosted = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(hosted)


class HostedPreflightTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="tank-hosted-schema-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.root.chmod(0o700)
        for name in ("alice.json", "bob.json"):
            path = self.root / name
            # Deliberately invalid contents prove validation checks metadata
            # without reading or parsing credential material.
            path.write_text("synthetic unread credential content")
            path.chmod(0o600)
        self.config = {
            "version": 1,
            "frontend_url": "https://workspace.tank-pilot.org",
            "coordinator": {"url": "https://api.tank-pilot.org", "ssh_alias": "tank-coordinator",
                            "failure_domain": "coordinator-zone", "service": "tank-coordinator.service",
                            "registration_service": "tank-registration-worker.service",
                            "registration_timer": "tank-registration-worker.timer"},
            "nodes": [
                {"url": f"https://node{index}.tank-pilot.org", "failure_domain": f"provider-zone-{index}",
                 "ssh_alias": f"tank-node-{index}", "service": "tank-node.service"}
                for index in range(4)
            ],
            "credentials": {"user_a": str(self.root / "alice.json"), "user_b": str(self.root / "bob.json")},
            "recovery": {"snapshot_binary": "/opt/tank/bin/tank-backup",
                         "live_database": "/var/lib/tank-coordinator/tank.sqlite",
                         "snapshot_database": "/var/lib/tank-recovery/snapshot.sqlite",
                         "restored_database": "/var/lib/tank-recovery/restored.sqlite"},
            "arc": {"rpc_url": "https://rpc.testnet.arc.io", "chain_id": 5042002, "registry": hosted.REGISTRY},
        }
        self.config["nodes"][0]["ssh_alias"] = "tank-active"
        self.config["nodes"][3]["ssh_alias"] = "tank-spare"

    def assert_invalid(self, config=None):
        with self.assertRaises(hosted.PreflightError):
            hosted.validate_config(self.config if config is None else config)

    def config_file(self):
        path = self.root / "config.json"
        path.write_text(json.dumps(self.config))
        path.chmod(0o600)
        return path

    def test_valid_schema_does_not_read_credentials(self):
        with mock.patch.object(Path, "read_text", side_effect=AssertionError("Unexpected private read")):
            hosted.validate_config(self.config)

    def test_private_config_loads(self):
        self.assertEqual(hosted.load_config(str(self.config_file())), self.config)

    def test_checked_in_template_is_refused(self):
        template = Path(__file__).resolve().parents[1] / "deploy/pilot/hosted-acceptance.example.json"
        self.assert_invalid(json.loads(template.read_text()))

    def test_unknown_field_refused(self):
        self.config["private_key"] = "not a credential"
        self.assert_invalid()

    def test_missing_field_refused(self):
        del self.config["recovery"]
        self.assert_invalid()

    def test_wrong_schema_version_refused(self):
        self.config["version"] = True
        self.assert_invalid()

    def test_requires_four_nodes(self):
        self.config["nodes"].pop()
        self.assert_invalid()

    def test_duplicate_failure_domains_refused_case_insensitively(self):
        self.config["nodes"][1]["failure_domain"] = self.config["nodes"][0]["failure_domain"].upper()
        self.assert_invalid()

    def test_duplicate_origins_refused_after_normalizing_slash(self):
        self.config["nodes"][1]["url"] = self.config["nodes"][0]["url"] + "/"
        self.assert_invalid()

    def test_duplicate_origins_refused_with_explicit_default_port(self):
        self.config["nodes"][1]["url"] = self.config["nodes"][0]["url"] + ":443"
        self.assert_invalid()

    def test_duplicate_ssh_aliases_refused(self):
        self.config["coordinator"]["ssh_alias"] = self.config["nodes"][0]["ssh_alias"]
        self.assert_invalid()

    def test_https_and_public_origin_required(self):
        for value in ("http://workspace.tank-pilot.org", "https://127.0.0.1", "https://10.0.0.1",
                      "https://localhost", "https://node.local", "https://node.invalid",
                      "https://workspace.tank-pilot.org:444", "https://user:password@workspace.tank-pilot.org",
                      "https://workspace.tank-pilot.org/?secret=private", "https://workspace.tank-pilot.org/path",
                      "https://workspace.tank-pilot.org/#fragment", "https://workspace.tank-pilot.org:bad",
                      "https://WORKSPACE.tank-pilot.org"):
            with self.subTest(value=value):
                self.config["frontend_url"] = value
                self.assert_invalid()

    def test_coordinator_failure_domain_must_be_safe_label(self):
        self.config["coordinator"]["failure_domain"] = "provider zone;id"
        self.assert_invalid()

    def test_placeholder_is_refused(self):
        for field in ("REPLACE_VALUE", "placeholder-host", "changeme", "TODO", "<host>"):
            with self.subTest(field=field):
                self.config["nodes"][0]["failure_domain"] = field
                self.assert_invalid()

    def test_mainnet_and_other_registry_refused(self):
        for chain in (5042, 1, 31337, 84532, "5042002"):
            with self.subTest(chain=chain):
                self.config["arc"]["chain_id"] = chain
                self.assert_invalid()
        self.config["arc"]["chain_id"] = 5042002
        self.config["arc"]["registry"] = "0x" + "0" * 40
        self.assert_invalid()

    def test_nonprivate_credential_refused(self):
        (self.root / "alice.json").chmod(0o644)
        self.assert_invalid()

    def test_nonprivate_parent_refused(self):
        self.root.chmod(0o755)
        self.assert_invalid()

    def test_symlink_credential_refused(self):
        link = self.root / "linked.json"
        link.symlink_to(self.root / "alice.json")
        self.config["credentials"]["user_a"] = str(link)
        self.assert_invalid()

    def test_parent_symlink_refused(self):
        link = self.root / "linked-directory"
        link.symlink_to(self.root, target_is_directory=True)
        self.config["credentials"]["user_a"] = str(link / "alice.json")
        self.assert_invalid()

    def test_same_credential_inode_refused(self):
        (self.root / "bob.json").unlink()
        os.link(self.root / "alice.json", self.root / "bob.json")
        self.assert_invalid()

    def test_remote_paths_canonical_and_nonconflicting(self):
        for value in ("relative.sqlite", "/data/../live.sqlite", "/data//live.sqlite", "/data/$(id).sqlite",
                      "/data/snapshot;id.sqlite", "/data/has space.sqlite"):
            with self.subTest(value=value):
                self.config["recovery"]["snapshot_database"] = value
                self.assert_invalid()
        self.config["recovery"]["snapshot_database"] = self.config["recovery"]["live_database"]
        self.assert_invalid()

    def test_alias_and_service_shell_injection_refused(self):
        for value in ("-oProxyCommand=id", "tank;id", "tank$(id)", "user@host", "tank host"):
            with self.subTest(value=value):
                config = copy.deepcopy(self.config)
                config["nodes"][0]["ssh_alias"] = value
                self.assert_invalid(config)
        for value in ("-bad.service", "tank;id.service", "tank$(id).service", "tank.service --bad"):
            with self.subTest(value=value):
                config = copy.deepcopy(self.config)
                config["nodes"][0]["service"] = value
                self.assert_invalid(config)

    def test_service_command_uses_fixed_argv(self):
        command = hosted.service_argv(self.config["nodes"][0], "stop")
        self.assertEqual(command, ["ssh", "-T", "-oBatchMode=yes", "-oStrictHostKeyChecking=yes",
                                  "-oConnectTimeout=10", "--", "tank-active", "sudo", "-n",
                                  "systemctl", "stop", "--", "tank-node.service"])
        with self.assertRaises(hosted.PreflightError):
            hosted.service_argv(self.config["nodes"][0], "delete")

    def test_snapshot_command_never_overwrites_live_path(self):
        command = hosted.snapshot_argv(self.config)
        self.assertEqual(command[-6:], ["--", "/opt/tank/bin/tank-backup", "--db",
                                      "/var/lib/tank-coordinator/tank.sqlite", "--out",
                                      "/var/lib/tank-recovery/snapshot.sqlite"])
        self.assertNotIn("sh", command)

    def test_preflight_checks_five_distinct_machine_ids(self):
        code = "0x6000"
        with mock.patch.object(hosted, "request") as request, \
             mock.patch.object(hosted, "rpc", side_effect=[hex(hosted.CHAIN_ID), code]) as rpc, \
             mock.patch.object(hosted, "identity", side_effect=[f"{index:032x}" for index in range(1, 6)]) as identity, \
             mock.patch.object(hosted.hashlib, "sha256") as sha:
            sha.return_value.hexdigest.return_value = hosted.RUNTIME_SHA256
            hosted.preflight(self.config)
        self.assertEqual(request.call_count, 6)
        self.assertEqual(rpc.call_args_list[0].args[1:], ("eth_chainId", []))
        self.assertEqual(rpc.call_args_list[1].args[1:],
                         ("eth_getCode", [self.config["arc"]["registry"], "latest"]))
        self.assertEqual(identity.call_count, 5)

    def test_preflight_refuses_duplicate_machine_ids(self):
        with mock.patch.object(hosted, "request"), \
             mock.patch.object(hosted, "rpc", side_effect=[hex(hosted.CHAIN_ID), "0x6000"]), \
             mock.patch.object(hosted, "identity", side_effect=["1" * 32] * 5), \
             mock.patch.object(hosted.hashlib, "sha256") as sha:
            sha.return_value.hexdigest.return_value = hosted.RUNTIME_SHA256
            with self.assertRaisesRegex(hosted.PreflightError, "duplicate machine identities"):
                hosted.preflight(self.config)

    def test_validate_only_never_uses_network_or_commands(self):
        with mock.patch.object(hosted, "preflight", side_effect=AssertionError("Unexpected network")), \
             mock.patch.object(hosted.subprocess, "run", side_effect=AssertionError("Unexpected command")), \
             contextlib.redirect_stdout(io.StringIO()) as output:
            self.assertEqual(hosted.main(["--config", str(self.config_file()), "--validate-config-only"]), 0)
        self.assertIn("Full hosted acceptance has NOT run", output.getvalue())
        self.assertNotIn(str(self.root), output.getvalue())

    def test_execute_fails_closed_before_network_or_commands(self):
        with mock.patch.object(hosted, "preflight", side_effect=AssertionError("Unexpected network")), \
             mock.patch.object(hosted.subprocess, "run", side_effect=AssertionError("Unexpected command")):
            with self.assertRaisesRegex(hosted.PreflightError, "execution is unavailable"):
                hosted.main(["--config", str(self.config_file()), "--execute-hosted-drill"])

    def test_invalid_arguments_are_redacted(self):
        with self.assertRaises(hosted.PreflightError) as failure:
            hosted.main(["--secret-value=must-not-be-printed"])
        self.assertNotIn("must-not-be-printed", str(failure.exception))


if __name__ == "__main__":
    unittest.main()
