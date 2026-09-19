# Burrow Ubuntu survey v1. Read-only, no sudo or package installation.
# The Ubuntu preset is also exercised against the disposable Linux SSH lab;
# missing tools and other userlands remain explicit coverage limitations.
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
LC_ALL=C
export LC_ALL
passed=0
failed=0
unavailable=0

check() {
    title=$1
    shift
    printf '\n## %s\n\nCommand: `%s`\n\n' "$title" "$*"
    if ! command -v "$1" >/dev/null 2>&1 || ! command -v timeout >/dev/null 2>&1; then
        printf 'Status: UNAVAILABLE (command or timeout utility missing).\n'
        unavailable=$((unavailable + 1))
        return
    fi
    output=$(timeout -s KILL 5 "$@" 2>&1)
    status=$?
    if [ "$status" -eq 0 ]; then
        printf 'Status: OBSERVED (exit 0).\n\n'
        passed=$((passed + 1))
    else
        printf 'Status: FAILED (exit %s; 124/137 can indicate the five-second timeout).\n\n' "$status"
        failed=$((failed + 1))
    fi
    if [ -n "$output" ]; then
        # Indentation keeps hostile Markdown headings/fences inside literal code.
        printf '%s\n' "$output" | sed 's/^/    /'
    else
        printf 'No output returned.\n'
    fi
}

printf '# Ubuntu host survey\n\nPreset: Ubuntu v1. Observations use the connected account; no privilege escalation.\n'
printf '\nOther Linux userlands are unverified; missing tools are recorded, never installed.\n'
check 'Observation time (UTC)' date -u '+%Y-%m-%dT%H:%M:%SZ'
check 'Operating system' cat /etc/os-release
check 'Hostname' hostname
check 'Kernel and architecture' uname -srmo
check 'Current user and groups' id
check 'Uptime (seconds)' cat /proc/uptime
check 'Load averages' cat /proc/loadavg
check 'Memory (kB)' cat /proc/meminfo
check 'Filesystem capacity (KiB)' df -Pk
check 'Network addresses' ip address show
check 'Routes' ip route show
check 'Listening TCP and UDP sockets' ss -lntu
check 'Failed services' systemctl --failed --no-pager --plain
printf '\n## Coverage\n\nObserved: %s; failed: %s; unavailable: %s.\n' "$passed" "$failed" "$unavailable"
printf '\nA completed capture does not imply every check succeeded. Raw observations are not vulnerability findings.\n'
if [ "$failed" -gt 0 ] || [ "$unavailable" -gt 0 ]; then exit 1; fi
