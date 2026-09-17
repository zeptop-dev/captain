#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Live regression against a running Captain + its nodes with a real client.

What it checks, in order (any failure exits 1 with a summary):
  1. every paired node is online and its last doctor report has no failed
     or "not applied" check;
  2. the test user's subscription renders for mihomo and every proxy in it
     answers a delay test through a headless mihomo (real handshakes);
  3. a download through one proxy is charged to the user within the
     report window (accounting end to end);
  4. with a temporary speed limit on the user, the same proxy still
     connects and the download is shaped (the marking outbound and the
     kernel shaper; a limited user who cannot connect at all is the
     regression this catches).

Needs only python3 and a mihomo binary (Clash Verge ships one). No
credentials are read from the repo: everything comes from the
environment, see scripts/e2e/README.md.
"""
import json
import os
import re
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

CAPTAIN_URL = os.environ.get("CAPTAIN_URL", "").rstrip("/")
TOKEN = os.environ.get("CAPTAIN_API_TOKEN", "")
USER_ID = os.environ.get("E2E_USER_ID", "")
MIHOMO = os.environ.get("MIHOMO_BIN", shutil.which("mihomo") or shutil.which("verge-mihomo") or "")
DOWNLOAD_BYTES = int(os.environ.get("E2E_DOWNLOAD_BYTES", str(5 * 1024 * 1024)))
DOWNLOAD_URL = os.environ.get("E2E_DOWNLOAD_URL", "https://speed.cloudflare.com/__down?bytes=%d" % DOWNLOAD_BYTES)
PROBE_URL = os.environ.get("E2E_PROBE_URL", "https://www.gstatic.com/generate_204")
SKIP = {s.strip() for s in os.environ.get("E2E_SKIP", "").split(",") if s.strip()}
TRAFFIC_PROXY = os.environ.get("E2E_TRAFFIC_PROXY", "")
HOSTS = dict(h.split("=", 1) for h in os.environ.get("E2E_HOSTS", "").split(",") if "=" in h)
ACCOUNT_WAIT = int(os.environ.get("E2E_ACCOUNT_WAIT", "240"))
DELAY_TIMEOUT_MS = int(os.environ.get("E2E_DELAY_TIMEOUT_MS", "12000"))
LIMIT_MBPS = int(os.environ.get("E2E_LIMIT_MBPS", "2"))
LIMIT_WAIT = int(os.environ.get("E2E_LIMIT_WAIT", "90"))
SKIP_LIMIT = os.environ.get("E2E_SKIP_LIMIT", "") == "1"

failures = []


def fail(msg):
    failures.append(msg)
    print("FAIL  " + msg)


def ok(msg):
    print("ok    " + msg)


def api(method, path, body=None, headers=None, raw=False, timeout=60):
    hdr = {"Authorization": "Bearer " + TOKEN, "Content-Type": "application/json"}
    hdr.update(headers or {})
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(CAPTAIN_URL + path, data=data, method=method, headers=hdr)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            payload = r.read()
            return r.status, payload if raw else json.loads(payload.decode() or "null")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(errors="replace")


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def check_nodes():
    st, nodes = api("GET", "/api/admin/nodes")
    if st != 200:
        fail("GET /api/admin/nodes -> %s %s" % (st, nodes))
        return
    if not nodes:
        fail("no nodes paired")
    for n in nodes:
        name = "%s(%s)" % (n.get("name"), n.get("id"))
        if not n.get("online"):
            fail("node %s is offline (last seen %s)" % (name, n.get("last_seen_at")))
            continue
        st, detail = api("GET", "/api/admin/nodes/%s" % n["id"])
        if st != 200:
            fail("GET node %s -> %s" % (name, st))
            continue
        doctor = (detail.get("status") or {}).get("doctor") or {}
        bad = []
        for c in doctor.get("checks") or []:
            if c.get("status") == "fail" or "not applied" in (c.get("detail") or ""):
                bad.append("%s: %s" % (c.get("id"), c.get("detail", "")))
        if bad:
            fail("node %s doctor: %s" % (name, "; ".join(bad)))
        else:
            ok("node %s online, %s, doctor clean (%d checks)" % (name, n.get("version"), len(doctor.get("checks") or [])))


def user_state():
    st, u = api("GET", "/api/admin/users/%s" % USER_ID)
    if st != 200:
        sys.exit("GET user %s -> %s %s" % (USER_ID, st, u))
    used = sum(s.get("used_bytes", 0) for s in u.get("subscriptions") or [])
    return u.get("sub_url") or "", used


def fetch_sub(url):
    req = urllib.request.Request(url, headers={"User-Agent": "clash-verge/2.0 mihomo e2e"})
    with urllib.request.urlopen(req, timeout=60) as r:
        return r.read().decode("utf-8"), r.headers.get("Subscription-Userinfo", "")


def proxies_block(sub):
    """The proxies: section as text, split per entry, plus the names."""
    if "proxies:" not in sub or "proxy-groups:" not in sub:
        sys.exit("subscription is not a clash document")
    block = sub[sub.index("proxies:"):sub.index("proxy-groups:")].strip("\n")
    parts = re.split(r"\n(?=    - )", block)
    entries = parts[1:]
    names = []
    for e in entries:
        m = re.search(r'^\s+(?:- )?name: "?([^"\n]+)"?\s*$', e, re.M)
        names.append(m.group(1) if m else "?")
    return parts[0], entries, names


def run_mihomo(head, entries, workdir):
    mixed, ctl = free_port(), free_port()
    hosts = "".join("  %s: %s\n" % (h, ip) for h, ip in HOSTS.items())
    cfg = "mixed-port: %d\nexternal-controller: 127.0.0.1:%d\nmode: global\nlog-level: warning\nipv6: false\n" % (mixed, ctl)
    if hosts:
        cfg += "hosts:\n" + hosts
    cfg += "dns:\n  enable: true\n  nameserver: [https://1.1.1.1/dns-query, https://8.8.8.8/dns-query]\n"
    cfg += head + "\n" + "\n".join(entries) + "\nproxy-groups:\n  - name: E2E\n    type: select\n    proxies: [DIRECT]\nrules:\n  - MATCH,E2E\n"
    with open(os.path.join(workdir, "config.yaml"), "w", encoding="utf-8") as f:
        f.write(cfg)
    log = open(os.path.join(workdir, "mihomo.log"), "w")
    proc = subprocess.Popen([MIHOMO, "-d", workdir, "-f", os.path.join(workdir, "config.yaml")], stdout=log, stderr=subprocess.STDOUT)
    for _ in range(40):
        try:
            with urllib.request.urlopen("http://127.0.0.1:%d/version" % ctl, timeout=2) as r:
                json.loads(r.read())
                return proc, mixed, ctl
        except Exception:
            time.sleep(0.25)
    proc.kill()
    sys.exit("mihomo did not start; see %s" % os.path.join(workdir, "mihomo.log"))


def ctl_get(ctl, path, timeout=30):
    with urllib.request.urlopen("http://127.0.0.1:%d%s" % (ctl, path), timeout=timeout) as r:
        return json.loads(r.read())


def delay(ctl, name):
    q = urllib.parse.urlencode({"url": PROBE_URL, "timeout": DELAY_TIMEOUT_MS})
    try:
        return ctl_get(ctl, "/proxies/%s/delay?%s" % (urllib.parse.quote(name), q), timeout=DELAY_TIMEOUT_MS / 1000 + 5).get("delay")
    except urllib.error.HTTPError as e:
        return None
    except Exception:
        return None


def select_proxy(ctl, name):
    req = urllib.request.Request("http://127.0.0.1:%d/proxies/GLOBAL" % ctl, data=json.dumps({"name": name}).encode(), method="PUT")
    urllib.request.urlopen(req, timeout=10).read()


def probe_via(ctl, mixed, name):
    """A real request through the proxy: mihomo's delay endpoint gives up
    on WireGuard before its first handshake completes, a plain GET does not."""
    try:
        select_proxy(ctl, name)
        proxy = urllib.request.ProxyHandler({"http": "http://127.0.0.1:%d" % mixed, "https": "http://127.0.0.1:%d" % mixed})
        t0 = time.time()
        urllib.request.build_opener(proxy).open(urllib.request.Request(PROBE_URL, headers={"User-Agent": "curl/8.0 captain-e2e"}), timeout=25).read()
        return int((time.time() - t0) * 1000)
    except Exception:
        return None


def download(mixed):
    proxy = urllib.request.ProxyHandler({"http": "http://127.0.0.1:%d" % mixed, "https": "http://127.0.0.1:%d" % mixed})
    opener = urllib.request.build_opener(proxy)
    t0 = time.time()
    req = urllib.request.Request(DOWNLOAD_URL, headers={"User-Agent": "curl/8.0 captain-e2e"})
    with opener.open(req, timeout=300) as r:
        n = 0
        while True:
            chunk = r.read(1 << 16)
            if not chunk:
                break
            n += len(chunk)
    return n, time.time() - t0


def check_speed_limit(ctl, mixed, pick):
    """Throttle the user (a temporary limit, the same mechanism the dynamic
    limiter uses), wait for the nodes to apply it, download again: the
    proxy must still work and the rate must be near the limit."""
    print("== speed limit (%d Mbps)" % LIMIT_MBPS)
    st, out = api("PUT", "/api/admin/users/%s/dyn-limit" % USER_ID, {"Mbps": LIMIT_MBPS, "Seconds": 900})
    if st != 200:
        fail("PUT dyn-limit -> %s %s (captain >= 0.57.4)" % (st, out))
        return
    try:
        time.sleep(LIMIT_WAIT)  # nodes pull the state and restart the cores
        select_proxy(ctl, pick)
        n = secs = None
        for attempt in range(4):
            try:
                n, secs = download(mixed)
                break
            except Exception as e:  # a core restarting mid-download
                print("      attempt %d: %s" % (attempt + 1, type(e).__name__))
                time.sleep(15)
        if n is None:
            fail("limited user cannot connect through %s (marking outbound broken?)" % pick)
            return
        mbps = n * 8 / secs / 1e6
        if mbps <= LIMIT_MBPS * 2.5:
            ok("limited download shaped: %.1f Mbps (limit %d)" % (mbps, LIMIT_MBPS))
        else:
            fail("limit not enforced: %.1f Mbps with a %d Mbps limit" % (mbps, LIMIT_MBPS))
    finally:
        api("DELETE", "/api/admin/users/%s/dyn-limit" % USER_ID)


def main():
    for k, v in (("CAPTAIN_URL", CAPTAIN_URL), ("CAPTAIN_API_TOKEN", TOKEN), ("E2E_USER_ID", USER_ID), ("MIHOMO_BIN", MIHOMO)):
        if not v:
            sys.exit("%s is required (see scripts/e2e/README.md)" % k)
    print("== nodes")
    check_nodes()

    print("== subscription")
    sub_url, used_before = user_state()
    if not sub_url:
        sys.exit("user %s has no subscription url" % USER_ID)
    sub, userinfo = fetch_sub(sub_url)
    head, entries, names = proxies_block(sub)
    ok("subscription: %d proxies, Subscription-Userinfo: %s" % (len(names), userinfo or "(missing)"))
    if not userinfo:
        fail("Subscription-Userinfo header missing")
    if not entries:
        fail("subscription has no proxies")
        return

    workdir = tempfile.mkdtemp(prefix="captain-e2e-")
    proc, mixed, ctl = run_mihomo(head, entries, workdir)
    try:
        print("== proxies (mihomo %s)" % ctl_get(ctl, "/version").get("version"))
        good = []
        for name in names:
            if name in SKIP:
                print("skip  %s" % name)
                continue
            d = delay(ctl, name)
            if d is None:
                time.sleep(3)  # WireGuard and QUIC need a second go after the first handshake
                d = delay(ctl, name)
            if d is None:
                d = probe_via(ctl, mixed, name)
            if d is None:
                fail("proxy %s: delay test failed" % name)
            else:
                ok("proxy %s: %d ms" % (name, d))
                good.append(name)
        if not good:
            return
        pick = TRAFFIC_PROXY or next((n for n in good if "wg" not in n.lower() and "wireguard" not in n.lower()), good[0])
        print("== traffic through %s" % pick)
        select_proxy(ctl, pick)
        n, secs = download(mixed)
        if n < DOWNLOAD_BYTES:
            fail("download returned %d of %d bytes" % (n, DOWNLOAD_BYTES))
            return
        ok("downloaded %d bytes in %.1fs (%.0f KB/s)" % (n, secs, n / secs / 1024))
        deadline = time.time() + ACCOUNT_WAIT
        delta = 0
        while time.time() < deadline:
            time.sleep(10)
            _, used_now = user_state()
            delta = used_now - used_before
            if delta >= int(0.9 * n):
                break
        if delta >= int(0.9 * n):
            ok("charged %d bytes to user %s within the report window" % (delta, USER_ID))
        else:
            fail("only %d of %d bytes charged to user %s after %ds" % (delta, n, USER_ID, ACCOUNT_WAIT))
        if not SKIP_LIMIT:
            check_speed_limit(ctl, mixed, pick)
    finally:
        proc.send_signal(signal.SIGTERM)
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
        if not failures:
            shutil.rmtree(workdir, ignore_errors=True)
        else:
            print("mihomo work dir kept: %s" % workdir)


if __name__ == "__main__":
    main()
    print("== %s" % ("PASS" if not failures else "%d FAILURE(S)" % len(failures)))
    for f in failures:
        print("  - " + f)
    sys.exit(1 if failures else 0)
