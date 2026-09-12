#!/usr/bin/env python3
"""Prepare untouched Vue copies, or serve their production builds locally."""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
OLD_REGISTRIES = {"registry.npm.taobao.org", "registry.npmmirror.com", "registry.nlark.com"}
IGNORED = {"node_modules", "dist", ".git", ".DS_Store", ".env.local", ".env.development.local", ".env.production.local"}


def normalized_lock(value):
    if isinstance(value, dict):
        result = {key: normalized_lock(item) for key, item in value.items()}
        resolved = result.get("resolved")
        if isinstance(resolved, str):
            parsed = urlsplit(resolved)
            if parsed.hostname in OLD_REGISTRIES:
                path = unquote(parsed.path)
                if "/download/" in path:
                    package, archive = path.split("/download/", 1)
                    path = package + "/-/" + archive.rsplit("/", 1)[-1]
                if "/-/" not in path or not path.endswith(".tgz"):
                    raise ValueError("Unsupported historical tarball URL: " + resolved)
                result["resolved"] = "https://registry.npmjs.org" + path
        return result
    if isinstance(value, list):
        return [normalized_lock(item) for item in value]
    return value


def snapshot(directory):
    result = {}
    for current, directories, files in os.walk(directory):
        directories[:] = sorted(name for name in directories if name not in IGNORED)
        for name in directories + sorted(files):
            path = Path(current) / name
            relative = path.relative_to(directory)
            if name in IGNORED or name.startswith(".prepared"):
                continue
            if path.is_symlink():
                raise ValueError("Frontend source contains a symlink: " + str(relative))
            if path.is_file():
                result[str(relative)] = hashlib.sha256(path.read_bytes()).hexdigest()
    return result


def prepare(source, destination):
    if not source.is_dir():
        raise ValueError("Frontend source is missing: " + str(source))
    original = snapshot(source)
    config = (source / "vue.config.js").read_text()
    if not re.search(r"target\s*:\s*['\"]http://localhost:8888['\"]", config) or "^/api" not in config:
        raise ValueError("Expected original /api -> localhost:8888 proxy: " + str(source))
    source_lock = json.loads((source / "package-lock.json").read_text())
    lock = normalized_lock(source_lock)
    expected = dict(original)
    lock_bytes = (json.dumps(lock, ensure_ascii=False, indent=2) + "\n").encode()
    expected["package-lock.json"] = hashlib.sha256(lock_bytes).hexdigest()
    marker = {"source": str(source.resolve()), "original": original, "prepared": expected}
    if destination.exists():
        stored = destination / ".prepared.json"
        if not stored.is_file() or json.loads(stored.read_text()) != marker:
            raise ValueError("Existing copy belongs to a different source; choose another --output: " + str(destination))
        current = snapshot(destination)
        if any(current.get(name) != digest for name, digest in expected.items()):
            raise ValueError("Prepared source was edited; it was preserved: " + str(destination))
        return destination
    destination.parent.mkdir(parents=True, exist_ok=True)
    stage = Path(tempfile.mkdtemp(prefix=".prepare-", dir=destination.parent))
    try:
        shutil.copytree(source, stage, dirs_exist_ok=True, ignore=lambda directory, names: [name for name in names if name in IGNORED])
        (stage / "package-lock.json").write_bytes(lock_bytes)
        (stage / ".prepared.json").write_text(json.dumps(marker, ensure_ascii=False, indent=2) + "\n")
        if snapshot(source) != original:
            raise ValueError("Source changed while copying; retry after editing finishes")
        stage.rename(destination)
    finally:
        if stage.exists():
            shutil.rmtree(stage)
    return destination


def node_env():
    env = os.environ.copy()
    options = env.get("NODE_OPTIONS", "").split()
    if "--openssl-legacy-provider" not in options:
        options.append("--openssl-legacy-provider")
    env["NODE_OPTIONS"] = " ".join(options)
    return env


def install_and_build(directory, install, build):
    before = (directory / "package-lock.json").read_bytes()
    if install:
        subprocess.run(["npm", "ci", "--legacy-peer-deps", "--registry=https://registry.npmjs.org"], cwd=directory, env=node_env(), check=True)
    if build:
        subprocess.run(["npm", "run", "build"], cwd=directory, env=node_env(), check=True)
    if (directory / "package-lock.json").read_bytes() != before:
        raise ValueError("npm changed the prepared lockfile; inspect the copy before continuing")


def serve(directory, port, backend):
    dist = (directory / "dist").resolve()
    if not (dist / "index.html").is_file():
        raise ValueError("Build output is missing: " + str(dist))
    target = urlsplit(backend)
    if target.scheme != "http" or target.hostname not in {"localhost", "127.0.0.1", "::1"} or target.path not in {"", "/"} or target.query or target.fragment or target.username:
        raise ValueError("--backend must be a local HTTP origin")
    hop = {"connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade"}

    class Handler(SimpleHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def __init__(self, *args, **kwargs):
            super().__init__(*args, directory=str(dist), **kwargs)

        def setup(self):
            super().setup()
            self.connection.settimeout(30)

        def request_body(self):
            maximum = 100 << 20
            if self.headers.get("Transfer-Encoding", "").lower() == "chunked":
                body = bytearray()
                while True:
                    line = self.rfile.readline(1024)
                    size = int(line.split(b";", 1)[0].strip(), 16)
                    if size < 0 or len(body) + size > maximum:
                        raise ValueError("Request is too large")
                    if size == 0:
                        while self.rfile.readline(8192).strip():
                            pass
                        return bytes(body)
                    chunk = self.rfile.read(size)
                    if len(chunk) != size or self.rfile.read(2) != b"\r\n":
                        raise ValueError("Invalid chunked request")
                    body.extend(chunk)
            length = int(self.headers.get("Content-Length", "0"))
            if length < 0 or length > maximum:
                raise ValueError("Request is too large")
            body = self.rfile.read(length)
            if len(body) != length:
                raise ValueError("Incomplete request")
            return body

        def proxy(self):
            connection = http.client.HTTPConnection(target.hostname, target.port or 80, timeout=30)
            responded = False
            try:
                body = self.request_body()
                headers = {key: value for key, value in self.headers.items() if key.lower() not in hop | {"host", "content-length"}}
                headers["Host"] = target.netloc
                headers["Content-Length"] = str(len(body))
                # Preserve raw query/escaping and the original Vue ^/api rewrite.
                path = self.path[4:]
                if not path or path.startswith("?"):
                    path = "/" + path
                connection.request(self.command, path, body=body, headers=headers)
                response = connection.getresponse()
                self.send_response(response.status, response.reason)
                for key, value in response.getheaders():
                    if key.lower() not in hop:
                        self.send_header(key, value)
                self.send_header("Connection", "close")
                self.end_headers()
                responded = True
                self.close_connection = True
                if self.command != "HEAD":
                    while True:
                        chunk = response.read(65536)
                        if not chunk:
                            break
                        self.wfile.write(chunk)
            except (ValueError, OSError, http.client.HTTPException) as error:
                self.close_connection = True
                if not responded and not self.wfile.closed:
                    self.send_error(502, "Local backend unavailable or request invalid")
                self.log_error("Proxy failed: %s", type(error).__name__)
            finally:
                connection.close()

        def dispatch(self):
            path = urlsplit(self.path).path
            if path == "/api" or path.startswith("/api/"):
                return self.proxy()
            if self.command not in {"GET", "HEAD"}:
                return self.send_error(405)
            relative = unquote(path).lstrip("/")
            requested = (dist / relative).resolve()
            if requested != dist and dist not in requested.parents:
                return self.send_error(403)
            if not requested.is_file() and not Path(relative).suffix:
                self.path = "/index.html"
            return super().do_HEAD() if self.command == "HEAD" else super().do_GET()

        do_GET = do_HEAD = do_POST = do_PUT = do_PATCH = do_DELETE = do_OPTIONS = dispatch

        def list_directory(self, path):
            self.send_error(404)
            return None

    server = ThreadingHTTPServer(("localhost", port), Handler)
    server.daemon_threads = True
    print(f"Preview: http://localhost:{port}  /api -> {backend}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-root", type=Path, default=ROOT.parent / "blog-vue")
    parser.add_argument("--output", type=Path, default=ROOT / ".local-frontends")
    parser.add_argument("--skip-install", action="store_true")
    parser.add_argument("--build", action="store_true")
    parser.add_argument("--serve", choices=["blog", "admin"], help="serve an existing production build without copying or installing")
    parser.add_argument("--port", type=int, help="override preview port for isolated testing")
    parser.add_argument("--backend", default="http://localhost:8888")
    args = parser.parse_args()
    if args.port is not None and not 1 <= args.port <= 65535:
        parser.error("--port must be in 1..65535")
    try:
        if args.serve:
            serve(args.output.resolve() / args.serve, args.port or (8080 if args.serve == "blog" else 8081), args.backend)
            return 0
        for name in ("blog", "admin"):
            destination = prepare(args.source_root.resolve() / name, args.output.resolve() / name)
            install_and_build(destination, not args.skip_install, args.build)
            print("Prepared " + str(destination))
        print("Preview built copies with --serve blog (8080) and --serve admin (8081).")
        return 0
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print("Preparation failed: " + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
