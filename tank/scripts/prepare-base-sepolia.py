#!/usr/bin/env python3
"""Offline registry plan and optional read-only chain check; no signing/broadcast."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import urlsplit
from urllib.request import Request, HTTPRedirectHandler, build_opener

PROJECT = Path(__file__).resolve().parents[1]
CHAIN_ID = 84532

class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, file, code, message, headers, target):
        return None


# A selected provider cannot redirect its authenticated URL onto another host.
urlopen = build_opener(NoRedirect()).open


class PreparationError(Exception):
    pass


def forge(*arguments):
    executable = os.environ.get('TANK_FORGE_BIN', str(Path.home() / '.foundry/bin/forge'))
    try:
        result = subprocess.run([executable, *arguments], cwd=PROJECT / 'contracts',
                                capture_output=True, timeout=120)
    except (OSError, subprocess.TimeoutExpired):
        raise PreparationError('Foundry preparation could not run.') from None
    if result.returncode:
        # Tools can include environment/RPC details in errors. Keep those private.
        raise PreparationError('Foundry preparation failed; inspect configuration privately.')
    return result.stdout.decode().strip()


def write_plan(destination):
    encoded = forge('inspect', 'src/TankRegistry.sol:TankRegistry', 'bytecode')
    if not re.fullmatch(r'0x[0-9a-fA-F]+', encoded) or len(encoded) % 2:
        raise PreparationError('Foundry returned invalid deployment bytecode.')
    gas = os.environ.get('TANK_BASE_SEPOLIA_MAX_GAS', '1000000')
    if not gas.isdecimal() or not 21000 <= int(gas) <= 10000000:
        raise PreparationError('Deployment gas ceiling must be 21000–10000000.')
    plan = {
        'version': 1, 'network': 'Base Sepolia', 'chain_id': CHAIN_ID,
        'contract': 'TankRegistry', 'creation_bytecode': encoded,
        'creation_bytecode_sha256': hashlib.sha256(bytes.fromhex(encoded[2:])).hexdigest(),
        'maximum_gas': int(gas), 'broadcast_enabled': False,
        'required_inputs': ['TANK_BASE_SEPOLIA_RPC', 'TANK_BASE_SEPOLIA_SENDER',
                            'TANK_BASE_SEPOLIA_ACCOUNT', 'deployment_authorization'],
        'registration_worker_ready': False,
    }
    try:
        # Destination must be new; no credential/RPC values enter the plan.
        with open(os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as output:
            json.dump(plan, output, indent=2)
            output.write('\n')
            output.flush()
            os.fsync(output.fileno())
    except OSError:
        raise PreparationError('Could not create a new private deployment plan.') from None


def check_rpc():
    address = os.environ.get('TANK_BASE_SEPOLIA_RPC', '')
    try:
        parsed = urlsplit(address)
        if parsed.scheme != 'https' or not parsed.hostname or parsed.username or parsed.password or parsed.fragment:
            raise ValueError()
        if 'REPLACE_' in address:
            raise ValueError()
    except ValueError:
        raise PreparationError('Configure the selected HTTPS Base Sepolia RPC privately.') from None
    payload = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': 'eth_chainId', 'params': []}).encode()
    request = Request(address, data=payload, headers={'Content-Type': 'application/json'}, method='POST')
    try:
        with urlopen(request, timeout=10) as response:
            data = response.read(4097)
        if len(data) > 4096:
            raise ValueError()
        result = json.loads(data)
        if result.get('error') or result.get('id') != 1 or int(result.get('result', ''), 16) != CHAIN_ID:
            raise ValueError()
    except Exception:
        raise PreparationError('RPC preflight failed or the endpoint is not Base Sepolia.') from None


def run(arguments):
    if arguments == ['validate-contracts']:
        forge('fmt', '--check')
        forge('build')
        forge('test')
        return 'Registry validated locally; no network or wallet was used.'
    if len(arguments) == 3 and arguments[:2] == ['plan', '--out']:
        write_plan(arguments[2])
        return 'Private unsigned Base Sepolia deployment plan created; broadcasting remains disabled.'
    if arguments == ['check-rpc']:
        check_rpc()
        return 'Selected RPC reports Base Sepolia chain ID 84532; no transaction was sent.'
    raise PreparationError('Use validate-contracts, plan --out NEW_PATH, or check-rpc. Signing and broadcasting are disabled.')


if __name__ == '__main__':
    try:
        print(run(sys.argv[1:]))
    except PreparationError as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
