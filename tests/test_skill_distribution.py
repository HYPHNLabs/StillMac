#!/usr/bin/env python3
"""Behavioural checks for the local StillMac Agent Skill preparation route."""

from __future__ import annotations

import json
import os
import pathlib
import re
import shutil
import subprocess
import tempfile
import time
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "install-skill.sh"
SKILL = ROOT / "skills" / "stillmac" / "SKILL.md"
VERSION = "v0.1.1"


def run_installer(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["sh", str(SCRIPT), *args],
        cwd=ROOT,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )


def write_fixture_skill(root: pathlib.Path, *, release: str = VERSION) -> pathlib.Path:
    skill_dir = root / "checkout" / "skills" / "stillmac"
    skill_dir.mkdir(parents=True)
    (skill_dir / "SKILL.md").write_text(
        "---\n"
        "name: stillmac\n"
        "description: Use StillMac for a local deterministic diagnostic workflow.\n"
        "metadata:\n"
        f'  cli-release: "{release}"\n'
        "---\n"
        "Run the installed StillMac binary and require approval before apply.\n",
        encoding="utf-8",
    )
    scripts = skill_dir / "scripts"
    scripts.mkdir()
    helper = scripts / "fixture-helper.sh"
    helper.write_text("#!/bin/sh\nprintf '%s\\n' fixture\n", encoding="utf-8")
    helper.chmod(0o755)
    return skill_dir


def mode(path: pathlib.Path) -> int:
    return path.stat().st_mode & 0o777


def assert_skill_frontmatter(test_case: unittest.TestCase, path: pathlib.Path) -> None:
    text = path.read_text(encoding="utf-8")
    test_case.assertTrue(text.startswith("---\n"))
    parts = text.split("\n---\n", 1)
    test_case.assertEqual(len(parts), 2)
    frontmatter = parts[0]
    name_lines = [line for line in frontmatter.splitlines() if line.startswith("name:")]
    description_lines = [
        line for line in frontmatter.splitlines() if line.startswith("description:")
    ]
    test_case.assertEqual(name_lines, ["name: stillmac"])
    test_case.assertEqual(len(description_lines), 1)
    test_case.assertRegex(description_lines[0], r"^description: .+$")


class SkillDistributionTests(unittest.TestCase):
    def test_requires_explicit_absolute_source_and_target(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            temp = pathlib.Path(raw)
            source = write_fixture_skill(temp)
            missing_target = temp / "agents" / "skills" / "stillmac"

            result = run_installer(VERSION, str(source))
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("VERSION SOURCE TARGET", result.stderr)

            relative_target = pathlib.Path("relative-target")
            result = run_installer(VERSION, str(source), str(relative_target))
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("absolute", result.stderr.lower())
            self.assertFalse(missing_target.exists())

    def test_installs_a_versioned_skill_with_restrictive_modes(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            temp = pathlib.Path(raw)
            source = write_fixture_skill(temp)
            target_parent = temp / "agents" / "skills"
            target_parent.mkdir(parents=True)
            target_parent.chmod(0o700)
            target = target_parent / "stillmac"

            result = run_installer(VERSION, str(source), str(target))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue(target.is_dir())
            self.assertEqual(
                (target / "SKILL.md").read_text(encoding="utf-8"),
                (source / "SKILL.md").read_text(encoding="utf-8"),
            )
            self.assertEqual(
                (target / "scripts" / "fixture-helper.sh").read_text(encoding="utf-8"),
                (source / "scripts" / "fixture-helper.sh").read_text(encoding="utf-8"),
            )
            self.assertEqual(mode(target), 0o700)
            self.assertEqual(mode(target / "scripts"), 0o700)
            self.assertEqual(mode(target / "SKILL.md"), 0o600)
            self.assertEqual(mode(target / "scripts" / "fixture-helper.sh"), 0o700)
            assert_skill_frontmatter(self, target / "SKILL.md")
            validator = shutil.which("skills-ref")
            if validator:
                validation = subprocess.run(
                    [validator, "validate", str(target)],
                    text=True,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                    check=False,
                )
                self.assertEqual(validation.returncode, 0, validation.stdout + validation.stderr)

            receipt = target / ".stillmac-install.json"
            self.assertEqual(mode(receipt), 0o600)
            metadata = json.loads(receipt.read_text(encoding="utf-8"))
            self.assertEqual(metadata["schema_version"], "stillmac.skill-install.v1")
            self.assertEqual(metadata["skill_name"], "stillmac")
            self.assertEqual(metadata["release_version"], VERSION)
            self.assertRegex(metadata["payload_sha256"], r"^[0-9a-f]{64}$")
            self.assertNotIn(str(temp), receipt.read_text(encoding="utf-8"))

    def test_refuses_to_overwrite_existing_target_and_cleans_stage(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            temp = pathlib.Path(raw)
            source = write_fixture_skill(temp)
            target_parent = temp / "agents" / "skills"
            target_parent.mkdir(parents=True)
            target_parent.chmod(0o700)
            target = target_parent / "stillmac"
            target.mkdir()
            sentinel = target / "user-owned.txt"
            sentinel.write_text("keep me", encoding="utf-8")

            result = run_installer(VERSION, str(source), str(target))
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("exists", result.stderr.lower())
            self.assertEqual(sentinel.read_text(encoding="utf-8"), "keep me")
            self.assertFalse((target / ".stillmac-install.json").exists())
            self.assertEqual(list(target_parent.glob(".stillmac-skill.*")), [])

    def test_late_target_is_not_nested_or_mutated(self) -> None:
        """A destination appearing after preflight must win without mutation."""

        with tempfile.TemporaryDirectory() as raw:
            temp = pathlib.Path(raw)
            source = write_fixture_skill(temp)
            payload_dir = source / "payload"
            payload_dir.mkdir()
            for index in range(32):
                (payload_dir / f"fixture-{index:04d}.txt").write_text("fixture\n", encoding="utf-8")

            target_parent = temp / "agents" / "skills"
            target_parent.mkdir(parents=True)
            target_parent.chmod(0o700)
            target = target_parent / "stillmac"
            command = ["sh", str(SCRIPT), VERSION, str(source), str(target)]
            process = subprocess.Popen(
                command,
                cwd=ROOT,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
            )

            stage_seen = False
            deadline = time.monotonic() + 8
            while time.monotonic() < deadline:
                stages = list(target_parent.glob(".stillmac-skill.*"))
                if stages:
                    stage_seen = True
                    target.mkdir(mode=0o700)
                    (target / "owner-sentinel.txt").write_text("keep me", encoding="utf-8")
                    break
                if process.poll() is not None:
                    break
                time.sleep(0.001)

            if not stage_seen:
                process.kill()
                process.communicate(timeout=5)
                self.fail("race fixture did not observe the private staging directory")

            try:
                stdout, stderr = process.communicate(timeout=8)
            except subprocess.TimeoutExpired:
                process.kill()
                stdout, stderr = process.communicate()
                self.fail("race fixture process did not finish: " + stdout + stderr)
            self.assertNotEqual(process.returncode, 0, stdout + stderr)
            self.assertEqual((target / "owner-sentinel.txt").read_text(encoding="utf-8"), "keep me")
            self.assertEqual(sorted(path.name for path in target.iterdir()), ["owner-sentinel.txt"])
            self.assertEqual(list(target_parent.glob(".stillmac-skill.*")), [])

    def test_rejects_mismatched_version_invalid_skill_and_symlinked_source(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            temp = pathlib.Path(raw)
            target_parent = temp / "targets"
            target_parent.mkdir()
            target_parent.chmod(0o700)

            mismatched = write_fixture_skill(temp / "mismatch", release="v9.9.9")
            result = run_installer(VERSION, str(mismatched), str(target_parent / "stillmac"))
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("release", result.stderr.lower())

            invalid_root = temp / "invalid" / "skills" / "stillmac"
            invalid_root.mkdir(parents=True)
            (invalid_root / "SKILL.md").write_text(
                "---\nname: StillMac\ndescription: bad\n---\n", encoding="utf-8"
            )
            result = run_installer(VERSION, str(invalid_root), str(target_parent / "invalid"))
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("name", result.stderr.lower())

            symlink_root = temp / "symlink"
            symlink_root.symlink_to(mismatched, target_is_directory=True)
            result = run_installer(VERSION, str(symlink_root), str(target_parent / "linked"))
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("symlink", result.stderr.lower())

    def test_rejects_control_character_filenames_before_digest(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            temp = pathlib.Path(raw)
            for index, bad_name in enumerate(("bad\tname.txt", "bad\nname.txt")):
                source = write_fixture_skill(temp / f"source-{index}")
                (source / bad_name).write_text("fixture\n", encoding="utf-8")
                target_parent = temp / f"target-{index}"
                target_parent.mkdir()
                target_parent.chmod(0o700)
                target = target_parent / "stillmac"

                result = run_installer(VERSION, str(source), str(target))
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("control", result.stderr.lower())
                self.assertFalse(target.exists())
                self.assertEqual(list(target_parent.glob(".stillmac-skill.*")), [])

    def test_same_versioned_source_produces_identical_receipts(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            temp = pathlib.Path(raw)
            source = write_fixture_skill(temp)
            first_parent = temp / "one"
            second_parent = temp / "two"
            first_parent.mkdir()
            second_parent.mkdir()
            first_parent.chmod(0o700)
            second_parent.chmod(0o700)

            first = first_parent / "stillmac"
            second = second_parent / "stillmac"
            self.assertEqual(run_installer(VERSION, str(source), str(first)).returncode, 0)
            self.assertEqual(run_installer(VERSION, str(source), str(second)).returncode, 0)
            first_receipt = (first / ".stillmac-install.json").read_text(encoding="utf-8")
            second_receipt = (second / ".stillmac-install.json").read_text(encoding="utf-8")
            self.assertEqual(first_receipt, second_receipt)

    def test_current_skill_has_version_boundary_and_valid_frontmatter(self) -> None:
        text = SKILL.read_text(encoding="utf-8")
        assert_skill_frontmatter(self, SKILL)
        frontmatter = text.split("\n---\n", 1)[0]
        self.assertRegex(frontmatter, r"(?m)^name: stillmac$")
        self.assertRegex(frontmatter, r"(?m)^description: .+$")
        self.assertRegex(frontmatter, r'(?m)^  cli-release: "v0\.1\.1"$')
        self.assertIn("v0.1.1", text)
        self.assertIn("inspect", text)
        self.assertIn("snapshot", text)
        self.assertIn("changes", text)
        self.assertIn("session-report", text)
        self.assertIn("retire", text)
        self.assertIn("They are not part of the", text)
        self.assertIn("released v0.1.1 CLI", text)

    def test_source_commands_require_exact_capability_negotiation(self) -> None:
        text = SKILL.read_text(encoding="utf-8")
        for fragment in (
            "stillmac capabilities --format json",
            "stillmac.capabilities.v1",
            "m1-m2-source",
            "explicit",
            "`commands` array",
            "human-selected scope",
            "user-managed",
            "session has ended",
            "retire plan --target PATH --user-managed --session-ended",
            "retire apply ID",
            "Codex compatibility remains unverified",
        ):
            self.assertIn(fragment, text)

        def negotiated_mode(response: dict[str, object]) -> str:
            if response.get("schema_version") != "stillmac.capabilities.v1":
                return "v0.1.1"
            if response.get("profile") != "m1-m2-source":
                return "v0.1.1"
            commands = response.get("commands")
            if not isinstance(commands, list) or "retire" not in commands:
                return "v0.1.1"
            return "source"

        source_response = {
            "schema_version": "stillmac.capabilities.v1",
            "profile": "m1-m2-source",
            "released": False,
            "commands": ["inspect", "retire"],
        }
        self.assertEqual(negotiated_mode(source_response), "source")
        self.assertEqual(
            negotiated_mode({"schema_version": "stillmac.capabilities.v1", "profile": "other", "commands": ["retire"]}),
            "v0.1.1",
        )
        self.assertEqual(negotiated_mode({"error": "unknown command"}), "v0.1.1")

        def source_retire(approved: bool, attested: bool) -> list[str]:
            calls = [
                "capabilities",
                "inspect",
                "retire plan --target fixture-target --user-managed --session-ended",
            ]
            if approved and attested:
                calls.append("retire apply fixture-plan-001")
            return calls

        self.assertNotIn("retire apply fixture-plan-001", source_retire(True, False))
        self.assertNotIn("retire apply fixture-plan-001", source_retire(False, True))
        self.assertIn("retire apply fixture-plan-001", source_retire(True, True))

    def test_workflow_and_homebrew_docs_keep_evidence_boundaries(self) -> None:
        workflow = (ROOT / "docs" / "AGENT-WORKFLOW.md").read_text(encoding="utf-8")
        homebrew = (ROOT / "docs" / "HOMEBREW-FEASIBILITY.md").read_text(encoding="utf-8")
        for fragment in (
            "install-skill.sh VERSION SOURCE TARGET",
            "stillmac.skill-install.v1",
            "stillmac.capabilities.v1",
            "retire plan --target PATH --user-managed --session-ended",
            "redacted feedback",
            "live end-to-end Codex activation evidence",
        ):
            self.assertIn(fragment, workflow)
        for fragment in (
            "inventory-only",
            "`REVIEW`",
            "https://docs.brew.sh/Manpage.html",
            "Homebrew Cleanup Ruby API reference",
            "No pilot result is claimed here",
            "public command cannot currently satisfy",
        ):
            self.assertIn(fragment, homebrew)

    def test_deterministic_approval_harness_never_applies_without_approval(self) -> None:
        """Use synthetic IDs only to test the skill's command and approval shape."""

        text = SKILL.read_text(encoding="utf-8")
        for fragment in (
            "stillmac scan --format json",
            "stable IDs",
            "stillmac plan",
            "approve that exact plan",
            "stillmac apply PLAN_ID",
            "Do not choose silently",
        ):
            self.assertIn(fragment, text)

        def simulate(approved: bool) -> list[tuple[str, ...]]:
            calls: list[tuple[str, ...]] = [("scan", "--format", "json")]
            fixture_id = "fixture-go-build-cache"
            calls.append(("plan", fixture_id, "--format", "json"))
            if approved:
                fixture_plan_id = "fixture-plan-001"
                calls.append(("apply", fixture_plan_id, "--format", "json"))
            return calls

        self.assertEqual(
            simulate(False),
            [("scan", "--format", "json"), ("plan", "fixture-go-build-cache", "--format", "json")],
        )
        self.assertEqual(simulate(True)[-1][0], "apply")
        self.assertNotIn("apply", [call[0] for call in simulate(False)])

    def test_installer_has_no_network_or_privilege_escalation_route(self) -> None:
        if not SCRIPT.exists():
            self.fail("install-skill.sh has not been implemented")
        text = SCRIPT.read_text(encoding="utf-8")
        for forbidden in ("curl", "wget", "npx", "sudo", "ssh"):
            self.assertNotIn(forbidden, text.lower())


if __name__ == "__main__":
    unittest.main()
