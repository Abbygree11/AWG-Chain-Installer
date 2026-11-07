package chain

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// AskHostKey is called only on the first SSH connection to an unknown server.
// Returning false rejects its key. Changed keys are always rejected.
type AskHostKey func(address, fingerprint string) bool

type Remote struct {
	client *ssh.Client
}

var hostKeysMu sync.Mutex

func Dial(ctx context.Context, server Server, ask AskHostKey) (*Remote, error) {
	callback, err := hostKeyCallback(ask)
	if err != nil {
		return nil, err
	}
	addr := SSHAddress(server)
	conn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("SSH %s: %w", addr, err)
	}
	cfg := &ssh.ClientConfig{
		User: server.User,
		Auth: []ssh.AuthMethod{ssh.Password(server.Password)},
		HostKeyCallback: callback,
		Timeout: 20 * time.Second,
	}
	clientConn, chans, requests, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH %s: %w", addr, err)
	}
	return &Remote{client: ssh.NewClient(clientConn, chans, requests)}, nil
}

func hostKeyCallback(ask AskHostKey) (ssh.HostKeyCallback, error) {
	dir, err := os.UserConfigDir()
	if err != nil { return nil, err }
	dir = filepath.Join(dir, "AWG-Chain-Installer")
	if err := os.MkdirAll(dir, 0700); err != nil { return nil, err }
	path := filepath.Join(dir, "known_hosts")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil { return nil, err }
	_ = f.Close()

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		hostKeysMu.Lock()
		defer hostKeysMu.Unlock()
		verify, err := knownhosts.New(path)
		if err != nil { return err }
		err = verify(hostname, remote, key)
		if err == nil { return nil }
		var mismatch *knownhosts.KeyError
		if !errors.As(err, &mismatch) || len(mismatch.Want) != 0 {
			return fmt.Errorf("SSH-ключ сервера %s изменился: %w; проверь сервер вручную", hostname, err)
		}
		if ask == nil || !ask(hostname, ssh.FingerprintSHA256(key)) {
			return fmt.Errorf("ключ SSH-сервера %s не подтверждён", hostname)
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil { return err }
		_, err = fmt.Fprintln(f, knownhosts.Line([]string{hostname}, key))
		closeErr := f.Close()
		if err != nil { return err }
		return closeErr
	}, nil
}

func (r *Remote) Close() error { return r.client.Close() }

// Run never logs or prints the script. Output is used for diagnostics and must
// never include secrets from the remote shell commands.
func (r *Remote) Run(ctx context.Context, script string) (string, error) {
	sess, err := r.client.NewSession()
	if err != nil { return "", err }
	defer sess.Close()
	sess.Stdin = strings.NewReader("set -euo pipefail\n" + script + "\n")
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done(): _ = sess.Close()
		case <-done:
		}
	}()
	output, runErr := sess.CombinedOutput("bash -se")
	close(done)
	if err := ctx.Err(); err != nil { return "", err }
	s := strings.TrimSpace(string(output))
	if runErr != nil {
		if len(s) > 3000 { s = s[len(s)-3000:] }
		return "", fmt.Errorf("%w: %s", runErr, s)
	}
	return s, nil
}

func (r *Remote) PutPrivateFile(ctx context.Context, path, data string) error {
	// Paths are controlled by the application, not by the user or remote host.
	if !strings.HasPrefix(path, "/etc/amnezia/amneziawg/") || !strings.HasSuffix(path, ".conf") {
		return errors.New("недопустимое имя файла VPN")
	}
	sess, err := r.client.NewSession()
	if err != nil { return err }
	defer sess.Close()
	sess.Stdin = strings.NewReader(data)
	target := path + ".new"
	command := fmt.Sprintf("umask 077; install -d -m 700 /etc/amnezia/amneziawg && cat > '%s' && chmod 600 '%s' && mv -f '%s' '%s'", target, target, target, path)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done(): _ = sess.Close()
		case <-done:
		}
	}()
	out, err := sess.CombinedOutput(command)
	close(done)
	if ctx.Err() != nil { return ctx.Err() }
	if err != nil { return fmt.Errorf("сохранение %s: %w: %s", path, err, strings.TrimSpace(string(out))) }
	return nil
}
