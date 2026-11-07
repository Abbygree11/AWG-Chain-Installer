package chain

import (
	"context"
	"time"
)

// Build the tested AmneziaWG 3.1.20260812 from official pinned GitHub commits.
// Source archives use HTTPS and immutable commit IDs, but unlike Ubuntu APT
// packages are not themselves authenticated by a GPG package signature.
const nativeAWGGithubScript = `
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a
version=3.1.20260812
kernel_sha=46803204e7ec3b068199cd671143bec661d3fe21
tools_sha=ee0f0a9aa34ff0a0da4b3433b9512781cfe02843
log=$(mktemp /tmp/awg-chain-build.XXXXXXXX)
workspace=$(mktemp -d /tmp/awg-chain-sources.XXXXXXXX)
trap 'rm -f "$log"; rm -rf "$workspace"' EXIT

fail() {
  echo "AWG Chain: $1" >&2
  if test -s "$log"; then tail -n 32 "$log" >&2; fi
  exit 1
}
status() { echo "AWG Chain: $1"; }

if command -v awg >/dev/null 2>&1 &&
   command -v awg-quick >/dev/null 2>&1 &&
   awg --version 2>/dev/null | grep -q 'v3\.1\.20260812' &&
   modinfo -F version amneziawg 2>/dev/null | grep -q '^3\.1\.20260812$'; then
  modprobe amneziawg || fail "модуль установлен, но не загружается"
  status "AmneziaWG 3.1 уже установлен — пропускаем сборку"
  exit 0
fi

# An interrupted previous installer may have left an unreachable Launchpad
# source that prevents normal Ubuntu APT updates. Disable only our own source.
source_file=/etc/apt/sources.list.d/awg-chain-amnezia.sources
if test -f "$source_file"; then
  if grep -q 'ppa.launchpad' "$source_file"; then
    mv "$source_file" "$source_file.disabled" || fail "не удалось отключить старый источник Launchpad"
  else
    fail "файл источника PPA существует, но не соответствует AWG Chain: проверка вручную"
  fi
fi

status "Обновляем стандартные индексы Ubuntu"
apt-get -o Acquire::Retries=1 -o APT::Update::Error-Mode=any update -qq > "$log" 2>&1 ||
  fail "не удалось загрузить индексы Ubuntu"

status "Проверяем и устанавливаем зависимости сборки"
apt-get -o Acquire::Retries=1 install -y -qq \
  ca-certificates curl build-essential dkms pkg-config libmnl-dev \
  libelf-dev libssl-dev iproute2 iptables iputils-ping \
  "linux-headers-$(uname -r)" > "$log" 2>&1 ||
  fail "не удалось установить зависимости из Ubuntu APT"

mkdir -p "$workspace/kernel" "$workspace/tools"
status "Загружаем исходники ядра AmneziaWG (зафиксированный commit)"
curl -4 -fLSs --retry 2 --connect-timeout 10 --max-time 90 \
  "https://codeload.github.com/amnezia-vpn/amneziawg-linux-kernel-module/tar.gz/$kernel_sha" \
  -o "$workspace/kernel.tar.gz" || fail "GitHub не отдал архив модуля"

status "Загружаем исходники утилит AmneziaWG (зафиксированный commit)"
curl -4 -fLSs --retry 2 --connect-timeout 10 --max-time 90 \
  "https://codeload.github.com/amnezia-vpn/amneziawg-tools/tar.gz/$tools_sha" \
  -o "$workspace/tools.tar.gz" || fail "GitHub не отдал архив утилит"

tar -xzf "$workspace/kernel.tar.gz" -C "$workspace/kernel" --strip-components=1 ||
  fail "не удалось распаковать исходники модуля"
tar -xzf "$workspace/tools.tar.gz" -C "$workspace/tools" --strip-components=1 ||
  fail "не удалось распаковать исходники утилит"
test -f "$workspace/kernel/src/dkms.conf" || fail "в исходниках модуля нет dkms.conf"
test -f "$workspace/tools/src/Makefile" || fail "в исходниках утилит нет Makefile"

# The upstream DKMS Makefile defaults to 1.0.0. Pin the intended 3.1 version.
sed -i 's/^PACKAGE_VERSION=.*/PACKAGE_VERSION="3.1.20260812"/' "$workspace/kernel/src/dkms.conf"
sed -i '/^REMAKE_INITRD=/d' "$workspace/kernel/src/dkms.conf"
status "Регистрируем модуль AmneziaWG 3.1 в DKMS"
make -s -C "$workspace/kernel/src" WIREGUARD_VERSION="$version" dkms-install > "$log" 2>&1 ||
  fail "ошибка подготовки исходников DKMS"

if test ! -e "/var/lib/dkms/amneziawg/$version/source"; then
  dkms add -m amneziawg -v "$version" > "$log" 2>&1 ||
    fail "не удалось зарегистрировать модуль DKMS"
fi
if ! dkms status -m amneziawg -v "$version" -k "$(uname -r)" 2>/dev/null | grep -q 'installed'; then
  status "Собираем модуль для ядра $(uname -r)"
  if ! dkms status -m amneziawg -v "$version" -k "$(uname -r)" 2>/dev/null | grep -q 'built'; then
    dkms build -m amneziawg -v "$version" -k "$(uname -r)" > "$log" 2>&1 ||
      fail "DKMS не смог собрать модуль (см. журнал ошибки)"
  fi
  status "Устанавливаем модуль DKMS"
  dkms install -m amneziawg -v "$version" -k "$(uname -r)" > "$log" 2>&1 ||
    fail "DKMS не смог установить модуль"
fi

status "Собираем утилиты awg и awg-quick"
make -s -C "$workspace/tools/src" -j"$(nproc)" > "$log" 2>&1 ||
  fail "не удалось собрать пользовательские утилиты"
make -s -C "$workspace/tools/src" WITH_WGQUICK=yes WITH_SYSTEMDUNITS=yes install > "$log" 2>&1 ||
  fail "не удалось установить утилиты и systemd-сервис"
systemctl daemon-reload > "$log" 2>&1 ||
  fail "не удалось обновить systemd"
modprobe amneziawg > "$log" 2>&1 ||
  fail "не удалось загрузить модуль ядра"
command -v awg >/dev/null 2>&1 || fail "команда awg не установлена"
command -v awg-quick >/dev/null 2>&1 || fail "команда awg-quick не установлена"
awg --version | grep -q 'v3\.1\.20260812' ||
  fail "версия утилит awg не соответствует 3.1.20260812"
modinfo -F version amneziawg | grep -q '^3\.1\.20260812$' ||
  fail "версия модуля ядра не соответствует 3.1.20260812"
systemctl cat awg-quick@.service >/dev/null 2>&1 ||
  fail "systemd не распознал сервис awg-quick@.service"
status "AmneziaWG 3.1.20260812 установлен из официальных исходников GitHub"
`

// installGithubPackages is the only package path used by the temporary Go runner.
func installGithubPackages(ctx context.Context, remote *Remote) error {
	_, err := runWithTimeout(ctx, remote, nativeAWGGithubScript, 35*time.Minute)
	return err
}
