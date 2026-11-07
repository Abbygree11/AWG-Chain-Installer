package chain

// Authenticated Amnezia PPA installation with IPv6/IPv4 and signed legacy fallback.
const nativeAWGInstallScript = `
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a
log=$(mktemp /tmp/awg-chain-apt.XXXXXXXX)
keyfile=$(mktemp /tmp/awg-chain-key.XXXXXXXX)
trap 'rm -f "$log" "$keyfile"' EXIT
fail() {
  echo "AWG Chain: $1" >&2
  grep -Ei '(^E:|^Err:|^W:|error:|failed|could not|timeout|unable|dkms)' "$log" | tail -n 10 >&2 || true
  exit 1
}
apt-get -o Acquire::Retries=2 update -qq > "$log" 2>&1 || fail "не удалось обновить Ubuntu APT"
apt-get -o Acquire::Retries=2 install -y -qq ca-certificates curl gnupg dkms build-essential iptables iproute2 iputils-ping "linux-headers-$(uname -r)" > "$log" 2>&1 || fail "не удалось установить зависимости"
if ! command -v awg >/dev/null || ! modinfo -F version amneziawg 2>/dev/null | grep -q '^3\.1\.'; then
  install -d -m 755 /etc/apt/keyrings
  keyring=/etc/apt/keyrings/awg-chain-amnezia.gpg
  fingerprint=75C9DD72C799870E310542E24166F2C257290828
  if test ! -e "$keyring"; then
    key_url="https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x$fingerprint"
    ok=false
    for family in -6 -4; do
      if curl "$family" -fsSL --retry 1 --connect-timeout 7 --max-time 20 "$key_url" -o "$keyfile" 2> "$log"; then ok=true; break; fi
    done
    if test "$ok" != true; then fail "ключ подписи PPA недоступен по IPv6 и IPv4"; fi
    actual=$(gpg --batch --show-keys --with-colons "$keyfile" 2> "$log" | awk -F: '$1=="fpr" {print $10; exit}')
    if test "$actual" != "$fingerprint"; then fail "полный отпечаток GPG-ключа не совпадает"; fi
    gpg --batch --yes --dearmor -o "$keyring" "$keyfile" 2> "$log" || fail "не удалось сохранить GPG-ключ"
    chmod 644 "$keyring"
  else
    actual=$(gpg --batch --show-keys --with-colons "$keyring" 2> "$log" | awk -F: '$1=="fpr" {print $10; exit}')
    if test "$actual" != "$fingerprint"; then fail "сохранённый GPG-ключ имеет неверный отпечаток"; fi
  fi
  ready=false
  for uri in "https://ppa.launchpadcontent.net/amnezia/ppa/ubuntu/" "http://ppa.launchpad.net/amnezia/ppa/ubuntu/"; do
    printf 'Types: deb\nURIs: %s\nSuites: noble\nComponents: main\nSigned-By: %s\n' "$uri" "$keyring" > /etc/apt/sources.list.d/awg-chain-amnezia.sources
    for family in auto ipv6 ipv4; do
      options=()
      case "$family" in
        ipv6) options=(-o Acquire::ForceIPv6=true) ;;
        ipv4) options=(-o Acquire::ForceIPv4=true) ;;
      esac
      if apt-get "${options[@]}" -o Acquire::Retries=1 -o Acquire::http::Timeout=15 -o Acquire::https::Timeout=15 -o APT::Update::Error-Mode=any update -qq > "$log" 2>&1 &&
         apt-cache policy amneziawg-dkms | grep -Eq 'Candidate: [0-9]' &&
         apt-cache policy amneziawg-tools | grep -Eq 'Candidate: [0-9]'; then
        ready=true
        echo "AWG Chain: подписанный PPA доступен ($uri; $family)"
        break 2
      fi
    done
  done
  if test "$ready" != true; then fail "PPA недоступен: проверены HTTPS/IPv6/IPv4 и резервный подписанный HTTP"; fi
  apt-get "${options[@]}" -o Acquire::Retries=2 install -y -qq amneziawg-dkms amneziawg-tools > "$log" 2>&1 || fail "не удалось установить пакеты AmneziaWG"
fi
modprobe amneziawg > "$log" 2>&1 || fail "не удалось загрузить модуль ядра через modprobe"
awg --version | grep -q 'v3\.1\.' || fail "утилиты awg не соответствуют версии 3.1"
modinfo -F version amneziawg | grep -q '^3\.1\.' || fail "модуль ядра не соответствует версии 3.1"
echo "AWG Chain: AmneziaWG 3.1 проверен"
`
