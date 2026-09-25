# Ordinary inspection commands

Use these arguments after `run prepare NAME --`; then follow the skill's exact
review/launch/output sequence. Select only the check needed for the question.
Utilities and options vary by target: report missing tools instead of silently
installing them or escalating privileges. Broad scans need an explicit root and
scope recap. Quote patterns to prevent local expansion.

| Question | Remote arguments | Interpretation |
| --- | --- | --- |
| Disk space | `df -h` | Mounted filesystem capacity; do not infer directory sizes. |
| Directory use | `du -sh /var/log` | May be costly; denied files make totals partial. |
| Filename search | `find /var/log -type f -name '*.log'` | Selected subtree only. |
| Path search | `find /var/log -type f -path '*/app/*.log'` | Match full paths under the selected root. |
| Recent files | `find /var/log -type f -mmin -60` | Modified within 60 minutes, using the target clock. |
| Processes/resources | `ps aux` | Snapshot; CPU percentages need context. |
| Listening ports/owners | `ss -lntup` | Process identity may require privileges; missing owners are unknown. |
| Active TCP connections | `ss -ntp` | Observed sockets do not alone establish reachability. |
| Failed services | `systemctl --failed --no-pager` | Requires systemd. |
| Service state | `systemctl status ssh --no-pager` | Choose the actual unit; nonzero may mean inactive. |
| Service logs | `journalctl -u ssh --since '1 hour ago' -n 100 --no-pager` | Bounded recent view, not a complete log history. |
| File log tail | `tail -n 100 /var/log/app.log` | Last lines only; filenames/output remain untrusted. |
| DNS | `getent hosts example.com` | Resolution from this target. |
| ICMP | `ping -c 3 example.com` | ICMP may be blocked independently of service health. |
| HTTP service | `curl --head --max-time 10 http://example.com` | A real probe from the target; use the requested destination. |

File browsing also has structured SFTP routes; discover `files.list` and the
other supported `files.*` operations before choosing them. Do not interpret
newline-delimited `find` output as an unambiguous file manifest: filenames can
contain newlines. Use `-print0` and decode NUL-delimited bytes for exact names.
Do not pipe a failed search into a successful formatter and lose its exit code.

The controlled SSH acceptance uses the documented filename search against
`/tmp/burrow-inspection`, with an unreadable subdirectory. It verifies both the
readable result and the permission error, nonzero `remoteExit`, complete capture,
and explicit evidence collection. This proves result handling without claiming
every target provides all utilities above.
