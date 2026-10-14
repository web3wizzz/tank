#!/usr/bin/env python3
"""Read-only hosted-pilot preflight; full failure-drill execution fails closed.

Local restore safeguards live in pilot-restore.py and tank-restore-check. Their
process ownership, maintenance fencing, strict snapshot comparison and canonical
chain reconciliation have isolated fixture coverage. Browser file/status IPC now
has correlated, allowlisted evidence; a reviewed remote adapter and candidate
activation remain unimplemented. Do not run this preflight as hosted acceptance.
"""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import shlex
import socket
import stat
import subprocess
import sys
import urllib.request
from urllib.parse import urlsplit


CHAIN_ID = 5042002
REGISTRY = "0xe1fc5025261af42dfe04788c7a09dad3ea1ec435"
RUNTIME_SHA256 = "1a6840831363e03153a827765a2a6f9d16099ab0e08c94d4cbb16aa35aed7bdc"
SAFE_NAME = re.compile(r"[A-Za-z][A-Za-z0-9_-]{0,63}\Z")
SERVICE = re.compile(r"[A-Za-z][A-Za-z0-9_.@-]{0,100}\.service\Z")
TIMER = re.compile(r"[A-Za-z][A-Za-z0-9_.@-]{0,100}\.timer\Z")
REMOTE_PATH = re.compile(r"/[A-Za-z0-9_./-]+\Z")
# This remote command is constant. All configured values travel as JSON on
# stdin or strictly validated argv; never interpolate them into a shell command.
IDENTITY_SCRIPT = '''import json, pathlib, subprocess, sys
settings = json.load(sys.stdin)
healthy = []
for service in settings["services"]:
    loaded = subprocess.run(["systemctl", "show", "--property=LoadState", "--value", service["name"]], capture_output=True, timeout=5)
    active = subprocess.run(["systemctl", "show", "--property=ActiveState", "--value", service["name"]], capture_output=True, timeout=5)
    expected = b"active" if service["active"] else b"inactive"
    healthy.append(loaded.returncode == 0 and loaded.stdout.strip() == b"loaded" and active.returncode == 0 and active.stdout.strip() == expected)
print(json.dumps({"machine_id": pathlib.Path("/etc/machine-id").read_text().strip(), "healthy": all(healthy)}))
'''
IDENTITY_COMMAND = "python3 -c " + shlex.quote(IDENTITY_SCRIPT)


class PreflightError(Exception):
    """Only static, redacted descriptions may be supplied."""


def require(condition, message="Hosted configuration is invalid."):
    if not condition:
        raise PreflightError(message)


def fields(value, expected):
    require(isinstance(value, dict) and set(value) == set(expected))


def text_value(value):
    require(isinstance(value, str) and 0 < len(value) <= 4096)
    lowered = value.lower()
    require(not any(word in lowered for word in ("replace", "placeholder", "changeme", "todo", "example", "<", ">")),
            "Placeholder configuration is refused.")
    require(not any(ord(char) < 32 for char in value))
    return value


def https_origin(value):
    text_value(value)
    try:
        parsed = urlsplit(value)
        port = parsed.port
    except ValueError:
        raise PreflightError("Hosted HTTPS origin is malformed.") from None
    require(parsed.scheme == "https" and parsed.hostname and parsed.username is None and
            parsed.password is None and not parsed.query and not parsed.fragment and
            parsed.path in ("", "/") and port in (None, 443),
            "Only public HTTPS origins without credentials or redirects are accepted.")
    require(parsed.netloc in (parsed.hostname, parsed.hostname + ":443") and
            not parsed.hostname.endswith((".localhost", ".local", ".invalid", ".test")) and
            parsed.hostname != "localhost" and "." in parsed.hostname)
    try:
        address = ipaddress.ip_address(parsed.hostname)
    except ValueError:
        require(re.fullmatch(r"[a-z0-9.-]+", parsed.hostname) is not None)
    else:
        require(address.is_global, "Private or loopback endpoints are refused.")
    return "https://" + parsed.hostname


def private_file(value):
    path = Path(text_value(str(value)))
    require(path.is_absolute(), "Private paths must be absolute.")
    try:
        require(all(not parent.is_symlink() for parent in (path, *path.parents)))
        info, parent = path.stat(), path.parent.stat()
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and
                info.st_mode & 0o077 == 0 and parent.st_uid == os.geteuid() and
                parent.st_mode & 0o077 == 0,
                "Private files and their parent directory must be owned and private.")
    except OSError:
        raise PreflightError("A required private file is unavailable.") from None
    return path


def remote_path(value):
    text_value(value)
    require(REMOTE_PATH.fullmatch(value) is not None and
            all(part not in ("", ".", "..") for part in value.split("/")[1:]),
            "Recovery paths must be absolute canonical paths.")
    return value


def host_control(value, coordinator=False):
    expected = (("url", "failure_domain", "ssh_alias", "service", "registration_service", "registration_timer")
                if coordinator else ("url", "failure_domain", "ssh_alias", "service"))
    fields(value, expected)
    https_origin(value["url"])
    require(SAFE_NAME.fullmatch(text_value(value["failure_domain"])) is not None)
    require(SAFE_NAME.fullmatch(text_value(value["ssh_alias"])) is not None)
    require(SERVICE.fullmatch(text_value(value["service"])) is not None)
    if coordinator:
        require(SERVICE.fullmatch(text_value(value["registration_service"])) is not None)
        require(TIMER.fullmatch(text_value(value["registration_timer"])) is not None)


def validate_config(config):
    fields(config, ("version", "frontend_url", "coordinator", "nodes", "credentials", "recovery", "arc"))
    require(type(config["version"]) is int and config["version"] == 1)
    origins = [https_origin(config["frontend_url"])]
    host_control(config["coordinator"], coordinator=True)
    origins.append(https_origin(config["coordinator"]["url"]))
    require(isinstance(config["nodes"], list) and len(config["nodes"]) == 4)
    domains = []
    for index, node in enumerate(config["nodes"]):
        host_control(node)
        origins.append(https_origin(node["url"]))
        domain = text_value(node["failure_domain"])
        require(SAFE_NAME.fullmatch(domain) is not None)
        domains.append(domain.lower())
    require(len(set(origins)) == len(origins), "Service HTTPS origins must be distinct.")
    require(len(set(domains)) == 4, "Four unique storage failure-domain labels are required.")
    aliases = [node["ssh_alias"] for node in config["nodes"]] + [config["coordinator"]["ssh_alias"]]
    require(len(set(alias.lower() for alias in aliases)) == 5, "SSH aliases must identify distinct machines.")
    fields(config["credentials"], ("user_a", "user_b"))
    paths = [private_file(config["credentials"][key]) for key in ("user_a", "user_b")]
    require(not os.path.samefile(*paths), "Two distinct synthetic credential files are required.")
    recovery = config["recovery"]
    fields(recovery, ("snapshot_binary", "live_database", "snapshot_database", "restored_database"))
    for value in recovery.values():
        remote_path(value)
    require(len(set(recovery.values())) == 4, "Recovery destinations must be distinct from live state.")
    fields(config["arc"], ("rpc_url", "chain_id", "registry"))
    https_origin(config["arc"]["rpc_url"])
    require(type(config["arc"]["chain_id"]) is int and config["arc"]["chain_id"] == CHAIN_ID and
            isinstance(config["arc"]["registry"], str) and config["arc"]["registry"].lower() == REGISTRY,
            "Only the existing authorized Arc Testnet registry is accepted.")
    return config


def load_config(path):
    config_path = private_file(path)
    try:
        require(config_path.stat().st_size <= 65536)
        return validate_config(json.loads(config_path.read_text()))
    except (OSError, ValueError, TypeError):
        raise PreflightError("Hosted configuration could not be parsed safely.") from None


def ssh_argv(alias, command):
    require(SAFE_NAME.fullmatch(text_value(alias)) is not None)
    return ["ssh", "-T", "-oBatchMode=yes", "-oStrictHostKeyChecking=yes",
            "-oConnectTimeout=10", "--", alias, *command]


def service_argv(host, action, registration=False):
    require(action in ("stop", "start", "restart"))
    name = host["registration_service" if registration else "service"]
    require(SERVICE.fullmatch(text_value(name)) is not None)
    return ssh_argv(host["ssh_alias"], ["sudo", "-n", "systemctl", action, "--", name])


def snapshot_argv(config):
    recovery = config["recovery"]
    values = [remote_path(recovery[key]) for key in ("snapshot_binary", "live_database", "snapshot_database")]
    return ssh_argv(config["coordinator"]["ssh_alias"],
                    ["sudo", "-n", "-u", "tank", "--", values[0], "--db", values[1], "--out", values[2]])


def public_dns(origin):
    try:
        answers = socket.getaddrinfo(urlsplit(origin).hostname, 443, type=socket.SOCK_STREAM)
        require(answers and all(ipaddress.ip_address(answer[4][0]).is_global for answer in answers),
                "Hosted DNS resolved to a private or loopback address.")
    except OSError:
        raise PreflightError("Hosted DNS resolution failed.") from None


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def request(origin, suffix="", payload=None):
    public_dns(origin)
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(origin.rstrip("/") + suffix, data=data,
                                 headers={"Content-Type": "application/json"} if data else {})
    try:
        with urllib.request.build_opener(NoRedirect).open(req, timeout=10) as response:
            require(response.status == 200, "Hosted HTTPS preflight returned an unexpected status.")
            body = response.read(262145)
            require(len(body) <= 262144, "Hosted HTTPS preflight response exceeded its bound.")
            return body
    except PreflightError:
        raise
    except Exception:
        raise PreflightError("Hosted TLS/HTTPS transport failed; provider details are redacted.") from None


def rpc(config, method, params):
    try:
        value = json.loads(request(config["arc"]["rpc_url"], payload={"jsonrpc": "2.0", "id": 1, "method": method, "params": params}))
        require(isinstance(value, dict) and value.get("id") == 1 and "error" not in value and "result" in value,
                "Arc read-only JSON-RPC preflight failed; response details are redacted.")
        return value["result"]
    except (ValueError, TypeError):
        raise PreflightError("Arc read-only JSON-RPC preflight returned an invalid response.") from None


def identity(host):
    services = [{"name": host["service"], "active": True}]
    if "registration_service" in host:
        services.append({"name": host["registration_service"], "active": False})
        services.append({"name": host["registration_timer"], "active": True})
    try:
        result = subprocess.run(ssh_argv(host["ssh_alias"], [IDENTITY_COMMAND]),
                                input=json.dumps({"services": services}).encode(),
                                capture_output=True, timeout=30, check=False)
        require(result.returncode == 0 and len(result.stdout) <= 4096,
                "Read-only SSH identity preflight failed; command output is redacted.")
        value = json.loads(result.stdout)
        require(value.get("healthy") is True and re.fullmatch(r"[0-9a-f]{32}", value.get("machine_id", "")) is not None,
                "Read-only SSH identity or installed-service verification failed.")
        return value["machine_id"]
    except (OSError, subprocess.TimeoutExpired, ValueError, TypeError):
        raise PreflightError("Read-only SSH identity preflight failed; command output is redacted.") from None


def preflight(config):
    request(config["frontend_url"])
    request(config["coordinator"]["url"], "/health")
    for node in config["nodes"]:
        request(node["url"], "/health")
    require(rpc(config, "eth_chainId", []) == hex(CHAIN_ID), "Arc RPC is not the authorized testnet.")
    code = rpc(config, "eth_getCode", [config["arc"]["registry"], "latest"])
    try:
        require(isinstance(code, str) and code.startswith("0x") and
                hashlib.sha256(bytes.fromhex(code[2:])).hexdigest() == RUNTIME_SHA256,
                "Arc registry runtime differs from the previously verified deployment.")
    except ValueError:
        raise PreflightError("Arc registry runtime response is invalid.") from None
    hosts = [*config["nodes"], config["coordinator"]]
    require(len(set(identity(host) for host in hosts)) == len(hosts),
            "SSH controls resolved to duplicate machine identities.")


class RedactedParser(argparse.ArgumentParser):
    def error(self, message):
        raise PreflightError("Invalid command arguments; supplied values are redacted.")


def main(argv=None):
    parser = RedactedParser(description=__doc__)
    parser.add_argument("--config", required=True, help="owned mode-0600 JSON in a private directory")
    parser.add_argument("--validate-config-only", action="store_true", help="skip all network and SSH operations")
    parser.add_argument("--dry-run", action="store_true", help="validate configuration and describe safety gates without network or service changes")
    parser.add_argument("--execute-hosted-drill", action="store_true", help="reserved execution gate; currently fails closed")
    args = parser.parse_args(argv)
    config = load_config(args.config)
    if args.execute_hosted_drill:
        raise PreflightError("Hosted drill execution is unavailable: local restore safeguards and correlated browser evidence require reviewed remote ownership/activation integration. Explicit hosts/operator access must also be configured. No services were changed.")
    if args.validate_config_only or args.dry_run:
        print("PASS: hosted configuration and private-file metadata validated; no network or host checks performed.")
        if args.dry_run:
            print("PLAN: identify five owned service hosts and selected node failure targets; acquire the shared signing lane; persist a maintenance gate; stop timer/coordinator and freeze metadata; reject stale state; create an inactive restore copy; verify signed history, revocations, budgets and canonical Arc receipts/nonces before any signing resumes. Remote execution is disabled.")
    else:
        preflight(config)
        print("PASS: read-only HTTPS, Arc chain/runtime, SSH machine identity and installed-service preflight.")
    print("Full hosted acceptance has NOT run. Failure-domain labels are operator declarations; machine IDs do not establish independent physical infrastructure.")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except PreflightError as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("Hosted preflight interrupted; no service mutations were performed.", file=sys.stderr)
        sys.exit(130)
    except Exception:
        print("Hosted preflight failed; diagnostic values are redacted.", file=sys.stderr)
        sys.exit(1)
