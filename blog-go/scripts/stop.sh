#!/usr/bin/env bash
set -euo pipefail
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
mkdir -p "$ROOT/bin"
if ! mkdir "$ROOT/bin/.start-lock" 2>/dev/null; then
  echo "Another start/stop is in progress; nothing was stopped." >&2
  exit 1
fi
trap 'rmdir "$ROOT/bin/.start-lock"' EXIT
python3 - "$ROOT" <<'PY'
import json, os, pathlib, signal, subprocess, sys, time
root = pathlib.Path(sys.argv[1]); pidfile = root / 'bin/blog.pid'
if not pidfile.exists():
    print('No recorded backend process.')
    raise SystemExit(0)
saved = json.loads(pidfile.read_text()); pid = int(saved['pid'])
expected = str(root / 'bin/blog-server')
if pid <= 1 or saved.get('executable') != expected:
    raise SystemExit('PID record is not owned by this checkout; nothing was stopped.')
def running():
    result = subprocess.run(['ps', '-p', str(pid), '-o', 'stat='], capture_output=True, text=True)
    return result.returncode == 0 and result.stdout.strip() and not result.stdout.strip().startswith('Z')
if not running():
    pidfile.unlink(); print('Recorded backend is already stopped.'); raise SystemExit(0)
process_path = pathlib.Path('/proc') / str(pid) / 'exe'
actual = os.readlink(process_path) if process_path.exists() else subprocess.check_output(['ps', '-ww', '-p', str(pid), '-o', 'comm='], text=True).strip()
started = subprocess.check_output(['ps', '-p', str(pid), '-o', 'lstart='], text=True).strip()
if actual != expected or started != saved.get('started'):
    raise SystemExit('PID now belongs to a different executable or start time; nothing was stopped.')
os.kill(pid, signal.SIGTERM)
for attempt in range(60):
    if not running():
        pidfile.unlink(); print('Backend stopped; database and files preserved.'); break
    time.sleep(0.1)
else:
    raise SystemExit('Backend did not stop within its 5-second cleanup budget; PID record retained. No SIGKILL was sent.')
PY
