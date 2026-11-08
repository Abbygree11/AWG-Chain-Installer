package chain

import (
	"strings"
	"testing"
)

func TestNetworkPresetsAreValidAndDisjoint(t *testing.T) {
	presets := NetworkPresets()
	if len(presets) < 3 { t.Fatalf("expected several choices, got %d", len(presets)) }
	for _, choice := range presets {
		if err := choice.Plan.Validate(); err != nil {
			t.Errorf("%s: %v", choice.Name, err)
		}
	}
	if presets[0].Plan != DefaultNetworkPlan() {
		t.Error("the old tested network plan must remain the first preset")
	}
}

func TestCustomNetworkPlanValidation(t *testing.T) {
	cases := []struct{ name string; plan NetworkPlan }{
		{"invalid character", NetworkPlan{Clients:"10.8.1.0/24;touch /tmp/oops",Hop:"10.77.77.0/30",Docker:"172.28.66.0/24",DNS:"172.29.172.0/24"}},
		{"public IP", NetworkPlan{Clients:"8.8.8.0/24",Hop:"10.77.77.0/30",Docker:"172.28.66.0/24",DNS:"172.29.172.0/24"}},
		{"host instead of network", NetworkPlan{Clients:"10.8.1.42/24",Hop:"10.77.77.0/30",Docker:"172.28.66.0/24",DNS:"172.29.172.0/24"}},
		{"wrong client prefix", NetworkPlan{Clients:"10.8.1.0/25",Hop:"10.77.77.0/30",Docker:"172.28.66.0/24",DNS:"172.29.172.0/24"}},
		{"wrong hop prefix", NetworkPlan{Clients:"10.8.1.0/24",Hop:"10.77.77.0/24",Docker:"172.28.66.0/24",DNS:"172.29.172.0/24"}},
		{"overlapping Docker networks", NetworkPlan{Clients:"10.8.1.0/24",Hop:"10.77.77.0/30",Docker:"172.28.66.0/24",DNS:"172.28.66.0/24"}},
		{"overlapping peer networks", NetworkPlan{Clients:"10.8.1.0/24",Hop:"10.8.1.0/30",Docker:"172.28.66.0/24",DNS:"172.29.172.0/24"}},
		{"leading whitespace", NetworkPlan{Clients:" 10.8.1.0/24",Hop:"10.77.77.0/30",Docker:"172.28.66.0/24",DNS:"172.29.172.0/24"}},
		{"empty field", NetworkPlan{Clients:"",Hop:"10.77.77.0/30",Docker:"172.28.66.0/24",DNS:"172.29.172.0/24"}},
	}
	for _, tc := range cases {
		t.Run(tc.name,func(t *testing.T) {
			if err:=tc.plan.Validate(); err==nil {t.Fatal("must reject invalid or overlapping network plan")}
		})
	}
}

func TestRenderedManagedScriptsAndConfigsHaveOnlySelectedAddresses(t *testing.T) {
	var keys Keys
	if err:=NewAllKeys(&keys); err!=nil {t.Fatal(err)}
	original := DefaultNetworkPlan()
	for _, choice := range NetworkPresets() {
		p:=choice.Plan
		t.Run(choice.Name,func(t *testing.T){
			server:=ManagedEntryHopConfig(keys,"2001:db8::2",p)
			exit:=ManagedExitConfigForPlan(keys,"2001:db8::1","eth9",p)
			docker:=renderManagedScript(managedDockerSetup,p)
			awg:=renderManagedScript(managedConfigSetup,p)
			reply:=renderManagedScript(managedReplyRoute,p)
			routing:=renderManagedScript(managedRoutingSetup,p)
			for _, item := range []struct{content,expected string}{
				{server,p.HopEntry()+"/30"},
				{server,"Address = "+p.HopEntry()+"/30"},
				{server,"ip -4 route replace default dev awg-mediator table 100"},
				{exit,"AllowedIPs = "+p.HopEntry()+"/32, "+p.DockerIP()+"/32"},
				{exit,"-s "+p.DockerIP()+"/32 -o eth9 -j MASQUERADE"},
				{docker,"--subnet "+p.Docker},
				{docker,"--gateway "+p.DockerGateway()},
				{docker,"--ip "+p.DockerIP()},
				{docker,"--subnet "+p.DNS},
				{docker,p.ClientBase()},
				{awg,"Address = "+p.ClientServer()+"/24"},
				{reply,"default via "+p.DNSGateway()+" dev eth1 src "+p.DNSIP()+" table 101"},
				{reply,"==\""+p.DNSIP()+"\""},
				{routing,"src="+p.DockerIP()+"/32"},
				{routing,"from "+p.DockerIP()+" iif awc0"},
			} {
				if !strings.Contains(item.content,item.expected) {
					t.Errorf("missing %q in rendered script/config",item.expected)
				}
			}
			if p != original {
				for _, template := range []struct{content,old string}{
					{server,original.HopEntry()}, {exit,original.DockerIP()},
					{docker,original.ClientBase()}, {docker,original.DockerGateway()},
					{awg,original.ClientServer()}, {reply,original.DNSGateway()}, {reply,original.DNSIP()},
					{routing,original.DockerIP()},
				} {
					if strings.Contains(template.content,template.old) {
						t.Errorf("stale default address %q",template.old)
					}
				}
			}
		})
	}
}

func TestNetworkPlanZeroOnlyUsesDefaultWhenCompletelyEmpty(t *testing.T){
	if DefaultNetworkPlan().OrDefault() != DefaultNetworkPlan() { t.Fatal("default changed") }
	if (NetworkPlan{}).OrDefault() != DefaultNetworkPlan() {t.Fatal("zero should use default") }
	partial := NetworkPlan{Clients:"10.8.1.0/24"}
	if partial.OrDefault() != partial {t.Fatal("partial user plan must be rejected, never silently defaulted") }
	if err:=partial.Validate();err==nil {t.Fatal("partial plan accepted") }
}
