# Private controller input

All control requests require `--request-stdin`, one JSON object on piped/file
stdin and captured stdout. Discover the operation's input schema first. Keep
tokens in process memory; a deliberately retained file must be mode 0600 and
kept out of transcripts/evidence. Capture claim/takeover responses without
printing them into the agent context. The label is descriptive, not liveness.

```python
import base64, json, subprocess
base = ["burrow", "--workspace", PATH, "session"]

def control(action, request):
    result = subprocess.run(base + [action, NAME, ID, "--request-stdin"],
                            input=json.dumps(request), text=True,
                            capture_output=True, check=True)
    return json.loads(result.stdout)

claim = control("claim", {"label": "maintenance"})
try:
    accepted = control("input", {"token": claim["token"],
                                "data": base64.b64encode(b"pwd\n").decode()})
    # Inspect acceptedBytes, backpressure, inputError and auditError privately.
finally:
    control("release", {"token": claim["token"]})
```

PATH, NAME and ID must come from the selected resources. This example assumes
`pwd` is the authorized input. For explicit takeover use
`{"generation": observed["controlGeneration"], "label": "maintenance"}`;
inspect first, then submit once. For resize send token plus columns/rows,
each 1–1000 and at most 20000 total cells. Omitted claim/takeover dimensions
preserve existing geometry.

Input contains 1–4096 decoded bytes. `acceptedBytes` is the accepted prefix,
not completed execution. Backpressure or `inputError` can leave an unaccepted
suffix; only that suffix is eligible for an intentional later write. If the
response is lost, acceptance is unknown: inspect first and do not automatically
replay bytes, claim or takeover. An `auditError` can follow successful control;
retain the token privately and report the evidence gap. Old input/resize/release
tokens fail after takeover. A failed release is not proof of detachment.
