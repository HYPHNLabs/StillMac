#!/usr/bin/env python3
"""Deterministic scan for accidental private/publicly unsafe residue."""
import pathlib, re, subprocess, unittest
ROOT = pathlib.Path(__file__).resolve().parents[1]
ALLOW = {"tests/test_public_residue.py", "tests/test_distribution.py"}
PATTERNS = {
    "workspace": re.compile(r"/Users/|\$GITHUB_WORKSPACE"),
    "private PRD": re.compile(r"(?:HYPHN-Labs-Open-Source-Programme-PRD|PRD-Accelerated-Beta|PRD-Mole)"),
    "telegram route": re.compile(r"telegram(?:/|:|\.)[A-Za-z0-9_/-]+", re.I),
    "credential assignment": re.compile(r"\b(?:API_KEY|ACCESS_TOKEN|AUTH_TOKEN|PASSWORD|SECRET)\s*[:=]\s*['\"][^'\"\r\n]{6,}['\"]"),
}
class PublicResidueTests(unittest.TestCase):
    def test_tracked_text_and_binaries_are_public_safe(self):
        probe = subprocess.run(["git", "ls-files"], cwd=ROOT, text=True, capture_output=True)
        if probe.returncode == 0:
            tracked = probe.stdout.splitlines()
        else:
            tracked = [
                str(path.relative_to(ROOT))
                for path in ROOT.rglob("*")
                if path.is_file() and ".git" not in path.parts
            ]
        for name in tracked:
            if name in ALLOW or name == ".gitignore" or name.endswith("_test.go"): continue
            p = ROOT / name
            if not p.is_file(): continue
            data = p.read_bytes()
            text = data.decode("utf-8", "ignore")
            for label, pattern in PATTERNS.items():
                # CI must inspect the runner workspace; this literal is the
                # documented synthetic scanner target, not leaked host state.
                if name == ".github/workflows/ci.yml" and label == "workspace":
                    text = text.replace("$GITHUB_WORKSPACE", "")
                if name == "scripts/package.sh" and label == "workspace":
                    text = text.replace("/Users/*/Library", "")
                self.assertIsNone(pattern.search(text), f"{label} residue in {name}")
if __name__ == "__main__": unittest.main(verbosity=2)
