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
        self.assertIn('TANK_REGISTRATION_MODE=arc-testnet-external', coordinator)
        self.assertIn('TANK_CHAIN_ID=5042002', coordinator)
        self.assertIn('TANK_REGISTRY_ADDR=0xe1fC5025261af42dfe04788c7a09DAd3ea1EC435', coordinator)
        self.assertNotIn('TANK_CHAIN_RPC=', coordinator)
        self.assertIn('TANK_DATABASE_PATH=/var/lib/tank-coordinator/', coordinator)
        self.assertIn('TANK_NODE_DATA_DIR=/var/lib/tank-node/', node)
        self.assertIn('TANK_NODE_ADDR=127.0.0.1:9101', node)
        self.assertNotIn('TANK_API_TOKEN=', frontend)
        self.assertNotIn('TANK_NODE_TOKEN=', frontend)
        credentials = json.loads((ROOT / 'deploy/pilot/node-credentials.json.example').read_text())
        self.assertEqual(len(credentials), 4)
        self.assertTrue(all(key.startswith('REPLACE_') and value.startswith('REPLACE_')
                            for key, value in credentials.items()))

    def test_arc_reference_template_keeps_approved_fee_budgets(self):
        config = (ROOT / 'deploy/arc-testnet/testnet.conf.example').read_text()
        values = dict(line.split('=', 1) for line in config.splitlines()
                      if line and not line.startswith('#') and '=' in line)
        self.assertEqual(values['TANK_CHAIN_ID'], '5042002')
        self.assertEqual(values['TANK_ARC_TESTNET_MAX_FEE'], '65000000000')
        self.assertEqual(values['TANK_ARC_DEPLOY_MAX_FEE'], '65000000000')
        self.assertEqual(values['TANK_ARC_TESTNET_MAX_COST'], '8000000000000000')
        self.assertEqual(values['TANK_ARC_DEPLOY_MAX_COST'], '40000000000000000')
        self.assertEqual(values['TANK_ARC_TESTNET_TOTAL_BUDGET'], '100000000000000000')

    def test_systemd_units_parse_without_installation(self):
        verifier = shutil.which('systemd-analyze')
        if verifier is None:
            self.skipTest('systemd-analyze unavailable')
        with tempfile.TemporaryDirectory(prefix='tank-unit-check-') as directory:
            units = []
            sources = sorted((ROOT / 'deploy/pilot/systemd').glob('*.service')) + sorted((ROOT / 'deploy/pilot/systemd').glob('*.timer'))
            for source in sources:
                body = source.read_text()
                if source.suffix == '.service':
                    self.assertIn('User=tank', body)
                    self.assertIn('UMask=0077', body)
                    self.assertIn('ProtectSystem=strict', body)
                    self.assertIn('StateDirectoryMode=0700', body)
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

    def test_hosted_worker_lifecycle_is_separate_and_testnet_bound(self):
        units = ROOT / 'deploy/pilot/systemd'
        frontend = (units / 'tank-frontend.service').read_text()
        worker = (units / 'tank-registration-worker.service').read_text()
        timer = (units / 'tank-registration-worker.timer').read_text()
        self.assertIn('--hostname 127.0.0.1 --port 3000', frontend)
        self.assertIn('/opt/tank/frontend/node_modules/next/dist/bin/next start', frontend)
        self.assertIn('Type=oneshot', worker)
        self.assertIn('/opt/tank/bin/tank-prepare-registration --broadcast', worker)
        self.assertIn('EnvironmentFile=/etc/tank/registration-worker.conf', worker)
        self.assertIn('ReadWritePaths=/var/lib/tank-coordinator', worker)
        self.assertNotIn('WantedBy=', worker)
        self.assertIn('OnUnitActiveSec=5min', timer)
        self.assertIn('Unit=tank-registration-worker.service', timer)


if __name__ == '__main__':
    unittest.main()
