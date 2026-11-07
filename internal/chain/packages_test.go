package chain

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestNativeAWGInstallScriptSyntax(t *testing.T) {
	if runtime.GOOS == "windows" { t.Skip("Bash is not required on Windows clients") }
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(nativeAWGInstallScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install script invalid: %v\n%s", err, out)
	}
}

func TestNativeAWGInstallScriptSecurity(t *testing.T) {
	for _, required := range []string{
		"Signed-By:", "keyring", "fingerprint",
		"ppa.launchpadcontent.net", "ppa.launchpad.net",
		"Acquire::ForceIPv6=true", "Acquire::ForceIPv4=true",
		"APT::Update::Error-Mode=any",
		"amneziawg-dkms", "amneziawg-tools",
	} {
		if !strings.Contains(nativeAWGInstallScript, required) {
			t.Fatalf("missing installation security/fallback step: %s", required)
		}
	}
	for _, forbidden := range []string{
		"allow-unauthenticated", "trusted=yes", "--insecure", "Acquire::AllowInsecureRepositories",
	} {
		if strings.Contains(nativeAWGInstallScript, forbidden) {
			t.Fatalf("insecure installation option: %s", forbidden)
		}
	}
}
