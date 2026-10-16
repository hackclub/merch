#!/usr/bin/env python3
"""Look up when USPS and UPS delivered US parcels, for shipping estimates.

USPS and UPS block scripted tracking lookups, but not Safari, so this drives
Safari through WebDriver (one-time setup: `sudo safaridriver --enable`, then
run `safaridriver -p 4455` in another terminal). It reads raw zenventory.sql
exports, looks up a random sample of US parcels not yet in the cache, and adds
`tracking,delivered_on` lines to the same cache file `cmd/shiphistory track`
uses. Keep the cache outside the repo: it has tracking numbers.

    scripts/shipping-history/us-tracking.py -cache ~/merch-ship/asendia.csv \
        -since 2026-08-01 -until 2026-10-05 -sample 400 zenventory.csv
"""
import argparse, csv, datetime as dt, json, random, re, sys, time, urllib.request

SD = "http://localhost:4455"


def call(method, path, body=None):
    req = urllib.request.Request(SD + path, method=method, headers={"Content-Type": "application/json"},
                                 data=json.dumps(body).encode() if body is not None else None)
    return json.loads(urllib.request.urlopen(req, timeout=120).read())["value"]


def page_text(sid, url, done):
    call("POST", f"/session/{sid}/url", {"url": url})
    t = ""
    for _ in range(30):
        time.sleep(2)
        t = call("POST", f"/session/{sid}/execute/sync", {"script": "return document.body ? document.body.innerText : ''", "args": []})
        if done(t):
            break
    return t


USPS_DATE = r"([A-Z][a-z]+ \d{1,2}, \d{4})"
UPS_DAY = r"(?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday), ([A-Z][a-z]+ \d{1,2})"


def usps(sid, nums):
    """Delivery dates for up to 35 USPS numbers (one page)."""
    t = page_text(sid, "https://tools.usps.com/tracking/?tLabels=" + ",".join(nums),
                  lambda t: t.count("Tracking Number:") >= len(nums))
    pos = sorted((t.find(n), n) for n in nums if t.find(n) >= 0)
    out = {}
    for i, (p, n) in enumerate(pos):
        block = t[p:pos[i + 1][0] if i + 1 < len(pos) else len(t)]
        m = re.search(r"\nDelivered[^\n]*\n(?:[^\n]*\n){0,3}?" + USPS_DATE, block)
        if m:
            out[n] = dt.datetime.strptime(m[1], "%B %d, %Y").date()
    return out


def ups(sid, n, shipped):
    t = page_text(sid, f"https://www.ups.com/track?loc=en_US&tracknum={n}&requester=ST/trackdetails",
                  lambda t: n in t and re.search(UPS_DAY, t))
    m = re.search(UPS_DAY, t)
    if not re.search(r"\nDelivered", t) or not m:
        return None
    d = dt.datetime.strptime(f"{m[1]} {shipped.year}", "%B %d %Y").date()  # UPS leaves out the year
    return d if d >= shipped else d.replace(year=shipped.year + 1)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-cache", required=True)
    ap.add_argument("-since", default="")
    ap.add_argument("-until", default="9999")
    ap.add_argument("-sample", type=int, default=400)
    ap.add_argument("files", nargs="+")
    a = ap.parse_args()
    try:
        cache = {r[0] for r in csv.reader(open(a.cache)) if r}
    except FileNotFoundError:
        cache = set()
    parcels = []  # (tracking, shipped)
    for f in a.files:
        for r in csv.reader(open(f)):
            if len(r) < 9 or r[0] != "zenventory" or r[1] != "US" or r[2] != "parcel" or r[7]:
                continue
            if r[8] and r[8] not in cache and a.since <= r[6] <= a.until and re.match(r"^(9\d{21}|1Z)", r[8]):
                parcels.append((r[8], dt.date.fromisoformat(r[6])))
    random.seed(1)
    parcels = random.sample(parcels, min(a.sample, len(parcels)))
    print(f"looking up {len(parcels)} US parcels", file=sys.stderr)
    sid = call("POST", "/session", {"capabilities": {"alwaysMatch": {"browserName": "safari"}}})["sessionId"]
    found = 0
    with open(a.cache, "a") as out:
        def keep(n, d):
            nonlocal found
            if d:
                out.write(f"{n},{d.isoformat()}\n")
                out.flush()
                found += 1
        try:
            p_usps = [p for p in parcels if p[0].startswith("9")]
            for i in range(0, len(p_usps), 35):
                got = usps(sid, [n for n, _ in p_usps[i:i + 35]])
                for n, _ in p_usps[i:i + 35]:
                    keep(n, got.get(n))
                time.sleep(4)
            for n, shipped in (p for p in parcels if p[0].startswith("1Z")):
                keep(n, ups(sid, n, shipped))
                time.sleep(2)
        finally:
            call("DELETE", f"/session/{sid}")
    print(f"{found} delivered of {len(parcels)}", file=sys.stderr)


if __name__ == "__main__":
    main()
