"""Create a portable source handoff with public dependencies declared normally."""

from __future__ import annotations

import argparse
import gzip
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile

ROOT_FILES = {"README.md", "AGENTS.md", "Makefile", ".gitignore", "go.work", "go.work.sum"}
SOURCE_DIRS = {"clients", "docs", "extensions", "gateway", "sdk", "spec", "sqlglot", "tests", "tools"}
EXCLUDED = {".git", ".bin", ".venv", ".pytest_cache", "__pycache__", "node_modules", "dist"}


def source_files(root: Path) -> list[Path]:
    if (root / ".git").exists():
        result = subprocess.run(
            ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
            cwd=root, check=True, capture_output=True,
        )
        candidates = {Path(p.decode()) for p in result.stdout.split(b"\0") if p}
    else:
        candidates = {p.relative_to(root) for p in root.rglob("*") if p.is_file() or p.is_symlink()}
    selected = []
    for relative in sorted(candidates):
        if relative.is_absolute() or ".." in relative.parts:
            raise ValueError(f"unsafe source path: {relative}")
        if relative.parts[0] not in SOURCE_DIRS and str(relative) not in ROOT_FILES:
            continue
        if any(part in EXCLUDED for part in relative.parts):
            continue
        if any(part.startswith(".") for part in relative.parts[:-1]):
            continue
        if relative.name.startswith(".") and relative.name != ".gitignore":
            continue
        if relative.suffix in {".pyc", ".pyo", ".test", ".pem", ".key"}:
            continue
        if relative.parts[:4] == ("clients", "python", "fiatlas", "bin"):
            continue
        if relative == Path("clients/cli/atlas"):
            continue
        path = root / relative
        if path.is_symlink():
            raise ValueError(f"source symlink is not portable: {relative}")
        if path.is_file():
            selected.append(relative)
    for required in ("README.md", "AGENTS.md", "go.work", "Makefile"):
        if Path(required) not in selected:
            raise ValueError(f"missing handoff entry point: {required}")
    return selected


def package(root: Path, output: Path) -> tuple[Path, int]:
    files = source_files(root)
    output.mkdir(parents=True, exist_ok=True)
    archive = output / "atlas-handoff.tar.gz"
    entries = []
    with archive.open("wb") as raw:
        with gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as tar:
                for relative in files:
                    path = root / relative
                    data = path.read_bytes()
                    mode = 0o755 if path.stat().st_mode & 0o111 else 0o644
                    info = tarfile.TarInfo("atlas/" + relative.as_posix())
                    info.size, info.mode = len(data), mode
                    tar.addfile(info, io.BytesIO(data))
                    entries.append({"path": relative.as_posix(), "size": len(data), "sha256": hashlib.sha256(data).hexdigest()})
                manifest = json.dumps({
                    "format": 1,
                    "contents": "Project source, documentation, fixtures and licenses",
                    "dependencies": "Public third-party dependencies are resolved from the included manifests",
                    "files": entries,
                }, indent=2, ensure_ascii=False).encode() + b"\n"
                info = tarfile.TarInfo("atlas/HANDOFF-MANIFEST.json")
                info.size, info.mode = len(manifest), 0o644
                tar.addfile(info, io.BytesIO(manifest))
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    (output / "atlas-handoff.tar.gz.sha256").write_text(f"{digest}  {archive.name}\n")
    return archive, len(files)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    archive, count = package(root, args.output or root / "dist")
    print(f"Packaged {count} source files: {archive}")
    print(f"Checksum: {archive.name}.sha256; per-file manifest is inside the archive")


if __name__ == "__main__":
    main()
