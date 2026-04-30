#!/usr/bin/env python3
import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
IPTABLES = ROOT / "module/scripts/iptables.sh"


def fail(message: str) -> None:
    print(f"runtime snapshot contract: {message}", file=sys.stderr)
    raise SystemExit(1)


text = IPTABLES.read_text()

save_match = re.search(
    r"for _name in (?P<body>.*?); do\s*\n\s*write_snapshot_var",
    text,
    re.S,
)
if not save_match:
    fail("save_snapshot variable list not found")

saved = [
    token
    for token in re.sub(r"\\\n", " ", save_match.group("body")).split()
    if re.fullmatch(r"[A-Z0-9_]+", token)
]
if not saved:
    fail("save_snapshot variable list is empty")

load_match = re.search(
    r"case \"\$_name\" in(?P<body>.*?)\n\s+\*\)",
    text,
    re.S,
)
if not load_match:
    fail("load_snapshot key dispatch not found")

loaded = set(re.findall(r"^\s+([A-Z0-9_]+)\)", load_match.group("body"), re.M))
missing = [name for name in saved if name not in loaded]
if missing:
    fail("saved keys are not accepted by load_snapshot: " + ", ".join(missing))

if "SHARING_MODE" not in saved:
    fail("SHARING_MODE is not persisted in save_snapshot")
if "SHARING_MODE" not in loaded:
    fail("SHARING_MODE is not accepted by load_snapshot")
if "SHARING_IFACES" in saved:
    fail("SHARING_IFACES must stay a start-time rendering input, not a runtime snapshot key")

print(f"runtime snapshot contract: {len(saved)} keys ok")
