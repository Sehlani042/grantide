#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p dist
for platform in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64; do
  target_os="${platform%/*}"
  target_arch="${platform#*/}"
  target_dir="dist/grantide_${target_os}_${target_arch}"
  mkdir -p "$target_dir"
  executable=grantide
  if [[ "$target_os" == windows ]]; then executable=grantide.exe; fi
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -ldflags='-s -w' -o "$target_dir/$executable" ./cmd/grantide
  cp LICENSE README.md README.zh-CN.md SECURITY.md PRD.md DESIGN.md TEST.md RELEASE.md "$target_dir/"
  cp -R docs "$target_dir/"
  cp -R extension "$target_dir/"
  python3 - "$target_dir" <<'PY_SKILL'
import pathlib, shutil, sys
shutil.copytree("skills", pathlib.Path(sys.argv[1])/"skills", dirs_exist_ok=True, ignore=shutil.ignore_patterns("__pycache__"))
PY_SKILL
  mkdir -p "$target_dir/scripts"
  cp scripts/install-native-host.py "$target_dir/scripts/"
  if [[ "$target_os" == windows ]]; then
    python3 - "$target_dir" <<'PY'
import pathlib, sys, zipfile
root = pathlib.Path(sys.argv[1])
with zipfile.ZipFile(str(root) + '.zip', 'w', zipfile.ZIP_DEFLATED) as archive:
    for path in root.rglob('*'):
        if path.is_file():
            archive.write(path, root.name + '/' + str(path.relative_to(root)))
PY
  else
    tar -czf "$target_dir.tar.gz" -C dist "$(basename "$target_dir")"
  fi
done
python3 - <<'PY'
import hashlib, pathlib
root = pathlib.Path('dist')
archives = sorted([*root.glob('*.tar.gz'), *root.glob('*.zip')])
(root/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name + '\n' for p in archives))
PY
