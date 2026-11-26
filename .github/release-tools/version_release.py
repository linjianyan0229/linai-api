#!/usr/bin/env python3
"""Publish only an explicit VERSION change; never move an existing release tag."""
import os
from pathlib import Path
import re
import subprocess


def version(value):
    value = value.strip()
    if not re.fullmatch(r'(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)', value):
        raise ValueError('VERSION must contain a stable major.minor.patch version')
    return tuple(map(int, value.split('.')))


def main():
    current = Path('backend/cmd/server/VERSION').read_text().strip()
    version(current)
    sha = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    tag = 'v' + current
    if os.environ.get('DRY_RUN') == 'true':
        ref = sha
    else:
        # An existing tag may only be reused for exactly the same source commit.
        existing = subprocess.run(['git', 'rev-parse', '--verify', f'refs/tags/{tag}^{{commit}}'], text=True, capture_output=True)
        if existing.returncode == 0:
            if existing.stdout.strip() != sha:
                raise ValueError(f'{tag} already belongs to another commit; increase VERSION')
        else:
            published = subprocess.check_output(['gh', 'api', f'repos/{os.environ["GITHUB_REPOSITORY"]}/releases', '--jq', '[.[] | select(.draft == false and .prerelease == false) | .tag_name] | .[]'], text=True)
            for previous in published.splitlines():
                try:
                    old = version(previous.removeprefix('v'))
                except ValueError:
                    continue
                if old >= version(current):
                    raise ValueError('VERSION must be newer than every published stable release')
            notes_file = Path('.github/release-notes') / f'{tag}.md'
            if not notes_file.is_file():
                raise ValueError(f'missing release notes: {notes_file.as_posix()}')
            notes = notes_file.read_text(encoding='utf-8').strip()
            if not notes:
                raise ValueError(f'release notes are empty: {notes_file.as_posix()}')
            if notes.splitlines()[0] != '## 更新日志':
                raise ValueError(f'release notes must start with "## 更新日志": {notes_file.as_posix()}')
            if not '\n'.join(notes.splitlines()[1:]).strip():
                raise ValueError(f'release notes have no content: {notes_file.as_posix()}')
            message = f'LinAI API {tag}\n\n{notes}'
            subprocess.run(['git', '-c', 'user.name=github-actions[bot]', '-c', 'user.email=41898282+github-actions[bot]@users.noreply.github.com', 'tag', '-a', tag, '-m', message], check=True)
            subprocess.run(['git', 'push', 'origin', f'refs/tags/{tag}'], check=True)
        ref = tag
    with Path(os.environ['GITHUB_OUTPUT']).open('a') as output:
        output.write(f'ref={ref}\n')


if __name__ == '__main__':
    main()
