package chain

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Progress func(percent float64, title, detail string)

type Installer struct {
	AskHostKey AskHostKey
	Progress   Progress
}

type serverInfo struct {
	IPv6   string
	Egress string
}

func (in *Installer) notify(progress float64, title, detail string) {
	if in.Progress != nil { in.Progress(progress, title, detail) }
}

func (in *Installer) Run(ctx context.Context, o Options) (result string, retErr error) {
	if err := o.Validate(); err != nil { return "", err }
	var k Keys
	if err := NewAllKeys(&k); err != nil { return "", err }
	if err := k.Validate(); err != nil { return "", err }

	in.notify(0.02, "Подключение к серверам", "Подключаемся к входному VPS по SSH")
	entry, err := Dial(ctx, o.Entry, in.AskHostKey)
	if err != nil { return "", err }
	defer entry.Close()

	in.notify(0.08, "Подключение к серверам", "Подключаемся к выходному VPS по SSH")
	exit, err := Dial(ctx, o.Exit, in.AskHostKey)
	if err != nil { return "", err }
	defer exit.Close()

	in.notify(0.14, "Диагностика", "Проверяем входной сервер: Ubuntu, IPv6, правила, порты")
	entryInfo, err := inspect(ctx, entry, EntryPort, "awg-home.conf", "awg-fin.conf")
	if err != nil { return "", fmt.Errorf("входной VPS: %w", err) }

	in.notify(0.20, "Диагностика", "Проверяем выходной сервер: Ubuntu, IPv6, правила, порты")
	exitInfo, err := inspect(ctx, exit, ExitPort, "awg-spb.conf")
	if err != nil { return "", fmt.Errorf("выходной VPS: %w", err) }

	if entryInfo.IPv6 == exitInfo.IPv6 { return "", errors.New("у серверов совпадает публичный IPv6") }
	in.notify(0.24, "Диагностика", "Проверяем IPv6-маршрут до выходного сервера")
	if _, err := runWithTimeout(ctx, entry, "ip -6 route get "+exitInfo.IPv6+" >/dev/null", 30*time.Second); err != nil {
		return "", fmt.Errorf("нет IPv6-маршрута до выходного VPS: %w", err)
	}

	in.notify(0.29, "Установка AmneziaWG", "Устанавливаем подписанные пакеты на выходной VPS")
	if err := installPackages(ctx, exit); err != nil { return "", fmt.Errorf("выходной VPS: %w", err) }

	in.notify(0.43, "Установка AmneziaWG", "Устанавливаем подписанные пакеты на входной VPS")
	if err := installPackages(ctx, entry); err != nil { return "", fmt.Errorf("входной VPS: %w", err) }

	// From here on, a failed run should remove only the profiles created by
	// this installer. Package installation and sysctl changes are intentionally
	// retained, and an interrupted process may require manual recovery.
	staged := false
	defer func() {
		if retErr == nil || !staged { return }
		in.notify(0, "Откат", "Останавливаем созданные VPN и убираем их конфигурации")
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var cleanupErrors []string
		if _, err := entry.Run(cleanupCtx, `systemctl disable --now awg-quick@awg-home 2>/dev/null || true
systemctl disable --now awg-quick@awg-fin 2>/dev/null || true
ip -4 rule del priority 1000 iif awg-home lookup 100 2>/dev/null || true
ip -4 route flush table 100 2>/dev/null || true
rm -f /etc/amnezia/amneziawg/awg-home.conf /etc/amnezia/amneziawg/awg-home.conf.new /etc/amnezia/amneziawg/awg-fin.conf /etc/amnezia/amneziawg/awg-fin.conf.new`); err != nil {
			cleanupErrors = append(cleanupErrors, "входной VPS: "+err.Error())
		}
		if _, err := exit.Run(cleanupCtx, `systemctl disable --now awg-quick@awg-spb 2>/dev/null || true
rm -f /etc/amnezia/amneziawg/awg-spb.conf /etc/amnezia/amneziawg/awg-spb.conf.new`); err != nil {
			cleanupErrors = append(cleanupErrors, "выходной VPS: "+err.Error())
		}
		if len(cleanupErrors)>0 { retErr=fmt.Errorf("%w; не удалось полностью откатить: %s",retErr,strings.Join(cleanupErrors,"; ")) }
	}()

	in.notify(0.57, "Настройка сети", "Включаем IPv4-маршрутизацию на обоих серверах")
	for _, r := range []*Remote{exit, entry} {
		_, err := runWithTimeout(ctx, r, `printf 'net.ipv4.ip_forward = 1\n' > /etc/sysctl.d/90-awg-chain-forwarding.conf
sysctl -p /etc/sysctl.d/90-awg-chain-forwarding.conf >/dev/null
test "$(sysctl -n net.ipv4.ip_forward)" = 1`, 40*time.Second)
		if err != nil { return "", fmt.Errorf("включение IP forwarding: %w", err) }
	}

	staged = true
	in.notify(0.62, "Настройка VPN", "Записываем защищённые конфигурации AWG")
	// Files are saved with 0600 mode; only the application's own fixed paths are used.
	if err := exit.PutPrivateFile(ctx, "/etc/amnezia/amneziawg/awg-spb.conf", ExitConfig(k, entryInfo.IPv6, exitInfo.Egress)); err != nil {
		return "", err
	}
	if err := entry.PutPrivateFile(ctx, "/etc/amnezia/amneziawg/awg-fin.conf", EntryHopConfig(k, exitInfo.IPv6)); err != nil {
		return "", err
	}
	if err := entry.PutPrivateFile(ctx, "/etc/amnezia/amneziawg/awg-home.conf", EntryHomeConfig(k)); err != nil {
		return "", err
	}

	in.notify(0.71, "Запуск туннелей", "Запускаем выходной AWG на Финляндии")
	if _, err := runWithTimeout(ctx, exit, "systemctl enable --now awg-quick@awg-spb\nsystemctl is-active --quiet awg-quick@awg-spb", 60*time.Second); err != nil {
		return "", fmt.Errorf("запуск выходного AWG: %w", err)
	}

	in.notify(0.76, "Запуск туннелей", "Подключаем входной VPS к выходному через IPv6")
	if _, err := runWithTimeout(ctx, entry, "systemctl enable --now awg-quick@awg-fin\nsystemctl is-active --quiet awg-quick@awg-fin", 60*time.Second); err != nil {
		return "", fmt.Errorf("запуск AWG между VPS: %w", err)
	}

	in.notify(0.82, "Проверка связи", "Проверяем туннель, handshake и внешний IPv4")
	verify := `ping -c 3 -W 3 10.77.77.2 >/dev/null
ip -4 route get 1.1.1.1 | grep -q 'dev ` + entryInfo.Egress + `'
curl -4 -fsS --retry 2 --connect-timeout 8 --max-time 25 --interface 'if!awg-fin' https://api.ipify.org`
	got, err := runWithTimeout(ctx, entry, verify, 65*time.Second)
	if err != nil {
		return "", fmt.Errorf("проверка соединения между VPS: %w. Возможна блокировка UDP %d у провайдера", err, ExitPort)
	}
	if strings.TrimSpace(got) != o.Exit.Host {
		return "", fmt.Errorf("выходной IP %q, ожидается %s; установка остановлена", got, o.Exit.Host)
	}

	in.notify(0.89, "Настройка маршрутизации", "Активируем входной AWG, PBR, NAT и kill switch")
	if _, err := runWithTimeout(ctx, entry, `systemctl enable --now awg-quick@awg-home
systemctl is-active --quiet awg-quick@awg-home
ip -4 route get 1.1.1.1 from 10.86.66.2 iif awg-home | grep -q 'dev awg-fin'
ip -4 route show table 100 | grep -q 'unreachable default'
iptables -C FORWARD -i awg-home ! -o awg-fin -j REJECT`, 60*time.Second); err != nil {
		return "", fmt.Errorf("PBR или firewall: %w", err)
	}
	if _, err := runWithTimeout(ctx, exit, "ip -4 route get 10.86.66.2 | grep -q 'dev awg-spb'\niptables -t nat -C POSTROUTING -s 10.86.66.0/24 -o "+exitInfo.Egress+" -j MASQUERADE", 30*time.Second); err != nil {
		return "", fmt.Errorf("обратный маршрут или NAT: %w", err)
	}

	in.notify(0.96, "Сохранение", "Создаём импортируемый файл для Keenetic")
	path, err := saveClientConfig(o.OutputDir, o.Profile, KeeneticConfig(k, o.Entry.Host))
	if err != nil { return "", err }

	in.notify(1, "Готово", "Серверы проверены. Импортируй созданный .conf в Keenetic")
	return path, nil
}

func runWithTimeout(parent context.Context, r *Remote, script string, max time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(parent, max)
	defer cancel()
	return r.Run(ctx, script)
}

func inspect(ctx context.Context, r *Remote, udpPort int, ownedConfigs ...string) (serverInfo, error) {
	// Keep the initial release conservative: never replace existing AWG services,
	// non-default firewall policies, or unrelated PBR rules.
	var checks strings.Builder
	for _, name := range ownedConfigs {
		checks.WriteString("test ! -e /etc/amnezia/amneziawg/" + name + " || { echo 'Уже существует профиль " + name + "'; exit 1; }\n")
	}
	script := fmt.Sprintf(`
. /etc/os-release
test "$ID" = ubuntu && test "$VERSION_ID" = 24.04 || { echo "Поддерживается только Ubuntu 24.04"; exit 1; }
test "$(id -u)" = 0 || { echo "Требуется root"; exit 1; }
test "$(uname -m)" = x86_64 || { echo "Поддерживается x86_64"; exit 1; }
uname -r | grep -Eq '^6\.8\.' || { echo "Нужно штатное GA-ядро 6.8 Ubuntu 24.04"; exit 1; }
%s
if command -v ufw >/dev/null && ufw status | grep -q 'Status: active'; then echo "Активен UFW: требуется отдельная настройка firewall"; exit 1; fi
if command -v iptables >/dev/null; then
  iptables -S INPUT | grep -q -- '^-P INPUT ACCEPT$' || { echo "INPUT firewall не ACCEPT"; exit 1; }
  iptables -S FORWARD | grep -q -- '^-P FORWARD ACCEPT$' || { echo "FORWARD firewall не ACCEPT"; exit 1; }
fi
if command -v ip6tables >/dev/null; then
  ip6tables -S INPUT | grep -q -- '^-P INPUT ACCEPT$' || { echo "IPv6 INPUT firewall не ACCEPT"; exit 1; }
fi
if ip -4 rule show | grep -Eq '^1000:|lookup 100$'; then echo "Конфликт PBR"; exit 1; fi
if ip -4 route show table 100 | grep -q .; then echo "Таблица 100 уже используется"; exit 1; fi
if ip -4 route show | grep -Eq '10\.(77\.77|86\.66)\.'; then echo "Конфликт VPN-подсетей"; exit 1; fi
if ss -H -lun | grep -Eq ':%d[[:space:]]'; then echo "UDP-порт %d занят"; exit 1; fi
v6=$(ip -6 route get 2606:4700:4700::1111 | sed -n 's/.* src \([^ ]*\).*/\1/p' | head -n 1)
egress=$(ip -4 route get 1.1.1.1 | sed -n 's/.* dev \([^ ]*\).*/\1/p' | awk '{print $1}' | head -n 1)
test -n "$v6" && test -n "$egress" || { echo "Не обнаружены публичный IPv6 или IPv4-интерфейс"; exit 1; }
printf 'IPv6=%%s\nEGRESS=%%s\n' "$v6" "$egress"
`, checks.String(), udpPort, udpPort)
	out, err := runWithTimeout(ctx, r, script, 45*time.Second)
	if err != nil { return serverInfo{}, err }
	info := serverInfo{}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "IPv6=") { info.IPv6 = strings.TrimPrefix(line,"IPv6=") }
		if strings.HasPrefix(line, "EGRESS=") { info.Egress = strings.TrimPrefix(line,"EGRESS=") }
	}
	ip, err := netip.ParseAddr(info.IPv6)
	if err != nil || !ip.Is6() || !ip.IsGlobalUnicast() { return serverInfo{}, errors.New("IPv6-адрес сервера не является публичным") }
	if !regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`).MatchString(info.Egress) { return serverInfo{}, errors.New("необычное имя выходного сетевого интерфейса") }
	return info, nil
}

func installPackages(ctx context.Context, r *Remote) error {
	const script = `
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a
apt-get -o Acquire::Retries=3 update -qq
apt-get -o Acquire::Retries=3 install -y -qq ca-certificates curl gnupg dkms build-essential iptables iproute2 iputils-ping linux-headers-$(uname -r)
if ! command -v awg >/dev/null || ! modinfo -F version amneziawg 2>/dev/null | grep -q '^3\.1\.'; then
    install -d -m 755 /etc/apt/keyrings
    url='https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x75C9DD72C799870E310542E24166F2C257290828'
    if ! curl -4 -fLsS --retry 3 --connect-timeout 10 --max-time 45 "$url" -o /tmp/awg-chain-ppa.asc; then
        curl -fLsS --retry 3 --connect-timeout 10 --max-time 45 "$url" -o /tmp/awg-chain-ppa.asc
    fi
    fpr=$(gpg --batch --show-keys --with-colons /tmp/awg-chain-ppa.asc | awk -F: '$1=="fpr" {print $10; exit}')
    test "$fpr" = "75C9DD72C799870E310542E24166F2C257290828" || { echo "Неверный отпечаток PPA Amnezia"; exit 1; }
    gpg --batch --yes --dearmor -o /etc/apt/keyrings/awg-chain-amnezia.gpg /tmp/awg-chain-ppa.asc
    chmod 644 /etc/apt/keyrings/awg-chain-amnezia.gpg
    cat > /etc/apt/sources.list.d/awg-chain-amnezia.sources <<'EOF'
Types: deb
URIs: https://ppa.launchpadcontent.net/amnezia/ppa/ubuntu/
Suites: noble
Components: main
Signed-By: /etc/apt/keyrings/awg-chain-amnezia.gpg
EOF
    apt-get -o Acquire::ForceIPv4=true -o Acquire::Retries=3 update -qq
    apt-get -o Acquire::ForceIPv4=true -o Acquire::Retries=3 install -y -qq amneziawg-dkms amneziawg-tools
    rm -f /tmp/awg-chain-ppa.asc
fi
modprobe amneziawg
awg --version | grep -q 'v3\.1\.'
modinfo -F version amneziawg | grep -q '^3\.1\.'
`
	_, err := runWithTimeout(ctx, r, script, 14*time.Minute)
	return err
}

func saveClientConfig(dir, profile, data string) (string,error) {
	if err := os.MkdirAll(dir,0700); err!=nil { return "",err }
	name := FileName(profile)
	for i:=0; i<100; i++ {
		n := name
		if i>0 { n = strings.TrimSuffix(name,".conf")+"-"+strconv.Itoa(i+1)+".conf" }
		path := filepath.Join(dir,n)
		f,err := os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
		if os.IsExist(err) { continue }
		if err!=nil { return "",err }
		_,err=f.WriteString(data)
		closeErr:=f.Close()
		if err==nil {err=closeErr}
		if err!=nil { _=os.Remove(path);return "",err }
		return path,nil
	}
	return "",errors.New("слишком много профилей с одинаковым именем; выбери другую папку")
}
