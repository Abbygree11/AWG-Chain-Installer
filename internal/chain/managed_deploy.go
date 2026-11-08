package chain

import (
	"context"
	"fmt"
	"time"
)

// ManagedAttacher configures official, discoverable AmneziaWG 3.1 Docker alongside
// a *preexisting, working* awg-mediator -> awg-destination chain. It is
// deliberately separate from the production GUI installer until field-tested.
type ManagedAttacher struct {
	Progress func(step, detail string)
}

func (a ManagedAttacher) Run(ctx context.Context, r *Remote) error {
	steps := []struct {
		label string
		script string
		timeout time.Duration
	}{
		{"Preflight", managedPreflight, 30*time.Second},
		{"Docker и официальный образ", managedDockerSetup, 20*time.Minute},
		{"Сервер AWG 3.1", managedConfigSetup, 3*time.Minute},
		{"Ответный маршрут Amnezia UDP", managedReplyRoute, 90*time.Second},
		{"PBR и защита от утечек", managedRoutingSetup, 2*time.Minute},
		{"Проверка финского выхода", managedVerify, 2*time.Minute},
	}
	for _, step := range steps {
		if a.Progress != nil { a.Progress(step.label, "Выполняется") }
		out, err := runWithTimeout(ctx, r, step.script, step.timeout)
		if err != nil { return fmt.Errorf("%s: %w", step.label, err) }
		if a.Progress != nil { a.Progress(step.label, out) }
	}
	return nil
}

// RepairReplies fixes the asymmetric Docker published-port reply path in an
// existing experiment without redeploying containers or touching AWG keys.
func (a ManagedAttacher) RepairReplies(ctx context.Context, r *Remote) error {
	if a.Progress != nil { a.Progress("Ответный маршрут Amnezia UDP", "Проверяем и исправляем") }
	out, err := runWithTimeout(ctx, r, managedReplyRoute, 90*time.Second)
	if err != nil { return fmt.Errorf("ответный маршрут Amnezia: %w", err) }
	if a.Progress != nil { a.Progress("Ответный маршрут Amnezia UDP", out) }
	return nil
}

// Immutable official amnezia-client dev commit for first laboratory test.
// Docker image still uses the upstream `amneziavpn/amneziawg-go:latest` tag.
const managedPreflight = `test "$(id -u)" = 0 || { echo "Нужен root"; exit 1; }
test "$(systemctl is-active awg-quick@awg-mediator)" = active || { echo "Не запущен awg-mediator. Сначала установи рабочую двухсерверную цепочку."; exit 1; }
ip -4 route show table 100 | grep -q 'dev awg-mediator' || { echo "Таблица 100 не ведёт на awg-mediator"; exit 1; }
test ! -e /etc/systemd/system/awg-chain-managed-route.service || { echo "Managed route уже установлен; автоматическое перезаписывание запрещено"; exit 1; }
if command -v docker >/dev/null 2>&1; then
  ! docker ps -a --format '{{.Names}}' | grep -Fxq amnezia-awg2 || { echo "amnezia-awg2 уже существует; не трогаем действующий контейнер"; exit 1; }
  ! docker network ls --format '{{.Name}}' | grep -Fxq awg-chain-managed || { echo "Docker-сеть awg-chain-managed уже существует"; exit 1; }
fi
! ip -4 route show | grep -q '172.28.66.0/24' || { echo "Подсеть 172.28.66.0/24 уже занята"; exit 1; }
! ip -4 rule show | grep -q '^1100:' || { echo "Priority 1100 уже занят"; exit 1; }
! ss -H -lun | grep -Eq ':55424[[:space:]]' || { echo "UDP 55424 уже занят"; exit 1; }
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q 'Status: active'; then
  echo "Обнаружен активный UFW; этот прототип не изменяет его правила"; exit 1
fi
echo "PREFLIGHT_OK"`

const managedDockerSetup = `export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a
echo "Устанавливаем Docker из Ubuntu..."
apt-get -o Acquire::Retries=1 -o APT::Update::Error-Mode=any update -qq
apt-get -o Acquire::Retries=1 install -y -qq docker.io ca-certificates curl iptables
systemctl enable --now docker
docker info >/dev/null
echo "Создаём сети Docker..."
docker network create --driver bridge --subnet 172.28.66.0/24 --gateway 172.28.66.1 \
  --opt com.docker.network.bridge.name=awc0 awg-chain-managed >/dev/null
if ! docker network inspect amnezia-dns-net >/dev/null 2>&1; then
  docker network create --driver bridge --subnet 172.29.172.0/24 \
    --opt com.docker.network.bridge.name=amn0 amnezia-dns-net >/dev/null
fi
mkdir -p /opt/amnezia/amnezia-awg2
cd /opt/amnezia/amnezia-awg2
commit=ac175c8a8147cfe56f98c8372ec17ea2ba00bb02
base=https://raw.githubusercontent.com/amnezia-vpn/amnezia-client/$commit/client/server_scripts/awg
curl -4 -fLSs --retry 2 --connect-timeout 15 --max-time 90 "$base/Dockerfile" -o Dockerfile
curl -4 -fLSs --retry 2 --connect-timeout 15 --max-time 90 "$base/start.sh" -o start.sh
sed -i 's/\$AWG_SUBNET_IP/10.8.1.0/g; s/\$WIREGUARD_SUBNET_CIDR/24/g' start.sh
# A second Docker network must never become the default egress interface.
sed -i '2a ip -4 route replace default via 172.28.66.1 dev eth0' start.sh
chmod 700 start.sh
echo "Собираем официальный AmneziaWG Docker-образ..."
docker build --pull -t amnezia-awg2 .
echo "Запускаем управляемый контейнер..."
docker run -d --log-driver json-file --log-opt max-size=5m --log-opt max-file=2 --restart always --privileged \
  --cap-add NET_ADMIN --cap-add SYS_MODULE -v /lib/modules:/lib/modules \
  --sysctl net.ipv4.conf.all.src_valid_mark=1 \
  --network awg-chain-managed --ip 172.28.66.2 \
  -p 55424:55424/udp --name amnezia-awg2 amnezia-awg2 >/dev/null
docker network connect amnezia-dns-net amnezia-awg2
docker cp start.sh amnezia-awg2:/opt/amnezia/start.sh
docker exec amnezia-awg2 chmod 755 /opt/amnezia/start.sh
echo "CONTAINER_CREATED"`

const managedConfigSetup = `echo "Создаём ключи и серверную конфигурацию внутри контейнера..."
docker exec -i amnezia-awg2 sh -se <<'INNER'
umask 077
mkdir -p /opt/amnezia/awg
cd /opt/amnezia/awg
private=$(awg genkey)
public=$(printf '%s\n' "$private" | awg pubkey)
psk=$(awg genpsk)
header=$(head -c 32 /dev/urandom | base64 | tr -d '\n')
h1=$(od -An -N4 -tu4 /dev/urandom | tr -d ' ')
h2=$(od -An -N4 -tu4 /dev/urandom | tr -d ' ')
h3=$(od -An -N4 -tu4 /dev/urandom | tr -d ' ')
h4=$(od -An -N4 -tu4 /dev/urandom | tr -d ' ')
h1=$((h1 % 200000000 + 1000))
h2=$((h2 % 200000000 + 200001000))
h3=$((h3 % 200000000 + 400001000))
h4=$((h4 % 200000000 + 600001000))
printf '%s\n' "$private" > wireguard_server_private_key.key
printf '%s\n' "$public" > wireguard_server_public_key.key
printf '%s\n' "$psk" > wireguard_psk.key
cat > awg0.conf <<EOF
[Interface]
PrivateKey = $private
Address = 10.8.1.1/24
ListenPort = 55424
MTU = 1280
Jc = 4
Jmin = 40
Jmax = 160
S1 = 64
S2 = 48
S3 = 24
S4 = 16
H1 = $h1
H2 = $h2
H3 = $h3
H4 = $h4
HeaderProtectionKey = $header
ContentPaddingAddition = 16-32
RekeyAfterTime = 100-120
RekeyTimeout = 3-7
RejectAfterTime = 150-180
KeepaliveTimeout = 5-15
MaxHandshakeAttempts = 15-20
RandomTrailers = on
DisableCookies = on
EOF
chmod 600 *.key awg0.conf
INNER
docker restart amnezia-awg2 >/dev/null
ready=false
for _ in $(seq 1 30); do
  if docker exec amnezia-awg2 awg show awg0 >/dev/null 2>&1; then ready=true; break; fi
  sleep 2
done
test "$ready" = true || { echo "AmneziaWG awg0 не запустился. Диагностика: docker logs --tail 40 amnezia-awg2"; exit 1; }
docker exec amnezia-awg2 test -s /opt/amnezia/awg/wireguard_server_public_key.key
docker exec amnezia-awg2 test -s /opt/amnezia/awg/wireguard_psk.key
echo "MANAGED_AWG_READY"`

const managedReplyRoute = `# Official Amnezia Docker publishes :55424 to the SECOND network (amn0).
# Its incoming packets reach 172.29.172.2 (eth1), whereas generic outbound
# routing uses 172.28.66.2 (eth0) for data traffic sent through Finland.
# Force only outer AWG UDP replies through eth1; leave all user/data traffic
# through eth0 and the host's existing PBR+kill switch.
test "$(id -u)" = 0 || { echo "Нужен root"; exit 1; }
docker inspect -f '{{.State.Running}}' amnezia-awg2 | grep -qx true ||
  { echo "amnezia-awg2 не запущен"; exit 1; }
docker exec amnezia-awg2 sh -c "ip -4 addr show dev eth0 | grep -q '172.28.66.2/'" ||
  { echo "У контейнера неожиданная сеть eth0, ничего не меняем"; exit 1; }
docker exec amnezia-awg2 sh -c "ip -4 addr show dev eth1 | grep -q '172.29.172.2/'" ||
  { echo "У контейнера неожиданная сеть eth1, ничего не меняем"; exit 1; }
test -f /opt/amnezia/amnezia-awg2/start.sh ||
  { echo "Отсутствует исходный start.sh; нельзя закрепить маршрут"; exit 1; }
start=/opt/amnezia/amnezia-awg2/start.sh
if ! grep -Fq 'AWG_CHAIN_UDP_REPLY_V1' "$start"; then
  grep -qx 'tail -f /dev/null' "$start" ||
    { echo "Формат start.sh изменился, автоматическая правка запрещена"; exit 1; }
  temp=$(mktemp)
  sed '/^tail -f \/dev\/null$/,$d' "$start" > "$temp"
  cat >>"$temp" <<'INNER'
# AWG_CHAIN_UDP_REPLY_V1: separate outer handshake from decrypted user traffic.
ip -4 route replace default via 172.29.172.1 dev eth1 src 172.29.172.2 table 101
if ip -4 rule show | grep -q '^90:'; then
  ip -4 rule show | grep -Eq '^90:.*ipproto udp sport 55424.*lookup 101' ||
    { echo 'ip rule priority 90 конфликтует с другой политикой' >&2; exit 1; }
else
  ip -4 rule add priority 90 ipproto udp sport 55424 lookup 101
fi
tail -f /dev/null
INNER
  cat "$temp" > "$start"
  rm -f "$temp"
fi
# Update on-disk startup file for subsequent docker restart (not recreate).
docker cp "$start" amnezia-awg2:/opt/amnezia/start.sh
docker exec amnezia-awg2 chmod 755 /opt/amnezia/start.sh
# Apply live without interrupting connected users or existing client peers.
docker exec amnezia-awg2 ip -4 route replace default via 172.29.172.1 dev eth1 src 172.29.172.2 table 101
if ! docker exec amnezia-awg2 ip -4 rule show | grep -Eq '^90:.*ipproto udp sport 55424.*lookup 101'; then
  if docker exec amnezia-awg2 ip -4 rule show | grep -q '^90:'; then
    echo "Внутри контейнера занят приоритет 90, не заменяем существующее правило" >&2
    exit 1
  fi
  docker exec amnezia-awg2 ip -4 rule add priority 90 ipproto udp sport 55424 lookup 101
fi
route=$(docker exec amnezia-awg2 ip -4 route get 1.1.1.1 ipproto udp sport 55424 dport 50000)
# iproute2 may insert "table 101" between "dev eth1" and "src ...".
# Verify the fields independently, without relying on their adjacency.
if ! printf '%s\n' "$route" | grep -Eq '(^|[[:space:]])dev eth1([[:space:]]|$)' ||
   ! printf '%s\n' "$route" | awk '{for(i=1;i<NF;i++) if($i=="src" && $(i+1)=="172.29.172.2")ok=1; exit !ok}'; then
  echo "Ответный маршрут не подтверждён; фактический результат:" >&2
  printf '%s\n' "$route" >&2
  docker exec amnezia-awg2 ip -4 rule show >&2
  docker exec amnezia-awg2 ip -4 route show table 101 >&2
  exit 1
fi
echo "AWG_UDP_REPLY_ROUTE_OK: $route"`

const managedRoutingSetup = `echo "Создаём постоянные PBR и kill-switch правила..."
cat > /usr/local/sbin/awg-chain-managed-route <<'ROUTE'
#!/bin/bash
set -euo pipefail
network=awc0
src=172.28.66.2/32
port=55424
mark=0x6601
chain=AWG_CHAIN_MNG
case "$1" in
start)
  ip -4 route replace unreachable default table 100 metric 10000
  iptables -t mangle -N "$chain" 2>/dev/null || true
  iptables -t mangle -F "$chain"
  iptables -t mangle -A "$chain" -p udp --sport "$port" -j RETURN
  iptables -t mangle -A "$chain" -m addrtype --dst-type LOCAL -j RETURN
  iptables -t mangle -A "$chain" -d 172.16.0.0/12 -j RETURN
  iptables -t mangle -A "$chain" -d 10.0.0.0/8 -j RETURN
  iptables -t mangle -A "$chain" -d 192.168.0.0/16 -j RETURN
  iptables -t mangle -A "$chain" -j MARK --set-mark "$mark"
  iptables -t mangle -C PREROUTING -i "$network" -s "$src" -j "$chain" 2>/dev/null ||
    iptables -t mangle -I PREROUTING 1 -i "$network" -s "$src" -j "$chain"
  if ! ip -4 rule show | grep -q '^1100:'; then
    ip -4 rule add priority 1100 fwmark "$mark"/0xffffffff lookup 100
  fi
  iptables -N DOCKER-USER 2>/dev/null || true
  iptables -C DOCKER-USER -i "$network" -s "$src" ! -o awg-mediator -j REJECT 2>/dev/null ||
    iptables -I DOCKER-USER 1 -i "$network" -s "$src" ! -o awg-mediator -j REJECT
  iptables -C DOCKER-USER -i "$network" -s "$src" -p udp --sport "$port" -j RETURN 2>/dev/null ||
    iptables -I DOCKER-USER 1 -i "$network" -s "$src" -p udp --sport "$port" -j RETURN
  # Docker often switches FORWARD policy to DROP. Preserve only the two known
  # VPN forwarding paths rather than globally reopening FORWARD.
  iptables -C DOCKER-USER -i "$network" -s "$src" -o awg-mediator -j ACCEPT 2>/dev/null ||
    iptables -I DOCKER-USER 1 -i "$network" -s "$src" -o awg-mediator -j ACCEPT
  iptables -C DOCKER-USER -i awg-mediator -o "$network" -d "$src" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT 2>/dev/null ||
    iptables -I DOCKER-USER 1 -i awg-mediator -o "$network" -d "$src" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  ;;
stop)
  iptables -t mangle -D PREROUTING -i "$network" -s "$src" -j "$chain" 2>/dev/null || true
  iptables -t mangle -F "$chain" 2>/dev/null || true
  iptables -t mangle -X "$chain" 2>/dev/null || true
  ip -4 rule del priority 1100 fwmark "$mark"/0xffffffff lookup 100 2>/dev/null || true
  iptables -D DOCKER-USER -i "$network" -s "$src" -p udp --sport "$port" -j RETURN 2>/dev/null || true
  iptables -D DOCKER-USER -i "$network" -s "$src" ! -o awg-mediator -j REJECT 2>/dev/null || true
  iptables -D DOCKER-USER -i "$network" -s "$src" -o awg-mediator -j ACCEPT 2>/dev/null || true
  iptables -D DOCKER-USER -i awg-mediator -o "$network" -d "$src" -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT 2>/dev/null || true
  ;;
*) echo "expected start or stop" >&2; exit 2 ;;
esac
ROUTE
chmod 700 /usr/local/sbin/awg-chain-managed-route
cat > /etc/systemd/system/awg-chain-managed-route.service <<'UNIT'
[Unit]
Description=Route Amnezia managed clients via Finnish AWG hop
After=network-online.target docker.service awg-quick@awg-mediator.service
Wants=docker.service awg-quick@awg-mediator.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/awg-chain-managed-route start
ExecStop=/usr/local/sbin/awg-chain-managed-route stop

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now awg-chain-managed-route.service
ip -4 route get 1.1.1.1 from 172.28.66.2 iif awc0 mark 0x6601 | grep -q 'dev awg-mediator'
echo "ROUTING_READY"`

const managedVerify = `echo "Проверяем выход через Финляндию..."
expected=$(curl -4 -fsS --retry 2 --connect-timeout 10 --max-time 30 --interface 'if!awg-mediator' https://api.ipify.org)
actual=$(docker exec amnezia-awg2 curl -4 -fsS --retry 2 --connect-timeout 10 --max-time 30 https://api.ipify.org)
test -n "$expected" && test -n "$actual"
printf 'Exit via mediator: %s\nExit via managed Docker: %s\n' "$expected" "$actual"
test "$expected" = "$actual" || { echo "Managed Docker выходит не через Финляндию. PBR не подтверждён!"; exit 1; }
docker ps --format '{{.Names}} {{.Ports}}' | grep 'amnezia-awg2'
systemctl is-active --quiet awg-chain-managed-route.service
echo "MANAGED_CHAIN_OK"`
