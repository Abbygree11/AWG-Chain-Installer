package chain

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		"AWG_CHAIN_DYNAMIC_IFACES_V1", "dev \"$data_if\"",
	} {
		if !strings.Contains(managedDockerSetup, required) {
			t.Errorf("Docker setup missing %q", required)
		}
	}
	for _, required := range []string{
		"ip -4 route replace default via 172.29.172.1 dev \"$reply_if\"",
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

func TestManagedBootstrapFindsBothDockerInterfaceOrders(t *testing.T) {
	if runtime.GOOS == "windows" { t.Skip("shell bootstrap test uses Bash") }
	if _, err := exec.LookPath("bash"); err != nil { t.Skip("Bash unavailable") }

	const header = "cat > /tmp/awg-chain-route-bootstrap <<'BOOTSTRAP'\n"
	start := strings.Index(managedDockerSetup, header)
	if start < 0 { t.Fatal("managed bootstrap not found") }
	start += len(header)
	end := strings.Index(managedDockerSetup[start:], "\nBOOTSTRAP")
	if end < 0 { t.Fatal("bootstrap closing marker missing") }
	script := managedDockerSetup[start:start+end]
	udpRoute := `ip -4 route replace default via 172.29.172.1 dev "$reply_if" src 172.29.172.2 table 101`
	if !strings.Contains(managedReplyRoute, udpRoute) {
		t.Fatal("UDP reply route must use the discovered interface")
	}

	for _, tc := range []struct{ name, dataIf, replyIf string }{
		{"original order", "eth0", "eth1"},
		{"Docker swapped order", "eth1", "eth0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fakeIP := filepath.Join(dir, "ip")
			prog := `#!/bin/sh
if [ "$1" = "-4" ] && [ "$2" = "-o" ] && [ "$3" = "addr" ]; then
  printf '%s\n' "$FAKE_ADDRS"
elif [ "$1" = "-4" ] && [ "$2" = "route" ]; then
  printf '%s\n' "$*" >> "$ROUTE_LOG"
else
  exit 9
fi
`
			if err := os.WriteFile(fakeIP, []byte(prog), 0755); err != nil { t.Fatal(err) }
			log := filepath.Join(dir, "routes.log")
			cmd := exec.Command("bash", "-eu", "-c", script+"\n"+udpRoute)
			cmd.Env = append(os.Environ(),
				"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"ROUTE_LOG="+log,
				"FAKE_ADDRS="+fmt.Sprintf("2: %s inet 172.28.66.2/24 scope global %s\n3: %s inet 172.29.172.2/24 scope global %s",
					tc.dataIf, tc.dataIf, tc.replyIf, tc.replyIf))
			if out, err := cmd.CombinedOutput(); err != nil { t.Fatalf("bootstrap: %v\n%s", err, out) }
			b, err := os.ReadFile(log)
			if err != nil { t.Fatal(err) }
			got := string(b)
			for _, want := range []string{
				"-4 route replace default via 172.28.66.1 dev "+tc.dataIf,
				"-4 route replace default via 172.29.172.1 dev "+tc.replyIf+" src 172.29.172.2 table 101",
			} {
				if !strings.Contains(got, want) { t.Errorf("expected route %q in %q", want, got) }
			}
		})
	}
}

func TestManagedBootstrapFailsClosedWithoutDockerServiceInterface(t *testing.T) {
	if runtime.GOOS == "windows" { t.Skip("shell bootstrap test uses Bash") }
	if _, err := exec.LookPath("bash"); err != nil { t.Skip("Bash unavailable") }
	const header = "cat > /tmp/awg-chain-route-bootstrap <<'BOOTSTRAP'\n"
	start := strings.Index(managedDockerSetup, header)
	if start < 0 { t.Fatal("bootstrap missing") }
	start += len(header)
	end := strings.Index(managedDockerSetup[start:], "\nBOOTSTRAP")
	if end < 0 { t.Fatal("bootstrap closing marker missing") }
	dir := t.TempDir()
	fakeIP := filepath.Join(dir, "ip")
	if err := os.WriteFile(fakeIP, []byte("#!/bin/sh\necho '2: eth0 inet 172.28.66.2/24 scope global eth0'\n"), 0755); err != nil { t.Fatal(err) }
	cmd := exec.Command("bash", "-eu", "-c", managedDockerSetup[start:start+end])
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("must fail closed without both Docker interfaces; output: %s", out)
	}
}
