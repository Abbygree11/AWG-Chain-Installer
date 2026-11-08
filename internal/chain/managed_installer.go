package chain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ManagedListenPort is the official AmneziaWG 3.1 UDP port exposed on the
// entry VPS. Client profiles are administered by the Amnezia VPN application.
const ManagedListenPort = 55424

type ManagedResult struct {
	EntryIP string
	ExitIP  string
	Port    int
	Networks NetworkPlan
}

// RunManaged is the default desktop flow for two CLEAN Ubuntu VPS.
// Unlike Run (legacy native Keenetic flow), it does not create awg-client,
// generate a client private key on disk or interfere with Amnezia users.
func (in *Installer) RunManaged(ctx context.Context, o Options) (ManagedResult, error) {
	var result ManagedResult
	if err := o.ValidateServers(); err != nil { return result, err }
	plan := o.Networks.OrDefault()
	if err := plan.Validate(); err != nil { return result, err }
	var keys Keys
	if err := NewAllKeys(&keys); err != nil { return result, err }
	if err := keys.Validate(); err != nil { return result, err }

	in.notify(0.02, "SSH", "Подключаемся к входному VPS")
	entry, err := Dial(ctx, o.Entry, in.AskHostKey)
	if err != nil { return result, err }
	defer entry.Close()

	in.notify(0.07, "SSH", "Подключаемся к выходному VPS")
	exit, err := Dial(ctx, o.Exit, in.AskHostKey)
	if err != nil { return result, err }
	defer exit.Close()

	in.notify(0.12, "Preflight", "Проверяем входной VPS и свободный UDP-порт Amnezia")
	entryInfo, err := inspectManaged(ctx, entry, ManagedListenPort, "awg-mediator.conf", "awg-client.conf")
	if err != nil { return result, fmt.Errorf("входной VPS: %w", err) }

	in.notify(0.18, "Preflight", "Проверяем выходной VPS, IPv6 и порт 51820")
	exitInfo, err := inspectManaged(ctx, exit, ExitPort, "awg-destination.conf")
	if err != nil { return result, fmt.Errorf("выходной VPS: %w", err) }
	if entryInfo.IPv6 == exitInfo.IPv6 {
		return result, errors.New("совпадает публичный IPv6 двух VPS")
	}
	// Run on BOTH hosts before installing anything. Checks IP routes, addresses
	// and Docker IPAM allocations (including stopped networks).
	in.notify(0.20, "Preflight", "Проверяем выбранные подсети и конфликты на обоих серверах")
	if err := checkNetworkAvailability(ctx, entry, plan); err != nil {
		return result, fmt.Errorf("входной VPS, выбранные подсети: %w", err)
	}
	if err := checkNetworkAvailability(ctx, exit, plan); err != nil {
		return result, fmt.Errorf("выходной VPS, выбранные подсети: %w", err)
	}
	in.notify(0.22, "Preflight", "Проверяем отсутствие предыдущей установки Amnezia Docker")
	if _, err := runWithTimeout(ctx, entry, managedCleanPreflight, 35*time.Second); err != nil {
		return result, fmt.Errorf("входной VPS не является чистым для Managed: %w", err)
	}
	if _, err := runWithTimeout(ctx, entry, "ip -6 route get "+exitInfo.IPv6+" >/dev/null", 20*time.Second); err != nil {
		return result, fmt.Errorf("нет IPv6-маршрута до выходного VPS: %w", err)
	}
	in.notify(0.27, "Установка AWG", "Устанавливаем нативный AmneziaWG на выходном VPS из GitHub")
	if err := installGithubPackages(ctx, exit); err != nil { return result, fmt.Errorf("выходной VPS: %w", err) }

	in.notify(0.41, "Установка AWG", "Устанавливаем межсерверный AWG на входном VPS из GitHub")
	if err := installGithubPackages(ctx, entry); err != nil { return result, fmt.Errorf("входной VPS: %w", err) }

	in.notify(0.55, "Маршрутизация", "Включаем IP forwarding на обоих VPS")
	for _, r := range []*Remote{entry, exit} {
		if _, err := runWithTimeout(ctx, r, `printf 'net.ipv4.ip_forward = 1\n' > /etc/sysctl.d/90-awg-chain-forwarding.conf
sysctl -p /etc/sysctl.d/90-awg-chain-forwarding.conf >/dev/null
test "$(sysctl -n net.ipv4.ip_forward)" = 1`, 40*time.Second); err != nil {
			return result, fmt.Errorf("IP forwarding: %w", err)
		}
	}

	// Deliberately do not roll back already-started VPN services on errors:
	// this flow is only for clean VPS and an interrupted Docker installation
	// can leave an intermediate state which requires operator inspection.
	// Never remove Docker keys/peers through an installer rollback.
	in.notify(0.61, "Конфигурация", "Создаём только межсерверные AWG-конфигурации")
	if err := exit.PutPrivateFile(ctx, "/etc/amnezia/amneziawg/awg-destination.conf",
		ManagedExitConfigForPlan(keys, entryInfo.IPv6, exitInfo.Egress, plan)); err != nil { return result, err }
	if err := entry.PutPrivateFile(ctx, "/etc/amnezia/amneziawg/awg-mediator.conf",
		ManagedEntryHopConfig(keys, exitInfo.IPv6, plan)); err != nil { return result, err }

	in.notify(0.68, "Туннель", "Запускаем awg-destination на выходном VPS")
	if _, err := runWithTimeout(ctx, exit, "systemctl enable --now awg-quick@awg-destination\nsystemctl is-active --quiet awg-quick@awg-destination", 60*time.Second); err != nil {
		return result, fmt.Errorf("выходной AWG: %w", err)
	}
	in.notify(0.73, "Туннель", "Запускаем awg-mediator через IPv6")
	if _, err := runWithTimeout(ctx, entry, "systemctl enable --now awg-quick@awg-mediator\nsystemctl is-active --quiet awg-quick@awg-mediator", 60*time.Second); err != nil {
		return result, fmt.Errorf("входной AWG: %w", err)
	}
	in.notify(0.79, "Проверка", "Проверяем межсерверную связь и IP выходного VPS")
	verifyHop := `ping -c 3 -W 3 10.77.77.2 >/dev/null
curl -4 -fsS --retry 2 --connect-timeout 8 --max-time 25 --interface 'if!awg-mediator' https://api.ipify.org`
	got, err := runWithTimeout(ctx, entry, renderManagedScript(verifyHop, plan), 65*time.Second)
	if err != nil { return result, fmt.Errorf("межсерверный VPN: %w", err) }
	if strings.TrimSpace(got) != o.Exit.Host {
		return result, fmt.Errorf("выходной IPv4 %q не совпадает с ожидаемым %s", strings.TrimSpace(got), o.Exit.Host)
	}

	in.notify(0.82, "Amnezia Docker", "Разворачиваем официальный управляемый контейнер amnezia-awg2")
	if _, err := runWithTimeout(ctx, entry, renderManagedScript(managedDockerSetup, plan), 20*time.Minute); err != nil {
		return result, fmt.Errorf("Docker/Amnezia: %w; возможно частичное состояние VPS", err)
	}
	in.notify(0.87, "Amnezia Docker", "Создаём сервер AWG 3.1 с ключами внутри Docker")
	if _, err := runWithTimeout(ctx, entry, renderManagedScript(managedConfigSetup, plan), 3*time.Minute); err != nil {
		return result, fmt.Errorf("конфигурация контейнера: %w", err)
	}
	in.notify(0.90, "Amnezia Docker", "Настраиваем правильный путь UDP-ответов для любого клиента")
	if _, err := runWithTimeout(ctx, entry, renderManagedScript(managedReplyRoute, plan), 90*time.Second); err != nil {
		return result, fmt.Errorf("маршрутизация UDP: %w", err)
	}
	in.notify(0.93, "PBR", "Подключаем Docker к таблице 100 с kill switch")
	if _, err := runWithTimeout(ctx, entry, renderManagedScript(managedRoutingSetup, plan), 2*time.Minute); err != nil {
		return result, fmt.Errorf("маршрутизация через выходной сервер: %w", err)
	}
	in.notify(0.97, "Проверка", "Проверяем IP выходного сервера и автозапуск")
	if _, err := runWithTimeout(ctx, entry, renderManagedScript(managedVerify, plan), 2*time.Minute); err != nil {
		return result, fmt.Errorf("финальная проверка Amnezia: %w", err)
	}
	if _, err := runWithTimeout(ctx, entry,
		"docker exec amnezia-awg2 ip -4 rule show | grep -Eq '^90:.*sport 55424.*lookup 101'\nsystemctl is-enabled --quiet awg-chain-managed-route.service", 30*time.Second); err != nil {
		return result, fmt.Errorf("контроль восстановления маршрутов после reboot: %w", err)
	}
	result = ManagedResult{EntryIP: o.Entry.Host, ExitIP: o.Exit.Host, Port: ManagedListenPort, Networks: plan}
	in.notify(1, "Готово", "AmneziaWG управляется из официального приложения; выходной IP: "+result.ExitIP)
	return result, nil
}

const managedCleanPreflight = `test "$(id -u)" = 0
test ! -e /etc/systemd/system/awg-chain-managed-route.service || { echo 'Существующий PBR Managed'; exit 1; }
test ! -e /usr/local/sbin/awg-chain-managed-route || { echo 'Существующий маршрут Managed'; exit 1; }
! ip -4 rule show | grep -q '^90:' || { echo 'Конфликт ip rule 90'; exit 1; }
! ip -4 rule show | grep -q '^1100:' || { echo 'Конфликт ip rule 1100'; exit 1; }
if command -v docker >/dev/null 2>&1; then
  ! docker ps -a --format '{{.Names}}' | grep -Fxq amnezia-awg2 || { echo 'amnezia-awg2 уже установлен'; exit 1; }
  ! docker network ls --format '{{.Name}}' | grep -Fxq awg-chain-managed || { echo 'Сеть awg-chain-managed уже создана'; exit 1; }
  ! docker network ls --format '{{.Name}}' | grep -Fxq amnezia-dns-net || { echo 'Сеть Amnezia уже используется'; exit 1; }
fi
echo 'MANAGED_PREFLIGHT_OK'`
