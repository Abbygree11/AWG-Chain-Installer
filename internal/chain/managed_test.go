package chain

import (
	"strings"
	"testing"
)

func TestManagedExitConfigRoutesDockerNetwork(t *testing.T) {
	var k Keys
	if err := NewAllKeys(&k); err != nil { t.Fatal(err) }
	conf := ManagedExitConfig(k, "2001:db8::1234", "eth9")
	if !strings.Contains(conf, "AllowedIPs = 10.77.77.1/32, 172.28.66.2/32") {
		t.Fatal("managed exit must allow the Docker subnet through AWG")
	}
	if !strings.Contains(conf, "-s 172.28.66.2/32 -o eth9 -j MASQUERADE") {
		t.Fatal("managed exit must NAT Docker client traffic")
	}
	if strings.Contains(conf, "10.86.66.0/24") {
		t.Fatal("managed exit must not contain legacy Keenetic client subnet")
	}
}

func TestManagedScriptsPreserveSplitRouting(t *testing.T) {
	for _, required := range []string{
		"amnezia-awg2", "amnezia-dns-net", "awg-chain-managed",
		"-p 55424:55424/udp", "172.28.66.2",
	} {
		if !strings.Contains(managedDockerSetup, required) {
			t.Errorf("Docker setup missing %q", required)
		}
	}
	for _, required := range []string{
		"ip -4 route replace default via 172.29.172.1 dev eth1",
		"ipproto udp sport 55424 lookup 101",
		"AWG_CHAIN_UDP_REPLY_V1",
	} {
		if !strings.Contains(managedReplyRoute, required) {
			t.Errorf("UDP reply repair missing %q", required)
		}
	}
	for _, required := range []string{
		"systemctl enable --now awg-chain-managed-route.service",
		"fwmark", "awg-mediator", "AWG_CHAIN_MNG",
	} {
		if !strings.Contains(managedRoutingSetup, required) {
			t.Errorf("PBR setup missing %q", required)
		}
	}
	for _, s := range []string{managedDockerSetup, managedRoutingSetup, managedConfigSetup} {
		if strings.Contains(s, "systemctl enable --now awg-quick@awg-client") {
			t.Fatal("managed install must not start native awg-client")
		}
	}
}

func TestManagedValidatesSSHWithoutLocalProfile(t *testing.T) {
	o := Options{
		Entry: Server{Host:"203.0.113.10", User:"root", Password:"password", Port:22},
		Exit:  Server{Host:"198.51.100.10", User:"root", Password:"password", Port:22},
	}
	if err := o.ValidateServers(); err != nil { t.Fatal(err) }
	if err := o.Validate(); err == nil { t.Fatal("legacy .conf mode should still require output path") }
}

func TestManagedInventorySummary(t *testing.T) {
	inv := ServerInventory{Kind:KindMixed, Components:[]Component{
		{Name:"awg-mediator", Running:true, Kind:"native"},
		{Name:"amnezia-awg2", Running:true, Kind:"managed"},
	}}
	if !strings.Contains(inv.Summary(), "штатная двухсерверная") {
		t.Fatalf("unexpected managed inventory summary: %s", inv.Summary())
	}
}
