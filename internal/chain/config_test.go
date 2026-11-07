package chain

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestConfigsContainMatchingTunnelParameters(t *testing.T) {
	k := Keys{}
	for i, field := range []*string{&k.EntryHomePrivate,&k.EntryHomePublic,&k.EntryHopPrivate,&k.EntryHopPublic,&k.ExitPrivate,&k.ExitPublic} {
		data := make([]byte, 32)
		data[0] = byte(i + 1)
		*field = base64.StdEncoding.EncodeToString(data)
	}
	if err := NewClientKeys(&k); err != nil { t.Fatal(err) }
	if err := k.Validate(); err != nil { t.Fatal(err) }

	home := EntryHomeConfig(k)
	hop := EntryHopConfig(k, "2001:db8::2")
	exit := ExitConfig(k, "2001:db8::1", "enp0s3")
	client := KeeneticConfig(k,"203.0.113.10")

	for _, pair := range [][2]string{{home,client},{hop,exit}} {
		for _, s := range []string{"HeaderProtectionKey = ", "ContentPaddingAddition = 16-32","S4 = 16"} {
			if !strings.Contains(pair[0],s)||!strings.Contains(pair[1],s) { t.Fatalf("missing %q",s) }
		}
	}
	if !strings.Contains(hop,"Table = off") || !strings.Contains(hop,"[2001:db8::2]:51820") { t.Fatal("IPv6 route policy lost") }
	if !strings.Contains(exit,"AllowedIPs = 10.77.77.1/32, 10.86.66.0/24") { t.Fatal("return route missing") }
	if !strings.Contains(home,"lookup 100") || !strings.Contains(home,"! -o awg-mediator") { t.Fatal("kill switch missing") }
	if !strings.Contains(client,"AllowedIPs = 0.0.0.0/0") { t.Fatal("client AllowedIPs missing") }
	if strings.Contains(client,k.EntryHomePrivate) { t.Fatal("server private key leaked into client config") }
}

func TestInvalidServerCredentials(t *testing.T) {
	o := Options{Entry:Server{Host:"203.0.113.10",User:"root",Password:"a",Port:22},Exit:Server{Host:"198.51.100.5",User:"root",Password:"b",Port:22},Profile:"vpn",OutputDir:"/tmp"}
	if err := o.Validate(); err!=nil { t.Fatal(err) }
	o.Entry.Password=""
	if err := o.Validate(); err==nil { t.Fatal("password must be provided") }
}

func TestRandomClientKeyPair(t *testing.T) {
	private, public, err := GeneratePrivatePublic()
	if err!=nil { t.Fatal(err) }
	b1,e1 := base64.StdEncoding.DecodeString(private); b2,e2:=base64.StdEncoding.DecodeString(public)
	if e1!=nil||e2!=nil||len(b1)!=32||len(b2)!=32 { t.Fatal("bad key encoding") }
}
