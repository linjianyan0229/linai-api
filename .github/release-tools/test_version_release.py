import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('version_release', Path(__file__).with_name('version_release.py'))
version_release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(version_release)


class VersionReleaseTest(unittest.TestCase):
    def setUp(self):
        previous = Path.cwd()
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        os.chdir(self.temp.name)
        self.addCleanup(os.chdir, previous)
        Path('backend/cmd/server').mkdir(parents=True)
        Path('backend/cmd/server/VERSION').write_text('0.3.54\n')
        self.environment = patch.dict(os.environ, {'GITHUB_OUTPUT': 'output', 'GITHUB_REPOSITORY': 'ExampleOwner/linai-api', 'DRY_RUN': 'false'})
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def test_dry_run_never_tags_or_pushes(self):
        with patch.dict(os.environ, {'DRY_RUN': 'true'}), patch.object(subprocess, 'check_output', return_value='a' * 40 + '\n'), patch.object(subprocess, 'run') as run:
            version_release.main()
        run.assert_not_called()
        self.assertEqual(Path('output').read_text(), 'ref=' + 'a' * 40 + '\n')

    def test_explicit_version_creates_one_immutable_tag(self):
        with patch.object(subprocess, 'check_output', side_effect=['a' * 40 + '\n', 'v0.3.53\n', '- add optional release updates (abcdef)']), patch.object(subprocess, 'run', side_effect=[subprocess.CompletedProcess([], 1), subprocess.CompletedProcess([], 0), subprocess.CompletedProcess([], 0)]) as run:
            version_release.main()
        self.assertIn('add optional release updates', run.call_args_list[-2].args[0][-1])
        self.assertEqual(run.call_args_list[-1].args[0], ['git', 'push', 'origin', 'refs/tags/v0.3.54'])
        self.assertEqual(Path('output').read_text(), 'ref=v0.3.54\n')

    def test_existing_tag_is_never_moved_to_another_commit(self):
        with patch.object(subprocess, 'check_output', return_value='a' * 40 + '\n'), patch.object(subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, stdout='b' * 40 + '\n')) as run:
            with self.assertRaisesRegex(ValueError, 'another commit'):
                version_release.main()
        self.assertEqual(run.call_count, 1)

    def test_published_versions_cannot_be_reused_or_downgraded(self):
        for previous in ('v0.3.54', 'v0.3.55', 'v1.0.0'):
            with self.subTest(previous=previous), patch.object(subprocess, 'check_output', side_effect=['a' * 40, previous]), patch.object(subprocess, 'run', return_value=subprocess.CompletedProcess([], 1)) as run:
                with self.assertRaisesRegex(ValueError, 'newer'):
                    version_release.main()
                self.assertEqual(run.call_count, 1)

    def test_versions_must_be_complete_stable_numbers(self):
        for value in ('1.2', '01.2.3', '1.2.3-beta', '1.2.3;echo bad'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                version_release.version(value)
