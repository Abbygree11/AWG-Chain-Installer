package chain

import (
    "context"
    "strings"
    "testing"
)

func TestManagedNewInstallHasIdempotentTCPMSS(t *testing.T) {
    for _, required := range []string{
        "AWG_CHAIN_TCP_MSS_V1",
        "FORWARD -i \"$network\" -s \"$src\" -o awg-mediator -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1160",
        "FORWARD -i awg-mediator -o \"$network\" -d \"$src\" -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1160",
        "iptables -t mangle -C FORWARD",
        "iptables -t mangle -D FORWARD",
    } {
        if !strings.Contains(managedRoutingSetup, required) {
            t.Errorf("new install missing persistent TCP MSS rule %q", required)
        }
    }
    if strings.Count(managedRoutingSetup, "AWG_CHAIN_TCP_MSS_V1") != 1 {
        t.Fatal("MSS startup marker duplicated")
    }
}

func TestManagedExistingInstallHasNoDowntimeRepair(t *testing.T) {
    for _, required := range []string{
        "AWG_CHAIN_TCP_MSS_V1", "awg-chain-managed-route.service",
        "/usr/local/sbin/awg-chain-managed-route start",
        "--set-mss 1160", "src.count", "os.replace",
    } {
        if !strings.Contains(managedTCPMSSRepair, required) {
            t.Errorf("existing install repair missing %q", required)
        }
    }
    for _, forbidden := range []string{
        "docker restart", "systemctl restart", "awg-quick down",
        "docker rm", "iptables -F", "ip route flush",
    } {
        if strings.Contains(managedTCPMSSRepair, forbidden) {
            t.Errorf("repair must not disrupt running VPN: %s", forbidden)
        }
    }
}

func TestManagedPerformanceDiagnosticsAreReadOnly(t *testing.T) {
    for _, required := range []string{
        "time_namelookup", "time_connect",
        "time_appconnect", "time_starttransfer",
        "time_total", "Docker awg0",
    } {
        if !strings.Contains(managedPerformanceDiagnostics, required) {
            t.Errorf("diagnostic missing %q", required)
        }
    }
    for _, forbidden := range []string{
        "iptables -I", "iptables -A", "docker restart",
        "awg-quick down", "systemctl restart",
    } {
        if strings.Contains(managedPerformanceDiagnostics, forbidden) {
            t.Errorf("diagnostics must remain read-only: %q", forbidden)
        }
    }
}

func TestPerformanceServerValidation(t *testing.T) {
    s := Server{Host:"198.51.100.42",User:"root",Port:22,Password:"secret"}
    if err := validatePerformanceServer(s); err != nil {t.Fatal(err)}
    for _, broken := range []Server{
        {Host:"",User:"root",Port:22,Password:"secret"},
        {Host:"localhost",User:"root",Port:22,Password:"secret"},
        {Host:"198.51.100.42",User:"other",Port:22,Password:"secret"},
        {Host:"198.51.100.42",User:"root",Port:0,Password:"secret"},
        {Host:"198.51.100.42",User:"root",Port:22,Password:""},
    } {
        if err:=validatePerformanceServer(broken); err==nil {t.Errorf("accepted invalid SSH parameters")}
    }
}

func TestPerformanceHookSignature(t *testing.T) {
    var diag func(context.Context,Server,AskHostKey)(string,error)=DiagnoseManagedPerformance
    var fix func(context.Context,Server,AskHostKey)(string,error)=OptimizeManagedTCP
    if diag==nil||fix==nil {t.Fatal("missing managed performance API")}
}
