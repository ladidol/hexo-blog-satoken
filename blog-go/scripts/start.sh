#!/usr/bin/env bash
set -euo pipefail
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
cd "$ROOT"
mkdir -p "$ROOT/bin"
if ! mkdir "$ROOT/bin/.start-lock" 2>/dev/null; then
  echo "Another start is in progress; inspect bin/.start-lock before retrying." >&2
  exit 1
fi
BUILD="$ROOT/bin/.blog-server-build-$$"
trap 'rm -f "$BUILD"; rmdir "$ROOT/bin/.start-lock"' EXIT
python3 - "$ROOT" <<'PY'
import json, os, pathlib, socket, sys
root = pathlib.Path(sys.argv[1]); metadata = root / 'bin/blog.pid'
if metadata.exists():
    saved = json.loads(metadata.read_text()); pid = int(saved['pid'])
    if pid <= 1 or saved.get('executable') != str(root / 'bin/blog-server'):
        raise SystemExit('PID record is not owned by this checkout; it was preserved.')
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        metadata.unlink()
    else:
        raise SystemExit('Recorded process is still present; run scripts/stop.sh first.')
address = os.environ.get('BLOG_ADDR', '127.0.0.1:8888')
host, port = address.rsplit(':', 1); host = host.strip('[]'); port = int(port)
if port < 1 or port > 65535:
    raise SystemExit('start.sh requires a fixed BLOG_ADDR port in 1..65535')
family = socket.AF_INET6 if ':' in host else socket.AF_INET
with socket.socket(family) as probe:
    probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    probe.bind((host or '127.0.0.1', port))
    probe.listen(1)
PY
CGO_ENABLED=1 go build -o "$BUILD" ./cmd/blog
if [[ "$(uname -s)" == "Darwin" ]]; then
  codesign --force --sign - "$BUILD"
  codesign --verify "$BUILD"
fi
mv "$BUILD" "$ROOT/bin/blog-server"
python3 - "$ROOT" <<'PY'
import json, os, pathlib, shutil, subprocess, sys, time, urllib.request
root = pathlib.Path(sys.argv[1]); executable = root / 'bin/blog-server'
pidfile = root / 'bin/blog.pid'; log = root / 'bin/blog.log'
with log.open('ab') as output:
    os.chmod(log, 0o600)
    process = subprocess.Popen([str(executable)], cwd=root, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
try:
    started = subprocess.check_output(['ps', '-p', str(process.pid), '-o', 'lstart='], text=True).strip()
    temporary = root / ('bin/.blog-' + str(process.pid) + '.pid')
    temporary.write_text(json.dumps({'pid': process.pid, 'executable': str(executable), 'started': started}) + '\n')
    os.chmod(temporary, 0o600)
    temporary.replace(pidfile)
except BaseException:
    process.terminate()
    process.wait(timeout=6)
    raise
host, port = os.environ.get('BLOG_ADDR', '127.0.0.1:8888').rsplit(':', 1)
host = host.strip('[]')
if host in ('', '0.0.0.0', '::'): host = 'localhost'
if ':' in host: host = '[' + host + ']'
health = 'http://' + host + ':' + port + '/healthz'
def owns_listener():
    proc = pathlib.Path('/proc') / str(process.pid)
    if (proc / 'fd').is_dir():
        sockets = set()
        for descriptor in (proc / 'fd').iterdir():
            try:
                value = os.readlink(descriptor)
                if value.startswith('socket:['): sockets.add(value[8:-1])
            except FileNotFoundError:
                continue
        for name in ('tcp', 'tcp6'):
            table = proc / 'net' / name
            if not table.exists(): continue
            for line in table.read_text().splitlines()[1:]:
                fields = line.split()
                if fields[3] == '0A' and int(fields[1].rsplit(':', 1)[1], 16) == int(port) and fields[9] in sockets:
                    return True
        return False
    lsof = shutil.which('lsof') or '/usr/sbin/lsof'
    result = subprocess.run([lsof, '-nP', '-a', '-p', str(process.pid), '-iTCP:' + port, '-sTCP:LISTEN', '-Fp'], capture_output=True, text=True)
    return result.returncode == 0 and ('p' + str(process.pid)) in result.stdout.splitlines()
for attempt in range(100):
    if process.poll() is not None:
        pidfile.unlink(missing_ok=True)
        raise SystemExit('Backend exited during startup; inspect ' + str(log))
    try:
        with urllib.request.urlopen(health, timeout=0.3) as response:
            ready = response.status == 200 and json.load(response).get('status') == 'ok'
        if ready and owns_listener():
            if process.poll() is None:
                print('Backend PID:', process.pid)
                print('URL:', os.environ.get('BLOG_PUBLIC_URL', 'http://localhost:8888'))
                print('Log:', log)
                print('Initial account file:', pathlib.Path(os.environ.get('BLOG_DATA_DIR', str(root / 'data'))) / 'initial-admin.txt')
                break
    except (OSError, ValueError):
        pass
    time.sleep(0.1)
else:
    process.terminate()
    try:
        process.wait(timeout=6)
        pidfile.unlink(missing_ok=True)
    except subprocess.TimeoutExpired:
        pass
    raise SystemExit('Backend did not become healthy; inspect ' + str(log))
PY
