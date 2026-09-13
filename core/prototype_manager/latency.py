"""Fresh phase comparison; shared host monotonic clock, never CLI-return timing."""
import base64
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import re
import select
import signal
import socket
import statistics
import subprocess
import sys
import threading
import time
import uuid


def measure(root, env, proof, baseline, wheel, key, port, config, secret, command, rpc, wait):
    samples=[]
    phases=[False,True] if "--trace-counts" in sys.argv else [False]
    provenance={"machine":platform.platform(),"cpu":next(line.split(":",1)[1].strip() for line in Path("/proc/cpuinfo").read_text().splitlines() if line.startswith("model name")),"initial_load":os.getloadavg(),"binary_sha256":{v:hashlib.sha256(Path(b).read_bytes()).hexdigest() for v,b in [("baseline",baseline),("manager",proof)]},"build":"Aspect fastbuild; phase timings untraced"}
    print("PROVENANCE "+json.dumps(provenance),flush=True)
    setup=[]
    tracing=["strace","--kill-on-exit","-f","-e","trace=process","-o"]
    def ended(pid):
        try:return Path(f"/proc/{pid}/stat").read_text().rsplit(")",1)[1].split()[0]=="Z"
        except (FileNotFoundError,ProcessLookupError):return True
    def execute(trace,*args):
        return json.loads(command(*([*tracing,trace] if traced else []),*args,env=env))
    def successful_execs(path):
        # Count successful executable starts, including short-lived children.
        # Threads are not executables; failed PATH searches are not starts.
        return len(re.findall(r'(?:execve\(.*|<\.\.\. execve resumed>.*)= 0$',path.read_text(),re.M))
    for variant, binary, traced in [(v,b,t) for t in phases for v,b in [("baseline",baseline),("manager",proof)]]:
        for index in range(2 if traced else 20):
            w=root/(("t" if traced else "u")+variant[0]+str(index))
            begin=time.monotonic()
            info=json.loads(command(proof,"setup",w,wheel,env=env))
            installed=time.monotonic()
            command(binary,"install",w,env=env)
            install_elapsed=time.monotonic()-installed
            os.kill(info["pid"],signal.SIGTERM);wait(lambda:ended(info["pid"]))
            # Harness-owned, empty runtime only; retain installed package/catalog.
            (w/"burrow").rmdir()
            for name in ["hoveld.sock","daemon.lock","daemon.json","burrow-launch.json","burrow-launch.log"]:
                (w/name).unlink(missing_ok=True)
            daemon_trace=root/(w.name+"-daemon.trace")
            output=root/(w.name+"-setup.json")
            started=time.monotonic()
            with output.open("w") as stream:
                tracer=subprocess.Popen([*([*tracing,str(daemon_trace)] if traced else []),proof,"setup",str(w),wheel],env=env,stdout=stream,stderr=subprocess.DEVNULL)
            info=None
            try:
                def read_setup():
                    try:return json.loads(output.read_text())
                    except json.JSONDecodeError:return None
                info=wait(read_setup)
                startup=time.monotonic()-started
                setup.append({"variant":variant,"traced":traced,"sample":index,"package_and_initial_setup_s":installed-begin,"module_install_s":install_elapsed,"verified_daemon_start_s":startup})
                print("SETUP "+json.dumps(setup[-1]),flush=True)
                owner=None
                for temperature in ["cold","warm"]:
                    prompt=index%2==1
                    name=temperature
                    front_trace=root/(w.name+"-"+temperature+".trace")
                    activation_trace=root/(w.name+"-activation.trace")
                    before=successful_execs(daemon_trace) if traced else 0
                    prompt_time=[]
                    prompt_socket=root/"measurement-prompt"
                    listener=None
                    responder=None
                    if variant=="manager" and prompt:
                        listener=socket.socket(socket.AF_UNIX)
                        listener.bind(str(prompt_socket));prompt_socket.chmod(0o600);listener.listen();listener.settimeout(15)
                        def answer():
                            peer,_=listener.accept()
                            with peer:
                                challenge=json.loads(peer.recv(8192))
                                assert challenge["Secret"]
                                prompt_time.append(time.monotonic_ns())
                                time.sleep(.02)
                                peer.sendall(json.dumps(base64.b64encode(secret.encode()).decode()).encode()+b"\n")
                        responder=threading.Thread(target=answer);responder.start()
                    submitted=time.monotonic_ns()
                    try:
                        if variant=="manager":
                            if owner is None:owner=execute(activation_trace,proof,"activate",w)
                            request={"id":uuid.uuid4().hex,"generation":owner["generation"],"session":owner["session"],
                                     "settings":{"workspace":str(w),"name":name,"host":"127.0.0.1","user":"tester","port":port,"key":"" if prompt else str(key),"sshConfig":str(config),"promptSocket":str(prompt_socket) if prompt else ""}}
                            raw=json.dumps(request);path=root/"measurement-request.json";path.write_text(raw);path.chmod(0o600)
                            state=execute(front_trace,proof,"connect",w,path,hashlib.sha256(raw.encode()).hexdigest())
                            def inspect():
                                result=rpc(w,"RunSessionCommand",{"SessionID":owner["session"],"Request":{"command":"list","args":[owner["generation"]]}})
                                return next(s for s in json.loads(result["stdout"]) if s["id"]==state["id"])
                        else:
                            args=[baseline,"prompt" if prompt else "exec",str(w),"connect",name,"127.0.0.1","tester","--port",str(port),"--ssh-config",str(config),"--yes"]
                            if not prompt:args += ["--key",str(key)]
                            if prompt:
                                process=subprocess.Popen([*([*tracing,str(front_trace)] if traced else []),*args],env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
                                try:
                                    assert select.select([process.stderr],[],[],20)[0],"baseline prompt timeout"
                                    line=process.stderr.readline().strip()
                                    assert line=="PRIVATE_PROMPT",line
                                    prompt_time.append(time.monotonic_ns());time.sleep(.02)
                                    stdout,stderr=process.communicate(secret+"\n",timeout=20)
                                    assert process.returncode==0,stderr
                                    state=json.loads(stdout)
                                finally:
                                    if process.poll() is None:process.kill();process.wait()
                            else:state=execute(front_trace,*args)
                            def inspect():
                                result=rpc(w,"RunSessionCommand",{"SessionID":state["session"],"Request":{"command":"connection-status"}})
                                return json.loads(result["stdout"])
                        def ready():
                            observed=inspect()
                            assert observed["state"]!="lost",observed
                            return observed if observed["state"]=="connected" else None
                        observed=wait(ready)
                        if responder:responder.join(timeout=2);assert not responder.is_alive()
                        assert observed["dispatch"]>=submitted and observed["connected"]>=observed["dispatch"]
                        item={"variant":variant,"traced":traced,"temperature":temperature,"auth":"password" if prompt else "key","sample":index,
                              "dispatch_s":(observed["dispatch"]-submitted)/1e9,"connected_s":(observed["connected"]-submitted)/1e9,
                              "prompt_s":(prompt_time[0]-observed["dispatch"])/1e9 if prompt else None,
                              "synthetic_response_delay_s":.02 if prompt else None,
                              "executable_starts":(successful_execs(daemon_trace)-before+successful_execs(front_trace)+(successful_execs(activation_trace) if variant=="manager" and temperature=="cold" else 0)) if traced else None}
                        samples.append(item)
                        print("SAMPLE "+json.dumps(item),flush=True)
                        if variant=="manager":
                            rpc(w,"RunSessionCommand",{"SessionID":owner["session"],"Request":{"command":"close","args":[owner["generation"],state["id"],"confirm"]}})
                        else:rpc(w,"RunSessionCommand",{"SessionID":state["session"],"Request":{"command":"connection-close","args":["confirm"]}})
                    finally:
                        if listener:listener.close();prompt_socket.unlink(missing_ok=True)
            finally:
                if info:
                    try:os.kill(info["pid"],signal.SIGTERM)
                    except ProcessLookupError:pass
                try:tracer.wait(timeout=15)
                except subprocess.TimeoutExpired:tracer.kill();tracer.wait(timeout=5)
    def stats(values):
        return {"n":len(values),"p50":statistics.median(values),"p95":sorted(values)[math.ceil(.95*len(values))-1]}
    groups={}
    for variant in ["baseline","manager"]:
        for temperature in ["cold","warm"]:
            group=[s for s in samples if s["variant"]==variant and s["temperature"]==temperature and not s["traced"]]
            groups[variant+"_"+temperature]={key:stats([s[key] for s in group if s[key] is not None]) for key in ["dispatch_s","connected_s","prompt_s"]}
    warm=groups["manager_warm"]["dispatch_s"];base=groups["baseline_warm"]["dispatch_s"]
    accepted=warm["p95"]<=1 and warm["p50"]<=.5*base["p50"] and groups["manager_cold"]["dispatch_s"]["p95"]<=1.1*groups["baseline_cold"]["dispatch_s"]["p95"]
    result={**provenance,"final_load":os.getloadavg(),"groups":groups,"process_samples":[s for s in samples if s["traced"]],"setup":setup,"historical_threshold_met":accepted,"warm_median_reduction":1-warm["p50"]/base["p50"]}
    print("RESULT "+json.dumps(result),flush=True)
    # Owner accepted the measured gain on 2026-09-12; no hard latency gate.
    print("PASS phase measurement; historical threshold is informational",flush=True)
