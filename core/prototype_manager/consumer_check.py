"""Issue #72: real confirmed consumers of the retained owner, never production."""
import hashlib
import http.server
import json
from pathlib import Path
import socket
import sqlite3
import subprocess
import threading
import os
import signal
import uuid
from contextlib import closing


def check(root, env, proof, w, daemon_pid, owner, first, second, workspace, run, command, rpc, control, connected, wait):
    requests = []
    stalled = {}
    evidence = []

    class Echo(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            nonce = self.path.removeprefix("/")
            requests.append(nonce)
            if nonce in stalled:
                stalled[nonce].wait(12)
            self.send_response(200)
            self.end_headers()
            try:
                self.wfile.write(nonce.encode())
            except (BrokenPipeError, ConnectionResetError):
                pass

        def log_message(self, *_):
            pass

    def free_port():
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            return sock.getsockname()[1]

    with http.server.ThreadingHTTPServer(("127.0.0.1", 0), Echo) as server:
        threading.Thread(target=server.serve_forever, daemon=True).start()
        # Reuse the existing nonce HTTP fixture through a remote loopback relay.
        # This fixture-only reverse forward makes the host HTTP server reachable
        # inside the disposable container; both hops use the selected SSH master.
        remote_port = free_port()
        ssh = ["ssh", "-F", "/dev/null", "-S", first["socket"], "-o", "ProxyCommand=/bin/false"]
        command(*ssh, "-O", "forward", "-R", f"127.0.0.1:{remote_port}:127.0.0.1:{server.server_port}", "unused", env=env)
        selection = {"workspace": str(w), "session": owner["session"], "generation": owner["generation"],
                     "connection": first["id"], "tunnel": uuid.uuid4().hex,
                     "bind": f"127.0.0.1:{free_port()}", "destination": f"127.0.0.1:{remote_port}"}

        def prepare(action, selected=None):
            request = {**(selected or selection), "action": action, "flow": uuid.uuid4().hex, "nonce": uuid.uuid4().hex[:16]}
            raw = json.dumps(request)
            path = root / (request["flow"] + ".json")
            path.write_text(raw)
            return request, [proof, action, str(w), str(path), hashlib.sha256(raw.encode()).hexdigest()]

        def submit(action, selected=None, ok=True):
            request, args = prepare(action, selected)
            result = run(*args, env=env, ok=ok)
            if ok and action == "consume":
                assert result["nonce"] == request["nonce"] and request["nonce"] in requests
                evidence.append((result, request))
            if not ok:
                assert request["nonce"] not in requests
            return result, request

        def tunnel_close(selected):
            request, _ = prepare("tunnel-close", selected)
            raw = json.dumps(request)
            rpc(w, "RunSessionCommand", {"SessionID": owner["session"], "Request": {
                "command": "tunnel-close", "args": [raw, hashlib.sha256(raw.encode()).hexdigest(), "operator-close"]}})

        try:
            submit("tunnel-open")
            result, request = submit("consume")
            assert result["nonce"] == request["nonce"] and request["nonce"] in requests
            assert result["ownerPID"] == owner["ownerPID"] and result["adapterPID"] != owner["ownerPID"]
            assert connected(w, owner, first["id"])["pid"] == first["pid"]
            print("PASS fresh confirmed consumer carries HTTP bytes through the same retained master", flush=True)
            sibling = {**selection, "connection": second["id"], "tunnel": uuid.uuid4().hex, "bind": f"127.0.0.1:{free_port()}"}
            submit("tunnel-open", sibling)
            submit("tunnel-open", {**selection, "tunnel": uuid.uuid4().hex}, ok=False)
            replay, replay_args = prepare("consume")
            run(*replay_args, env=env)
            before = len(requests)
            run(*replay_args, env=env, ok=False)
            assert len(requests) == before

            # Bypass frontend validation to exercise the real confirmed adapter:
            # modifying the approved request or action still cannot dispatch.
            hovel = root / "cache/burrow/hovel/0.4.2/hovel"
            for mutation in ["request", "action", "owner", "policy"]:
                approved, _ = prepare("consume")
                raw = json.dumps(approved)
                config = {"workspace": str(w), "action": "consume", "session": owner["session"],
                          "generation": owner["generation"], "request": raw,
                          "review": hashlib.sha256(raw.encode()).hexdigest()}
                if mutation == "request":
                    config["request"] = json.dumps({**approved, "nonce": "0123456789abcdef"})
                elif mutation == "action":
                    config["action"] = "tunnel-open"
                elif mutation == "owner":
                    config["generation"] = "changed-owner"
                op = "consumer-refusal-" + mutation
                rpc(w, "CreateOperation", {"Operation": op})
                rpc(w, "CreateChain", {"Operation": op, "Chain": "request"})
                rpc(w, "AddModule", {"Operation": op, "Chain": "request", "ModuleID": "burrow@0.1.0"})
                rpc(w, "AddTarget", {"Operation": op, "Chain": "request", "Target": "local://consumer-proof"})
                for key, value in config.items():
                    rpc(w, "SetChainConfig", {"Operation": op, "Chain": "request", "Key": key, "Value": value})
                if mutation == "policy":
                    rpc(w, "AttachEntity", {"id": "consumer-reviewer", "kind": "cli", "operation": op, "activeChain": "request"})
                    rpc(w, "SetLaunchKeyPolicy", {"operation": op, "mode": "all_connected"})
                args = [hovel, "run", "--workspace", w, "--daemon-endpoint", w / "hoveld.sock",
                        "--op", op, "--chain", "request", "--", "throw", "--now", "--allow-dangerous", "--json"]
                before = len(requests)
                if mutation == "policy":
                    command(*args, env=env, ok=False)
                else:
                    outcome = json.loads(command(*args, env=env))
                    assert outcome["results"][0]["state"] == "failed", outcome
                assert len(requests) == before
            print("PASS confirmed adapter binds request/action/owner; replay and launch-key refusal preserve traffic", flush=True)
            # Stale selectors must fail in the confirmed adapter/owner path.
            for key, value in [("generation", "stale"), ("session", "missing"), ("connection", uuid.uuid4().hex),
                               ("tunnel", uuid.uuid4().hex), ("destination", f"127.0.0.1:{free_port()}"),
                               ("bind", f"127.0.0.1:{free_port()}")]:
                submit("consume", {**selection, key: value}, ok=False)
            other, _ = workspace("consumer-other")
            changed, args = prepare("consume", {**selection, "workspace": str(other)})
            args[2] = str(other)
            run(*args, env=env, ok=False)
            assert changed["nonce"] not in requests

            # Generic TCP exploration: a foreign retained ID cannot be adopted.
            body = rpc(w, "OpenMeshStream", {"ModuleID": "burrow@0.1.0", "Request": {
                "runId": "consumer-stream-proof", "nodeId": first["id"], "protocol": "tcp",
                "destinationHost": "127.0.0.1", "destinationPort": remote_port,
                "config": {"request": json.dumps(request)}}}, ok=False)
            assert "already tracked" in body, body
            for kind in ["command", "execute", "upload_execute", "load"]:
                rpc(w, "RunMeshTask", {"ModuleID": "burrow@0.1.0", "Request": {"kind": kind, "nodeId": first["id"]}}, ok=False)
            submit("consume", sibling)
            print("PASS negative generic TCP adoption: existing session already tracked; execution Mesh tasks rejected", flush=True)

            # Recreation at the same port gives a new identity; the old ID fails.
            tunnel_close(selection)
            old = selection
            selection = {**selection, "tunnel": uuid.uuid4().hex}
            submit("tunnel-open")
            submit("consume", old, ok=False)
            submit("consume")
            submit("consume", sibling)
            print("PASS stale identities, workspace/destination refusal and same-port recreation", flush=True)

            # Disconnect does not claim cancellation. The bounded owner operation
            # can finish; explicit close cancels only the selected active flow.
            for mode in ["disconnect", "close"]:
                pending, args = prepare("consume")
                stalled[pending["nonce"]] = threading.Event()
                process = subprocess.Popen(args, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                try:
                    wait(lambda: pending["nonce"] in requests)
                    submit("consume", sibling)
                    assert not control(w, owner, "flow-status", pending["flow"])["finished"]
                    if mode == "disconnect":
                        process.kill()
                        process.communicate(timeout=5)
                        stalled[pending["nonce"]].set()
                    else:
                        result = control(w, owner, "flow-close", pending["flow"], "confirm")
                        assert result["state"] == "cancellation-requested"
                        process.communicate(timeout=15)
                        assert process.returncode != 0
                    wait(lambda: control(w, owner, "flow-status", pending["flow"])["finished"])
                    submit("consume", sibling)
                    submit("consume")
                    assert connected(w, owner, first["id"])["pid"] == first["pid"]
                finally:
                    stalled[pending["nonce"]].set()
                    if process.poll() is None:
                        process.kill(); process.communicate(timeout=5)
            print("PASS consumer disconnect/explicit flow close preserve connection and sibling traffic", flush=True)

            pending, args = prepare("consume")
            stalled[pending["nonce"]] = threading.Event()
            process = subprocess.Popen(args, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                wait(lambda: pending["nonce"] in requests)
                os.kill(owner["ownerPID"], signal.SIGKILL)
                process.communicate(timeout=15)
                assert process.returncode != 0
                submit("consume", ok=False)
                run(proof, "activate", w, env=env, ok=False)
                for state in [first, second]:
                    def ended():
                        try:
                            return (Path(f"/proc/{state['pid']}/stat").read_text().rsplit(")", 1)[1].split()[0] == "Z")
                        except FileNotFoundError:
                            return True
                    wait(ended)
                print("PASS owner loss terminates flow/masters and refuses fallback or adoption", flush=True)
            finally:
                stalled[pending["nonce"]].set()
                if process.poll() is None:
                    process.kill(); process.communicate(timeout=5)
            # Inspect durable evidence only after Hovel has released its private
            # database. Direct live SQL inspection is not a supported RPC path.
            os.kill(daemon_pid, signal.SIGTERM)
            def daemon_ended():
                try:
                    return Path(f"/proc/{daemon_pid}/stat").read_text().rsplit(")", 1)[1].split()[0] == "Z"
                except FileNotFoundError:
                    return True
            wait(daemon_ended)
            with closing(sqlite3.connect(w / "workspace.db")) as db:
                for result, request in evidence:
                    plans = [json.loads(row[0]) for row in db.execute("select p.plan_json from throw_plans p join throw_records r on r.plan_id=p.id, json_each(r.throw_json, '$.runs') j where json_extract(j.value, '$.runId')=?", (result["runID"],))]
                    assert len(plans) == 1 and plans[0]["confirmationId"], {"run": result["runID"], "plans": plans}
                    assert db.execute("select count(*) from throw_confirmations where id=?", (plans[0]["confirmationId"],)).fetchone()[0] == 1
                    assert json.loads(plans[0]["chainConfig"]["request"]) == request
                    artifact = db.execute("select path from artifacts where run_id=?", (result["runID"],)).fetchone()[0]
                    report = json.loads((w / artifact).read_text())
                    assert report == result and report["selection"] == request
            print("PASS all collected consumer results retain exact confirmed plans and artifacts after daemon shutdown", flush=True)
        finally:
            server.shutdown()
