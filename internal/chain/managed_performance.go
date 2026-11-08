package chain

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// DiagnoseManagedPerformance measures only traffic generated inside the
// Amnezia Docker container on the entrance VPS. It does not measure the
// client's DNS, Wi-Fi or ISP. Never reads VPN keys or user profiles.
func DiagnoseManagedPerformance(ctx context.Context, server Server, ask AskHostKey) (string, error) {
	if err := validatePerformanceServer(server); err != nil { return "", err }
	remote, err := Dial(ctx, server, ask)
	if err != nil { return "", err }
	defer remote.Close()
	return runWithTimeout(ctx, remote, managedPerformanceDiagnostics, 100*time.Second)
}

// OptimizeManagedTCP safely repairs an existing AWG Chain-managed VPS:
// no container restart, no change to keys, peers, routing or DNS settings.
// Limits new TCP SYN/SYN-ACK MSS to 1160 under the original outer MTU=1200.
func OptimizeManagedTCP(ctx context.Context, server Server, ask AskHostKey) (string, error) {
	if err := validatePerformanceServer(server); err != nil { return "", err }
	remote, err := Dial(ctx, server, ask)
	if err != nil { return "", err }
	defer remote.Close()
	return runWithTimeout(ctx, remote, managedTCPMSSRepair, 65*time.Second)
}

func validatePerformanceServer(s Server) error {
	ip, err := netip.ParseAddr(strings.TrimSpace(s.Host))
	if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() || s.User != "root" ||
		s.Port < 1 || s.Port > 65535 || s.Password == "" {
		return fmt.Errorf("нужен публичный IPv4, SSH-порт и пароль пользователя root")
	}
	return nil
}

const managedPerformanceDiagnostics = `test "$(id -u)" = 0
test "$(docker inspect -f '{{.State.Running}}' amnezia-awg2 2>/dev/null)" = true ||
  { echo "Нет работающего контейнера amnezia-awg2"; exit 1; }
ip link show dev awg-mediator >/dev/null 2>&1 ||
  { echo "Нет работающего межсерверного туннеля awg-mediator"; exit 1; }
echo '=== Два уровня MTU ==='
printf 'Межсерверный awg-mediator: '
cat /sys/class/net/awg-mediator/mtu
printf 'Клиентский Docker awg0: '
docker exec amnezia-awg2 cat /sys/class/net/awg0/mtu
echo '=== TCP MSS (только новые соединения) ==='
if test -f /usr/local/sbin/awg-chain-managed-route &&
   grep -q 'AWG_CHAIN_TCP_MSS_V1' /usr/local/sbin/awg-chain-managed-route; then
  echo 'Правило сохраняется в systemd: да'
else
  echo 'Правило сохраняется в systemd: нет'
fi
if iptables -t mangle -S FORWARD | grep -q -- '-j TCPMSS --set-mss 1160'; then
  echo 'Ограничение MSS 1160: активно'
else
  echo 'Ограничение MSS 1160: не обнаружено'
fi
echo '=== Время загрузки с выходного VPS (секунды) ==='
echo 'DNS = разрешение имени, TCP = установление соединения, TLS = HTTPS, TTFB = первый байт.'
for address in https://example.com/ https://www.cloudflare.com/; do
  for attempt in 1 2; do
    printf '%s попытка %s: ' "$address" "$attempt"
    if ! docker exec amnezia-awg2 curl -4 -sSL -o /dev/null \
      --connect-timeout 7 --max-time 17 \
      --write-out 'DNS=%{time_namelookup} TCP=%{time_connect} TLS=%{time_appconnect} TTFB=%{time_starttransfer} Итого=%{time_total} HTTP=%{http_code}\n' \
      "$address"; then
      echo 'Запрос не удался: проверь доступность сайта/DNS.'
    fi
  done
done
echo 'Замеры выполняются на VPS, задержку DNS/сети телефона они не показывают.'
`

// The original v0.3.0 route service remains on a running VPS after GUI
// upgrade. Patch it atomically and apply the rules live, without restarting
// awg-mediator, Docker or any peer. The script is idempotent and deliberately
// rejects foreign/custom routing services.
const managedTCPMSSRepair = `test "$(id -u)" = 0
route=/usr/local/sbin/awg-chain-managed-route
test -f "$route" || { echo "Не установлен маршрут AWG Chain managed"; exit 1; }
test "$(systemctl is-active awg-chain-managed-route.service)" = active ||
  { echo "Служба awg-chain-managed-route не активна"; exit 1; }
test "$(docker inspect -f '{{.State.Running}}' amnezia-awg2 2>/dev/null)" = true ||
  { echo "Контейнер amnezia-awg2 не запущен"; exit 1; }
test "$(cat /sys/class/net/awg-mediator/mtu 2>/dev/null)" = 1200 ||
  { echo "MTU awg-mediator отличается от 1200. Для него MSS 1160 может быть неверным"; exit 1; }
grep -Fqx 'network=awc0' "$route" ||
  { echo "Неизвестная маршрутизация: имя Docker-моста изменено"; exit 1; }
grep -Fqx 'chain=AWG_CHAIN_MNG' "$route" ||
  { echo "Неизвестная версия managed-маршрута: ничего не изменено"; exit 1; }
python3 - <<'PY'
import os, pathlib
path = pathlib.Path('/usr/local/sbin/awg-chain-managed-route')
src = path.read_text()
marker = 'AWG_CHAIN_TCP_MSS_V1'
if marker not in src:
    if src.count('\nstart)\n') != 1 or src.count('\nstop)\n') != 1:
        raise SystemExit('Неизвестная структура managed-маршрута')
    begin = """  # AWG_CHAIN_TCP_MSS_V1: clamp both SYN and SYN/ACK for nested MTU=1200.
  iptables -t mangle -C FORWARD -i "$network" -s "$src" -o awg-mediator -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1160 2>/dev/null ||
    iptables -t mangle -I FORWARD 1 -i "$network" -s "$src" -o awg-mediator -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1160
  iptables -t mangle -C FORWARD -i awg-mediator -o "$network" -d "$src" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1160 2>/dev/null ||
    iptables -t mangle -I FORWARD 1 -i awg-mediator -o "$network" -d "$src" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1160
"""
    end = """  iptables -t mangle -D FORWARD -i "$network" -s "$src" -o awg-mediator -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1160 2>/dev/null || true
  iptables -t mangle -D FORWARD -i awg-mediator -o "$network" -d "$src" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1160 2>/dev/null || true
"""
    src = src.replace('\nstart)\n', '\nstart)\n'+begin, 1).replace('\nstop)\n', '\nstop)\n'+end, 1)
    temp = path.with_suffix('.mss-new')
    with temp.open('w') as handle:
        handle.write(src)
    temp.chmod(0o700)
    os.replace(temp, path)
PY
# The service is Type=oneshot and already active; executing its idempotent
# start routine updates iptables without tearing down any live connection.
/usr/local/sbin/awg-chain-managed-route start
. /dev/null
network=awc0
src=$(sed -n 's/^src=//p' "$route")
test -n "$src"
iptables -t mangle -C FORWARD -i "$network" -s "$src" -o awg-mediator -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1160
iptables -t mangle -C FORWARD -i awg-mediator -o "$network" -d "$src" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1160
echo 'TCP_MSS_1160_OK: правило установлено, сохранено для перезагрузок, VPN не перезапускался.'
`
