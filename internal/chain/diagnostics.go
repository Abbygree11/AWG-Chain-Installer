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
	Kind       InstallationKind
	Components []Component
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
	return ParseInventory(output)
}

func (i ServerInventory) Summary() string {
	switch i.Kind {
	case KindManaged: return "Найдены компоненты Amnezia VPN (Docker). Можно проверить их через официальное приложение."
	case KindStandalone: return "Найден нативный AWG. Он не управляется официальным приложением Amnezia VPN."
	case KindMixed: return "Найдены нативный AWG и контейнеры Amnezia. Не изменяй конфигурацию до проверки портов, правил и маршрутов."
	default: return "Не найдены известные компоненты. Это не гарантирует, что сервер чистый."
	}
}
