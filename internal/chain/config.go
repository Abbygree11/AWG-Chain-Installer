package chain

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"
)

const (
	HomeNetwork = "10.86.66.0/24"
	HomeServer  = "10.86.66.1"
	HomeClient  = "10.86.66.2"
	HopNetwork  = "10.77.77.0/30"
	HopEntry    = "10.77.77.1"
	HopExit     = "10.77.77.2"
	ExitPort    = 51820
	EntryPort   = 51821
)

type Server struct {
	Host     string
	User     string
	Password string
	Port     int
}

type Options struct {
	Entry      Server
	Exit       Server
	Profile    string
	OutputDir  string
}

var safeUser = regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)

func (o Options) Validate() error {
	if o.Entry.Host == o.Exit.Host {
		return errors.New("адреса VPS должны отличаться")
	}
	for _, s := range []Server{o.Entry, o.Exit} {
		ip, err := netip.ParseAddr(strings.TrimSpace(s.Host))
		if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() {
			return fmt.Errorf("нужен корректный публичный IPv4 сервера, получено %q", s.Host)
		}
		if s.Port < 1 || s.Port > 65535 {
			return fmt.Errorf("неверный SSH-порт сервера %s", s.Host)
		}
		if s.User != "root" {
			return errors.New("в первой версии поддерживается только SSH-пользователь root (безопаснее явно отклонить sudo-конфигурацию)")
		}
		if !safeUser.MatchString(s.User) {
			return errors.New("некорректное имя SSH-пользователя")
		}
		if s.Password == "" {
			return fmt.Errorf("не указан SSH-пароль сервера %s", s.Host)
		}
	}
	if strings.TrimSpace(o.OutputDir) == "" {
		return errors.New("выбери папку для сохранения")
	}
	if strings.TrimSpace(o.Profile) == "" {
		return errors.New("укажи название подключения")
	}
	return nil
}

func GeneratePrivatePublic() (private, public string, err error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(key.Bytes()), base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

func RandomKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b[:]), nil
}

type Keys struct {
	EntryHomePrivate string
	EntryHomePublic  string
	EntryHopPrivate  string
	EntryHopPublic   string
	ExitPrivate      string
	ExitPublic       string
	ClientPrivate    string
	ClientPublic     string
	HomeHeader       string
	HopHeader        string
	HomeMagic        [4]uint32
	HopMagic         [4]uint32
}

func NewAllKeys(k *Keys) error {
	var err error
	k.EntryHomePrivate, k.EntryHomePublic, err = GeneratePrivatePublic()
	if err != nil { return err }
	k.EntryHopPrivate, k.EntryHopPublic, err = GeneratePrivatePublic()
	if err != nil { return err }
	k.ExitPrivate, k.ExitPublic, err = GeneratePrivatePublic()
	if err != nil { return err }
	return NewClientKeys(k)
}

func NewClientKeys(k *Keys) error {
	var err error
	k.ClientPrivate, k.ClientPublic, err = GeneratePrivatePublic()
	if err != nil { return err }
	k.HomeHeader, err = RandomKey()
	if err != nil { return err }
	k.HopHeader, err = RandomKey()
	if err != nil { return err }
	k.HomeMagic, err = generateMagic()
	if err != nil { return err }
	k.HopMagic, err = generateMagic()
	return err
}

func generateMagic() ([4]uint32, error) {
	var m [4]uint32
	for i := 0; i < 4; i++ {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil { return m, err }
		// Positive, unique header values; avoid reserved zero and small literals.
		v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		v = (v & 0x7fffffff) + 1000
		for j := 0; j < i; j++ {
			if m[j] == v { i--; break }
		}
		if m[i] == 0 { m[i] = v }
	}
	return m, nil
}

func validKey(s string) bool {
	data, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(data) == 32
}

func (k Keys) Validate() error {
	for _, s := range []string{k.EntryHomePrivate, k.EntryHomePublic, k.EntryHopPrivate, k.EntryHopPublic, k.ExitPrivate, k.ExitPublic, k.ClientPrivate, k.ClientPublic, k.HomeHeader, k.HopHeader} {
		if !validKey(s) { return errors.New("неверный формат 32-байтового ключа AWG") }
	}
	return nil
}

func obfuscation(header string, magic [4]uint32) string {
	return fmt.Sprintf(`Jc = 4
Jmin = 40
Jmax = 160
S1 = 64
S2 = 48
S3 = 24
S4 = 16
H1 = %d
H2 = %d
H3 = %d
H4 = %d
HeaderProtectionKey = %s
ContentPaddingAddition = 16-32`, magic[0], magic[1], magic[2], magic[3], header)
}

func EntryHomeConfig(k Keys) string {
	return fmt.Sprintf(`[Interface]
Address = %s/24
ListenPort = %d
PrivateKey = %s
MTU = 1200
%s

PostUp = ip -4 route replace unreachable default table 100 metric 10000
PostUp = ip -4 rule show | grep -Eq '^1000:.*iif awg-home.*lookup 100' || ip -4 rule add priority 1000 iif awg-home lookup 100
PostUp = iptables -w -C FORWARD -i awg-home ! -o awg-fin -j REJECT 2>/dev/null || iptables -w -I FORWARD 1 -i awg-home ! -o awg-fin -j REJECT
PostUp = ip6tables -w -C INPUT -p udp --dport %d -j DROP 2>/dev/null || ip6tables -w -I INPUT 1 -p udp --dport %d -j DROP
PostDown = ip -4 rule del priority 1000 iif awg-home lookup 100 2>/dev/null || true
PostDown = iptables -w -D FORWARD -i awg-home ! -o awg-fin -j REJECT 2>/dev/null || true
PostDown = ip6tables -w -D INPUT -p udp --dport %d -j DROP 2>/dev/null || true

[Peer]
PublicKey = %s
AllowedIPs = %s/32
`, HomeServer, EntryPort, k.EntryHomePrivate, obfuscation(k.HomeHeader, k.HomeMagic), EntryPort, EntryPort, EntryPort, k.ClientPublic, HomeClient)
}

func EntryHopConfig(k Keys, exitIPv6 string) string {
	return fmt.Sprintf(`[Interface]
Address = %s/30
PrivateKey = %s
MTU = 1200
Table = off
%s

PostUp = ip -4 route replace default dev awg-fin table 100 metric 100

[Peer]
PublicKey = %s
Endpoint = [%s]:%d
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
`, HopEntry, k.EntryHopPrivate, obfuscation(k.HopHeader, k.HopMagic), k.ExitPublic, exitIPv6, ExitPort)
}

func ExitConfig(k Keys, entryIPv6, egressIF string) string {
	config := fmt.Sprintf(`[Interface]
Address = %s/30
ListenPort = %d
PrivateKey = %s
MTU = 1200
%s

PostUp = iptables -w -C FORWARD -i awg-spb -o enp0s3 -j ACCEPT 2>/dev/null || iptables -w -I FORWARD 1 -i awg-spb -o enp0s3 -j ACCEPT
PostUp = iptables -t nat -C POSTROUTING -s %s/32 -o enp0s3 -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s %s/32 -o enp0s3 -j MASQUERADE
PostUp = iptables -t nat -C POSTROUTING -s %s -o enp0s3 -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s %s -o enp0s3 -j MASQUERADE
PostUp = iptables -w -C INPUT -p udp --dport %d -j DROP 2>/dev/null || iptables -w -I INPUT 1 -p udp --dport %d -j DROP
PostUp = ip6tables -w -C INPUT -p udp --dport %d ! -s %s/128 -j DROP 2>/dev/null || ip6tables -w -I INPUT 1 -p udp --dport %d ! -s %s/128 -j DROP
PostDown = iptables -w -D FORWARD -i awg-spb -o enp0s3 -j ACCEPT 2>/dev/null || true
PostDown = iptables -t nat -D POSTROUTING -s %s/32 -o enp0s3 -j MASQUERADE 2>/dev/null || true
PostDown = iptables -t nat -D POSTROUTING -s %s -o enp0s3 -j MASQUERADE 2>/dev/null || true
PostDown = iptables -w -D INPUT -p udp --dport %d -j DROP 2>/dev/null || true
PostDown = ip6tables -w -D INPUT -p udp --dport %d ! -s %s/128 -j DROP 2>/dev/null || true

[Peer]
PublicKey = %s
AllowedIPs = %s/32, %s
`, HopExit, ExitPort, k.ExitPrivate, obfuscation(k.HopHeader, k.HopMagic),
		HopEntry, HopEntry, HomeNetwork, HomeNetwork,
		ExitPort, ExitPort, ExitPort, entryIPv6, ExitPort, entryIPv6,
		HopEntry, HomeNetwork, ExitPort, ExitPort, entryIPv6,
		k.EntryHopPublic, HopEntry, HomeNetwork)
	return strings.ReplaceAll(config, "enp0s3", egressIF)
}

func KeeneticConfig(k Keys, entryIPv4 string) string {
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32
MTU = 1200
%s

[Peer]
PublicKey = %s
Endpoint = %s:%d
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
`, k.ClientPrivate, HomeClient, obfuscation(k.HomeHeader, k.HomeMagic), k.EntryHomePublic, entryIPv4, EntryPort)
}

// Example used in UI for a human-readable profile name, not in server config.
func FileName(profile string) string {
	clean := regexp.MustCompile(`[^a-zA-Z0-9_-]+`).ReplaceAllString(strings.TrimSpace(profile), "-")
	clean = strings.Trim(clean, "-")
	if clean == "" { clean = "keenetic" }
	if len(clean) > 40 { clean = clean[:40] }
	return clean + ".conf"
}

func SSHAddress(s Server) string { return net.JoinHostPort(s.Host, fmt.Sprint(s.Port)) }
