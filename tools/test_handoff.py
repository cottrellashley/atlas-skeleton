"""Check that handoff exports preserve source without local metadata."""

import hashlib
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

from handoff import package


class HandoffTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "source"
        self.root.mkdir()
        for name in ("README.md", "AGENTS.md", "Makefile", "go.work"):
            (self.root / name).write_text(name + "\n")

    def write(self, name, value="local data"):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(value)

    def test_export_without_git_preserves_docs_and_checksums(self):
        self.write("docs/LESSONS.md", "meaningful lessons\n")
        self.write("tools/spec/.gitignore", ".venv/\n")
        self.write("sdk/go/go.mod", "module atlas.local/sdk/go\n")
        self.write(".git/config", "private metadata")
        # Exercise the fallback independently of Git metadata.
        (self.root / ".git/config").unlink()
        (self.root / ".git").rmdir()
        self.write("clients/python/.venv/private.txt")
        self.write("clients/node/node_modules/private.txt")
        self.write("gateway/.env")
        self.write("gateway/secret.key")
        self.write("clients/python/fiatlas/bin/atlas-engine")
        archive, count = package(self.root, Path(self.temp.name) / "out")
        with tarfile.open(archive) as tar:
            names = tar.getnames()
            self.assertEqual(len(names), count + 1)
            self.assertIn("atlas/docs/LESSONS.md", names)
            self.assertIn("atlas/tools/spec/.gitignore", names)
            self.assertFalse(any("private" in name or ".env" in name or ".key" in name or "atlas-engine" in name for name in names))
            manifest = json.load(tar.extractfile("atlas/HANDOFF-MANIFEST.json"))
            for entry in manifest["files"]:
                data = tar.extractfile("atlas/" + entry["path"]).read()
                self.assertEqual(hashlib.sha256(data).hexdigest(), entry["sha256"])

    def test_git_metadata_ignored_files_and_deleted_paths_are_excluded(self):
        subprocess.run(["git", "init", "-q", str(self.root)], check=True)
        self.write(".gitignore", ".bin/\n")
        self.write("docs/retained.md")
        self.write("docs/deleted.md")
        subprocess.run(["git", "add", "."], cwd=self.root, check=True)
        (self.root / "docs/deleted.md").unlink()
        self.write("docs/new.md")
        self.write(".bin/executable")
        archive, _ = package(self.root, Path(self.temp.name) / "out")
        with tarfile.open(archive) as tar:
            names = tar.getnames()
            self.assertIn("atlas/docs/new.md", names)
            self.assertIn("atlas/docs/retained.md", names)
            self.assertNotIn("atlas/docs/deleted.md", names)
            self.assertFalse(any("/.git/" in name or "/.bin/" in name for name in names))

    def test_repeated_export_is_reproducible(self):
        archive, _ = package(self.root, Path(self.temp.name) / "out")
        first = archive.read_bytes()
        package(self.root, archive.parent)
        self.assertEqual(first, archive.read_bytes())

    def test_source_symlink_is_rejected(self):
        self.write("docs/notes.md")
        (self.root / "docs/external.md").symlink_to(Path(self.temp.name) / "outside")
        with self.assertRaisesRegex(ValueError, "symlink"):
            package(self.root, Path(self.temp.name) / "out")


if __name__ == "__main__":
    unittest.main()
