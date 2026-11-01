"""python -m atlasref write | check

write: regenerates every file under spec/protocol/references/ (except the
hand-written README.md and QUESTIONS.md) and removes files no generator
makes.

check: regenerates everything in memory and fails unless every file on disk
is byte-identical; then validates every schema, profile, document and
vector, and runs the differential checks (see README.md, "What check
proves").
"""

import hashlib
import json
import pathlib
import sys

from . import references_root

HAND_WRITTEN = {"README.md", "QUESTIONS.md"}


def generators():
    from .gen import arrow, docs, fingerprints, grammar, guard, handoff, headers, problems, resolution, scenarios
    from .gen import schemavectors, sf

    return [docs, fingerprints, problems, grammar, schemavectors, headers, sf, arrow, resolution, handoff, guard,
            scenarios]


def generate():
    out = {}
    for g in generators():
        for path, data in g.build().items():
            if path in out:
                raise SystemExit(f"two generators write {path}")
            out[path] = data
    return out


def on_disk(root):
    return {str(p.relative_to(root)): p for p in root.rglob("*") if p.is_file() and p.name not in HAND_WRITTEN
            and ".DS_Store" not in p.name}


def write():
    root = references_root()
    files = generate()
    for rel, data in sorted(files.items()):
        path = root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        if not path.exists() or path.read_bytes() != data:
            path.write_bytes(data)
            print("wrote", rel)
    for rel, path in sorted(on_disk(root).items()):
        if rel not in files:
            path.unlink()
            print("removed", rel)
    for d in sorted((p for p in root.rglob("*") if p.is_dir()), reverse=True):
        if not any(d.iterdir()):
            d.rmdir()
    print(f"{len(files)} files")


def check():
    from . import selfcheck

    root = references_root()
    files = generate()
    failures = []
    disk = on_disk(root)
    for rel, data in sorted(files.items()):
        if rel not in disk:
            failures.append(f"missing: {rel}")
        elif disk[rel].read_bytes() != data:
            failures.append(f"differs from its regeneration: {rel}")
    for rel in sorted(set(disk) - set(files)):
        failures.append(f"no generator makes: {rel}")
    for name in sorted(HAND_WRITTEN):
        if not (root / name).exists():
            failures.append(f"missing: {name}")
    if failures:
        for f in failures:
            print("FAIL", f)
        print("run `python -m atlasref write` to regenerate")
        return 1
    print(f"regenerated {len(files)} files byte-identically "
          f"({hashlib.sha256(b''.join(files[k] for k in sorted(files))).hexdigest()[:16]})")
    problems = selfcheck.run(files)
    for p in problems:
        print("FAIL", p)
    if problems:
        return 1
    print("every check passed")
    return 0


def main(argv):
    if len(argv) != 2 or argv[1] not in ("write", "check"):
        print(__doc__)
        return 2
    if argv[1] == "write":
        write()
        return 0
    return check()


if __name__ == "__main__":
    sys.exit(main(sys.argv))
