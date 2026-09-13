"""Disposable #71 public throws/session controls and real loopback SSH proof."""
import base64
import concurrent.futures
import hashlib
import http.client
import json
import os
from pathlib import Path
import signal
import socket
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
import uuid


def command(*args, env=None, ok=True, input=None):
    p = subprocess.run(list(map(str, args)), env=env, input=input, capture_output=True, text=True, timeout=45)
    assert (p.returncode == 0) == ok, (p.stdout, p.stderr)
    return p.stdout


def run(*args, env, ok=True):
    output = command(*args, env=env, ok=ok)
    return json.loads(output) if ok else None


def wait(check):
    end = time.monotonic() + 15
    while time.monotonic() < end:
        result = check()
        if result:
            return result
        time.sleep(.03)
    raise AssertionError("bounded transition timed out")


def rpc(w, method, data, ok=True):
    conn = http.client.HTTPConnection("localhost", timeout=10)
    conn.sock = socket.socket(socket.AF_UNIX)
    conn.sock.settimeout(10)
    conn.sock.connect(str(w / "hoveld.sock"))
    try:
        conn.request("POST", "/hovel.daemon.v1.DaemonService/" + method, json.dumps(data), {"Content-Type":"application/json"})
        response = conn.getresponse()
        raw = response.read()
        body = json.loads(raw) if response.status == 200 else raw.decode()
        assert (response.status == 200) == ok, body
        return body
    finally:
        conn.close()


def artifact(arg):
    path=Path(arg)
    if path.exists():return str(path.resolve())
    runfiles=Path(os.environ.get("RUNFILES_DIR",str(Path(__file__).absolute().parents[3])))
    return str((runfiles/arg.removeprefix("external/") if arg.startswith("external/") else runfiles/"_main"/arg).resolve())


proof, production, wheel, image_file, baseline = map(artifact,sys.argv[1:6])
with tempfile.TemporaryDirectory(prefix="bm-") as scratch:
    root = Path(scratch)
    env = {k: v for k, v in os.environ.items() if not k.startswith(("HOVEL_", "SSH_", "BURROW_"))}
    env.update(HOME=scratch, XDG_CACHE_HOME=str(root / "cache"), XDG_CONFIG_HOME=str(root / "config"))
    # Isolate normal OpenSSH config/default identities from the operator's keys.
    # OpenSSH gets the actual account home through passwd, not HOME.
    key = root / "client"
    command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key)
    ssh_config=root/"ssh_config"
    ssh_config.write_text("Host *\n IdentityFile none\n IdentityAgent none\n")
    daemons = []
    container = None
    agent = None
    try:
        container = command("docker", "run", "-d", "--rm", "--publish", "127.0.0.1::2222",
                            "--env", "USER_NAME=tester", "--env", "PASSWORD_ACCESS=true", "--env", "PUBLIC_KEY_FILE=/client.pub",
                            "--mount", f"type=bind,src={key}.pub,dst=/client.pub,readonly", Path(image_file).read_text().strip()).strip()
        port = int(command("docker", "port", container, "2222/tcp").strip().rsplit(":",1)[1])
        def ready():
            try:
                with socket.create_connection(("127.0.0.1",port), timeout=1) as sock:
                    return sock.recv(128).startswith(b"SSH-")
            except OSError:
                return False
        wait(ready)
        if "--consumer" in sys.argv:
            command("docker", "exec", container, "sh", "-c",
                    "sed -i 's/^AllowTcpForwarding no$/AllowTcpForwarding yes/' /config/sshd/sshd_config && kill -HUP $(cat /config/sshd.pid)")
        secret = "proof-" + uuid.uuid4().hex
        command("docker", "exec", "-i", container, "chpasswd", input="tester:"+secret+"\n")
        def workspace(name):
            w = root / name
            info = run(production, "--workspace", w, "--hovel-package", wheel, "status", env=env)
            daemons.append(info["pid"])
            run(proof, "install", w, env=env)
            return w, info
        if "--measure" in sys.argv:
            from core.prototype_manager.latency import measure
            measure(root, env, proof, baseline, wheel, key, port, ssh_config, secret, command, rpc, wait)
            sys.exit(0)
        w, info = workspace("w")
        run(proof,"activate",w,"undispatched-generation","cancel",env=env)
        assert not (w/"burrow/manager").exists()
        # Concurrent first attachments must create one owner or refuse safely.
        def activate():
            p = subprocess.run([proof, "activate", str(w)], env=env, capture_output=True, text=True, timeout=45)
            return json.loads(p.stdout) if p.returncode == 0 else None
        with concurrent.futures.ThreadPoolExecutor(2) as pool:
            attached = list(pool.map(lambda _: activate(), range(2)))
        assert sum(x is not None for x in attached) == 1, attached
        owner = next(x for x in attached if x)
        observed = run(proof, "forward", w, owner["session"], owner["generation"], env=env)
        assert observed["generation"] == owner["generation"] and observed["ownerPID"] == owner["ownerPID"]
        assert observed["runID"] != owner["runID"]
        def control(w, owner, cmd, *args, ok=True):
            result = rpc(w,"RunSessionCommand",{"SessionID":owner["session"],"Request":{"command":cmd,"args":[owner["generation"],*args]}},ok=ok)
            return json.loads(result["stdout"]) if ok else None
        def submit(w, owner, name, prompt="", identity=key, ok=True):
            request = {"id":uuid.uuid4().hex,"generation":owner["generation"],"session":owner["session"],
                       "settings":{"workspace":str(w),"name":name,"host":"127.0.0.1","user":"tester","port":port,
                                   "key":str(identity) if identity else "", "promptSocket":str(prompt), "sshConfig":str(ssh_config)}}
            path = root / (request["id"] + ".json")
            raw = json.dumps(request)
            path.write_text(raw);path.chmod(0o600)
            digest = hashlib.sha256(raw.encode()).hexdigest()
            started = time.monotonic_ns()
            result = run(proof,"connect",w,path,digest,env=env,ok=ok)
            if ok:
                assert result["id"] == request["id"]
                result["submission"] = started
            return result, path, digest
        def connected(w,owner,id):
            return next((s for s in control(w,owner,"list") if s["id"]==id and s["state"]=="connected"),None)
        # Prepare snapshots the current frontend's agent before review/digest.
        # The daemon was started without that socket; no daemon env refresh.
        agent_socket=root/"agent"
        agent=subprocess.Popen(["ssh-agent","-D","-a",str(agent_socket)],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        wait(agent_socket.exists)
        frontend_env={**env,"SSH_AUTH_SOCK":str(agent_socket)}
        command("ssh-add",key,env=frontend_env)
        agent_config=root/"agent_config";agent_config.write_text("Host *\n IdentityFile none\n")
        configured_key=root/"configured_key";configured_key.write_text(f"Host *\n IdentityFile {key}\n IdentityAgent none\n")
        for name,config,frontenv in [("agent-key",agent_config,frontend_env),("config-key",configured_key,env)]:
            prepared=run(proof,"prepare",w,owner["session"],owner["generation"],name,"127.0.0.1","tester","--port",port,"--ssh-config",config,env=frontenv)
            assert not prepared["settings"].get("key")
            if name=="agent-key":assert prepared["settings"]["agent"]==str(agent_socket)
            path=root/(name+".json");raw=json.dumps(prepared);path.write_text(raw)
            reviewed=hashlib.sha256(raw.encode()).hexdigest()
            run(proof,"connect",w,path,reviewed,"cancel",env=frontenv)
            assert not (w/"burrow"/name).exists()
            # Deliberately discard the launch acknowledgement, then inspect the
            # known generation/creation from a new frontend without resubmitting.
            command(proof,"connect",w,path,reviewed,env=frontenv)
            state=run(proof,"reconcile",w,owner["generation"],prepared["id"],env=env)
            assert state["id"]==prepared["id"]
            wait(lambda:connected(w,owner,state["id"]))
            control(w,owner,"close",state["id"],"confirm")
        print("PASS frontend agent/config identities, pre-dispatch cancellation and lost-ack reconciliation",flush=True)
        # Two separate frontends submit immutable requests concurrently.
        with concurrent.futures.ThreadPoolExecutor(2) as pool:
            attempts = list(pool.map(lambda name: submit(w,owner,name), ["first","second"]))
        first = wait(lambda: connected(w,owner,attempts[0][0]["id"]))
        second = wait(lambda: connected(w,owner,attempts[1][0]["id"]))
        assert first["pid"] != second["pid"]
        for state in [first,second]:
            assert int(Path(f"/proc/{state['pid']}/stat").read_text().rsplit(")",1)[1].split()[1]) == owner["ownerPID"]
        with concurrent.futures.ThreadPoolExecutor(2) as pool:
            views = list(pool.map(lambda _: run(proof,"control",w,owner["session"],"list",owner["generation"],env=env),range(2)))
        assert {s["id"] for s in views[0]} == {s["id"] for s in views[1]} == {first["id"],second["id"]}
        print("PASS two real SSH masters, one retained base-module manager, two frontends",flush=True)
        if "--consumer" in sys.argv:
            from core.prototype_manager.consumer_check import check
            check(root, env, proof, w, owner, first, second, workspace, run, command, rpc, control, connected, wait)
            sys.exit(0)
        # A changed request never dispatches; replay cannot recreate a connection.
        path,digest = attempts[0][1:]
        raw = path.read_text()
        changed = json.loads(raw);changed["settings"]["name"]="changed"
        path.write_text(json.dumps(changed))
        run(proof,"connect",w,path,digest,env=env,ok=False)
        assert not (w/"burrow/changed").exists()
        path.write_text(raw)
        run(proof,"connect",w,path,digest,env=env,ok=False)
        run(proof,"forward",w,owner["session"],"wrong-generation",env=env,ok=False)
        control(w,owner,"close","wrong-creation","confirm",ok=False)
        # A reviewed request still obeys dangerous-module and launch-key policy.
        op="policy-proof"
        rpc(w,"CreateOperation",{"Operation":op})
        rpc(w,"CreateChain",{"Operation":op,"Chain":"request"})
        rpc(w,"AddModule",{"Operation":op,"Chain":"request","ModuleID":"burrow@0.1.0"})
        rpc(w,"AddTarget",{"Operation":op,"Chain":"request","Target":"ssh://127.0.0.1"})
        denied=json.loads(raw);denied["id"]=uuid.uuid4().hex;denied["settings"]["name"]="policy-denied"
        denied_raw=json.dumps(denied)
        for k,v in {"workspace":str(w),"action":"connect","generation":owner["generation"],"session":owner["session"],"request":denied_raw,"review":hashlib.sha256(denied_raw.encode()).hexdigest()}.items():
            rpc(w,"SetChainConfig",{"Operation":op,"Chain":"request","Key":k,"Value":v})
        hovel=root/"cache/burrow/hovel/0.4.2/hovel"
        prefix=[hovel,"run","--workspace",w,"--daemon-endpoint",w/"hoveld.sock","--op",op,"--chain","request","--","throw","--now","--json"]
        command(*prefix,env=env,ok=False)
        rpc(w,"AttachEntity",{"id":"unapproved-reviewer","kind":"cli","operation":op,"activeChain":"request"})
        rpc(w,"SetLaunchKeyPolicy",{"operation":op,"mode":"all_connected"})
        command(*prefix,"--allow-dangerous",env=env,ok=False)
        assert not (w/"burrow/policy-denied").exists()
        other, other_info = workspace("other")
        generation=uuid.uuid4().hex
        command(proof,"activate",other,generation,env=env) # lost acknowledgement
        other_owner = run(proof,"reconcile",other,generation,env=env)
        run(proof,"reconcile",other,"unknown-generation",env=env,ok=False)
        run(proof,"activate",other,generation,env=env,ok=False)
        twin = submit(other,other_owner,"first")[0]
        twin = wait(lambda: connected(other,other_owner,twin["id"]))
        assert twin["socket"] != first["socket"]
        # Session IDs and names from another workspace must never redirect controls.
        run(proof,"forward",other,owner["session"],owner["generation"],env=env,ok=False)
        # Quitting a frontend with keep-running is just dropping its attachment.
        assert connected(w,owner,first["id"])["pid"] == first["pid"]
        # Private askpass answer uses only a same-user Unix socket and anonymous pipe.
        prompt_path = root / "prompt"
        with socket.socket(socket.AF_UNIX) as listener:
            listener.bind(str(prompt_path));prompt_path.chmod(0o600);listener.listen();listener.settimeout(10)
            pending = submit(w,owner,"password",prompt=prompt_path,identity=None)[0]
            peer,_ = listener.accept()
            with peer:
                prompt_started=time.monotonic_ns()
                challenge=json.loads(peer.recv(8192))
                assert challenge["Secret"] and "password" in challenge["Text"]
                assert pending["dispatch"] <= prompt_started
                # Keep this challenge stalled while another frontend lists/closes.
                begin=time.monotonic()
                assert connected(w,owner,second["id"])
                control(w,owner,"close",first["id"],"confirm")
                elapsed=time.monotonic()-begin
                assert elapsed<1,elapsed
                assert connected(w,owner,second["id"])
                peer.sendall(json.dumps(base64.b64encode(secret.encode()).decode()).encode()+b"\n")
            password = wait(lambda: connected(w,owner,pending["id"]))
            print(f"PASS private password and stalled-prompt list/sibling close ({elapsed:.3f}s)",flush=True)
            # Cancellation targets one creation, preserving its siblings.
            cancelled = submit(w,owner,"cancelled",prompt=prompt_path,identity=None)[0]
            peer,_=listener.accept()
            with peer:
                assert json.loads(peer.recv(8192))["Secret"]
                # Two frontends can cancel the same creation simultaneously;
                # neither may resurrect its closed inventory entry.
                with concurrent.futures.ThreadPoolExecutor(2) as pool:
                    list(pool.map(lambda _:control(w,owner,"close",cancelled["id"],"confirm"),range(2)))
            assert not (w/"burrow/cancelled").exists()
            assert all(s["id"]!=cancelled["id"] for s in control(w,owner,"list"))
            assert connected(w,owner,password["id"]) and connected(w,owner,second["id"])
        prompt_path.unlink()
        # One aggregate budget, including both connections and repeated management.
        for _ in range(265):
            assert len(control(w,owner,"list"))==2
        assert connected(w,owner,password["id"])
        # Manager-aware #76 backend review across both opened workspaces.
        def quit_review():
            snapshot=run(proof,"quit-review",w,other,env=env)
            path=root/"quit-review.json";raw=json.dumps(snapshot);path.write_text(raw)
            return path,hashlib.sha256(raw.encode()).hexdigest()
        review_path,review_digest=quit_review()
        for choice in ["keep","cancel"]:
            run(proof,"quit",review_path,review_digest,choice,env=env)
            assert len(control(w,owner,"list"))==2 and connected(other,other_owner,twin["id"])
        changed=submit(w,owner,"changed-quit-review")[0]
        wait(lambda:connected(w,owner,changed["id"]))
        run(proof,"quit",review_path,review_digest,"close",env=env,ok=False)
        assert len(control(w,owner,"list"))==3 and connected(other,other_owner,twin["id"])
        control(w,owner,"close",changed["id"],"confirm")
        evidence=w/"operator-evidence";evidence.write_text("retain")
        saved=w/"burrow-profiles.json";saved.write_text('{"saved":"retain"}')
        unknown=Path(second["socket"]).parent/"unknown";unknown.write_text("retain")
        review_path,review_digest=quit_review()
        run(proof,"quit",review_path,review_digest,"close",env=env,ok=False)
        assert unknown.read_text()=="retain" and evidence.read_text()=="retain"
        assert connected(other,other_owner,twin["id"])
        unknown.unlink()
        review_path,review_digest=quit_review()
        run(proof,"quit",review_path,review_digest,"close",env=env)
        assert control(w,owner,"list")==[] and evidence.read_text()=="retain"
        assert control(other,other_owner,"list")==[] and saved.read_text()=='{"saved":"retain"}'
        twin=submit(other,other_owner,"first")[0]
        twin=wait(lambda:connected(other,other_owner,twin["id"]))
        print("PASS quit keep/cancel/changed-review refusal/verified close across two workspaces",flush=True)
        # Inspect actual persisted evidence; no invented plan or payload records.
        with sqlite3.connect(w/"workspace.db") as db:
            plans=[json.loads(r[0]) for r in db.execute("select plan_json from throw_plans")]
            assert plans and all(p["confirmationId"] for p in plans)
            assert db.execute("select count(*) from throw_confirmations").fetchone()[0]>=len(plans)
        for file in root.rglob("*"):
            if file.is_file() and not file.is_symlink():
                assert secret.encode() not in file.read_bytes(),file
        print("PASS immutable/refused routing, workspace isolation, detach, selected cleanup and aggregate log ceiling",flush=True)
        # Owner failure loses all its workspace connections, never adopts them.
        lost=[submit(w,owner,name)[0] for name in ["lost1","lost2"]]
        lost=[wait(lambda s=s:connected(w,owner,s["id"])) for s in lost]
        os.kill(owner["ownerPID"],signal.SIGKILL)
        def ended(pid):
            try:return Path(f"/proc/{pid}/stat").read_text().rsplit(")",1)[1].split()[0]=="Z"
            except (FileNotFoundError,ProcessLookupError):return True
        for state in lost:wait(lambda:ended(state["pid"]))
        control(w,owner,"list",ok=False)
        run(proof,"activate",w,env=env,ok=False)
        assert all(Path(s["socket"]).parent.exists() for s in lost)
        assert connected(other,other_owner,twin["id"])
        os.kill(other_info["pid"],signal.SIGKILL)
        wait(lambda:ended(twin["pid"]))
        wait(lambda:ended(other_owner["ownerPID"]))
        run(proof,"forward",other,other_owner["session"],other_owner["generation"],env=env,ok=False)
        print("PASS owner/daemon loss refuses observation and adoption; reconnect remains manual",flush=True)
    finally:
        if agent:agent.terminate();agent.wait(timeout=5)
        for pid in daemons:
            try:os.kill(pid,signal.SIGTERM)
            except ProcessLookupError:pass
        if container:command("docker","rm","-f",container)
