"""Exercise the host rollout without AWS or Docker."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("remote-deploy.sh").resolve()
REPO = "ghcr.io/mynameis-nigel/ssh-bulletin"
OLD = f"{REPO}@sha256:{'a' * 64}"
NEW = f"{REPO}@sha256:{'b' * 64}"

DOCKER = """#!/bin/sh
set -eu
echo "$*" >> calls
case "$*" in
  'compose config --services') printf '%s\\n' router "$SERVICE" ;;
  'compose config --images bulletin')
    if [ "$USE_IMAGE_VAR" = yes ]; then sed -n 's/^BULLETIN_IMAGE=//p' .env
    else echo "$OLD"; fi ;;
  'compose ps --status running --quiet bulletin')
    if [ "$RUNNING" = yes ]; then echo bulletin-container; fi ;;
  'inspect --format {{.Config.Image}} bulletin-container') echo "$OLD" ;;
  'compose up -d --no-deps --pull never bulletin')
    sed -n 's/^BULLETIN_IMAGE=//p' .env > running-image ;;
  'compose stop bulletin') rm -f running-image ;;
  'compose exec -T bulletin '*)
    if [ "$(cat running-image)" = "$OLD" ]; then printf 'SSH-2.0-1.0.0\\r\\n'
    else printf 'SSH-2.0-%s\\r\\n' "$NEW_BANNER"; fi ;;
  'pull --quiet '*) [ "$PULL" = yes ] ;;
  *) exit 99 ;;
esac
"""


class DeployTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        fake_docker = self.root / "docker"
        fake_docker.write_text(DOCKER)
        fake_docker.chmod(0o755)
        fake_sleep = self.root / "sleep"
        fake_sleep.write_text("#!/bin/sh\nexit 0\n")
        fake_sleep.chmod(0o755)
        self.dotenv = self.root / ".env"
        self.original = f"ROUTER_IMAGE=router:stable\nBULLETIN_IMAGE={OLD}\n"
        self.dotenv.write_text(self.original)
        self.running = self.root / "running-image"
        self.running.write_text(OLD + "\n")

    def deploy(self, **override):
        env = dict(os.environ, PATH=f"{self.root}:{os.environ['PATH']}",
                   COMPOSE_DIR=str(self.root), IMAGE=NEW, VERSION="1.1.0",
                   PROBE_ATTEMPTS="1", SERVICE="bulletin", USE_IMAGE_VAR="yes",
                   RUNNING="yes", PULL="yes", OLD=OLD, NEW_BANNER="1.1.0")
        env.update(override)
        return subprocess.run(["sh", SCRIPT], env=env, capture_output=True,
                              text=True, timeout=10)

    def test_rolls_out_only_bulletin_by_digest(self):
        result = self.deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.dotenv.read_text(), self.original.replace(OLD, NEW))
        self.assertEqual(self.running.read_text().strip(), NEW)

    def test_bad_candidate_banner_restores_previous_build(self):
        result = self.deploy(NEW_BANNER="0.0.0")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Previous Bulletin build restored.", result.stdout)
        self.assertEqual(self.dotenv.read_text(), self.original)
        self.assertEqual(self.running.read_text().strip(), OLD)

    def test_failed_first_rollout_stops_candidate(self):
        self.running.unlink()
        self.dotenv.unlink()
        result = self.deploy(RUNNING="no", NEW_BANNER="0.0.0")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.running.exists())
        self.assertFalse(self.dotenv.exists())

    def test_missing_service_pull_failure_and_wrong_image_mapping_leave_fleet_intact(self):
        for overrides in [{"SERVICE": "farm"}, {"PULL": "no"}, {"USE_IMAGE_VAR": "no"}]:
            with self.subTest(overrides=overrides):
                calls = self.root / "calls"
                calls.unlink(missing_ok=True)
                result = self.deploy(**overrides)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.dotenv.read_text(), self.original)
                self.assertEqual(self.running.read_text().strip(), OLD)
                self.assertNotIn("compose up", calls.read_text())


if __name__ == "__main__":
    unittest.main()
