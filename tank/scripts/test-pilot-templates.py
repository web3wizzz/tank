#!/usr/bin/env python3
"""Validate preparation templates without installing services or reading secrets."""
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class PilotTemplates(unittest.TestCase):
    def test_configs_have_placeholders_and_private_persistent_paths(self):
        coordinator = (ROOT / 'deploy/pilot/coordinator.conf.example').read_text()
        node = (ROOT / 'deploy/pilot/node.conf.example').read_text()
        frontend = (ROOT / 'deploy/pilot/frontend.conf.example').read_text()
        self.assertIn('TANK_COORDINATOR_ADDR=127.0.0.1:8080', coordinator)
        self.assertIn('TANK_NODE_CREDENTIALS_FILE=/etc/tank/node-credentials.json', coordinator)
        self.assertIn('TANK_DATABASE_PATH=/var/lib/tank-coordinator/', coordinator)
        self.assertIn('TANK_NODE_DATA_DIR=/var/lib/tank-node/', node)
        self.assertIn('TANK_NODE_ADDR=127.0.0.1:9101', node)
        self.assertNotIn('TANK_API_TOKEN=', frontend)
        self.assertNotIn('TANK_NODE_TOKEN=', frontend)
        credentials = json.loads((ROOT / 'deploy/pilot/node-credentials.json.example').read_text())
        self.assertEqual(len(credentials), 4)
        self.assertTrue(all(key.startswith('REPLACE_') and value.startswith('REPLACE_')
                            for key, value in credentials.items()))

    def test_systemd_units_parse_without_installation(self):
        verifier = shutil.which('systemd-analyze')
        if verifier is None:
            self.skipTest('systemd-analyze unavailable')
        with tempfile.TemporaryDirectory(prefix='tank-unit-check-') as directory:
            units = []
            for source in sorted((ROOT / 'deploy/pilot/systemd').glob('*.service')):
                body = source.read_text()
                self.assertIn('User=tank', body)
                self.assertIn('UMask=0077', body)
                self.assertIn('StateDirectoryMode=0700', body)
                self.assertIn('ProtectSystem=strict', body)
                # Validate the real unit directives; substitute only the binary
                # that is deliberately not installed on this preparation host.
                lines = [f'ExecStart={shutil.which("true")}' if line.startswith('ExecStart=')
                         else line for line in body.splitlines()]
                target = Path(directory) / source.name
                target.write_text('\n'.join(lines) + '\n')
                target.chmod(0o644)
                units.append(str(target))
            result = subprocess.run([verifier, 'verify', *units], capture_output=True, timeout=15)
            self.assertEqual(result.returncode, 0, 'systemd template validation failed')


if __name__ == '__main__':
    unittest.main()
