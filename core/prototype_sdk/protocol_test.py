"""Disposable wire check. No daemon, network, or SSH behavior is implied."""
import base64
import json
import signal
import subprocess
import sys

signal.alarm(20)
with subprocess.Popen([sys.argv[1]], stdin=subprocess.PIPE, stdout=subprocess.PIPE) as process:
    notifications = []

    def rpc(method, params=None):
        body = json.dumps(dict(jsonrpc="2.0", id=1, method=method, params=params or {})).encode()
        process.stdin.write(f"Content-Length: {len(body)}\r\n\r\n".encode() + body)
        process.stdin.flush()
        while True:
            header = process.stdout.readline()
            assert header.startswith(b"Content-Length: "), header
            assert process.stdout.readline() == b"\r\n"
            message = json.loads(process.stdout.read(int(header.split(b":")[1])))
            if "id" in message:
                assert "error" not in message, message
                return message["result"]
            notifications.append(message)

    info = rpc("handshake")
    assert "burrow-sdk-prototype" in json.dumps(info), info
    rpc("schema")
    result = rpc("execute", dict(runId="proof", moduleId="burrow-sdk-prototype@0.0.0", target="inert"))
    session = result["sessions"][0]["id"]
    assert any(n["method"] == "module/log" for n in notifications)
    rpc("session/write", dict(sessionId=session, data=base64.b64encode(b"hello\n").decode()))
    output = b""
    for _ in range(3):
        response = rpc("session/read", dict(sessionId=session, timeoutMs=100))
        assert not response["closed"]
        output += base64.b64decode(response["data"])
    assert b"inert: hello" in output, output
    rpc("session/close", dict(sessionId=session, reason="proof complete"))
    assert rpc("session/read", dict(sessionId=session, timeoutMs=0))["closed"]
    rpc("shutdown")
    assert process.wait(timeout=5) == 0
    assert process.stdout.read() == b""
print("PASS: handshake/schema, framed logs, retained session I/O, explicit close, shutdown")
