#!/usr/bin/env python3
"""Exercise the actual release binary, its migrations and embedded SPA assets."""
from html.parser import HTMLParser
import json
from pathlib import Path
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.parse
import urllib.request


class Assets(HTMLParser):
    def __init__(self):
        super().__init__()
        self.urls = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "script" and attrs.get("src"):
            self.urls.append(attrs["src"])
        if tag == "link" and attrs.get("rel") == "stylesheet" and attrs.get("href"):
            self.urls.append(attrs["href"])


def smoke(binary, version):
    binary = str(Path(binary).resolve())
    actual = subprocess.check_output([binary, "version"], text=True, timeout=10).strip()
    if actual != "captain " + version:
        raise RuntimeError("release binary version mismatch")
    with tempfile.TemporaryDirectory(prefix="captain-release-smoke-") as directory:
        root = Path(directory)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        origin = f"http://127.0.0.1:{port}"
        config = root / "config.yaml"
        config.write_text(f"listen: 127.0.0.1:{port}\nbase_url: {origin}\n"
                          f"data_dir: {json.dumps(directory)}\nlog_level: error\n"
                          "database:\n  driver: sqlite\n"
                          f"  dsn: {json.dumps(str(root / 'captain.db'))}\n")
        # Run migration explicitly, then ensure serving the same database succeeds.
        subprocess.run([binary, "migrate", "-c", str(config)], check=True, timeout=90)
        # Enable the public probe only in this freshly migrated temporary DB.
        with sqlite3.connect(root / "captain.db") as database:
            database.execute("INSERT INTO settings(key,value_json) VALUES(?,?) "
                             "ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json",
                             ("probe", json.dumps({"enabled": True, "page_enabled": True,
                                                   "path": "/status", "visibility": "public"})))
        with (root / "server.log").open("wb") as log:
            process = subprocess.Popen([binary, "serve", "-c", str(config)], stdout=log, stderr=log)
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            try:
                deadline = time.monotonic() + 90
                while True:
                    if process.poll() is not None:
                        raise RuntimeError("release binary exited during startup")
                    try:
                        with opener.open(origin + "/api/health", timeout=2) as response:
                            if response.status == 200 and json.load(response).get("ok") is True:
                                break
                    except OSError:
                        pass
                    if time.monotonic() >= deadline:
                        raise RuntimeError("release binary health check timed out")
                    time.sleep(0.2)
                # Exercise all four independently built and embedded SPA bundles.
                for route in ("/admin/", "/portal/", "/downloads", "/status/"):
                    with opener.open(origin + route, timeout=5) as response:
                        page = response.read().decode()
                        if response.status != 200 or "text/html" not in response.headers.get("Content-Type", ""):
                            raise RuntimeError("embedded SPA response is not HTML")
                    assets = Assets()
                    assets.feed(page)
                    if not assets.urls:
                        raise RuntimeError("embedded SPA has no production assets")
                    for asset in assets.urls:
                        url = urllib.parse.urljoin(origin + route, asset)
                        if urllib.parse.urlsplit(url).netloc != f"127.0.0.1:{port}":
                            raise RuntimeError("smoke test asset must be local")
                        with opener.open(url, timeout=5) as response:
                            if response.status != 200 or not response.read() or "text/html" in response.headers.get("Content-Type", ""):
                                raise RuntimeError("embedded SPA asset is missing")
                if process.poll() is not None:
                    raise RuntimeError("release binary stopped during smoke test")
            finally:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
        print("Release binary: version, migrations, health and embedded SPA assets passed.")


if __name__ == "__main__":
    smoke(*sys.argv[1:])
