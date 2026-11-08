package chain

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// NetworkPlan describes ALL IPv4 networks created by the managed installer.
// Changing a plan is supported only on fresh installations, never as an
// in-place migration of already provisioned Amnezia peers.
type NetworkPlan struct {
	Clients string // Amnezia user tunnel /24
	Hop     string // Inter-server point-to-point /30
	Docker  string // Dedicated managed egress bridge /24
	DNS     string // Amnezia service / DNS bridge /24
}

type NetworkPreset struct {
	Name string
	Plan NetworkPlan
}

func DefaultNetworkPlan() NetworkPlan {
	return NetworkPlan{
		Clients: "10.8.1.0/24",
		Hop: "10.77.77.0/30",
		Docker: "172.28.66.0/24",
		DNS: "172.29.172.0/24",
	}
}

// Each preset is disjoint within the four network allocations.
func NetworkPresets() []NetworkPreset {
	return []NetworkPreset{
		{"Стандартный", DefaultNetworkPlan()},
		{"Альтернатива 1", NetworkPlan{
			Clients: "10.88.10.0/24", Hop: "10.88.11.0/30",
			Docker: "172.26.40.0/24", DNS: "172.26.41.0/24",
		}},
		{"Альтернатива 2", NetworkPlan{
			Clients: "10.120.50.0/24", Hop: "10.120.51.0/30",
			Docker: "172.22.50.0/24", DNS: "172.22.51.0/24",
		}},
		{"Альтернатива 3", NetworkPlan{
			Clients: "192.168.240.0/24", Hop: "192.168.241.0/30",
			Docker: "172.25.80.0/24", DNS: "172.25.81.0/24",
		}},
	}
}

func (p NetworkPlan) IsZero() bool {
	return p.Clients == "" && p.Hop == "" && p.Docker == "" && p.DNS == ""
}

func (p NetworkPlan) OrDefault() NetworkPlan {
	if p.IsZero() { return DefaultNetworkPlan() }
	return p
}

func (p NetworkPlan) String() string {
	return fmt.Sprintf("Клиенты %s · Межсерверная %s · Docker %s · Сервисная %s",
		p.Clients, p.Hop, p.Docker, p.DNS)
}

func (p NetworkPlan) Validate() error {
	entries := []struct{ name, cidr string; bits int }{
		{"Клиенты", p.Clients, 24},
		{"Межсерверная сеть", p.Hop, 30},
		{"Docker", p.Docker, 24},
		{"Сервисная сеть", p.DNS, 24},
	}
	nets := make([]netip.Prefix, len(entries))
	for i, e := range entries {
		n, err := netip.ParsePrefix(strings.TrimSpace(e.cidr))
		if err != nil || e.cidr != strings.TrimSpace(e.cidr) ||
			!n.Addr().Is4() || n.Bits() != e.bits ||
			n != n.Masked() || !n.Addr().IsPrivate() {
			return fmt.Errorf("%s: укажи корректную частную IPv4-подсеть /%d, начиная с адреса сети (получено %q)", e.name, e.bits, e.cidr)
		}
		nets[i] = n
	}
	for i := range nets {
		for j := 0; j < i; j++ {
			if nets[i].Overlaps(nets[j]) {
				return fmt.Errorf("Подсети %s и %s пересекаются", entries[i].name, entries[j].name)
			}
		}
	}
	return nil
}

func hostAt(cidr string, number int) string {
	ip := netip.MustParsePrefix(cidr).Addr()
	for i := 0; i < number; i++ { ip = ip.Next() }
	return ip.String()
}
func (p NetworkPlan) ClientServer() string { return hostAt(p.Clients, 1) }
func (p NetworkPlan) HopEntry() string { return hostAt(p.Hop, 1) }
func (p NetworkPlan) HopExit() string { return hostAt(p.Hop, 2) }
func (p NetworkPlan) DockerGateway() string { return hostAt(p.Docker, 1) }
func (p NetworkPlan) DockerIP() string { return hostAt(p.Docker, 2) }
func (p NetworkPlan) DNSGateway() string { return hostAt(p.DNS, 1) }
func (p NetworkPlan) DNSIP() string { return hostAt(p.DNS, 2) }
func (p NetworkPlan) ClientBase() string { return netip.MustParsePrefix(p.Clients).Addr().String() }

// renderManagedScript replaces every IP literal in the field-tested CLI
// scripts in one nonrecursive pass. The plan is validated before use, so
// no user-supplied shell metacharacters can enter the remote scripts.
func renderManagedScript(script string, p NetworkPlan) string {
	d := DefaultNetworkPlan()
	return strings.NewReplacer(
		d.Clients, p.Clients,
		d.Hop, p.Hop,
		d.Docker, p.Docker,
		d.DNS, p.DNS,
		d.ClientBase(), p.ClientBase(),
		d.ClientServer(), p.ClientServer(),
		d.HopEntry(), p.HopEntry(),
		d.HopExit(), p.HopExit(),
		d.DockerGateway(), p.DockerGateway(),
		d.DockerIP(), p.DockerIP(),
		d.DNSGateway(), p.DNSGateway(),
		d.DNSIP(), p.DNSIP(),
	).Replace(script)
}

func ManagedEntryHopConfig(k Keys, exitIPv6 string, p NetworkPlan) string {
	return renderManagedScript(EntryHopConfig(k, exitIPv6), p)
}

func ManagedExitConfigForPlan(k Keys, entryIPv6, egressIF string, p NetworkPlan) string {
	return renderManagedScript(
		exitConfigWithNetwork(k, entryIPv6, egressIF, DefaultNetworkPlan().DockerIP()+"/32"), p,
	)
}

// checkNetworkAvailability guards against collisions before provisioning any
// containers or WG interfaces. Both VPS are inspected. Docker IPAM is included
// because stopped bridges may no longer have a route in the host route table.
// This cannot discover a LAN subnet behind a user's router: the GUI documents
// that additional check separately.
func checkNetworkAvailability(ctx context.Context, r *Remote, p NetworkPlan) error {
	if err := p.Validate(); err != nil { return err }
	script := fmt.Sprintf(`python3 - <<'PY'
import ipaddress, json, subprocess, sys
wanted = [ipaddress.ip_network(x) for x in [%q, %q, %q, %q]]
existing = []
def record(network, origin):
    try:
        n = ipaddress.ip_network(network, strict=False)
        if isinstance(n, ipaddress.IPv4Network) and n.prefixlen > 0:
            existing.append((n, origin))
    except (ValueError, TypeError):
        pass

for row in json.loads(subprocess.check_output(
        ['ip', '-4', '-j', 'route', 'show', 'table', 'all'], text=True)):
    record(row.get('dst', ''), 'route: ' + str(row.get('dev', row.get('table', 'unknown'))))
for row in json.loads(subprocess.check_output(['ip', '-4', '-j', 'address', 'show'], text=True)):
    for address in row.get('addr_info', []):
        if address.get('family') == 'inet':
            record(str(address['local']) + '/' + str(address['prefixlen']),
                   'interface: ' + row.get('ifname', 'unknown'))
try:
    ids = subprocess.check_output(['docker', 'network', 'ls', '-q'],
                                 stderr=subprocess.DEVNULL, text=True).split()
    if ids:
        networks = json.loads(subprocess.check_output(['docker', 'network', 'inspect', *ids],
                                                      stderr=subprocess.DEVNULL, text=True))
        for network in networks:
            for config in (network.get('IPAM') or {}).get('Config') or []:
                record(config.get('Subnet'), 'Docker network: ' + network.get('Name', 'unknown'))
except FileNotFoundError:
    pass
except subprocess.CalledProcessError as err:
    print('Не удалось проверить существующие сети Docker:', err, file=sys.stderr)
    sys.exit(1)
conflicts = [(str(a), str(b), where)
             for a in wanted for b, where in existing if a.overlaps(b)]
if conflicts:
    for a, b, where in conflicts:
        print('Конфликт подсетей: %s пересекается с %s (%s)' % (a, b, where),
              file=sys.stderr)
    sys.exit(1)
print('NETWORK_PLAN_AVAILABLE')
PY`, p.Clients, p.Hop, p.Docker, p.DNS)
	_, err := runWithTimeout(ctx, r, script, 35*time.Second)
	return err
}
