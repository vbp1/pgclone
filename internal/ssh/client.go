package ssh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Config describes connection parameters for an SSH session.
type Config struct {
	User     string        // remote user (required)
	Host     string        // remote host or host:port (required)
	KeyPath  string        // path to private key; if empty, DefaultKeyPaths will be tried and agent auth is allowed as fallback
	Insecure bool          // if true – skip host key verification (StrictHostKeyChecking=no analogue)
	Timeout  time.Duration // dial timeout; if 0 – DefaultTimeout
}

// DefaultTimeout used when Config.Timeout==0.
const DefaultTimeout = 10 * time.Second

// DefaultKeyPaths tried when Config.KeyPath is empty.
var DefaultKeyPaths = []string{
	os.Getenv("HOME") + "/.ssh/id_ed25519",
	os.Getenv("HOME") + "/.ssh/id_rsa",
	os.Getenv("HOME") + "/.ssh/id_ecdsa",
}

// Client wraps ssh.Client and simplifies command execution.
// Close must be called when no longer needed.
type Client struct {
	cfg    Config
	client *ssh.Client
}

// Dial establishes SSH connection according to cfg.
func Dial(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.User == "" || cfg.Host == "" {
		return nil, fmt.Errorf("ssh: User and Host required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}

	authMethods, err := authMethodsForKey(cfg.KeyPath)
	if err != nil {
		return nil, err
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback(cfg.Insecure),
		Timeout:         cfg.Timeout,
	}

	// allow host:port in Host; default 22 if missing
	addr := cfg.Host
	if !hasPort(addr) {
		addr = addr + ":22"
	}

	slog.Debug("ssh dial", "addr", addr, "user", cfg.User)

	connCh := make(chan *ssh.Client, 1)
	errCh := make(chan error, 1)
	go func() {
		c, err := ssh.Dial("tcp", addr, sshCfg)
		if err != nil {
			errCh <- err
			return
		}
		connCh <- c
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case c := <-connCh:
		return &Client{cfg: cfg, client: c}, nil
	}
}

// Close underlying ssh.Client.
func (c *Client) Close() error { return c.client.Close() }

// Run executes cmd on remote host, attaching std streams to provided writers. If stdout/stderr nil – they are discarded.
func (c *Client) Run(ctx context.Context, cmd string, stdout, stderr io.Writer) error {
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer func() {
		if err := session.Close(); err != nil {
			slog.Debug("ssh session close", "err", err)
		}
	}()

	if stdout != nil {
		session.Stdout = stdout
	}
	if stderr != nil {
		session.Stderr = stderr
	}

	slog.Debug("ssh run", "cmd", cmd, "host", c.cfg.Host)

	if err := session.Start(cmd); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- session.Wait() }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// Output runs cmd and returns combined stdout/stderr output.
func (c *Client) Output(ctx context.Context, cmd string) ([]byte, error) {
	lb := &limitedBuffer{N: 1 << 20}
	if err := c.Run(ctx, cmd, lb, lb); err != nil {
		return nil, err
	}
	return lb.Bytes(), nil
}

// ----------------- helpers ------------------

func hasPort(addr string) bool {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return true
		}
		if addr[i] == ']' { // IPv6 literals
			return false
		}
	}
	return false
}

func hostKeyCallback(insecure bool) ssh.HostKeyCallback {
	if insecure {
		return ssh.InsecureIgnoreHostKey()
	}

	// use standard OpenSSH known_hosts file
	knownPath := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")
	cb, err := knownhosts.New(knownPath)
	if err != nil {
		slog.Warn("ssh: cannot load known_hosts, falling back to insecure", "err", err)
		return ssh.InsecureIgnoreHostKey()
	}
	return cb
}

func authMethodsForKey(keyPath string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if keyPath != "" {
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("ssh: read key %s: %w", keyPath, err)
		}
		signer, err := signerFromKey(key)
		if err != nil {
			return nil, err
		}
		methods = append(methods, ssh.PublicKeys(signer))
	} else {
		// try default keys
		for _, p := range DefaultKeyPaths {
			if _, err := os.Stat(p); err == nil {
				key, err := os.ReadFile(p)
				if err != nil {
					continue
				}
				signer, err := signerFromKey(key)
				if err != nil {
					continue
				}
				methods = append(methods, ssh.PublicKeys(signer))
			}
		}
	}

	// agent
	if a, err := sshAgent(); err == nil && a != nil {
		methods = append(methods, ssh.PublicKeysCallback(a.Signers))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("ssh: no auth methods found (provide key or ensure agent running)")
	}
	return methods, nil
}

func signerFromKey(key []byte) (ssh.Signer, error) {
	// support encrypted keys (promptless) – fail if passphrase protected
	signer, err := ssh.ParsePrivateKey(key)
	if err == nil {
		return signer, nil
	}
	return nil, fmt.Errorf("ssh: parse key: %w", err)
}

// sshAgent tries to connect to ssh-agent and return its client.
func sshAgent() (agent.Agent, error) {
	env := os.Getenv("SSH_AUTH_SOCK")
	if env == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK not set")
	}
	conn, err := net.Dial("unix", env)
	if err != nil {
		return nil, err
	}
	return agent.NewClient(conn), nil
}

// limitedBuffer prevents unbounded memory when capturing command output.
type limitedBuffer struct {
	buf bytes.Buffer
	N   int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.N > 0 && b.buf.Len()+len(p) > b.N {
		return 0, fmt.Errorf("ssh output exceeds %d bytes", b.N)
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Read(p []byte) (int, error) { return b.buf.Read(p) }

func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }
