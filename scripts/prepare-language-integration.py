"""Download publisher binaries into an isolated fixture directory for integration tests.

Uses only Python's standard library. No install scripts, PATH or user SDK settings
are changed. The resulting manifest is consumed by TestRealAdditionalLanguages.
"""
import argparse
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import time
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--directory', required=True)
parser.add_argument('--only', default='godot,vyper,arturo,factor,haxe,nim')
parser.add_argument('--use-gh', action='store_true', help='Use an authenticated gh process for public release metadata')
args = parser.parse_args()
directory = Path(args.directory).resolve()
directory.mkdir(parents=True, exist_ok=True)
system = {'Windows': 'windows', 'Linux': 'linux', 'Darwin': 'darwin'}[platform.system()]
arch = 'arm64' if platform.machine().lower() in ('arm64', 'aarch64') else 'amd64'


def request(url):
    headers = {'User-Agent': 'vsm-language-integration'}
    if url.startswith('https://api.github.com/'):
        token = os.environ.get('GH_TOKEN') or os.environ.get('GITHUB_TOKEN')
        if token:
            headers['Authorization'] = 'Bearer ' + token
    return urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=120)


def file_digest(file):
    with file.open('rb') as data:
        digest = hashlib.sha256()
        while block := data.read(1024 * 1024):
            digest.update(block)
        return digest.hexdigest()


def metadata(repo):
    if args.use_gh:
        raw = subprocess.check_output(['gh', 'api', 'repos/' + repo + '/releases/latest'], timeout=60)
    else:
        with request('https://api.github.com/repos/' + repo + '/releases/latest') as response:
            raw = response.read(16 * 1024 * 1024 + 1)
    return json.loads(raw)


def download(name):
    if name == 'nim':
        with request('https://nim-lang.org/channels/stable') as response:
            version = response.read(100).decode().strip()
        if not re.fullmatch(r'\d+\.\d+\.\d+', version):
            raise ValueError('Invalid Nim stable channel')
        suffix = {('windows', 'amd64'): 'windows_x64.zip', ('linux', 'amd64'): 'linux_x64.tar.xz',
                  ('linux', 'arm64'): 'linux_arm64.tar.xz', ('darwin', 'amd64'): 'macosx_x64.tar.xz',
                  ('darwin', 'arm64'): 'macosx_arm64.tar.xz'}.get((system, arch))
        if not suffix:
            return None
        with request('https://nim-lang.org/install.html') as response:
            html = response.read(16 * 1024 * 1024).decode()
        filename = 'nim-' + version + '-' + suffix
        links = re.findall(r'href="(https://[^"<>\s]+)"', html)
        candidates = [url for url in links if url.endswith('/' + filename) and
                      (url.startswith('https://nim-lang.org/download/') or
                       url.startswith('https://github.com/nim-lang/nightlies/releases/download/'))]
        if not candidates:
            raise ValueError('No stable Nim binary for this platform')
        url = candidates[0]
        with request(url + '.sha256') as response:
            checksum = response.read(1024).decode().split()[0]
    else:
        repos = {'godot': 'godotengine/godot', 'vyper': 'vyperlang/vyper', 'arturo': 'arturo-lang/arturo',
                 'factor': 'factor/factor', 'haxe': 'HaxeFoundation/haxe'}
        if name == 'vyper' and arch != 'amd64' or name == 'factor' and (arch != 'amd64' or system == 'darwin'):
            return None
        if name == 'haxe' and (system == 'darwin' and arch == 'arm64' or system == 'windows' and arch == 'arm64'):
            return None
        release = metadata(repos[name])
        tag = release['tag_name']
        if release.get('draft') or release.get('prerelease'):
            raise ValueError('Expected stable publisher release')
        version = tag.removeprefix('v')
        if name == 'godot':
            version = tag.removesuffix('-stable')
            suffix = {('windows', 'amd64'): 'win64', ('windows', 'arm64'): 'windows_arm64',
                      ('linux', 'amd64'): 'linux.x86_64', ('linux', 'arm64'): 'linux.arm64',
                      ('darwin', 'amd64'): 'macos.universal', ('darwin', 'arm64'): 'macos.universal'}[(system, arch)]
            filename = 'Godot_v' + tag + '_' + suffix + '.zip'
        elif name == 'vyper':
            suffix = {'windows': 'windows.exe', 'linux': 'linux', 'darwin': 'darwin'}[system]
            filenames = [a['name'] for a in release['assets'] if re.fullmatch(
                r'vyper\.' + re.escape(version) + r'\+commit\.[0-9a-f]+\.' + re.escape(suffix), a['name'])]
            if len(filenames) != 1:
                raise ValueError('Vyper asset is not unique')
            filename = filenames[0]
        elif name == 'arturo':
            filename = 'arturo-' + version + '-' + {'darwin': 'macos'}.get(system, system) + '-' + arch + '.zip'
        elif name == 'factor':
            filename = 'factor-' + system + '-x86-64-' + version + ('.zip' if system == 'windows' else '.tar.gz')
        else:
            suffix = {('windows', 'amd64'): 'win64.zip', ('linux', 'amd64'): 'linux64.tar.gz',
                      ('linux', 'arm64'): 'linux-arm64.tar.gz', ('darwin', 'amd64'): 'osx.tar.gz'}[(system, arch)]
            filename = 'haxe-' + version + '-' + suffix
        assets = [a for a in release['assets'] if a['name'] == filename]
        if len(assets) != 1:
            raise ValueError('Publisher archive is not unique: ' + name)
        asset = assets[0]
        url = asset['browser_download_url']
        checksum = (asset.get('digest') or '').removeprefix('sha256:')
        if not checksum and name != 'haxe':
            raise ValueError('Publisher SHA256 missing: ' + name)

    if checksum and not re.fullmatch(r'[0-9a-fA-F]{64}', checksum):
        raise ValueError('Invalid publisher SHA256')
    archive = directory / filename
    if not archive.is_file() or checksum and file_digest(archive) != checksum:
        for attempt in range(3):
            try:
                temporary = archive.with_name(archive.name + '.part')
                if args.use_gh:
                    subprocess.run(['curl', '--silent', '--show-error', '--fail', '--location', '--continue-at', '-', '--max-time', '120',
                                    '--output', str(temporary), url], check=True, timeout=130)
                else:
                    with request(url) as response, temporary.open('wb') as out:
                        total = 0
                        while block := response.read(64 * 1024):
                            out.write(block)
                            total += len(block)
                            if total > 1024 * 1024 * 1024:
                                raise ValueError('Fixture archive exceeds limit')
                actual = file_digest(temporary)
                if checksum and actual != checksum:
                    raise ValueError('Publisher SHA256 mismatch: ' + name)
                temporary.replace(archive)
                break
            except Exception:
                if attempt == 2:
                    raise
                time.sleep(1)
    file_format = 'binary' if name == 'vyper' else next(ext for ext in ['tar.gz', 'tar.xz', 'zip'] if filename.endswith('.' + ext))
    print(name + ' ' + version + ': ' + ('publisher SHA256 verified' if checksum else 'publisher digest absent; strict installation will refuse'), flush=True)
    return dict(name=name, version=version, archive=str(archive), url=url, sha256=checksum.lower(), size=archive.stat().st_size, format=file_format)


with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
    records = [row for row in pool.map(download, args.only.split(',')) if row]
manifest = directory / 'artifacts.json'
manifest.write_text(json.dumps(records, indent=2, ensure_ascii=False), encoding='utf-8')
print('VSM_TEST_LANGUAGE_ARTIFACTS=' + str(manifest), flush=True)
