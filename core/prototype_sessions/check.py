"""#87 bounded real SSH proof; negative observations are not production parity."""
import base64
import fcntl
import http.client
import json
import os
from pathlib import Path
import signal
import socket
import pty
import re
import select
import struct
import subprocess
import sys
import tempfile
import termios
import time


def command(*args, env=None):
    result = subprocess.run(list(map(str, args)), env=env, stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, timeout=45, start_new_session=True)
    assert result.returncode == 0, (args[:3], result.stdout, result.stderr)
    return result.stdout


def wait(check):
    end = time.monotonic() + 15
    while time.monotonic() < end:
        value = check()
        if value:
            return value
        time.sleep(.05)
    raise AssertionError("bounded transition timed out")


def rpc(workspace, method, data):
    conn = http.client.HTTPConnection("localhost", timeout=5)
    conn.sock = socket.socket(socket.AF_UNIX)
    conn.sock.settimeout(5)
    try:
        conn.sock.connect(str(workspace / "hoveld.sock"))
        conn.request("POST", "/hovel.daemon.v1.DaemonService/" + method,
                     json.dumps(data), {"Content-Type": "application/json"})
        response = conn.getresponse()
        body = response.read(2 * 1024 * 1024)
        assert response.status == 200, (method, response.status, body)
        return json.loads(body)
    finally:
        conn.close()


proof, production, wheel, image_file, decoder = [str(Path(p).resolve()) for p in sys.argv[1:]]


def interrupted(signum, _frame):
    raise TimeoutError(f"session proof interrupted by signal {signum}")


for signum in (signal.SIGALRM, signal.SIGTERM, signal.SIGINT):
    signal.signal(signum, interrupted)
signal.alarm(240)
with tempfile.TemporaryDirectory(prefix="b87-") as scratch:
    root = Path(scratch)
    workspace = root / "w"
    env = {k: v for k, v in os.environ.items() if not k.startswith(("HOVEL_", "SSH_", "BURROW_"))}
    env.update(HOME=scratch, XDG_CACHE_HOME=str(root / "cache"),
               XDG_CONFIG_HOME=str(root / "config"), TERM="xterm-256color", NO_COLOR="1")
    key = root / "client"
    command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key)
    config = root / "ssh_config"
    config.write_text("Host *\n IdentityFile none\n IdentityAgent none\n")
    daemon = container = None
    viewers = []
    evidence = Path(os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR", scratch))

    def burrow(*args):
        return json.loads(command(production, "--workspace", workspace, *args, env=env))

    try:
        container = command("docker", "run", "-d", "--rm", "--publish", "127.0.0.1::2222",
                            "--env", "USER_NAME=tester", "--env", "PUBLIC_KEY_FILE=/client.pub",
                            "--mount", f"type=bind,src={key}.pub,dst=/client.pub,readonly",
                            Path(image_file).read_text().strip()).strip()
        port = int(command("docker", "port", container, "2222/tcp").strip().rsplit(":", 1)[1])

        def ready():
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=1) as sock:
                    return sock.recv(128).startswith(b"SSH-")
            except OSError:
                return False

        wait(ready)
        daemon = burrow("--hovel-package", wheel, "status")["pid"]
        burrow("connect", "shared", "127.0.0.1", "tester", "--port", port,
               "--key", key, "--ssh-config", config, "--yes")
        first = wait(lambda: (s if (s := burrow("inspect", "shared"))["state"] == "connected" else None))
        command(proof, "install", workspace, env=env)
        hovel = root / "cache/burrow/hovel/0.4.2/hovel"

        def hv(*args):
            return json.loads(command(hovel, "run", "--workspace", workspace, "--daemon-endpoint", workspace / "hoveld.sock",
                                      "--", "session", *args, "--json", env=env))
        rpc(workspace, "CreateOperation", {"Operation": "session-proof"})
        rpc(workspace, "CreateChain", {"Operation": "session-proof", "Chain": "shell"})
        rpc(workspace, "AddModule", {"Operation": "session-proof", "Chain": "shell", "ModuleID": "burrow@0.1.0"})
        rpc(workspace, "AddTarget", {"Operation": "session-proof", "Chain": "shell", "Target": "local://shared-session-proof"})
        for key, value in {"workspace": str(workspace), "action": "prototype-shell", "command": "shared"}.items():
            rpc(workspace, "SetChainConfig", {"Operation": "session-proof", "Chain": "shell", "Key": key, "Value": value})
        result = json.loads(command(hovel, "run", "--workspace", workspace, "--daemon-endpoint", workspace / "hoveld.sock",
                                    "--op", "session-proof", "--chain", "shell", "--", "throw", "--now", "--allow-dangerous", "--json", env=env))
        assert result["results"][0]["state"] == "succeeded", result
        ids = json.loads(result["results"][0]["summary"])
        assert len(ids) == 3, "proof needs a typed candidate alongside two native shells"
        one, two, three = ids
        sessions = rpc(workspace, "ListSessions", {})["Sessions"]
        assert len([s for s in sessions if s["Kind"] == "shared-shell-proof"]) == 3

        def client(method, body, ok=True):
            p = subprocess.run([proof, "rpc", str(workspace), method], input=json.dumps(body),
                               env=env, capture_output=True, text=True, timeout=15, start_new_session=True)
            assert (p.returncode == 0) == ok, (method, p.stdout, p.stderr)
            return json.loads(p.stdout) if ok else p.stderr

        def write(sid, data):
            client("WriteSession", {"SessionID": sid, "Data": base64.b64encode(data).decode()})

        def tail(sid, consume=False):
            return base64.b64decode(client("TailSession", {"SessionID": sid, "MaxBytes": 65536, "Consume": consume}).get("Data") or "")

        def expect(sid, data):
            return wait(lambda: (out if data in (out := tail(sid)) else None))

        def send(sid, line, expected):
            write(sid, line.encode() + b"\n")
            return expect(sid, expected.encode())

        send(one, "printf 'SHARED_%s\\n' SHELL", "SHARED_SHELL")
        print("PASS no-TTY prototype CLI drives a real retained SSH shell through public daemon RPC", flush=True)
        native = subprocess.run([str(hovel), "run", "--workspace", str(workspace), "--daemon-endpoint", str(workspace / "hoveld.sock"),
                                 "--", "session", "send", one, "printf 'NATIVE_%s\\n' CLI", "--json"],
                                env=env, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=15, start_new_session=True)
        print("NATIVE CLI", native.returncode, native.stdout, native.stderr, flush=True)
        if native.returncode == 0:
            expect(one, b"NATIVE_CLI")
        else:
            assert "interactive" in native.stderr or "interactive" in native.stdout, native
            print("GAP registered Hovel session send refuses headless run", flush=True)
        assert "NATIVE_CLI" in hv("tail", one, "--bytes", "65536")["data"]
        assert hv("read", one)["sessionId"] == one

        # Native reads share one pending position: a second reader misses A's bytes.
        tail(one, consume=True)
        send(one, "printf 'READER_%s\\n' MARKER", "READER_MARKER")
        read_a = client("ReadSession", {"SessionID": one, "TimeoutMs": 50})
        read_b = client("ReadSession", {"SessionID": one, "TimeoutMs": 50})
        assert b"READER_MARKER" in base64.b64decode(read_a["Data"])
        assert b"READER_MARKER" not in base64.b64decode(read_b.get("Data") or "")
        assert b"READER_MARKER" in tail(one) and b"READER_MARKER" in tail(one)
        print("GAP native ReadSession consumes the other reader's output; non-consuming tails repeat history without cursor metadata", flush=True)
        # A bounded candidate uses separate client positions into complete history.
        # ponytail: prefix positions stop below 64 KiB; production needs cursor/gap metadata.
        positions = {"human": len(tail(one)), "agent": len(tail(one))}

        def positioned(reader, sid):
            snapshot = tail(sid)
            if len(snapshot) >= 65536:
                raise ValueError("complete-history proof ceiling reached; reader is unsynchronized")
            chunk = snapshot[positions[reader]:]
            positions[reader] = len(snapshot)
            return chunk

        send(one, "printf 'INDEPENDENT_%s\\n' POSITIONS", "INDEPENDENT_POSITIONS")
        for reader in positions:
            assert b"INDEPENDENT_POSITIONS" in positioned(reader, one)
        print("PASS two client positions against non-consuming complete history below the explicit 64 KiB ceiling", flush=True)

        send(one, "printf 'NATIVE_SIZE='; stty size", "NATIVE_SIZE=0 0")
        send(two, "printf 'INITIAL_SIZE='; stty size", "INITIAL_SIZE=24 80")
        resize = lambda sid, cols, rows, ok=True: client("RunSessionCommand", {"SessionID": sid, "Request": {"command": "resize", "args": [str(cols), str(rows)]}}, ok)
        resize(two, 120, 40)
        send(two, "printf 'LIVE_SIZE='; stty size", "LIVE_SIZE=40 120")
        resize(two, 0, 24, False)
        send(two, "printf 'VALID_SIZE='; stty size", "VALID_SIZE=40 120")
        print("GAP native SDK initial geometry is 0x0; PASS explicit prototype initial geometry and public session-command resize, invalid dimensions refused", flush=True)
        result = hv("call", two, "resize", "--arg", "100", "--arg", "30")
        assert result["stdout"] == "geometry applied", result
        send(two, "printf 'CLI_SIZE='; stty size", "CLI_SIZE=30 100")
        print("PASS native no-TTY CLI send/read/tail/call, including typed geometry without shell-input encoding", flush=True)

        def attach(sid, burrow_tui=False):
            master, slave = pty.openpty()
            before = termios.tcgetattr(slave)
            width, height = (160, 40) if burrow_tui else (100, 30)
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))

            def controlling():
                os.setsid()
                fcntl.ioctl(0, termios.TIOCSCTTY, 0)

            argv = [production, "--workspace", str(workspace), "tui"] if burrow_tui else [str(hovel), "session", "connect", sid, "--workspace", str(workspace)]
            process = subprocess.Popen(argv,
                                       env=env | {"HOVEL_DAEMON_ENDPOINT": str(workspace / "hoveld.sock")},
                                       stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
            viewer = {"process": process, "master": master, "slave": slave, "before": before,
                      "output": bytearray(), "dimensions": [str(width), str(height)]}
            viewers.append(viewer)
            return viewer

        def screen(viewer):
            if select.select([viewer["master"]], [], [], .05)[0]:
                viewer["output"].extend(os.read(viewer["master"], 65536))
            return subprocess.run([decoder, *viewer["dimensions"]], input=viewer["output"], capture_output=True, timeout=5, check=True).stdout.decode()

        def visible(viewer, expected):
            end = time.monotonic() + 15
            while time.monotonic() < end:
                view = screen(viewer)
                if expected in view:
                    return view
                assert viewer["process"].poll() is None, (viewer["process"].returncode, view)
            raise AssertionError((expected, view))

        def detach(viewer, label):
            os.write(viewer["master"], b"\x1d")
            end = time.monotonic() + 5
            while viewer["process"].poll() is None and time.monotonic() < end:
                screen(viewer)
            assert viewer["process"].wait(timeout=1) == 0
            screen(viewer)
            assert termios.tcgetattr(viewer["slave"]) == viewer["before"], "termios not restored"
            (evidence / (label + ".ansi")).write_bytes(viewer["output"])

        # Native Hovel connector in a separate real terminal; agent stays headless.
        viewer = attach(two)
        visible(viewer, "Connected to session")
        hv("send", two, "PROOF_STATE=retained; printf 'HUMAN_%s PID=%s\\n' OBSERVED $$")
        view = visible(viewer, "HUMAN_OBSERVED")
        pid = re.search(r"HUMAN_OBSERVED PID=(\d+)", view).group(1)
        assert b"HUMAN_OBSERVED" in tail(two), "observer consumed agent history"
        # Different clients' raw fragments are combined, not arbitrated.
        write(two, b"printf 'MIXED_%s\\n' ")
        os.write(viewer["master"], b"WRITERS\r")
        visible(viewer, "MIXED_WRITERS")
        print("GAP two writers can splice one command; native transport has no controller fencing", flush=True)
        # Ctrl+C reaches the remote foreground job, not the connector.
        hv("send", two, "sleep 60")
        visible(viewer, "sleep 60")
        os.write(viewer["master"], b"\x03")
        hv("send", two, "printf 'CTRL_C_%s\\n' OK")
        visible(viewer, "CTRL_C_OK")
        hv("send", two, "top -d 1")
        visible(viewer, "%Cpu(s):")
        # Native connector resize does not update the remote PTY.
        fcntl.ioctl(viewer["slave"], termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
        os.kill(viewer["process"].pid, signal.SIGWINCH)
        os.write(viewer["master"], b"q")
        hv("send", two, "printf 'ATTACH_SIZE='; stty size")
        visible(viewer, "ATTACH_SIZE=30 100")
        detach(viewer, "first-attachment")
        send(one, "printf 'OTHER_%s\\n' SHELL", "OTHER_SHELL")
        hv("send", two, "printf 'DETACHED_%s PID=%s STATE=%s\\n' LIVE $$ \"$PROOF_STATE\"")
        expect(two, f"DETACHED_LIVE PID={pid} STATE=retained".encode())
        viewer = attach(two)
        visible(viewer, f"DETACHED_LIVE PID={pid} STATE=retained")
        print("PASS headless agent + separate terminal, two shells, Ctrl+C, top, termios restoration and same-PID/state detach/reattach", flush=True)
        print("GAP native connector ignores live resize; prototype resize command must be explicitly driven", flush=True)

        # Reproduce full-screen presentation cleanup independently of top's modes.
        hv("send", two, "printf '\\033[?1049h\\033[?25l\\033[2J\\033[HRESTORE_%s' PROBE")
        visible(viewer, "RESTORE_PROBE")
        detach(viewer, "alternate-screen-detach")
        raw = viewer["output"]
        assert raw.rfind(b"\x1b[?1049h") > raw.rfind(b"\x1b[?1049l"), raw[-500:]
        assert raw.rfind(b"\x1b[?25l") > raw.rfind(b"\x1b[?25h"), raw[-500:]
        print("GAP native detach restores termios but leaves alternate screen and hidden cursor active", flush=True)
        write(two, b"printf '\\033[?1049l\\033[?25h'\n")
        # The existing embedded Hovel tab can observe the retained shell even
        # though Burrow's management inventory refuses its unfamiliar kind.
        tui = attach(two, burrow_tui=True)
        visible(tui, "WORKSPACES")
        os.write(tui["master"], b"\x1bh")
        visible(tui, "h0v3l")
        line = "session connect " + two + " --no-history"
        os.write(tui["master"], line.encode())
        visible(tui, line)
        os.write(tui["master"], b"\r")
        visible(tui, "Connected to session")
        hv("send", two, "printf 'BURROW_TUI_%s\\n' OBSERVES")
        visible(tui, "BURROW_TUI_OBSERVES")
        assert b"BURROW_TUI_OBSERVES" in tail(two)
        tui["process"].terminate()
        end = time.monotonic() + 10
        while tui["process"].poll() is None and time.monotonic() < end:
            screen(tui)
        assert tui["process"].wait(timeout=1) == 0
        while select.select([tui["master"]], [], [], 0)[0]:
            screen(tui)
        assert termios.tcgetattr(tui["slave"]) == tui["before"]
        raw = tui["output"]
        assert raw.rfind(b"\x1b[?1049l") > raw.rfind(b"\x1b[?1049h"), "Burrow left alternate screen active"
        assert raw.rfind(b"\x1b[?25h") > raw.rfind(b"\x1b[?25l"), "Burrow left cursor hidden"
        (evidence / "burrow-observer.ansi").write_bytes(tui["output"])
        send(two, "printf 'FRONTEND_EXIT_%s PID=%s\\n' RETAINED $$", f"FRONTEND_EXIT_RETAINED PID={pid}")
        print("PASS actual Burrow Hovel tab observes agent input; frontend exit restores terminal and retains same shell", flush=True)
        # The production frontend intentionally refuses unfamiliar Burrow kinds.
        refused = subprocess.run([production, "--workspace", str(workspace), "inspect", "shared"],
                                 env=env, capture_output=True, text=True, timeout=15)
        assert refused.returncode != 0 and "incompatible retained Burrow owner" in refused.stderr, refused
        print("GAP production owner discovery refuses the prototype shell kind", flush=True)
        assert Path(f'/proc/{first["masterPID"]}').exists()
        print("PASS real retained shell through the single Burrow module and existing SSH master", flush=True)
        close_output = send(one, "printf 'CLOSE_%s=%s\\n' PID $$", "CLOSE_PID=")
        close_pid = re.search(rb"CLOSE_PID=(\d+)", close_output).group(1).decode()
        # Exercise actual daemon history truncation, not just a short tail request.
        send(one, "head -c 11000000 /dev/zero | tr '\\0' x; printf 'TRUNCATED_%s\\n' HISTORY", "TRUNCATED_HISTORY")
        history = hv("tail", one, "--bytes", "12000000")
        assert len(history["data"].encode()) == 10 * 1024 * 1024
        assert "READER_MARKER" not in history["data"] and "TRUNCATED_HISTORY" in history["data"]
        assert set(history) == {"sessionId", "data", "closed"}, history.keys()
        try:
            positioned("agent", one)
        except ValueError as error:
            assert "unsynchronized" in str(error)
        else:
            raise AssertionError("bounded candidate silently continued after history ceiling")
        print("GAP actual 10 MiB history rollover loses earlier output without cursor/gap metadata; bounded candidate refuses its 64 KiB ceiling", flush=True)
        client("CloseSession", {"SessionID": one})
        assert one not in [s["ID"] for s in client("ListSessions", {})["Sessions"]]
        wait(lambda: subprocess.run(["docker", "exec", container, "sh", "-c", f"test ! -d /proc/{close_pid}"],
                                    capture_output=True, timeout=5).returncode == 0)
        send(two, "printf 'SIBLING_%s PID=%s\\n' SURVIVES $$", f"SIBLING_SURVIVES PID={pid}")
        assert Path(f'/proc/{first["masterPID"]}').exists()
        print("PASS explicit close ends only one shell; sibling and original master remain", flush=True)

        def typed(name, *args, ok=True):
            response = client("RunSessionCommand", {"SessionID": three, "Request": {"command": name, "args": list(map(str, args))}}, ok)
            return json.loads(response["stdout"]) if ok else response

        def typed_input(token, line, ok=True):
            return typed("input", token, base64.b64encode(line).decode(), ok=ok)

        def typed_expect(marker):
            return wait(lambda: (out if marker in base64.b64decode((out := typed("observe", 0))["data"] or "") else None))

        agent = typed("control", 0, "agent", 100, 30)
        ack = typed_input(agent["token"], b"printf 'TYPED_%s SIZE=' SHARED; stty size\n")
        assert "acceptedBytes" in ack and "exitCode" not in ack
        snapshot = typed_expect(b"TYPED_SHARED SIZE=30 100")
        # Native reads cannot steal candidate bytes and native writes cannot bypass control.
        assert not client("ReadSession", {"SessionID": three, "TimeoutMs": 0}).get("Data")
        client("WriteSession", {"SessionID": three, "Data": base64.b64encode(b"echo BYPASS\n").decode()}, False)
        typed("observe", -1, ok=False)
        typed("observe", snapshot["next"] + 1000000, ok=False)
        human_position = snapshot["next"]
        agent_position = snapshot["next"]
        typed_input(agent["token"], b"printf 'BOTH_%s\\n' READERS\n")
        typed_expect(b"BOTH_READERS")
        human = typed("observe", human_position)
        agent_read = typed("observe", agent_position)
        assert human["data"] == agent_read["data"] and human["next"] == agent_read["next"]
        assert b"BOTH_READERS" in base64.b64decode(human["data"])
        controller = typed("control", 1, "human", 120, 40)
        typed("control", 1, "stale rival", 80, 24, ok=False)
        typed_input(agent["token"], b"echo STALE\n", ok=False)
        typed("resize", agent["token"], 80, 24, ok=False)
        typed("resize", controller["token"], 0, 24, ok=False)
        typed_input(controller["token"], b"printf 'TAKEOVER_SIZE='; stty size\n")
        typed_expect(b"TAKEOVER_SIZE=40 120")
        typed_input(controller["token"], b"sleep 60\n")
        typed_expect(b"sleep 60")
        typed_input(controller["token"], b"\x03")
        typed_input(controller["token"], b"printf 'TYPED_CTRL_C_%s\\n' OK\n")
        typed_expect(b"TYPED_CTRL_C_OK")
        typed("release", controller["token"])
        assert typed("observe", 0)["controller"] == ""
        typed_input(controller["token"], b"echo AFTER_RELEASE\n", ok=False)
        resumed = typed("control", 3, "returned agent", 100, 30)
        typed_input(resumed["token"], b"printf 'TYPED_REATTACH_%s\\n' OK\n")
        typed_expect(b"TYPED_REATTACH_OK")
        typed_input(resumed["token"], b"head -c 70000 /dev/zero | tr '\\0' x; printf 'RING_%s\\n' END\n")
        gap = wait(lambda: (out if (out := typed("observe", 0))["gap"] else None))
        assert gap["oldest"] > 0 and not gap["data"]
        wait(lambda: b"RING_END" in base64.b64decode(typed("observe", typed("observe", 0)["oldest"])["data"] or ""))
        print("PASS typed candidate: independent monotonic cursors, explicit gaps, takeover fencing, validated geometry, Ctrl+C, release/reattach and raw-I/O bypass refusal", flush=True)

        os.kill(first["masterPID"], signal.SIGKILL)
        wait(lambda: client("TailSession", {"SessionID": two, "MaxBytes": 1024}).get("Closed"))
        # SDK Write currently returns nil for a closed PTY: never call this execution success.
        closed_ack = client("WriteSession", {"SessionID": two, "Data": base64.b64encode(b"echo SHOULD_NOT_RUN\n").decode()})
        print("GAP closed SDK PTY still acknowledges input; lifecycle must be checked, not inferred from a write ACK", flush=True)
        assert "SHOULD_NOT_RUN" not in hv("tail", two, "--bytes", "65536")["data"]
        client("CloseSession", {"SessionID": two})
        wait(lambda: typed("observe", typed("observe", 0)["next"])["closed"])
        typed_input(resumed["token"], b"echo AFTER_LOSS\n", ok=False)
        client("CloseSession", {"SessionID": three})
        lost = burrow("inspect", "shared")
        assert lost["state"] == "lost", lost
        assert lost["masterPID"] == first["masterPID"]
        assert not any(s["Kind"] == "shared-shell-proof" for s in client("ListSessions", {})["Sessions"])
        print("PASS master loss closes shared shell; no fallback login/recreated master; explicit cleanup removes shell records", flush=True)
    finally:
        signal.alarm(0)
        for viewer in viewers:
            if viewer["process"].poll() is None:
                viewer["process"].kill()
                viewer["process"].wait(timeout=5)
            os.close(viewer["master"])
            os.close(viewer["slave"])
        if daemon:
            try:
                os.kill(daemon, signal.SIGTERM)
            except ProcessLookupError:
                pass
        if container:
            subprocess.run(["docker", "rm", "-f", container], capture_output=True, timeout=20)
