# Scripts and local automation

Discover `run.prepare` options and inspect `local` first. Script and independent
stdin files must already be in the selected upload root. A user's explicit
request to create a script there is different from copying arbitrary local
files there to evade containment. Sources and arguments are non-secret; prepared
runs keep private snapshots, so changing a source needs a new preparation.

Select the requested mode explicitly:

| Mode | Behavior |
| --- | --- |
| `stream` | Interpreter reads script from stdin; independent `--stdin` is unavailable. |
| `inline` | Source is passed in argv (up to 64 KiB); can use independent stdin. |
| `stage` | Remote-only owned temporary script; cleanup is tracked, `--keep` explicitly retains it. |

```sh
burrow --workspace PATH run prepare NAME --script check.sh --mode stream --interpreter /bin/sh -- arg1
burrow --workspace PATH run prepare NAME --script check.sh --mode stage --interpreter /bin/sh --stdin input.bin -- arg1
```

These are alternative preparations, not a sequence. Follow `burrow-run` to
review the snapshot, target, arguments, staging and capture, then launch the
chosen RUN. Inspect script cleanup separately from exit/capture; owner loss or
ordinary cancellation does not prove remote temporary files were removed.

For an explicitly selected local tool use
`run prepare NAME --local -- /absolute/tool ARG...`; it runs on the daemon host
in the workspace, with the documented allowlisted environment. Report its
`localExit`, not a remote result. Local scripts use the same explicit stream or
inline modes; discover options before combining them.

For reusable automation, prefer existing Hovel operations and saved chains.
`chain connect` exports connection settings for a separate confirmed Hovel
throw. `chain select`, `chain export` and `chain http` consume live forwarding;
load `burrow-tunnels` for that workflow. Exports do not execute or grant approval.
Preserve Hovel planning, launch keys, confirmation and audit policy; generated
adapter requests and control sockets are not a scripting API to recreate.
