package chain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type InstallationKind string

const (
	KindEmpty      InstallationKind = "empty"
	KindStandalone InstallationKind = "standalone"
	KindManaged    InstallationKind = "managed"
	KindMixed      InstallationKind = "mixed"
)

type Component struct {
	Name    string
	Running bool
	Kind    string
}

type ServerInventory struct {
	Kind          InstallationKind
	Components    []Component
	RoutingStatus string // read-only diagnostics, never contains secrets
}

var nativeInterfaces = []string{
	"awg-client", "awg-mediator", "awg-destination",
	"awg-home", "awg-fin", "awg-spb",
}
var managedContainers = []string{"amnezia-awg2", "amnezia-awg"}

// ParseInventory accepts only a limited and non-secret machine-generated
// snapshot. Never request or render private keys or complete VPN configs.
func ParseInventory(output string) (ServerInventory, error) {
	inv := ServerInventory{Kind: KindEmpty}
	native, managed := false, false
	seen := make(map[string]bool)
	for _, row := range strings.Split(strings.TrimSpace(output), "\n") {
		row = strings.TrimSpace(row)
		if row == "" {continue}
		fields := strings.Split(row, "|")
		if len(fields) != 3 {return ServerInventory{}, errors.New("неизвестный формат проверки VPS")}
		kind, name, state := fields[0], fields[1], fields[2]
		if state != "active" && state != "inactive" {return ServerInventory{}, errors.New("неизвестное состояние VPN-компонента")}
		valid := false
		switch kind {
		case "native":
			for _, item := range nativeInterfaces {if name==item {valid=true;break}}
			native = true
		case "managed":
			for _, item := range managedContainers {if name==item {valid=true;break}}
			managed = true
		}
		if !valid {return ServerInventory{}, errors.New("обнаружен неожиданный VPN-компонент")}
		id:=kind+"/"+name
		if seen[id] {continue}
		seen[id]=true
		inv.Components=append(inv.Components,Component{Name:name,Kind:kind,Running:state=="active"})
	}
	switch {
	case native && managed: inv.Kind=KindMixed
	case native: inv.Kind=KindStandalone
	case managed: inv.Kind=KindManaged
	}
	return inv,nil
}

func DetectServer(ctx context.Context, server Server, ask AskHostKey) (ServerInventory,error) {
	if strings.TrimSpace(server.Host)=="" || strings.TrimSpace(server.Password)=="" || server.User!="root" || server.Port<1 || server.Port>65535 {
		return ServerInventory{},errors.New("укажи IPv4-адрес, порт и SSH-пароль пользователя root")
	}
	remote,err:=Dial(ctx,server,ask)
	if err!=nil {return ServerInventory{},err}
	defer remote.Close()

	// All remote operations are read-only. No sudo, installation, firewall,
	// Docker modifications, routing changes or config content reads.
	script:=`for iface in awg-client awg-mediator awg-destination awg-home awg-fin awg-spb; do
  if ip link show dev "$iface" >/dev/null 2>&1; then
    printf 'native|%s|active\n' "$iface"
  elif test -f "/etc/amnezia/amneziawg/$iface.conf"; then
    printf 'native|%s|inactive\n' "$iface"
  fi
done
if command -v docker >/dev/null 2>&1; then
  for name in amnezia-awg2 amnezia-awg; do
    if docker container inspect "$name" >/dev/null 2>&1; then
      status=$(docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null || true)
      if test "$status" = true; then state=active; else state=inactive; fi
      printf 'managed|%s|%s\n' "$name" "$state"
    fi
  done
fi`
	taskCtx,cancel:=context.WithTimeout(ctx,30*time.Second)
	defer cancel()
	output,err:=remote.Run(taskCtx,script)
	if err!=nil {return ServerInventory{},fmt.Errorf("не удалось проверить VPN-компоненты: %w",err)}
	inv, err := ParseInventory(output)
	if err != nil { return ServerInventory{}, err }
	if inv.Kind == KindManaged || inv.Kind == KindMixed {
		// Extra read-only checks; changes to Docker network topology after an
		// official Amnezia upgrade should be visible to the user. Never
		// assume an existing managed container belongs to AWG Chain.
		check := `if ! docker inspect amnezia-awg2 >/dev/null 2>&1; then exit 0; fi
if docker network inspect awg-chain-managed >/dev/null 2>&1 &&
   docker inspect -f '{{range .NetworkSettings.Networks}}{{println .NetworkID}}{{end}}' amnezia-awg2 2>/dev/null |
   grep -Fq "$(docker network inspect -f '{{.Id}}' awg-chain-managed 2>/dev/null)"; then
  echo 'Docker-сеть awg-chain-managed: подключена'
else
  echo 'Docker-сеть awg-chain-managed: не обнаружена — требуется проверка после обновления'
fi
# Read container addresses from named networks: works for custom subnet plans.
data_ip=$(docker inspect -f '{{with index .NetworkSettings.Networks "awg-chain-managed"}}{{.IPAddress}}{{end}}' amnezia-awg2 2>/dev/null || true)
reply_ip=$(docker inspect -f '{{with index .NetworkSettings.Networks "amnezia-dns-net"}}{{.IPAddress}}{{end}}' amnezia-awg2 2>/dev/null || true)
addr_if() {
  docker exec amnezia-awg2 ip -4 -o addr show 2>/dev/null |
    awk -v addr="$1" '{split($4, parts, "/"); if (parts[1]==addr) {print $2; exit}}'
}
uses_dev() {
  printf '%s\n' "$1" | awk -v want="$2" '{for(i=1;i<NF;i++) if($i=="dev" && $(i+1)==want)ok=1; exit !ok}'
}
data_if=
reply_if=
if [ -n "$data_ip" ] && [ -n "$reply_ip" ]; then
  data_if=$(addr_if "$data_ip")
  reply_if=$(addr_if "$reply_ip")
fi
local_route=$(docker exec amnezia-awg2 ip -4 route get 1.1.1.1 2>/dev/null || true)
client_ip=$(docker exec amnezia-awg2 ip -4 -o addr show dev awg0 2>/dev/null |
  awk '{split($4, net, "/"); split(net[1], a, "."); if (length(a)==4) {print a[1]"."a[2]"."a[3]".2"; exit}}')
client_route=
if [ -n "$client_ip" ]; then
  client_route=$(docker exec amnezia-awg2 ip -4 route get 1.1.1.1 from "$client_ip" iif awg0 2>/dev/null || true)
fi
reply_route=$(docker exec amnezia-awg2 ip -4 route get 1.1.1.1 ipproto udp sport 55424 dport 50000 2>/dev/null || true)
if [ -n "$data_if" ] && [ -n "$reply_if" ] && [ "$data_if" != "$reply_if" ] &&
   uses_dev "$local_route" "$data_if" && uses_dev "$client_route" "$data_if"; then
  echo 'Маршрут Docker и клиентского трафика: через выделенную egress-сеть'
else
  echo 'Маршрут Docker или клиента: неправильный либо не подтверждён (возможен прямой выход!)'
fi
if [ -n "$reply_if" ] && uses_dev "$reply_route" "$reply_if" &&
   docker exec amnezia-awg2 ip -4 rule show 2>/dev/null | grep -Eq '^90:.*sport 55424.*lookup 101'; then
  echo 'Ответные UDP-пакеты: через служебную Docker-сеть'
else
  echo 'Ответные UDP-пакеты: маршрут не подтверждён'
fi
if test -f /opt/amnezia/amnezia-awg2/start.sh &&
   grep -Fq 'AWG_CHAIN_DYNAMIC_IFACES_V1' /opt/amnezia/amnezia-awg2/start.sh; then
  echo 'Автозапуск: динамическое определение Docker-интерфейсов включено'
else
  echo 'Автозапуск: динамическая маршрутизация не обнаружена; после рестарта возможен обход Финляндии'
fi
if [ -n "$data_ip" ] &&
   ip -4 route show table 100 | grep -q 'dev awg-mediator' &&
   ip -4 rule show | grep -q 'fwmark 0x6601 lookup 100' &&
   iptables -t mangle -C PREROUTING -i awc0 -s "$data_ip/32" -j AWG_CHAIN_MNG 2>/dev/null &&
   iptables -C DOCKER-USER -i awc0 -s "$data_ip/32" ! -o awg-mediator -j REJECT 2>/dev/null &&
   systemctl is-active --quiet awg-chain-managed-route.service; then
  echo 'PBR и защитные правила: присутствуют (не заменяет тест с реальным клиентом)'
else
  echo 'PBR или защитные правила: не подтверждены'
fi`
		out, err := runWithTimeout(ctx, remote, check, 20*time.Second)
		if err == nil { inv.RoutingStatus = strings.TrimSpace(out) }
		if err != nil { inv.RoutingStatus = "Проверка таблиц маршрутизации недоступна: "+err.Error() }
	}
	return inv, nil
}

func (i ServerInventory) Summary() string {
	switch i.Kind {
	case KindManaged: return "Найдены компоненты Amnezia VPN (Docker). Можно проверить их через официальное приложение."
	case KindStandalone: return "Найден нативный AWG. Он не управляется официальным приложением Amnezia VPN."
	case KindMixed:
		hasHop, hasDocker := false, false
		for _, component := range i.Components {
			if component.Name == "awg-mediator" && component.Running { hasHop = true }
			if component.Name == "amnezia-awg2" && component.Running { hasDocker = true }
		}
		if hasHop && hasDocker {
			return "Найдена штатная двухсерверная схема AWG Chain: Amnezia-managed Docker + нативный awg-mediator."
		}
		return "Найдены и нативный AWG, и контейнеры Amnezia. Проверь маршрутизацию и принадлежность сервисов."
	default: return "Не найдены известные компоненты. Это не гарантирует, что сервер чистый."
	}
}
