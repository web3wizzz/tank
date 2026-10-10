#!/usr/bin/env python3
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('prepare', Path(__file__).with_name('prepare-base-sepolia.py'))
prepare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare)


class BasePreparation(unittest.TestCase):
    def test_plan_is_private_unsigned_and_excludes_configuration_secrets(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(prepare, 'forge', return_value='0x6000'):
            output = Path(directory) / 'plan.json'
            with patch.dict(os.environ, {'TANK_BASE_SEPOLIA_RPC': 'https://selected.invalid/private-rpc-token',
                                         'TANK_BASE_SEPOLIA_ACCOUNT': 'private-keystore-name'}):
                prepare.run(['plan', '--out', str(output)])
            plan = json.loads(output.read_text())
            self.assertEqual(plan['chain_id'], 84532)
            self.assertFalse(plan['broadcast_enabled'])
            self.assertFalse(plan['registration_worker_ready'])
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)
            self.assertNotIn('private-rpc-token', output.read_text())
            self.assertNotIn('private-keystore-name', output.read_text())
            with self.assertRaises(prepare.PreparationError):
                prepare.run(['plan', '--out', str(output)])

    def test_broadcast_and_signer_arguments_are_rejected_without_echo(self):
        secret = 'synthetic-private-key-must-not-be-printed'
        for arguments in [['deploy'], ['broadcast'], ['plan', '--broadcast'], ['--private-key', secret]]:
            with self.assertRaises(prepare.PreparationError) as error:
                prepare.run(arguments)
            self.assertNotIn(secret, str(error.exception))

    def test_rpc_is_read_only_chain_bound_and_errors_are_redacted(self):
        class Response:
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def read(self, limit): return b'{"jsonrpc":"2.0","id":1,"result":"0x14a34"}'
        captured = []
        def request(value, timeout):
            captured.append(json.loads(value.data))
            return Response()
        with patch.dict(os.environ, {'TANK_BASE_SEPOLIA_RPC': 'https://selected.invalid/secret-provider-token'}), patch.object(prepare, 'urlopen', request):
            prepare.run(['check-rpc'])
        self.assertEqual(captured, [{'jsonrpc': '2.0', 'id': 1, 'method': 'eth_chainId', 'params': []}])
        with patch.dict(os.environ, {'TANK_BASE_SEPOLIA_RPC': 'https://selected.invalid/secret-provider-token'}), patch.object(prepare, 'urlopen', side_effect=RuntimeError('secret-provider-token')):
            with self.assertRaises(prepare.PreparationError) as error: prepare.run(['check-rpc'])
            self.assertNotIn('secret-provider-token', str(error.exception))
        class WrongNetwork(Response):
            def read(self, limit): return b'{"jsonrpc":"2.0","id":1,"result":"0x7a69"}'
        with patch.dict(os.environ, {'TANK_BASE_SEPOLIA_RPC': 'https://selected.invalid/secret-provider-token'}), patch.object(prepare, 'urlopen', return_value=WrongNetwork()):
            with self.assertRaises(prepare.PreparationError): prepare.run(['check-rpc'])
        self.assertIsNone(prepare.NoRedirect().redirect_request(None, None, 302, '', {}, 'https://unselected.invalid'))
        for url in ['', 'REPLACE_WITH_SELECTED_ENDPOINT', 'http://localhost:8545']:
            with patch.dict(os.environ, {'TANK_BASE_SEPOLIA_RPC': url}):
                with self.assertRaises(prepare.PreparationError): prepare.run(['check-rpc'])


if __name__ == '__main__':
    unittest.main()
