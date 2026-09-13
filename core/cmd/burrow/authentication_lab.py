"""Issue #46: real terminal secret delivery through the retained Hovel owner."""
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import termios
import time


def authentication_matrix(binary, workspace, root, env, container, port, key, fingerprint, burrow, wait, screen_check, prompt_only=False):
    secret = "synthetic-auth-" + os.urandom(16).hex()
    encrypted = root / "auth-key"
    subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", secret, "-f", str(encrypted)], check=True, capture_output=True)
    subprocess.run(["docker", "exec", "-i", container, "chpasswd"],
                   input=("tester:" + secret + "\n").encode(), check=True, capture_output=True)
    subprocess.run(["docker", "exec", "-i", container, "sh", "-c", "cat >> /config/.ssh/authorized_keys"],
                   input=encrypted.with_suffix(".pub").read_bytes(), check=True, capture_output=True)

    def no_leaks(output=b""):
        assert secret.encode() not in output, "secret appeared in terminal output"
        for process in Path("/proc").iterdir():
            if not process.name.isdigit():
                continue
            try:
                assert secret.encode() not in (process / "cmdline").read_bytes(), "secret appeared in argv"
            except (FileNotFoundError, PermissionError, ProcessLookupError):
                pass
        for path in workspace.rglob("*"):
            if path.is_file():
                data = path.read_bytes()
                assert secret.encode() not in data, "secret persisted in workspace"
                assert b"BEGIN OPENSSH PRIVATE KEY" not in data, "private key persisted in workspace"

    def terminal(args, answers, success=True, size=(30, 120), terminal_env=None, redirect=False, save=False, observe=False):
        outer, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", *size, 0, 0))
        before = termios.tcgetattr(slave)

        def controlling():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)

        p = subprocess.Popen([binary, "--workspace", str(workspace), *map(str, args)],
                             stdin=slave, stdout=subprocess.PIPE if redirect else slave, stderr=slave, env=terminal_env or env,
                             preexec_fn=controlling)
        output = bytearray()

        def read():
            if select.select([outer], [], [], .1)[0]:
                output.extend(os.read(outer, 65536))
            return bytes(output)

        try:
            offset = 0
            previous = None
            if success:
                answers = [*answers, ("Save profile as", b"\r" if save else b"\x1b")]
            for needle, answer in answers:
                deadline = time.monotonic() + 20
                while True:
                    data=read()
                    screen=subprocess.run([screen_check,str(size[1]),str(size[0])],input=data,capture_output=True,check=True).stdout
                    if needle.encode() in screen and (needle!=previous or needle.encode() in data[offset:]):
                        break
                    assert time.monotonic() < deadline and p.poll() is None, ("missing prompt", needle, bytes(output))
                offset = len(output)
                previous = needle
                if needle.startswith(("SSH password", "SSH key passphrase")):
                    assert not termios.tcgetattr(slave)[3] & termios.ECHO, "secret prompt published with echo enabled"
                no_leaks(output)
                if observe:
                    began = time.monotonic()
                    inventory = burrow(workspace,"connections")
                    listed = time.monotonic()-began
                    print(f"TIMING stalled-prompt CLI list: {listed:.3f}s", flush=True)
                    # Bound completion while the secret remains unanswered;
                    # #73 supersedes the historical one-second latency cutoff.
                    assert listed < 5, "list blocked behind a private prompt"
                    assert any(s["name"]=="prompt-sibling" for s in inventory)
                    began = time.monotonic()
                    burrow(workspace,"close","prompt-sibling","--yes")
                    closed = time.monotonic()-began
                    print(f"TIMING stalled-prompt CLI close: {closed:.3f}s", flush=True)
                    assert closed < 5, "sibling close blocked behind a private prompt"
                    assert burrow(workspace,"inspect","gateway")["state"]=="connected"
                    observe=False
                if answer is None:
                    p.send_signal(signal.SIGTERM)
                else:
                    os.write(outer, answer)
            deadline = time.monotonic() + 20
            while p.poll() is None:
                read()
                assert time.monotonic() < deadline, "interactive command did not finish"
            read()
            no_leaks(output)
            if "--no-color" in args:
                assert not re.search(rb"\x1b\[[0-9;:]*[34]8[;:]", output), "--no-color leaked form/recap colors"
            assert (p.returncode == 0) == success, bytes(output)
            assert termios.tcgetattr(slave) == before, "terminal modes not restored"
            if redirect:
                result=p.stdout.read()
                no_leaks(result)
                assert json.loads(result)["state"]=="connected", "forms contaminated JSON stdout"
            elif success and "--no-color" not in args and not (terminal_env or env).get("NO_COLOR"):
                assert re.search(rb'\x1b\[[0-9;:]*m"state"', output), "completed CLI result lost semantic colors"
            return bytes(output)
        finally:
            if p.poll() is None:
                p.kill()
                p.wait()
            os.close(outer)
            os.close(slave)

    base = ["127.0.0.1", "tester", "--port", port, "--prompt", "--yes"]
    terminal(["connect", "password", *base], [("SSH password", secret.encode()+b"\r")],redirect=True,save=True)
    assert burrow(workspace, "inspect", "password")["state"] == "connected"
    assert burrow(workspace, "profile", "select", "password")["host"] == "127.0.0.1"
    no_leaks()
    burrow(workspace, "close", "password", "--yes")
    burrow(workspace,"connect","prompt-sibling","127.0.0.1","tester","--key",key,"--port",port,"--yes")
    wait(lambda:burrow(workspace,"inspect","prompt-sibling")["state"]=="connected")
    color_env = {k:v for k,v in env.items() if k != "NO_COLOR"} | {"COLORTERM":"truecolor"}
    terminal(["connect", "passphrase", *base, "--key", encrypted], [("SSH key passphrase", secret.encode()+b"\r")],observe=True,terminal_env=color_env)
    assert burrow(workspace, "inspect", "passphrase")["state"] == "connected"
    burrow(workspace, "close", "passphrase", "--yes")
    if prompt_only:
        return secret, encrypted
    terminal(["connect", "signal-cancel", *base],[("SSH password",None)],success=False)
    terminal(["connect", "cancel-key", *base, "--key", encrypted], [("SSH key passphrase", b"\x03")], success=False)
    terminal(["connect", "bad-password", *base], [("SSH password", b"wrong\r")] * 3, success=False)
    for name in ("cancel-key", "bad-password", "signal-cancel"):
        assert not (workspace / "burrow" / name).exists(), "failed authentication left a reservation"

    # The target is reachable from the container only at port 2222. Both the
    # jump's published port and target's private port must verify host keys.
    config = root / "ssh-config"
    config.write_text(f"Host lab-jump\n HostName 127.0.0.1\n Port {port}\n User tester\n IdentityFile \"{key}\"\n"
                      "Host lab-target\n HostName 127.0.0.1\n Port 2222\n User tester\n ProxyJump lab-jump\n"
                      "Host *\n StrictHostKeyChecking no\n UserKnownHostsFile /dev/null\n")
    alias_args = ["lab-target", "-", "--ssh-config", config, "--key", key, "--prompt", "--yes"]
    terminal(["connect", "jumped", *alias_args], [])
    jumped = burrow(workspace, "inspect", "jumped")
    assert jumped["state"] == "connected" and jumped["port"] == 2222 and jumped["host"] == "127.0.0.1"
    burrow(workspace, "close", "jumped", "--yes")
    # Native aliases and command-line jumps use the same path after trust persists.
    terminal(["connect", "jump-again", *alias_args], [])
    burrow(workspace, "close", "jump-again", "--yes")
    terminal(["connect", "explicit-jump", "127.0.0.1", "tester", "--port", "2222", "--jump", "lab-jump",
              "--ssh-config", config, "--key", key, "--prompt", "--yes"], [])
    burrow(workspace, "close", "explicit-jump", "--yes")
    # Host-specific agents override an ambient socket, but explicit --agent
    # overrides config. Exercise the real owner, not generated config text.
    agent_socket = root / "agent.sock"
    agent_config = root / "agent-config"
    agent_config.write_text(f'Host agent-host\n HostName 127.0.0.1\n Port {port}\n User tester\n'
                            f' IdentityAgent "{agent_socket}"\n IdentityFile none\n')
    absent_env = env | {"SSH_AUTH_SOCK": str(root / "absent-agent")}
    agent_args = ["agent-host", "-", "--ssh-config", agent_config, "--prompt", "--yes"]
    terminal(["connect", "config-agent", *agent_args], [], terminal_env=absent_env)
    burrow(workspace, "close", "config-agent", "--yes")
    agent_config.write_text(agent_config.read_text().replace(f'"{agent_socket}"', "none"))
    terminal(["connect", "config-no-agent", *agent_args], [("SSH password", b"\x03")],
             success=False, terminal_env=env | {"SSH_AUTH_SOCK": str(agent_socket)})
    assert not (workspace / "burrow/config-no-agent").exists()
    terminal(["connect", "override-agent", *agent_args, "--agent", agent_socket], [])
    burrow(workspace, "close", "override-agent", "--yes")
    hop_config = root / "hop-agents"
    hop_config.write_text(f'Host lab-jump\n HostName 127.0.0.1\n Port {port}\n User tester\n'
                          f' IdentityAgent "{agent_socket}"\n IdentityFile none\n'
                          'Host lab-target\n HostName 127.0.0.1\n Port 2222\n User tester\n'
                          ' IdentityAgent none\n ProxyJump lab-jump\n')
    terminal(["connect", "hop-agent", "lab-target", "-", "--ssh-config", hop_config,
              "--key", key, "--prompt", "--yes"], [], terminal_env=absent_env)
    burrow(workspace, "close", "hop-agent", "--yes")

    # More unrelated agent keys than MaxAuthTries must not defeat an alias
    # selecting a public identity with IdentitiesOnly. No usable private key
    # file is selected, so success must come from that identity in the agent.
    crowded_socket = root / "crowded.sock"
    crowded = subprocess.Popen(["ssh-agent", "-D", "-a", str(crowded_socket)],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        wait(crowded_socket.exists)
        crowded_env = env | {"SSH_AUTH_SOCK": str(crowded_socket)}
        for i in range(8):
            noise = root / f"unrelated-{i}"
            subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(noise)], check=True, capture_output=True)
            subprocess.run(["ssh-add", str(noise)], env=crowded_env, check=True, capture_output=True)
        subprocess.run(["ssh-add", str(key)], env=crowded_env, check=True, capture_output=True)
        agent_config.write_text(f'Host agent-host\n HostName 127.0.0.1\n Port {port}\n User tester\n'
                                f' IdentityFile "{key}.pub"\n IdentitiesOnly yes\n')
        terminal(["connect", "selected-identity", *agent_args], [], terminal_env=crowded_env)
        burrow(workspace, "close", "selected-identity", "--yes")
    finally:
        crowded.terminate()
        crowded.wait(timeout=5)
    # Existing changed host entries are ignored under the accepted host policy.
    trust = workspace / "burrow-known_hosts"
    approved = trust.read_text()
    trust.write_text(f"[127.0.0.1]:{port} " + encrypted.with_suffix(".pub").read_text())
    terminal(["connect", "changed-jump", *alias_args], [])
    burrow(workspace, "close", "changed-jump", "--yes")
    assert trust.read_text() != approved
    trust.write_text(approved)
    bad = root / "bad-config"
    bad.write_text(config.read_text().replace(f"Port {port}", "Port 1"))
    terminal(["connect", "jump-failed", *[bad if a == config else a for a in alias_args]], [], success=False)
    assert not (workspace / "burrow/jump-failed").exists()
    terminal(["connect", "missing-agent", *base, "--agent", root / "absent-agent"], [("SSH password", b"\x03")], success=False)
    terminal(["connect", "missing-key", *base, "--key", root / "absent-key"], [("SSH password", b"\x03")], success=False)

    # LazySSH-style guided entry, review rejection, and a completed password
    # connection work on a narrow no-color terminal without command secrets.
    answers = [("Host / IP", b"127.0.0.1\r"), ("SSH port", str(port).encode()+b"\r"),
               ("Username", b"tester\r"), ("Connection name", b"guided\r"),
               ("SSH key path", b"\r"), ("SOCKS proxy port", b"\r"), ("Jump host", b"\r"), ("Agent socket", b"\r"),
               ("SSH config path", b"\r"), ("Proceed?", b"\t\r"),
               ("SSH password", secret.encode()+b"\r")]
    terminal(["--no-color", "connect"], answers, size=(24, 80), terminal_env={k:v for k,v in env.items() if k != "NO_COLOR"})
    burrow(workspace, "close", "guided", "--yes")
    terminal(["connect", "review-cancel", "127.0.0.1", "tester", "--port", port, "--prompt"],
             [("Proceed?", b"\r")], success=False)
    assert not (workspace / "burrow/review-cancel").exists()
    no_leaks()
    print("PASS password/encrypted-key prompts, no-echo, cancellation, aliases, per-hop agents, IdentitiesOnly, LazySSH jump policy/failure, guided review and leakage checks", flush=True)
    return secret, encrypted
