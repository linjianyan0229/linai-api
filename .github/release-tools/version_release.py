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
            candidates = []
            for previous in published.splitlines():
                try:
                    candidates.append((version(previous.removeprefix('v')), previous))
                except ValueError:
                    continue
            history = f'{max(candidates)[1]}..HEAD' if candidates else 'HEAD'
            notes = subprocess.check_output(['git', 'log', '--no-merges', '--max-count=30', '--format=- %s (%h)', history], text=True).strip()
            message = f'LinAI API {tag}\n\n## 更新内容\n{notes}'
            subprocess.run(['git', '-c', 'user.name=github-actions[bot]', '-c', 'user.email=41898282+github-actions[bot]@users.noreply.github.com', 'tag', '-a', tag, '-m', message], check=True)
            subprocess.run(['git', 'push', 'origin', f'refs/tags/{tag}'], check=True)
        ref = tag
    with Path(os.environ['GITHUB_OUTPUT']).open('a') as output:
        output.write(f'ref={ref}\n')


if __name__ == '__main__':
    main()
