"""Disposable pinned OpenSSH container; actual Burrow -> Hovel -> module path."""
import base64
import argparse
from contextlib import closing
import hashlib
import http.client
import fcntl
import json
import os
from pathlib import Path
import signal
import pty
import select
import shlex
import shutil
import sqlite3
import socket
import struct
import subprocess
import tarfile
import tempfile
import termios
import time

from core.cmd.burrow.authentication_lab import authentication_matrix
from core.cmd.burrow.manager_lab import manager_checks, audit_cleanup_checks
from core.cmd.burrow.latency_lab import measure, phase_totals
from core.cmd.burrow.shell_lab import shell_checks
from core.cmd.burrow.forward_lab import forward_checks, reverse_checks, forward_ui
from core.cmd.burrow.files_lab import file_checks, load_checks, file_ui
from core.cmd.burrow.runs_lab import run_checks, run_ui
from core.cmd.burrow.follow_lab import follow_checks
from core.cmd.burrow.automation_lab import automation_checks
from core.cmd.burrow.reports_lab import report_checks

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("paths", nargs=7, metavar="PATH")
parser.add_argument("--smoke", action="store_true", help="check key/trust/retention/close only; not full acceptance")
parser.add_argument("--runs-check", action="store_true", help="check retained remote command lifecycle only")
parser.add_argument("--follow-check", action="store_true", help="check independent live output readers and viewers")
parser.add_argument("--reports-check", action="store_true", help="check Ubuntu survey, artifacts and independent report reader")
parser.add_argument("--scripts-check", action="store_true", help="check script inputs and staging through retained runs")
parser.add_argument("--automation-check", action="store_true", help="check selected local tools and supported Hovel automation")
parser.add_argument("--shell-check", action="store_true", help="check real interactive SSH shell only")
parser.add_argument("--files-check", action="store_true", help="check real SFTP browsing only")
parser.add_argument("--forward-check", action="store_true", help="check real local forwarding only")
parser.add_argument("--reverse-check", action="store_true", help="check real reverse forwarding only")
parser.add_argument("--proxy-check", action="store_true", help="check real connection-owned SOCKS traffic and cleanup")
parser.add_argument("--measure", action="store_true", help="record production phase samples and separate process traces")
parser.add_argument("--prompt-check", action="store_true", help="check private prompt and sibling-control responsiveness only")
parser.add_argument("--auth-check", action="store_true", help="check private prompts, cancellation and rejected passwords only")
args = parser.parse_args()
smoke = args.smoke
binary, wheel, image_file, screen_check, legacy_binary, vim_apk, survey_script = [str(Path(p).resolve()) for p in args.paths]
image = Path(image_file).read_text().strip()
started = stage_started = time.monotonic()

def timing(stage):
    global stage_started
    now = time.monotonic()
    print(f"TIMING {stage}: {now - stage_started:.1f}s (total {now - started:.1f}s)", flush=True)
    stage_started = now

def interrupted(signum, _frame):
    raise SystemExit(f"acceptance interrupted by signal {signum}")
for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGALRM):
    signal.signal(signum, interrupted)
signal.alarm(1200 if args.measure else 600)

def command(*args, env=None, ok=True):
    p = subprocess.run(list(map(str, args)), env=env, capture_output=True, text=True, timeout=60)
    assert (p.returncode == 0) == ok, (args[:3], p.stdout, p.stderr)
    return p.stdout if ok else p.stdout + p.stderr

def wait(check):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        result = check()
        if result:
            return result
        time.sleep(.1)
    raise AssertionError("transition timed out")

with tempfile.TemporaryDirectory(prefix="bs-") as scratch:
    root = Path(scratch)
    # Frontends and retained modules must keep the same build even if the
    # shared checkout is rebuilt while this disposable lab is running.
    binary = str(shutil.copy2(binary, root / "burrow"))
    env = {k: v for k, v in os.environ.items() if not k.startswith(("HOVEL_", "SSH_"))}
    env.update(HOME=scratch, XDG_CACHE_HOME=str(root / "cache"), XDG_CONFIG_HOME=str(root / "config"), NO_COLOR="1", TERM="xterm-256color")
    env["AUTOMATION_SECRET_CANARY"] = "AUTOMATION-NOT-A-CREDENTIAL"
    daemons = []
    children = []
    forward_evidence = []
    full_evidence = False
    container = None
    key = root / "client key"
    command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key)
    try:
        container = command("docker", "run", "-d", "--rm", "--publish", "127.0.0.1::2222",
                            "--env", "USER_NAME=tester", "--env", "PASSWORD_ACCESS=true",
                            "--env", "PUBLIC_KEY_FILE=/client.pub", "--mount",
                            f"type=bind,src={key}.pub,dst=/client.pub,readonly", image).strip()
        port = int(command("docker", "port", container, "2222/tcp").strip().rsplit(":", 1)[1])
        def ready():
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=1) as s:
                    return s.recv(128).startswith(b"SSH-")
            except OSError:
                return False
        wait(ready)
        # This sandbox image disables forwarding by default; the jump-host
        # fixture explicitly enables TCP forwarding and reloads only its sshd.
        # OpenSSH 9.8+ also penalizes a source address after rapid authentication
        # failures; the deliberate failure scenarios below would then block the
        # following connects (#77 measured this once Burrow got faster). The
        # disposable server's brute-force defense is not under test here.
        command("docker", "exec", container, "sh", "-c",
                "sed -i 's/^AllowTcpForwarding no$/AllowTcpForwarding yes/' /config/sshd/sshd_config && "
                "echo 'PerSourcePenalties no' >> /config/sshd/sshd_config && kill -HUP $(cat /config/sshd.pid)")
        hostkey = command("docker", "exec", container, "cat", "/config/ssh_host_keys/ssh_host_ed25519_key.pub").split()
        fingerprint = "SHA256:" + base64.b64encode(hashlib.sha256(base64.b64decode(hostkey[1])).digest()).decode().rstrip("=")
        def burrow(w, *args, ok=True):
            submitted = time.monotonic()
            out = command(binary, "--workspace", w, *args, env=env, ok=ok)
            if ok and args[0] in ("connect", "reconnect") and "--yes" in args:
                # Response can precede authentication: never label this dispatch
                # or full connection latency. --measure records the separate phases.
                print(f"TIMING {args[0]} {args[1]} submission-to-CLI-return: "
                      f"{time.monotonic() - submitted:.3f}s", flush=True)
            return json.loads(out) if ok else out
        w = root / "w"
        info = burrow(w, "--hovel-package", wheel, "status")
        daemons.append(info["pid"])
        hovel = root / "cache/burrow/hovel/0.4.2/hovel"
        def hv(*args, chain="consolidation"):
            return command(hovel, "run", "--workspace", w, "--daemon-endpoint", w / "hoveld.sock",
                           "--op", "burrow", "--chain", chain, "--", *args, env=env)
        def catalog():
            modules = json.loads(hv("module", "list", "--json"))["modules"]
            return sorted(m["id"] for m in modules if m["name"].startswith("burrow"))
        assert catalog() == ["burrow@0.1.0"]
        timing("fixture and workspace setup")
        if args.measure:
            measure(binary,root,env,container,port,key,command,wait)
            raise SystemExit(0)
        options = ["--key", str(key), "--port", str(port), "--yes"]
        phases = root / "phases.jsonl"
        env["BURROW_PHASE_TRACE"] = str(phases)
        def state_is(w, name, expected):
            s = burrow(w, "inspect", name)
            return s if s["state"] == expected else None
        if args.prompt_check or args.auth_check:
            burrow(w,"connect","gateway","127.0.0.1","tester",*options)
            wait(lambda:state_is(w,"gateway","connected"))
            authentication_matrix(binary,w,root,env,container,port,key,fingerprint,burrow,wait,screen_check,prompt_only=args.prompt_check,auth_only=args.auth_check)
            raise SystemExit(0)
        # Owner-approved LazySSH policy authenticates without host-key approval.
        plain = ["--key", str(key), "--port", str(port), "--yes"]
        burrow(w, "connect", "unknown", "127.0.0.1", "tester", *plain)
        wait(lambda: state_is(w, "unknown", "connected"))
        burrow(w, "close", "unknown", "--yes")
        review = burrow(w, "connect", "gateway", "127.0.0.1", "tester", *options[:-1])
        assert "SSH command:" in review["review"] and "StrictHostKeyChecking=no" in review["review"]
        assert "Generated config:" not in review["review"] and not (w / "burrow/gateway").exists()
        assert review["review"].startswith("SSH command:\n/usr/bin/ssh -F ")
        assert "SOCKS proxy: Off" in review["review"]
        phase_offset = phases.stat().st_size if phases.exists() else 0
        submitted = time.monotonic_ns()
        first = burrow(w, "connect", "gateway", "127.0.0.1", "tester", *options)
        def connected():
            state = burrow(w, "inspect", "gateway")
            assert state["state"] != "lost", state
            return state if state["state"] == "connected" else None
        first = wait(connected)
        # Check placement (#77): a warm connect re-verifies the daemon before every
        # RPC, CLI launch, adapter start and reservation, but hashes each unchanged
        # executable once per process (frontend, throw adapter) and never re-reads
        # the module inventory; that is startup work bound by the throw's build digest.
        # The window also includes the inspect polling above, which only adds
        # verifications; the hash bound is what proves reuse.
        totals = phase_totals(phases, phase_offset, submitted, first["connected"])
        assert totals["status"]["count"] >= 10 and totals["hovel-cli:throw"]["count"] == 1, totals
        assert totals["hash"]["count"] <= 4, totals
        assert not {"hovel-cli:module", "register-module", "call:GetModuleCatalog"} & set(totals), totals
        actual = Path(f'/proc/{first["masterPID"]}/cmdline').read_bytes().split(b"\0")[:-1]
        shown = review["review"].split("SSH command:\n",1)[1].split("\n\n",1)[0]
        assert shlex.split(shown) == [a.decode() for a in actual]
        assert b"-D" not in actual and not first.get("proxyPort")
        assert first["generation"] and first["creation"] and first["runID"]
        assert first["connected"] >= first["dispatch"] > 0
        if args.reports_check:
            report_checks(burrow, w, first, hv, binary, env, screen_check, survey_script)
            burrow(w, "close", "gateway", "--yes")
            retained = burrow(w, "reports")
            assert retained, "connection close lost reports"
            assert burrow(w, "report", retained[0]["id"])["markdown"], "closed connection report unreadable"
            raise SystemExit(0)
        if args.automation_check:
            automation_checks(burrow, w, first, hovel, env, hv, options)
            run_ui(binary, env, screen_check, burrow, w, first["name"], local=True)
            burrow(w, "close", "gateway", "--yes")
            raise SystemExit(0)
        if args.follow_check:
            follow_checks(burrow, w, first, container, command, hv, binary, env, screen_check)
            burrow(w, "close", "gateway", "--yes")
            raise SystemExit(0)
        if args.runs_check or args.scripts_check:
            run_checks(burrow, w, first, container, command, hv, binary, env, screen_check, scripts_only=args.scripts_check)
            burrow(w, "close", "gateway", "--yes")
            raise SystemExit(0)
        if not (smoke or args.proxy_check or args.shell_check or args.forward_check or args.reverse_check or args.files_check):
            burrow(w, "connect", "runs", "127.0.0.1", "tester", *options)
            run_owner = wait(lambda: state_is(w, "runs", "connected"))
            report_checks(burrow, w, run_owner, hv, binary, env, screen_check, survey_script)
            automation_checks(burrow, w, run_owner, hovel, env, hv, options)
            run_ui(binary, env, screen_check, burrow, w, run_owner["name"], local=True)
            follow_checks(burrow, w, run_owner, container, command, hv, binary, env, screen_check)
            run_checks(burrow, w, run_owner, container, command, hv, binary, env, screen_check)
            burrow(w, "close", "runs", "--yes")
            timing("retained remote runs")
        if args.files_check or not (smoke or args.proxy_check or args.shell_check or args.forward_check or args.reverse_check):
            file_checks(burrow, w, first, container, command)
            file_ui(binary, env, screen_check, w)
            load_checks(burrow, w, container, command, options, binary, env, screen_check)
        if args.files_check:
            audit_cleanup_checks(burrow, root, options, daemons)
            burrow(w, "close", "gateway", "--yes")
            retained = burrow(w, "downloads")
            os.kill(first["ownerPID"], signal.SIGKILL)
            durable = burrow(w, "downloads")
            assert durable["files"] == retained["files"] > 0
            assert durable["bytes"] == retained["bytes"]
            assert {d["id"] for d in durable["records"]} == {d["id"] for d in retained["records"]}
            print("PASS durable download outcomes/totals after retained owner loss", flush=True)
            raise SystemExit(0)
        if not smoke and not args.proxy_check and not args.shell_check and not args.forward_check:
            forward_evidence.append(reverse_checks(binary, env, screen_check, burrow, w, first, options, container, command))
        if args.reverse_check:
            burrow(w, "close", "gateway", "--yes")
            raise SystemExit(0)
        if not smoke and not args.proxy_check:
            forward_evidence.append(forward_checks(binary, env, screen_check, burrow, w, first, options, container, command))
        if args.forward_check:
            burrow(w, "close", "gateway", "--yes")
            raise SystemExit(0)
        if not smoke and not args.proxy_check:
            # Extract only the declared executable, never APK paths or scripts.
            with tarfile.open(vim_apk, "r:gz", ignore_zeros=True) as archive:
                vim = root / "vim"
                vim.write_bytes(archive.extractfile("usr/bin/vim").read())
            vim.chmod(0o755)
            command("docker", "cp", vim, container + ":/usr/local/bin/vim")
            first = shell_checks(binary, w, env, screen_check, burrow, first, options)
        if args.shell_check:
            burrow(w, "close", "gateway", "--yes")
            raise SystemExit(0)
        # Real SOCKS5 negotiation reaches the container's loopback SSH service,
        # including remote DNS. No proxy implementation or extra tool dependency.
        with socket.socket() as held:
            held.bind(("127.0.0.1", 0))
            held.listen()
            occupied = held.getsockname()[1]
            proxy_config = root / "proxy-config"
            proxy_config.write_text(f"Host *\n DynamicForward 127.0.0.1:{occupied}\n GatewayPorts yes\n")
            with socket.socket() as free:
                free.bind(("127.0.0.1", 0))
                proxy_port = free.getsockname()[1]
            proxy_options = [*options, "--ssh-config", str(proxy_config), "-proxy", str(proxy_port)]
            burrow(w, "connect", "socks", "127.0.0.1", "tester", *proxy_options)
            proxied = wait(lambda: state_is(w, "socks", "connected"))
            assert proxied["proxyPort"] == proxy_port
            proxy = burrow(w, "proxy", "inspect", "socks")
            assert proxy["listen"] == f"127.0.0.1:{proxy_port}" and proxy["state"] == "listening"
            assert proxy["connectionCreation"] == proxied["creation"]
            assert burrow(w, "tunnel", "list") == [] and proxied["tunnelCount"] == 0
            def socks_banner(bind="127.0.0.1", port=None):
                with socket.create_connection((bind, port or proxy_port), timeout=3) as client:
                    stream = client.makefile("rb")
                    client.sendall(b"\x05\x01\x00")
                    assert stream.read(2) == b"\x05\x00"
                    host = b"localhost"
                    client.sendall(b"\x05\x01\x00\x03" + bytes([len(host)]) + host + struct.pack("!H", 2222))
                    reply = stream.read(4)
                    assert reply[:3] == b"\x05\x00\x00", reply
                    address_size = {1: 4, 4: 16}.get(reply[3])
                    assert address_size is not None, reply
                    assert len(stream.read(address_size + 2)) == address_size + 2
                    assert stream.readline().startswith(b"SSH-"), "SOCKS did not reach remote SSH"
                    stream.close()
            socks_banner()
            actual_proxy = Path(f'/proc/{proxied["masterPID"]}/cmdline').read_bytes().split(b"\0")
            assert actual_proxy[actual_proxy.index(b"-D")+1] == f"127.0.0.1:{proxy_port}".encode()
            burrow(w, "profile", "save", "socks", "--as", "saved-socks")
            assert burrow(w, "profile", "select", "saved-socks")["proxyPort"] == proxy_port
            burrow(w, "connect", "socks-busy", "127.0.0.1", "tester", *options, "-proxy", str(occupied))
            failed = wait(lambda: state_is(w, "socks-busy", "lost"))
            assert "proxy could not bind" in failed["detail"], failed
            burrow(w, "close", "socks-busy", "--yes")
            # Failed connection cleanup must not close another process's listener.
            with socket.create_connection(("127.0.0.1", occupied), timeout=1):
                pass
            def proxy_closed():
                with socket.socket() as probe:
                    probe.settimeout(.5)
                    return probe.connect_ex(("127.0.0.1", proxy_port)) != 0
            # Remove connect-time -D without touching the authenticated master.
            removal = burrow(w, "proxy", "remove", "socks")
            socks_banner()
            burrow(w, "proxy", "remove", "socks", "--review", "0" * 64, "--yes", ok=False)
            socks_banner()
            burrow(w, "proxy", "remove", "socks", "--review", removal["digest"], "--yes")
            wait(proxy_closed)
            assert burrow(w, "proxy", "inspect", "socks")["state"] == "off"
            assert not burrow(w, "inspect", "socks").get("proxyPort")
            create = ("proxy", "create", "socks", str(proxy_port))
            review = burrow(w, *create)
            assert proxy_closed(), "review allocated a SOCKS listener"
            burrow(w, *create, "--review", "0" * 64, "--yes", ok=False)
            assert proxy_closed()
            burrow(w, "proxy", "create", "socks", str(occupied), "--yes", ok=False)
            assert burrow(w, "proxy", "inspect", "socks")["state"] == "off"
            created_proxy = burrow(w, *create, "--review", review["digest"], "--yes")
            forward_evidence.append(created_proxy)
            assert created_proxy["id"] != proxy["id"] and created_proxy["direction"] == "D"
            assert burrow(w, "inspect", "socks")["masterPID"] == proxied["masterPID"]
            socks_banner()
            burrow(w, "proxy", "remove", "socks", "--review", removal["digest"], "--yes", ok=False)
            socks_banner()
            # One connection-owned proxy, with no L/R identity or count consumption.
            burrow(w, *create, "--yes", ok=False)
            assert burrow(w, "tunnel", "list") == []
            with socket.socket() as free:
                free.bind(("127.0.0.1", 0))
                local_port = free.getsockname()[1]
            local = burrow(w, "tunc", "socks", "l", str(local_port), "localhost", "2222", "--yes")
            reverse = burrow(w, "tunc", "socks", "r", "0", "127.0.0.1", str(local_port), "--yes")
            assert burrow(w, "inspect", "socks")["tunnelCount"] == 2
            assert len(burrow(w, "tunnel", "list")) == 2
            burrow(w, "proxy", "remove", "socks", "--yes")
            wait(proxy_closed)
            assert burrow(w, "tunnel", "check", local["id"])["state"] == "traffic-observed"
            assert burrow(w, "tunnel", "check", reverse["id"])["state"] == "traffic-observed"
            assert burrow(w, "inspect", "socks")["masterPID"] == proxied["masterPID"]
            # Explicit non-default loopback, wildcard and IPv6 binds are observed.
            for bind in ("127.0.0.2", "0.0.0.0", "[::1]"):
                endpoint = f"{bind}:{proxy_port}"
                broad = burrow(w, "proxy", "create", "socks", endpoint, "--yes")
                assert burrow(w, "proxy", "inspect", "socks")["listen"] == endpoint
                assert broad["state"] == "listening"
                socks_banner({"0.0.0.0": "127.0.0.1", "[::1]": "::1"}.get(bind, bind))
                burrow(w, "proxy", "remove", "socks", "--yes")
            burrow(w, *create, "--yes")
            socks_banner()
            burrow(w, "close", "socks", "--yes")
            wait(proxy_closed)
            burrow(w, "profile", "connect", "saved-socks", "--as", "socks-restored", "--yes")
            restored = wait(lambda: state_is(w, "socks-restored", "connected"))
            assert restored["proxyPort"] == proxy_port
            socks_banner()
            os.kill(restored["masterPID"], signal.SIGKILL)
            wait(lambda: state_is(w, "socks-restored", "lost"))
            assert burrow(w, "proxy", "inspect", "socks-restored")["state"] == "unavailable"
            burrow(w, "proxy", "create", "socks-restored", str(proxy_port), "--yes", ok=False)
            burrow(w, "proxy", "remove", "socks-restored", "--yes", ok=False)
            wait(proxy_closed)
            burrow(w, "close", "socks-restored", "--yes")
            assert burrow(w, "inspect", "gateway")["masterPID"] == first["masterPID"]
            retained = forward_ui(binary, env, screen_check, burrow, w, lambda: proxy_port, [], proxy=True)
            socks_banner()
            forward_evidence.append(retained)
            burrow(w, "proxy", "remove", "gateway", "--yes")
            wait(proxy_closed)
        print("PASS SOCKS traffic/remote DNS, explicit loopback, config forwarding isolation, occupied-port refusal, profile round-trip, close/loss and sibling preservation", flush=True)
        if args.proxy_check:
            burrow(w, "close", "gateway", "--yes")
            raise SystemExit(0)
        audit_cleanup_checks(burrow, root, options, daemons)
        manager_checks(binary,w,root,env,port,key,first,burrow,wait)
        # Both production capabilities use the one public identity.
        assert catalog() == ["burrow@0.1.0"]
        hv("chain", "create", "consolidation")
        hv("chain", "add", "burrow@0.1.0")
        hv("target", "add", "local")
        hv("chain", "config", "set", "workspace", str(w))
        hv("chain", "config", "set", "command", "profile create catalog-profile host user")
        profile_result = json.loads(hv("throw", "--now", "--allow-dangerous", "--json"))
        assert profile_result["results"][0]["state"] == "succeeded", profile_result
        assert burrow(w, "profile", "select", "catalog-profile")["host"] == "host"
        assert catalog() == ["burrow@0.1.0"]
        burrow(w,"profile","save","gateway","--as","saved-gateway")
        profile = burrow(w,"profile","select","saved-gateway")
        assert profile["key"] == str(key) and "trust" not in profile and "promptSocket" not in profile
        profile_backup = root / "saved-backup.json"
        burrow(w,"profile","backup",str(profile_backup))
        burrow(w,"profile","edit","saved-gateway","192.0.2.50","tester","--yes")
        burrow(w,"profile","delete","saved-gateway","--yes")
        assert burrow(w,"inspect","gateway")["masterPID"] == first["masterPID"]
        burrow(w,"profile","load",str(profile_backup))
        saved_review=burrow(w,"profile","connect","saved-gateway","--as","saved-live")
        assert "Generated config:" not in saved_review["review"] and saved_review["digest"]
        burrow(w,"profile","connect","saved-gateway","--as","saved-live","--yes")
        wait(lambda: state_is(w,"saved-live","connected"))
        burrow(w,"close","saved-live","--yes")
        # Default selection restored for the remaining independent checks.
        burrow(w,"profile","load",str(w / "burrow-profiles.json"))
        assert Path(first["socket"]).is_socket()
        assert Path(f'/proc/{first["masterPID"]}').exists()
        assert b"-N\0" in Path(f'/proc/{first["masterPID"]}/cmdline').read_bytes()
        assert burrow(w, "--offline", "status")["pid"] == info["pid"]
        assert burrow(w, "inspect", "gateway")["masterPID"] == first["masterPID"]
        burrow(w, "connect", "gateway", "127.0.0.1", "tester", *options, ok=False)
        assert burrow(w, "inspect", "gateway")["socketInode"] == first["socketInode"]
        timing("key authentication, trust, retention and collision")
        if smoke:
            burrow(w, "close", "gateway", "--yes")
            assert not Path(first["socket"]).parent.exists()
            timing("explicit close")
            print("PASS SSH smoke only; full authentication/PTY/lifecycle matrix not run", flush=True)
            raise SystemExit(0)
        # Both historical catalog identities remain inspectable without adoption.
        for module in ("burrow-connection", "burrow"):
            legacy_package = root / ("legacy-package-" + module)
            legacy_package.mkdir(mode=0o700)
            (legacy_package / "burrow").write_bytes(Path(legacy_binary).read_bytes())
            (legacy_package / "burrow").chmod(0o700)
            (legacy_package / "hovel-module.yaml").write_text(f"""apiVersion: hovel.dev/v1alpha1
kind: ModulePackage
metadata:
  name: {module}
  version: 0.1.0
  moduleType: survey
runtime:
  protocol: jsonrpc-stdio
launch:
  - selector:
      os: linux
      arch: amd64
    command: {json.dumps(["burrow", "base-module"] if module == "burrow" else ["burrow"])}
""")
            hv("module", "install", "--link", str(legacy_package), "--no-scripts")
            hv("chain", "create", module, chain=module)
            hv("chain", "add", module + "@0.1.0", chain=module)
            hv("target", "add", "ssh://127.0.0.1", chain=module)
            hv("chain", "config", "set", "workspace", str(w), chain=module)
            legacy_config = dict(workspace=str(w), name="legacy", host="127.0.0.1", user="tester",
                                 port=port, key=str(key), knownHosts=str(w / "burrow-known_hosts"), trust=fingerprint)
            hv("chain", "config", "set", "connection", json.dumps(legacy_config), chain=module)
            result = json.loads(hv("throw", "--now", "--allow-dangerous", "--json", chain=module))
            assert result["results"][0]["state"] == "succeeded", result
            legacy = wait(lambda: state_is(w, "legacy", "connected"))
            evidence = w / "legacy-evidence"
            evidence.write_text("preserve legacy evidence")
            assert burrow(w, "--offline", "status")["pid"] == info["pid"]
            assert burrow(w, "inspect", "legacy")["masterPID"] == legacy["masterPID"]
            burrow(w, "connect", "legacy", "127.0.0.1", "tester", *options, ok=False)
            assert burrow(w, "inspect", "legacy")["socketInode"] == legacy["socketInode"]
            burrow(w, "profile", "save", "legacy", "--as", "legacy-settings")
            assert "review" in burrow(w, "close", "legacy")
            burrow(w, "close", "legacy", "--yes")
            assert burrow(w, "inspect", "gateway")["masterPID"] == first["masterPID"]
            assert evidence.read_text() == "preserve legacy evidence"
            assert burrow(w, "profile", "select", "legacy-settings")["host"] == "127.0.0.1"
            # Manual uninstall after owners are closed; chain/evidence remain historical.
            if module != "burrow":
                hv("module", "uninstall", module + "@0.1.0")
            assert catalog() == ["burrow@0.1.0"]
        retired = subprocess.run([binary, "connection-module"], env=env, capture_output=True, text=True)
        assert retired.returncode != 0 and not retired.stdout and "retired" in retired.stderr
        timing("single module and legacy owner transition")
        # Refusals leave symlinks, unsafe permissions and substituted roots intact.
        alias = w / "burrow/alias"
        alias.symlink_to(Path(first["socket"]).parent, target_is_directory=True)
        burrow(w, "connect", "alias", "127.0.0.1", "tester", *options, ok=False)
        assert alias.is_symlink()
        alias.unlink()
        runtime = w / "burrow"
        runtime.chmod(0o755)
        burrow(w, "connect", "unsafe", "127.0.0.1", "tester", *options, ok=False)
        assert runtime.stat().st_mode & 0o777 == 0o755 and not (runtime / "unsafe").exists()
        runtime.chmod(0o700)
        reservation = Path(first["socket"]).parent
        reservation.chmod(0o755)
        burrow(w, "close", "gateway", "--yes", ok=False)
        assert reservation.stat().st_mode & 0o777 == 0o755
        reservation.chmod(0o700)
        moved = runtime / "moved"
        reservation.rename(moved)
        reservation.mkdir(mode=0o700)
        marker = reservation / "unknown"
        marker.write_text("leave me")
        burrow(w, "close", "gateway", "--yes", ok=False)
        assert marker.read_text() == "leave me"
        marker.unlink()
        reservation.rmdir()
        moved.rename(reservation)
        assert burrow(w, "inspect", "gateway")["state"] == "connected"
        # The same public operation works in another verified workspace.
        other = root / "other"
        daemons.append(burrow(other, "--offline", "status")["pid"])
        burrow(other, "connect", "gateway", "127.0.0.1", "tester", *options)
        wait(lambda: burrow(other, "inspect", "gateway")["state"] == "connected")
        assert burrow(other, "inspect", "gateway")["socket"] != first["socket"]
        # Kill the actual master; read-only inspection must never authenticate.
        os.kill(first["masterPID"], signal.SIGKILL)
        wait(lambda: burrow(w, "inspect", "gateway")["state"] == "lost")
        assert burrow(other, "inspect", "gateway")["state"] == "connected"
        burrow(w, "reconnect", "gateway", "127.0.0.1", "tester", *options)
        second = wait(connected)
        assert second["masterPID"] != first["masterPID"]
        # Closing/reconnecting does not introduce another host-trust prompt.
        burrow(w, "close", "gateway", "--yes")
        without_trust = ["--key", str(key), "--port", str(port), "--yes"]
        burrow(w, "connect", "gateway", "127.0.0.1", "tester", *without_trust)
        first = wait(connected)
        # Legacy host records are preserved, but no longer govern authentication.
        trust_file = w / "burrow-known_hosts"
        trusted = trust_file.read_bytes() if trust_file.exists() else b""
        trust_file.write_text(f"[127.0.0.1]:{port} " + key.with_suffix(".pub").read_text())
        burrow(w, "connect", "changed", "127.0.0.1", "tester", *options)
        changed = wait(lambda: state_is(w, "changed", "connected"))
        burrow(w, "close", "changed", "--yes")
        trust_file.write_bytes(trusted)
        # Failed public-key authentication and cancellation of slow discovery.
        wrong = root / "wrong"
        command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", wrong)
        burrow(w, "connect", "denied", "127.0.0.1", "tester", "--key", wrong, "--port", port, "--yes")
        denied = wait(lambda: state_is(w, "denied", "lost"))
        assert "authentication failed" in denied["detail"]
        burrow(w, "close", "denied", "--yes")
        with socket.socket() as stalled:
            stalled.bind(("127.0.0.1", 0))
            stalled.listen(16)
            burrow(w, "connect", "cancelled", "127.0.0.1", "tester", "--key", key,
                   "--port", stalled.getsockname()[1], "--yes")
            burrow(w, "close", "cancelled", "--yes")
            assert not (w / "burrow/cancelled").exists()
        # Agent-only authentication with an isolated agent, never the user's.
        agent_socket = root / "agent.sock"
        agent = subprocess.Popen(["ssh-agent", "-D", "-a", str(agent_socket)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        children.append(agent)
        wait(agent_socket.exists)
        command("ssh-add", key, env=env | {"SSH_AUTH_SOCK": str(agent_socket)})
        burrow(w, "connect", "agent", "127.0.0.1", "tester", "--agent", agent_socket, "--port", port, "--yes")
        wait(lambda: state_is(w, "agent", "connected"))
        burrow(w, "close", "agent", "--yes")
        # Synthetic credential material is never persisted by the production
        # command path, even when an encrypted key cannot authenticate in batch.
        canary = "burrow-synthetic-credential-" + os.urandom(16).hex()
        encrypted = root / "encrypted"
        command("ssh-keygen", "-q", "-t", "ed25519", "-N", canary, "-f", encrypted)
        burrow(w, "connect", "encrypted", "127.0.0.1", "tester", "--key", encrypted, "--port", port, "--yes")
        wait(lambda: state_is(w, "encrypted", "lost"))
        burrow(w, "close", "encrypted", "--yes")
        for process in (first["masterPID"], first["ownerPID"], info["pid"]):
            assert canary.encode() not in Path(f"/proc/{process}/cmdline").read_bytes()
        for path in w.rglob("*"):
            if path.is_file():
                assert canary.encode() not in path.read_bytes(), path
                assert b"BEGIN OPENSSH PRIVATE KEY" not in path.read_bytes(), path
        timing("connection lifecycle and batch authentication")
        auth_secret, auth_key = authentication_matrix(binary, w, root, env, container, port, key, fingerprint, burrow, wait, screen_check)
        timing("interactive authentication matrix")
        trusted = trust_file.read_bytes()  # Includes the explicitly approved jump destination.
        # Actual TUI commands, terminal restoration, narrow rendering and quit
        # retention. The screen oracle is the existing pinned VT emulator.
        for i in range(7):
            burrow(w, "connect", f"row{i}", "127.0.0.1", "tester", *plain)
            wait(lambda: state_is(w, f"row{i}", "connected"))
        timing("TUI inventory setup")
        outer, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
        before = termios.tcgetattr(slave)
        def controlling():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        tui = subprocess.Popen([binary, "--workspace", str(w), "--offline", "tui"],
                               env=env | {"TERM": "xterm-256color"}, stdin=slave, stdout=slave, stderr=slave,
                               preexec_fn=controlling)
        output = bytearray()
        dimensions = ["160", "40"]
        def screen_contains(needle, cursor=False):
            if select.select([outer], [], [], .1)[0]:
                output.extend(os.read(outer, 65536))
            rendered = subprocess.run([screen_check, *dimensions, *(["--cursor-line"] if cursor else [])], input=bytes(output), capture_output=True)
            assert rendered.returncode == 0, rendered.stderr
            return needle in rendered.stdout
        try:
            wait(lambda: screen_contains(b"gateway"))
            wait(lambda: screen_contains(b"of 8"))
            # The same named connection in another workspace stays independent
            # when selected through production navigation, with drafts retained.
            os.write(outer, b"ins\x1bn")
            wait(lambda: screen_contains(b"Workspace name"))
            os.write(outer, other.name.encode() + b"\t" + str(other.parent).encode() + b"\r")
            wait(lambda: screen_contains(("● " + other.name).encode()))
            wait(lambda: screen_contains(b"gateway"))
            assert burrow(other, "inspect", "gateway")["socket"] != first["socket"]
            os.write(outer, b"\x1bw")
            wait(lambda: screen_contains(b"[Esc close]"))
            os.write(outer, b"\x1b[A\r")
            wait(lambda: screen_contains(b"COMPLETION"))
            os.write(outer, b"\x15")
            os.write(outer, b"\x1b[1;3B" * 8)  # Alt+Down scrolls connection inventory
            wait(lambda: screen_contains(b"row6"))
            os.write(outer, b"\x1b[1;3A" * 8)
            os.write(outer, b"ins\t")
            wait(lambda: screen_contains(b"inspect gateway"))
            os.write(outer, b"\r")
            wait(lambda: screen_contains(b'"name"'))
            os.write(outer, b"\x1b[6~" * 3)
            wait(lambda: screen_contains(b'"socket"'))
            os.write(outer, b"\x1b[6~" * 5)  # PgDn reveals the remaining inspect fields
            wait(lambda: screen_contains(b"socketInode"))
            os.write(outer, b"close gateway\r")
            wait(lambda: screen_contains(b"Close gateway"))
            os.write(outer,b"\r")
            wait(lambda: not screen_contains(b"Proceed?"))
            assert burrow(w, "inspect", "gateway")["state"] == "connected"
            os.write(outer, b"ins")
            wait(lambda: screen_contains(b"COMPLETION"))
            os.write(outer, b"\x1b[B" * 7)
            wait(lambda: screen_contains("› inspect row6".encode()))
            os.write(outer, b"\x15")  # Clear the completion draft before the next command.
            ui_command = shlex.join(["connect", "-ip", "127.0.0.1", "-socket", "terminal", "-user", "tester", "-ssh-key", str(key), "-port", str(port), "--yes"])
            os.write(outer, ui_command.encode() + b"\r")
            wait(lambda: screen_contains(b'"terminal"'))
            wait(lambda: state_is(w, "terminal", "connected"))
            wait(lambda: screen_contains(b"Save profile as"))
            os.write(outer,b"\r")
            wait(lambda: burrow(w,"profiles")["profiles"] and any(p["name"]=="terminal" for p in burrow(w,"profiles")["profiles"]))
            wait(lambda: screen_contains(b'"save"'))
            os.write(outer,b"close terminal\r")
            wait(lambda: screen_contains(b"Close terminal"))
            assert screen_contains(b"Proceed]")
            os.write(outer,b"\t\r")
            wait(lambda: screen_contains(b'"closed"'))
            assert all(s["name"] != "terminal" for s in burrow(w, "connections"))
            # The owner requested visible entry in the fullscreen popup only.
            # Submission removes it; command/history and persisted state stay clean.
            ui_command = shlex.join(["connect", "terminal-secret", "127.0.0.1", "tester", "--port", str(port), "--yes"])
            os.write(outer, ui_command.encode() + b"\r")
            wait(lambda: screen_contains("Connecting…".encode()))
            wait(lambda: screen_contains(b"SSH password"))
            assert screen_contains(b"WORKSPACES"), "authentication lost management backdrop"
            assert b"\x1b[?1049l" not in output, "authentication released alternate screen"
            os.write(outer, auth_secret.encode())
            wait(lambda: screen_contains(auth_secret[-16:].encode()))
            os.write(outer, b"\r")
            wait(lambda: screen_contains(b'"terminal-secret"'))
            assert burrow(w, "inspect", "terminal-secret")["state"] == "connected"
            assert not screen_contains(auth_secret[-16:].encode()), "submitted password remained visible"
            wait(lambda: screen_contains(b"Save profile as"))
            os.write(outer,b"\x1b")
            wait(lambda: not screen_contains(b"Save profile as"))
            os.write(outer, b"close terminal-secret --yes\r")
            wait(lambda: screen_contains(b'"closed"'))
            ui_command = shlex.join(["connect", "terminal-cancel", "127.0.0.1", "tester", "--key", str(auth_key), "--port", str(port), "--yes"])
            os.write(outer, ui_command.encode() + b"\r")
            wait(lambda: screen_contains(b"SSH key passphrase"))
            os.write(outer, b"\x03")
            wait(lambda: screen_contains(b"attempt closed"))
            assert not (w / "burrow/terminal-cancel").exists()
            assert not screen_contains(auth_secret[-16:].encode()), "password leaked into later view"
            for path in w.rglob("*"):
                if path.is_file():
                    assert auth_secret.encode() not in path.read_bytes(), "TUI password persisted in workspace"
            # Bare connect is optional guided entry in the same production frame.
            os.write(outer,b"connect\r")
            for label,value in [(b"Host / IP",b"127.0.0.1"),(b"SSH port",str(port).encode()),
                                (b"Username",b"tester"),(b"Connection name",b"guided-tui"),
                                (b"SSH key path",str(key).encode()),(b"SOCKS proxy port",b""),(b"Jump host",b""),(b"Agent socket",b""),
                                (b"SSH config path",b"")]:
                # Every label is visible now; wait for the actual caret before
                # sending the next field's value through the real PTY.
                wait(lambda: screen_contains(label, cursor=True))
                assert screen_contains(b"WORKSPACES")
                os.write(outer,value+b"\r")
            wait(lambda: screen_contains(b"Proceed?"))
            assert screen_contains(b"Proceed]")
            assert screen_contains(b"127.0.0.1")
            os.write(outer,b"\t\r")
            wait(lambda: screen_contains(b'"guided-tui"'))
            assert burrow(w,"inspect","guided-tui")["state"]=="connected"
            wait(lambda: screen_contains(b"Save profile as"))
            os.write(outer,b"\x1b")
            wait(lambda: not screen_contains(b"Save profile as"))
            os.write(outer,b"close guided-tui --yes\r")
            wait(lambda: screen_contains(b'"closed"'))
            # Repaint after resize starts a new screen; do not replay old 160-column
            # cursor coordinates into an emulator that was only ever 120 columns.
            while select.select([outer], [], [], 0)[0]:
                os.read(outer, 65536)
            output.clear()
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 120, 0, 0))
            dimensions = ["120", "30"]
            os.kill(tui.pid, signal.SIGWINCH)
            wait(lambda: screen_contains(b"gateway"))
            os.write(outer, b"\x03")
            wait(lambda: screen_contains(b"Keep running"))
            assert screen_contains(b"gateway")
            os.write(outer, b"\r")
            assert tui.wait(timeout=5) == 0
            assert termios.tcgetattr(slave) == before
            assert b"38;2;" not in output
        finally:
            if tui.poll() is None:
                tui.kill()
                tui.wait()
            os.close(outer)
            os.close(slave)
        assert burrow(w, "inspect", "gateway")["masterPID"] == first["masterPID"]
        for i in range(7):
            burrow(w, "close", f"row{i}", "--yes")
        timing("TUI and inventory cleanup")
        # Non-secret diagnostics cannot fill Hovel's retained notification queue.
        def rpc(method, data):
            conn = http.client.HTTPConnection("localhost", timeout=15)
            conn.sock = socket.socket(socket.AF_UNIX)
            conn.sock.settimeout(15)
            conn.sock.connect(str(w / "hoveld.sock"))
            conn.request("POST", "/hovel.daemon.v1.DaemonService/" + method,
                         json.dumps(data), {"Content-Type": "application/json"})
            response = conn.getresponse()
            body = response.read()
            status = response.status
            conn.close()
            return status, json.loads(body)
        for _ in range(265):
            code, result = rpc("RunSessionCommand", {"SessionID": first["session"], "Request": {"command": "list", "args": [first["generation"]]}})
            assert code == 200 and any(s["name"] == "gateway" and s["state"] == "connected" for s in json.loads(result["stdout"])), result
        timing("retained log ceiling")
        # Cleanup failures preserve unknown contents and the control session.
        evidence = w / "operator-evidence"
        evidence.write_text("retain evidence")
        leftover = Path(first["socket"]).parent / "unknown-file"
        leftover.write_text("not owned by connection")
        burrow(w, "close", "gateway", "--yes", ok=False)
        assert leftover.read_text() == "not owned by connection" and evidence.read_text() == "retain evidence"
        # Quit reviews both opened workspaces and refuses to exit on uncertain cleanup.
        outer, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
        before = termios.tcgetattr(slave)
        tui = subprocess.Popen([binary, "--workspace", str(w), "--offline", "tui"],
                               env=env, stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
        output = bytearray()
        dimensions = ["160", "40"]
        try:
            wait(lambda: screen_contains(b"gateway"))
            os.write(outer, b"\x1bn")
            wait(lambda: screen_contains(b"Workspace name"))
            os.write(outer, other.name.encode() + b"\t" + str(other.parent).encode() + b"\r")
            wait(lambda: screen_contains(("● " + other.name).encode()))
            wait(lambda: screen_contains(b"gateway"))
            os.write(outer, b"\x03")
            wait(lambda: screen_contains(b"Close connections"))
            assert screen_contains(b"WORKSPACE") and screen_contains(b"STATUS")
            assert screen_contains(w.name.encode()) and screen_contains(other.name.encode())
            os.write(outer, b"\t\r")  # explicit cleanup, default is Keep running
            wait(lambda: screen_contains(b"Connections remain or changed"))
            assert tui.poll() is None and leftover.exists()
            assert burrow(other, "connections") == []
            leftover.unlink()  # harness owns this injected conflict
            os.write(outer, b"\t\r")  # retry the freshly reviewed remaining owner
            assert tui.wait(timeout=10) == 0
            assert termios.tcgetattr(slave) == before
            assert burrow(w, "connections") == []
        finally:
            if tui.poll() is None:
                tui.kill(); tui.wait()
            os.close(outer); os.close(slave)
        assert not Path(first["socket"]).parent.exists()
        assert evidence.read_text() == "retain evidence" and trust_file.read_bytes() == trusted
        # Hovel's dangerous-operation allowance is required before module launch.
        hovel = root / "cache/burrow/hovel/0.4.2/hovel"
        prefix = [hovel, "run", "--workspace", w, "--daemon-endpoint", w / "hoveld.sock",
                  "--op", "burrow", "--chain", "refused", "--"]
        config = dict(workspace=str(w), name="unconfirmed", host="127.0.0.1", user="tester", port=port,
                      key=str(key), knownHosts=str(trust_file))
        for args in [("chain", "create", "refused"), ("chain", "add", "burrow@0.1.0"),
                     ("target", "add", "ssh://127.0.0.1"), ("chain", "config", "set", "workspace", str(w)), ("chain", "config", "set", "connection", json.dumps(config))]:
            command(*prefix, *args, env=env)
        denied = subprocess.run(list(map(str, [*prefix, "throw", "--now", "--json"])), env=env, capture_output=True, text=True)
        assert denied.returncode != 0 and "dangerous" in (denied.stdout + denied.stderr).lower()
        assert not (w / "burrow/unconfirmed").exists()
        # Module loss must not adopt any remaining reservation after relaunch.
        burrow(w, "connect", "ownerloss", "127.0.0.1", "tester", *plain)
        lost = wait(lambda: state_is(w, "ownerloss", "connected"))
        retained_downloads = burrow(w, "downloads")
        os.kill(lost["ownerPID"], signal.SIGKILL)
        def ended(pid):
            try:
                return Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()[0] == "Z"
            except (FileNotFoundError, ProcessLookupError):
                return True
        wait(lambda: ended(lost["masterPID"]))
        reported = burrow(w, "inspect", "ownerloss")
        assert reported["state"] == "lost" and reported["socket"] == lost["socket"]
        durable_downloads = burrow(w, "downloads")
        assert durable_downloads["files"] == retained_downloads["files"] > 0
        assert durable_downloads["bytes"] == retained_downloads["bytes"]
        assert {d["id"] for d in durable_downloads["records"]} == {d["id"] for d in retained_downloads["records"]}
        assert burrow(w, "--offline", "status")["pid"] == info["pid"]
        burrow(w, "connect", "ownerloss", "127.0.0.1", "tester", *plain, ok=False)
        burrow(w, "close", "ownerloss", "--yes", ok=False)
        assert Path(lost["socket"]).parent.exists()
        # An unrelated surviving master is refused without touching its process.
        unknown_dir = w / "burrow/stranger"
        unknown_dir.mkdir(mode=0o700)
        unknown_socket = unknown_dir / "master"
        stranger = subprocess.Popen(["ssh", "-F", "/dev/null", "-M", "-N", "-S", str(unknown_socket),
                                     "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o",
                                     f"UserKnownHostsFile={trust_file}", "-i", str(key), "-p", str(port), "tester@127.0.0.1"],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        children.append(stranger)
        wait(unknown_socket.exists)
        inode = unknown_socket.stat().st_ino
        burrow(w, "connect", "stranger", "127.0.0.1", "tester", *plain, ok=False)
        assert stranger.poll() is None and unknown_socket.stat().st_ino == inode
        # Daemon loss cannot silently recreate a daemon, master or trust record.
        os.kill(info["pid"], signal.SIGKILL)
        burrow(w, "--offline", "status", ok=False)
        burrow(w, "connect", "afterloss", "127.0.0.1", "tester", *plain, ok=False)
        assert not (w / "burrow/afterloss").exists() and evidence.read_text() == "retain evidence"
        full_evidence = True
        timing("failure ownership and evidence")
        print("PASS production key/agent auth, trust, cancellation, ownership, cross-workspace isolation, loss/reconnect, bounded logs and truthful close", flush=True)
    finally:
        for child in children:
            child.terminate()
            child.wait(timeout=10)
        for pid in daemons:
            try:
                os.kill(pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
        if container:
            command("docker", "rm", "-f", container)
        # Live Python SQLite readers previously reproduced database corruption;
        # see docs/research/retained-consumer-proof.md. Inspect only after exit.
        def exited(pid):
            try:
                return Path(f"/proc/{pid}/stat").read_text().split(") ", 1)[1].startswith("Z")
            except FileNotFoundError:
                return True
        wait(lambda: all(exited(pid) for pid in daemons))
        if forward_evidence or full_evidence:
            with closing(sqlite3.connect(w / "workspace.db")) as db:
                assert db.execute("pragma integrity_check").fetchone() == ("ok",)
                for tunnel in forward_evidence:
                    plans = [json.loads(row[0]) for row in db.execute("select p.plan_json from throw_plans p join throw_records r on r.plan_id=p.id, json_each(r.throw_json, '$.runs') j where json_extract(j.value, '$.runId')=?", (tunnel["runID"],))]
                    assert len(plans) == 1 and plans[0]["confirmationId"]
                    assert db.execute("select count(*) from throw_confirmations where id=?", (plans[0]["confirmationId"],)).fetchone()[0] == 1
                    request = json.loads(plans[0]["chainConfig"]["request"])["tunnel"]
                    for field in ("id", "direction", "listen", "destination"):
                        assert request[field] == tunnel[field], (field, request, tunnel)
                if full_evidence:
                    plans = [json.loads(r[0]) for r in db.execute("select plan_json from throw_plans")]
                    assert plans and all(p["confirmationId"] for p in plans)
                    assert db.execute("select count(*) from throw_confirmations").fetchone()[0] >= len(plans)
            print("PASS post-shutdown database integrity and confirmed Hovel forwarding evidence", flush=True)
        timing("fixture cleanup")
