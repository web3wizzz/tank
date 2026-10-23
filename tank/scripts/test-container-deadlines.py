"""Exercise daemon stalls using synthetic executables, never a real Docker host."""
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("test-containers.sh").resolve()
REAL_TIMEOUT = shutil.which("timeout")

# Shorten deadlines only in this injected executable. The acceptance script has
# no test-mode or environment switch that can disable its production deadlines.
TIMEOUT = r'''
import json,os,re,sys
a=sys.argv[1:]
assert a[0]=='--kill-after=5s', 'Missing forced termination grace'
assert re.fullmatch(r'[1-9][0-9]*(s|m)',a[1]), 'Unbounded/invalid deadline'
with open(os.environ['STUB_LOG'],'a') as f:
 f.write(json.dumps({'timeout':a[1], 'command':a[2]})+'\n')
os.environ['STUB_BOUNDED']='1'
os.execv(os.environ['REAL_TIMEOUT'],['timeout','--kill-after=0.1s','0.5s']+a[2:])
'''
DOCKER = r'''
import json,os,pathlib,signal,sys,time
a=sys.argv[1:]
if a[0]=='compose':
 project=a[a.index('-p')+1]
 index=1
 while a[index] in ('--env-file','-p','-f'):index+=2
 command=a[index]
 if command=='down' and '--volumes' in a: command='cleanup-down'
 elif command=='exec' and 'arc-cast' in a: command='nonce'
elif a[0]=='run':
 project=a[a.index('--name')+1].removesuffix('-runtime-inventory')
 command='inventory'
elif a[0]=='rm':
 project=a[-1].removesuffix('-runtime-inventory')
 command='cleanup-rm'
else: raise AssertionError('Unexpected Docker command')
with open(os.environ['STUB_LOG'],'a') as f:
 f.write(json.dumps({'docker':command,'project':project,'args':a,
                     'bounded':os.environ.get('STUB_BOUNDED')=='1'})+'\n')
if command==os.environ.get('STUB_FAIL'):sys.exit(2)
if command==os.environ.get('STUB_HANG'):
 signal.signal(signal.SIGTERM,signal.SIG_IGN)
 with open(os.environ['STUB_PID'],'w') as f:f.write(str(os.getpid()))
 while True:time.sleep(60)
if command=='cp':
 pathlib.Path(a[-1]).write_text(json.dumps({'token':'synthetic-only'}))
elif command=='nonce':print('7')
'''
PYTHON = r'''
import os,sys
if len(sys.argv)>1 and sys.argv[1]=='scripts/container-state-check.py':sys.exit(0)
os.execv(os.environ['REAL_PYTHON'],[os.environ['REAL_PYTHON']]+sys.argv[1:])
'''


class ContainerDeadlines(unittest.TestCase):
    def run_fixture(self, hang="", timeout_source=TIMEOUT):
        self.assertIsNotNone(REAL_TIMEOUT, "GNU timeout required")
        with tempfile.TemporaryDirectory(prefix="tank-deadline-test-") as directory:
            root = Path(directory)
            binary = root / "bin"
            binary.mkdir()
            for name, body in (("docker", DOCKER), ("timeout", timeout_source),
                               ("python3", PYTHON), ("npm", "import sys;sys.exit(0)")):
                path = binary / name
                path.write_text("#!" + sys.executable + "\n" + body)
                path.chmod(0o700)
            log, pid = root / "calls.jsonl", root / "hung.pid"
            env = {key: value for key, value in os.environ.items()
                   if not key.startswith(("TANK_", "STUB_", "GITHUB_"))}
            env.update(PATH=str(binary) + os.pathsep + os.environ["PATH"],
                       GITHUB_RUN_ID="918273", GITHUB_RUN_ATTEMPT="1",
                       TMPDIR=directory, STUB_HANG=hang, STUB_LOG=str(log),
                       STUB_FAIL="config" if hang in ("logs", "cleanup-rm") else "",
                       STUB_PID=str(pid), REAL_TIMEOUT=REAL_TIMEOUT,
                       REAL_PYTHON=sys.executable)
            process = subprocess.Popen(["bash", str(SCRIPT)], env=env,
                                       stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       start_new_session=True)
            try:
                stdout, stderr = process.communicate(timeout=8)
            except subprocess.TimeoutExpired:
                # GNU timeout uses a separate process group. A broken timeout
                # wrapper can leave its child holding our output pipes open.
                # Kill only the recorded synthetic executable, never a daemon.
                if pid.exists():
                    child = int(pid.read_text())
                    try:
                        command = (Path("/proc") / str(child) / "cmdline").read_bytes()
                        if os.fsencode(binary / "docker") in command.split(b"\0"):
                            os.kill(child, signal.SIGKILL)
                    except (FileNotFoundError, ProcessLookupError):
                        pass
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                try:
                    process.communicate(timeout=2)
                except subprocess.TimeoutExpired:
                    process.stdout.close()
                    process.stderr.close()
                    process.wait(timeout=2)
                self.fail("Acceptance/cleanup exceeded bounded daemon-stall deadline")
            calls = [json.loads(line) for line in log.read_text().splitlines()]
            docker = [call for call in calls if "docker" in call]
            self.assertTrue(docker, "Fake daemon was not exercised")
            self.assertTrue(all(call["bounded"] for call in docker),
                            "Docker operation bypassed deadline wrapper")
            projects = {call["project"] for call in docker}
            self.assertEqual(len(projects), 1, "Cleanup escaped its generated project")
            self.assertRegex(projects.pop(), r"^tank-ci-918273-1-[0-9a-f]{12}$")
            self.assertIn("cleanup-rm", [call["docker"] for call in docker])
            self.assertIn("cleanup-down", [call["docker"] for call in docker])
            for call in docker:
                if call["docker"] == "cleanup-rm":
                    self.assertEqual(call["args"],
                                     ["rm", "-f", call["project"] + "-runtime-inventory"])
                elif call["docker"] == "cleanup-down":
                    self.assertEqual(call["args"][-4:], ["down", "--volumes", "--timeout", "30"])
            # Only the harness-owned bin/log/PID remain; generated credentials and
            # fixture config are removed even after daemon-side cleanup fails.
            self.assertEqual([p for p in root.iterdir() if p.is_dir()], [binary])
            if hang:
                self.assertNotEqual(process.returncode, 0, "Stalled acceptance reported success")
                self.assertTrue(pid.exists(), "Requested stall never occurred")
                status = Path("/proc") / pid.read_text() / "stat"
                try:
                    self.assertEqual(status.read_text().split(") ", 1)[1][0], "Z",
                                     "TERM-ignoring daemon stub survived forced termination")
                except FileNotFoundError:
                    pass
                if hang == "cleanup-down":
                    self.assertIn(b"CI-owned container cleanup failed", stderr)
            else:
                self.assertEqual(process.returncode, 0, stderr.decode())
                self.assertIn(b"PASS: disposable container", stdout)
            return {call["docker"] for call in docker}

    def test_success_covers_every_docker_operation(self):
        self.assertEqual(self.run_fixture(), {
            "config", "build", "inventory", "up", "stop", "exec", "cp", "run",
            "down", "nonce", "cleanup-rm", "cleanup-down",
        })

    def test_stalled_daemon_and_cleanup_ignore_term(self):
        for command in ("config", "build", "inventory", "up", "stop", "exec", "cp",
                        "run", "down", "nonce", "logs", "cleanup-rm", "cleanup-down"):
            with self.subTest(command=command):
                self.run_fixture(command)

    def test_harness_collects_child_when_force_kill_regresses(self):
        # Deliberately exceed the harness deadline. Its failure collector must
        # terminate the separate timeout group child and finish without hanging.
        broken = TIMEOUT.replace("'--kill-after=0.1s'", "'--kill-after=60s'")
        with self.assertRaisesRegex(AssertionError, "bounded daemon-stall deadline"):
            self.run_fixture("config", timeout_source=broken)


if __name__ == "__main__":
    unittest.main()
