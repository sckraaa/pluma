#!/usr/bin/env bash
# Installs ffmpeg from the packages in ~/ffmpeg-debs, or downloads them there
# (action.yml).
set -euo pipefail

if compgen -G ~/ffmpeg-debs/'*.deb' >/dev/null && sudo dpkg -i ~/ffmpeg-debs/*.deb >/dev/null; then
  echo "installed from the cached packages"
else
  # Packages fetched by a timed-out attempt stay in apt's cache, so
  # every attempt continues where the previous one stopped.
  apt_opts=(-q -o Acquire::Retries=3 -o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30)
  for attempt in 1 2 3; do
    if timeout 240 sudo apt-get "${apt_opts[@]}" update &&
      timeout 420 sudo apt-get "${apt_opts[@]}" install -y --no-install-recommends ffmpeg; then
      break
    fi
    if [ "$attempt" = 3 ]; then exit 1; fi
    echo "apt attempt $attempt failed, retrying"
    sleep 10
  done
  mkdir -p ~/ffmpeg-debs
  cp /var/cache/apt/archives/*.deb ~/ffmpeg-debs/
fi
ffmpeg -hide_banner -version | head -n 1
