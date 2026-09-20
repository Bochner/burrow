"""Local forwarding through the production command seam and real SSH master."""
import socket
import socketserver
import threading
import concurrent.futures
import fcntl
import os
import pty
import select
import shlex
import signal
import struct
import subprocess
import termios
import time
import json


def forward_checks(binary, env, decoder, burrow, w, first, options, container, command):
    def free_port():
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            return s.getsockname()[1]

    def absent(port):
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=.2):
                return False
        except OSError:
            return True

    def traffic(t):
        host, port = t["listen"].rsplit(":", 1)
        if host == "0.0.0.0":
            host = "127.0.0.1"
        with socket.create_connection((host, int(port)), timeout=3) as stream:
            assert stream.recv(128).startswith(b"SSH-")
            stream.sendall(b"SSH-2.0-burrow_forward_check\r\n")
            assert stream.recv(4096), "no return traffic after client version"

    with socket.socket() as free:
        free.bind(("127.0.0.1", 0))
        port = free.getsockname()[1]
    args = ("tunnel", "create", "gateway", "forward", str(port), "localhost", "2222")
    review = burrow(w, *args)
    contract = burrow(w, "capabilities", "tunnel.create")
    review_shape = contract["results"]["TunnelReview"]["anyOf"][0]
    assert set(review_shape["required"]) <= review.keys() <= review_shape["properties"].keys(), (review_shape, review)
    alias = ("tunc", "gateway", "l", str(port), "localhost", "2222")
    assert burrow(w, *alias) == review, "alias changed reviewed endpoints or owner binding"
    assert f"127.0.0.1:{port}" in review["review"]
    assert burrow(w, "tunnel", "list") == []
    assert absent(port), "review created a listener"
    burrow(w, *args, "--review", "0" * 64, "--yes", ok=False)
    assert absent(port)
    created = burrow(w, *alias, "--review", review["digest"], "--yes")
    assert created["id"].startswith("gateway/") and created["creation"]
    assert created["connectionCreation"] == first["creation"]
    assert created["listen"] == f"127.0.0.1:{port}" and created["destination"] == "localhost:2222"
    assert burrow(w, "tunnel", "list") == [created]
    with socket.create_connection(("127.0.0.1", port), timeout=3) as stream:
        assert stream.recv(128).startswith(b"SSH-")
    assert burrow(w, "tunnel", "check", created["id"])["state"] == "traffic-observed"
    before = burrow(w, "tunnel", "list")
    assert burrow(w, "inspect", "gateway")["tunnelCount"] == 1
    burrow(w, *args, "--yes", ok=False)
    assert burrow(w, "tunnel", "list") == before
    with socket.socket() as held:
        held.bind(("127.0.0.1", 0))
        held.listen()
        burrow(w, "tunnel", "create", "gateway", "forward", str(held.getsockname()[1]), "localhost", "2222", "--yes", ok=False)
        assert burrow(w, "tunnel", "list") == before, "failed bind fabricated inventory"
    burrow(w, "tunnel", "remove", created["id"], "--yes")
    assert burrow(w, "tunnel", "list") == []
    replacement = burrow(w, *args, "--yes")
    assert replacement["id"] != created["id"]
    burrow(w, "tunnel", "remove", created["id"], "--yes", ok=False)
    assert burrow(w, "tunnel", "check", replacement["id"])["state"] == "traffic-observed"
    burrow(w, "tund", replacement["id"], "--yes")
    assert absent(port)
    # Explicit exposure and retained inventory beyond the visible table window.
    siblings = [burrow(w, "tunnel", "create", "gateway", "forward", f"127.0.0.1:{free_port()}", "localhost", "2222", "--yes") for _ in range(7)]
    broad = burrow(w, "tunnel", "create", "gateway", "forward", f"0.0.0.0:{free_port()}", "localhost", "2222", "--yes")
    siblings.append(broad)
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        list(pool.map(traffic, siblings))
    assert len(burrow(w, "tunnel", "list")) == 8
    assert burrow(w, "inspect", "gateway")["tunnelCount"] == 8
    removed = siblings.pop(3)
    assert "review" in burrow(w, "tunnel", "remove", removed["id"])
    traffic(removed)
    burrow(w, "tunnel", "remove", removed["id"], "--yes")
    assert absent(int(removed["listen"].rsplit(":", 1)[1]))
    for t in siblings:
        traffic(t)
    denied = burrow(w, "tunnel", "create", "gateway", "forward", str(free_port()), "127.0.0.1", "1", "--yes")
    check = burrow(w, "tunnel", "check", denied["id"])
    assert check["state"] == "failed" and "AllowTcpForwarding" in check["detail"]
    burrow(w, "tunnel", "remove", denied["id"], "--yes")
    # Hostile/secret-like service bytes must not enter diagnostics or evidence.
    canary = "forward-canary-" + os.urandom(12).hex()
    command("docker", "exec", "-d", container, "sh", "-c", f"printf '\\033]52;c;{canary}\\007\\n' | nc -l -p 23456")
    hostile = burrow(w, "tunnel", "create", "gateway", "forward", str(free_port()), "localhost", "23456", "--yes")
    result = burrow(w, "tunnel", "check", hostile["id"])
    assert result["state"] == "traffic-observed" and canary not in json.dumps(result)
    burrow(w, "tunnel", "remove", hostile["id"], "--yes")
    for path in w.rglob("*"):
        if path.is_file():
            assert canary.encode() not in path.read_bytes(), path
    forward_ui(binary, env, decoder, burrow, w, free_port, siblings)
    for t in siblings:
        traffic(t)
        burrow(w, "tunnel", "remove", t["id"], "--yes")

    # Server refusal is observed when traffic opens its destination channel.
    command("docker", "exec", container, "sh", "-c", "sed -i 's/^AllowTcpForwarding yes$/AllowTcpForwarding no/' /config/sshd/sshd_config; kill -HUP $(cat /config/sshd.pid)")
    try:
        burrow(w, "connect", "restricted", "127.0.0.1", "tester", *options)
        for _ in range(100):
            if burrow(w, "inspect", "restricted")["state"] == "connected":
                break
            time.sleep(.1)
        t = burrow(w, "tunnel", "create", "restricted", "forward", str(free_port()), "localhost", "2222", "--yes")
        assert burrow(w, "tunnel", "check", t["id"])["state"] == "failed"
        burrow(w, "close", "restricted", "--yes")
        assert absent(int(t["listen"].rsplit(":", 1)[1]))
    finally:
        command("docker", "exec", container, "sh", "-c", "sed -i 's/^AllowTcpForwarding no$/AllowTcpForwarding yes/' /config/sshd/sshd_config; kill -HUP $(cat /config/sshd.pid)")

    burrow(w, "connect", "forward-owner", "127.0.0.1", "tester", *options)
    for _ in range(100):
        owner = burrow(w, "inspect", "forward-owner")
        if owner["state"] == "connected":
            break
        time.sleep(.1)
    port = free_port()
    t = burrow(w, "tunnel", "create", "forward-owner", "forward", str(port), "localhost", "2222", "--yes")
    traffic(t)
    old_review = burrow(w, "tunnel", "create", "forward-owner", "forward", str(port), "localhost", "2222")
    os.kill(owner["masterPID"], signal.SIGKILL)
    for _ in range(100):
        if burrow(w, "inspect", "forward-owner")["state"] == "lost":
            break
        time.sleep(.1)
    assert absent(port)
    assert burrow(w, "tunnel", "list")[0]["state"] == "unavailable"
    burrow(w, "tunnel", "check", t["id"], ok=False)
    burrow(w, "reconnect", "forward-owner", "127.0.0.1", "tester", *options)
    for _ in range(100):
        if burrow(w, "inspect", "forward-owner")["state"] == "connected":
            break
        time.sleep(.1)
    assert burrow(w, "tunnel", "list") == [] and absent(port)
    burrow(w, "tunnel", "create", "forward-owner", "forward", str(port), "localhost", "2222", "--review", old_review["digest"], "--yes", ok=False)
    fresh = burrow(w, "tunnel", "create", "forward-owner", "forward", str(port), "localhost", "2222", "--yes")
    burrow(w, "tunnel", "remove", t["id"], "--yes", ok=False)
    traffic(fresh)
    burrow(w, "close", "forward-owner", "--yes")
    assert absent(port) and burrow(w, "tunnel", "list") == []
    assert burrow(w, "inspect", "gateway")["masterPID"] == first["masterPID"]
    print("PASS reviewed local forward, real traffic, retained inventory and selected removal", flush=True)
    return created


def forward_ui(binary, env, decoder, burrow, workspace, free_port, siblings, reverse=False, destination_port="2222", proxy=False):
    outer, slave = pty.openpty()
    before = termios.tcgetattr(slave)
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
    def controlling():
        os.setsid()
        fcntl.ioctl(0, termios.TIOCSCTTY, 0)
    frontend = subprocess.Popen([binary, "--workspace", str(workspace), "tui"], env=env,
                                stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
    output = bytearray()
    def wait(needle, present=True):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if select.select([outer], [], [], .05)[0]:
                output.extend(os.read(outer, 65536))
            screen = subprocess.run([decoder, "160", "40"], input=output, capture_output=True, check=True).stdout.decode()
            if (needle in screen) == present:
                return
            assert frontend.poll() is None, screen
        raise AssertionError((needle, screen))
    try:
        if proxy:
            wait("gateway")
            port = free_port()
            os.write(outer, b"proxy create \t")
            wait("proxy create gateway")
            os.write(outer, f"{port}\r".encode())
            wait("Proceed?")
            os.write(outer, b"\x1b")
            wait("COMMAND OUTPUT")
            assert burrow(workspace, "proxy", "inspect", "gateway")["state"] == "off"
            os.write(outer, f"proxy create gateway {port}\r".encode())
            wait("Proceed?")
            os.write(outer, b"\t\r")
            wait(f"Yes :{port}")
            created = burrow(workspace, "proxy", "inspect", "gateway")
            os.write(outer, b"proxy remove \t\r")
            wait("Proceed?")
            os.write(outer, b"\t\r")
            wait('"removed"')
            assert burrow(workspace, "proxy", "inspect", "gateway")["state"] == "off"
            retained = burrow(workspace, "proxy", "create", "gateway", str(port), "--yes")
            assert retained["id"] != created["id"]
            wait(f"Yes :{port}")
            os.write(outer, b"quit\r")
            wait("Keep running")
            os.write(outer, b"\r")
            frontend.wait(timeout=10)
            assert frontend.returncode == 0 and termios.tcgetattr(slave) == before
            assert burrow(workspace, "proxy", "inspect", "gateway") == retained
            assert burrow(workspace, "tunnel", "list") == siblings
            return retained
        wait("gateway" if reverse else "Alt+Shift+")
        port = free_port()
        os.write(outer, b"tunnel create \t\t" if reverse else b"tunnel create \t")
        wait("tunnel create gateway reverse" if reverse else "tunnel create gateway forward")
        wait("Example:")
        os.write(outer, f"{port} localhost {destination_port}\r".encode())
        wait("Proceed?")
        os.write(outer, b"\x1b")
        wait("COMMAND OUTPUT")
        assert len(burrow(workspace, "tunnel", "list")) == len(siblings)
        os.write(outer, b"tunc \t\t" if reverse else b"tunc \t")
        wait("tunc gateway r" if reverse else "tunc gateway l")
        os.write(outer, f"{port} localhost {destination_port}\r".encode())
        wait("Proceed?")
        os.write(outer, b"\t\r")
        wait('"listening"')
        added = next(t for t in burrow(workspace, "tunnel", "list") if t["listen"] == f"127.0.0.1:{port}")
        os.write(outer, ("tunnel remove " + added["id"] + "\r").encode())
        wait("Proceed?")
        os.write(outer, b"\t\r")
        wait('"removed"')
        # External automation and the watching TUI share the retained inventory.
        external = burrow(workspace, "tunc", "gateway", "r" if reverse else "l",
                          str(port), "localhost", destination_port, "--yes")
        inventory_count = f"of {len(siblings)+1} · Alt+Shift+"
        wait(inventory_count)
        burrow(workspace, "tund", external["id"], "--yes")
        wait(inventory_count, present=False)
        os.write(outer, b"quit\r")
        wait("Keep running")
        os.write(outer, b"\r")
        frontend.wait(timeout=10)
        assert frontend.returncode == 0
        assert termios.tcgetattr(slave) == before
        assert len(burrow(workspace, "tunnel", "list")) == len(siblings)
    finally:
        if frontend.poll() is None:
            frontend.terminate()
            frontend.wait(timeout=5)
        os.close(outer)
        os.close(slave)


def reverse_checks(binary, env, decoder, burrow, w, first, options, container, command):
    canary = "reverse-canary-" + os.urandom(12).hex()
    class Echo(socketserver.BaseRequestHandler):
        def handle(self):
            try:
                self.request.settimeout(3)
                self.request.sendall(("reverse-ready\n\x1b]52;c;" + canary + "\x07\n").encode())
                data = self.request.recv(1024)
                self.request.sendall(data)
            except OSError:
                pass  # The passive production check intentionally closes after one byte.

    def listeners(port):
        return command("docker", "exec", container, "sh", "-c",
                       f"awk '$4 == \"0A\" && $2 ~ /:{int(port):04X}$/ {{print $2}}' /proc/net/tcp /proc/net/tcp6").split()

    def traffic(t):
        host, port = t["listen"].rsplit(":", 1)
        host = {"0.0.0.0": "127.0.0.1", "[::]": "::1"}.get(host, host.strip("[]"))
        response = command("docker", "exec", container, "sh", "-c",
                           f"printf 'reverse-request\\n' | nc -w 1 {host} {port}")
        assert response.startswith("reverse-ready\n") and response.endswith("reverse-request\n"), response
        assert burrow(w, "tunnel", "check", t["id"])["state"] == "traffic-observed"

    def policy(gateway="no", forwarding="yes"):
        command("docker", "exec", container, "sh", "-c",
                "sed -i '/^GatewayPorts /d; /^AllowTcpForwarding /d' /config/sshd/sshd_config; "
                f"printf 'GatewayPorts {gateway}\\nAllowTcpForwarding {forwarding}\\n' >> /config/sshd/sshd_config; "
                "kill -HUP $(cat /config/sshd.pid)")

    def connect(name):
        burrow(w, "connect", name, "127.0.0.1", "tester", *options)
        for _ in range(100):
            state = burrow(w, "inspect", name)
            if state["state"] == "connected":
                return state
            time.sleep(.1)
        raise AssertionError(state)

    with socketserver.ThreadingTCPServer(("127.0.0.1", 0), Echo) as echo:
        thread = threading.Thread(target=echo.serve_forever, daemon=True)
        thread.start()
        destination = str(echo.server_address[1])
        try:
            args = ("tunnel", "create", "gateway", "reverse", "32451", "127.0.0.1", destination)
            alias = ("tunc", "gateway", "r", "32451", "127.0.0.1", destination)
            review = burrow(w, *args)
            assert burrow(w, *alias) == review
            assert "Remote listener:" in review["review"] and "Local destination:" in review["review"]
            burrow(w, "tunc", "gateway", "r", "32451", ok=False)
            burrow(w, *alias, "--review", "0" * 64, "--yes", ok=False)
            assert burrow(w, "tunnel", "list") == [] and not listeners(32451)
            local_review = burrow(w, "tunc", "gateway", "l", "32451", "127.0.0.1", destination)
            burrow(w, *alias, "--review", local_review["digest"], "--yes", ok=False)
            first_tunnel = burrow(w, *alias, "--review", review["digest"], "--yes")
            assert first_tunnel["direction"] == "R" and first_tunnel["listen"] == "127.0.0.1:32451"
            assert first_tunnel["connectionCreation"] == first["creation"]
            assert listeners(32451) == ["0100007F:7EC3"]
            traffic(first_tunnel)
            # A same-port local greeting must never satisfy a reverse check.
            with socket.socket() as held:
                held.bind(("127.0.0.1", 0))  # Reserved but deliberately not listening.
                denied = burrow(w, "tunc", "gateway", "r", destination, "127.0.0.1", str(held.getsockname()[1]), "--yes")
                assert burrow(w, "tunnel", "check", denied["id"])["state"] == "failed"
                burrow(w, "tund", denied["id"], "--yes")
            burrow(w, *args, "--yes", ok=False)
            # An actual server-side occupied port is never replaced.
            burrow(w, "tunc", "gateway", "r", "2222", "127.0.0.1", destination, "--yes", ok=False)
            random_args = ("tunc", "gateway", "r", "0", "127.0.0.1", destination)
            randoms = [burrow(w, *random_args, "--yes") for _ in range(2)]
            assert len({t["listen"] for t in randoms}) == 2
            for t in randoms:
                assert t["requestedListen"] == "127.0.0.1:0"
                assert 49152 <= int(t["listen"].rsplit(":", 1)[1]) <= 65535
                traffic(t)
            assert burrow(w, "inspect", "gateway")["tunnelCount"] == 3
            burrow(w, "tund", first_tunnel["id"], "--yes")
            assert not listeners(32451)
            replacement = burrow(w, *args, "--yes")
            burrow(w, "tund", first_tunnel["id"], "--yes", ok=False)
            assert replacement["id"] != first_tunnel["id"]
            traffic(replacement)
            for t in randoms:
                traffic(t)
            # Rejected TUI review, alias approval, qualified removal and keep-running quit.
            forward_ui(binary, env, decoder, burrow, w, lambda: 32452,
                       [replacement, *randoms], reverse=True, destination_port=destination)
            for t in [replacement, *randoms]:
                traffic(t)
                burrow(w, "tund", t["id"], "--yes")
                assert not listeners(t["listen"].rsplit(":", 1)[1])
            for bind in ("0.0.0.0:32451", "127.0.0.2:32451"):
                burrow(w, "tunc", "gateway", "r", bind, "127.0.0.1", destination, "--yes", ok=False)
                assert not listeners(32451) and burrow(w, "tunnel", "list") == []
            with socket.socket() as free:
                free.bind(("127.0.0.1", 0))
                shared_port = str(free.getsockname()[1])
            local = burrow(w, "tunc", "gateway", "l", shared_port, "localhost", "2222", "--yes")
            reverse = burrow(w, "tunc", "gateway", "r", shared_port, "127.0.0.1", destination, "--yes")
            traffic(reverse)
            burrow(w, "tund", reverse["id"], "--yes")
            assert burrow(w, "tunnel", "check", local["id"])["state"] == "traffic-observed"
            burrow(w, "tund", local["id"], "--yes")
            print("PASS reverse review, remote-origin round trips, random siblings, stale IDs and real TUI completion", flush=True)

            for gateway, forwarding, binds in [
                ("yes", "yes", ["127.0.0.1:32451"]),
                ("clientspecified", "yes", ["0.0.0.0:32451", "127.0.0.2:32451", "[::1]:32451", "[::]:32451"]),
                ("no", "no", ["127.0.0.1:32451"]),
            ]:
                policy(gateway, forwarding)
                connect("reverse-policy")
                for bind in binds:
                    if gateway != "clientspecified":
                        burrow(w, "tunc", "reverse-policy", "r", bind, "127.0.0.1", destination, "--yes", ok=False)
                    else:
                        t = burrow(w, "tunc", "reverse-policy", "r", bind, "127.0.0.1", destination, "--yes")
                        traffic(t)
                        burrow(w, "tund", t["id"], "--yes")
                    assert not listeners(32451) and burrow(w, "tunnel", "list") == []
                burrow(w, "close", "reverse-policy", "--yes")
            policy()
            # Verification can be unavailable before allocation, or fail after
            # allocation. Actual SSH ForceCommand behavior drives all three paths.
            for failure in ("always", "second", "after-first"):
                script = """#!/bin/sh
n=$(cat /tmp/reverse-probe-count 2>/dev/null || printf 0)
n=$((n + 1))
printf '%s' "$n" > /tmp/reverse-probe-count
"""
                condition = {"always": "true", "second": '[ "$n" = 2 ]', "after-first": '[ "$n" -ge 2 ]'}[failure]
                script += f"if {condition}; then printf '%s' {shlex.quote(canary)}; exit 0; fi\n"
                script += 'exec /bin/sh -c "$SSH_ORIGINAL_COMMAND"\n'
                command("docker", "exec", container, "sh", "-c",
                        f"printf %s {shlex.quote(script)} > /tmp/reverse-probe; "
                        "printf 0 > /tmp/reverse-probe-count; "
                        "chmod 666 /tmp/reverse-probe-count; "
                        "printf 'ForceCommand sh /tmp/reverse-probe\\n' >> /config/sshd/sshd_config; "
                        "kill -HUP $(cat /config/sshd.pid)")
                try:
                    connect("reverse-unverified")
                    burrow(w, "tunc", "reverse-unverified", "r", "32451", "127.0.0.1", destination, "--yes", ok=False)
                    assert not listeners(32451), "verification failure leaked its listener"
                    inventory = burrow(w, "tunnel", "list")
                    observed = int(command("docker", "exec", container, "cat", "/tmp/reverse-probe-count"))
                    assert observed == (1 if failure == "always" else 3), (failure, observed)
                    if failure == "after-first":
                        assert len(inventory) == 1 and inventory[0]["state"] == "unverified", inventory
                    else:
                        assert inventory == []
                    burrow(w, "close", "reverse-unverified", "--yes")
                    assert burrow(w, "tunnel", "list") == []
                finally:
                    command("docker", "exec", container, "sh", "-c",
                            "sed -i '/^ForceCommand /d' /config/sshd/sshd_config; kill -HUP $(cat /config/sshd.pid)")
            print("PASS failed remote observation refuses allocation or verifies cleanup; uncertainty remains visible", flush=True)
            owner = connect("reverse-owner")
            t = burrow(w, "tunc", "reverse-owner", "r", "32451", "127.0.0.1", destination, "--yes")
            traffic(t)
            old_review = burrow(w, "tunc", "reverse-owner", "r", "32451", "127.0.0.1", destination)
            os.kill(owner["masterPID"], signal.SIGKILL)
            for _ in range(100):
                if burrow(w, "inspect", "reverse-owner")["state"] == "lost" and not listeners(32451):
                    break
                time.sleep(.1)
            assert burrow(w, "tunnel", "list")[0]["state"] == "unavailable" and not listeners(32451)
            burrow(w, "tunnel", "check", t["id"], ok=False)
            burrow(w, "close", "reverse-owner", "--yes")
            connect("reverse-owner")
            assert burrow(w, "tunnel", "list") == []
            burrow(w, "tunc", "reverse-owner", "r", "32451", "127.0.0.1", destination, "--review", old_review["digest"], "--yes", ok=False)
            fresh = burrow(w, "tunc", "reverse-owner", "r", "32451", "127.0.0.1", destination, "--yes")
            burrow(w, "tund", t["id"], "--yes", ok=False)
            traffic(fresh)
            evidence = w / "reverse-evidence"
            evidence.write_text("preserve reverse evidence")
            burrow(w, "close", "reverse-owner", "--yes")
            assert not listeners(32451) and burrow(w, "tunnel", "list") == []
            assert evidence.read_text() == "preserve reverse evidence"
            for path in w.rglob("*"):
                if path.is_file():
                    assert canary.encode() not in path.read_bytes(), path
            assert burrow(w, "inspect", "gateway")["masterPID"] == first["masterPID"]
            print("PASS reverse GatewayPorts policies, IPv6, server refusal, loss/close and secret exclusion", flush=True)
            return first_tunnel
        finally:
            policy()
            echo.shutdown()
            thread.join(timeout=5)
