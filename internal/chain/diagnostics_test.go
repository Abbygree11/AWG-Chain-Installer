package chain

import (
	"strings"
	"testing"
)

func TestParseInventory(t *testing.T) {
	for _,tc :=range []struct{
		name string
		output string
		kind InstallationKind
		running int
	}{
		{"empty","",KindEmpty,0},
		{"native old","native|awg-home|active\nnative|awg-fin|active",KindStandalone,2},
		{"native new","native|awg-client|active\nnative|awg-mediator|inactive",KindStandalone,1},
		{"managed","managed|amnezia-awg2|active",KindManaged,1},
		{"mixed","native|awg-home|active\nmanaged|amnezia-awg2|inactive",KindMixed,1},
	}{
		t.Run(tc.name,func(t *testing.T){
			inv,err:=ParseInventory(tc.output)
			if err!=nil {t.Fatal(err)}
			if inv.Kind!=tc.kind {t.Fatalf("got %s want %s",inv.Kind,tc.kind)}
			n:=0
			for _,c:=range inv.Components {if c.Running {n++}}
			if n!=tc.running {t.Fatalf("running: got %d want %d",n,tc.running)}
		})
	}
}

func TestParseInventoryRejectsUnknownOrSensitiveInput(t *testing.T){
	for _,s:=range []string{
		"native|awg-rogue|active",
		"managed|other|active",
		"native|awg-home|UNKNOWN",
		"PrivateKey = REDACTED",
		"native|awg-home|active|extra",
	} {
		if _,err:=ParseInventory(s);err==nil {t.Fatal("expected error for "+s)}
	}
}

func TestInventorySummaryDoesNotContainSecrets(t *testing.T) {
	i,err:=ParseInventory("native|awg-home|active")
	if err!=nil {t.Fatal(err)}
	if strings.Contains(i.Summary(),"PrivateKey") {t.Fatal("private key mentioned")}
}
