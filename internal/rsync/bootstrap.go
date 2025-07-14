package rsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/vbp1/pgclone/internal/ssh"
)

// Daemon represents a running remote rsync daemon started via SSH.
// Stop must be called to terminate it and cleanup remote dir.
type Daemon struct {
	Port      int
	Secret    string
	RemoteDir string
	stopFunc  func(context.Context) error
}

// Stop terminates remote rsyncd and deletes its temporary directory.
func (d *Daemon) Stop(ctx context.Context) error {
	if d.stopFunc != nil {
		return d.stopFunc(ctx)
	}
	return nil
}

// BootstrapOptions configures StartRemote.
type BootstrapOptions struct {
	RemoteTempDir string            // if empty – /tmp/pgclone_<rand>
	PortMin       int               // inclusive; default 45000
	PortMax       int               // inclusive; default 45100
	Modules       map[string]string // module name -> path
	MaxConn       int               // max connections parameter
	Timeout       time.Duration     // timeout waiting for port line
}

// StartRemote starts rsync --daemon on the remote host via SSH.
// It uploads minimal config and secret file. Returns Daemon with port and secret.
func StartRemote(ctx context.Context, client *ssh.Client, opts BootstrapOptions) (*Daemon, error) {
	if opts.PortMin == 0 {
		opts.PortMin = 45000
	}
	if opts.PortMax == 0 {
		opts.PortMax = 45100
	}
	if opts.MaxConn == 0 {
		opts.MaxConn = 16
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}

	// generate secret
	secRaw := make([]byte, 8)
	_, _ = rand.Read(secRaw)
	secret := hex.EncodeToString(secRaw)

	// remote dir tag
	tagRaw := make([]byte, 4)
	_, _ = rand.Read(tagRaw)
	tag := hex.EncodeToString(tagRaw)

	remoteDir := opts.RemoteTempDir
	if remoteDir == "" {
		remoteDir = fmt.Sprintf("/tmp/pgclone_%s", tag)
	}

	// Build rsyncd.conf
	var conf bytes.Buffer
	fmt.Fprintf(&conf, "use chroot = no\nmax connections = %d\npid file = %s/rsyncd.pid\nlog file = %s/rsyncd.log\nlock file = %s/rsyncd.lock\nsockopts = TCP_NODELAY,SO_SNDBUF=512000,SO_RCVBUF=512000\n\n", opts.MaxConn, remoteDir, remoteDir, remoteDir)
	for m, path := range opts.Modules {
		fmt.Fprintf(&conf, "[%s]\n    path = %s\n    read only = yes\n    auth users = replica\n    secrets file = %s/rsyncd.secrets\n\n", m, path, remoteDir)
	}

	// Script body executed on remote via bash -c
	script := fmt.Sprintf(`bash -c 'set -euo pipefail
RD=%s
mkdir -p "$RD"
cat > "$RD/rsyncd.conf" <<CONF
%sCONF
echo "replica:%s" > "$RD/rsyncd.secrets"
chmod 600 "$RD/rsyncd.secrets"
PORT=""
for p in $(seq %d %d); do
  (echo >/dev/tcp/127.0.0.1/$p) >/dev/null 2>&1 || { PORT=$p; break; }
done
[ -z "$PORT" ] && { echo no_port >&2; exit 1; }
# write port to file so caller can poll
echo "$PORT" > "$RD/PORT"

# also print to stdout (for debugging)
echo "$PORT"
nohup rsync --daemon --config="$RD/rsyncd.conf" --port=$PORT >/dev/null 2>&1 &
'`, remoteDir, conf.String(), secret, opts.PortMin, opts.PortMax)

	slog.Debug("rsync bootstrap: running remote script")

	var out bytes.Buffer
	if err := client.Run(ctx, script, &out, &out); err != nil {
		return nil, fmt.Errorf("remote bootstrap: %w; output=%s", err, out.String())
	}

	// Poll remote $RD/PORT file for up to opts.Timeout
	rePort := regexp.MustCompile(`^\d+$`)
	var port int
	deadline := time.Now().Add(opts.Timeout)
	for {
		// cat port file (suppress errors)
		data, _ := client.Output(ctx, fmt.Sprintf("cat '%s/PORT' 2>/dev/null || true", remoteDir))
		s := strings.TrimSpace(string(data))
		if rePort.MatchString(s) {
			if _, err := fmt.Sscanf(s, "%d", &port); err == nil && port > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("remote bootstrap: PORT file not found within timeout; out=%q", out.String())
		}
		time.Sleep(200 * time.Millisecond)
	}

	// define stopFunc which kills rsyncd and removes dir
	stopScript := fmt.Sprintf(`set -euo pipefail
RD=%s
if [ -f "$RD/rsyncd.pid" ]; then
  kill -9 $(cat "$RD/rsyncd.pid") || true
fi
# remove temporary directory completely
rm -rf "$RD"
`, remoteDir)

	stop := func(ctx context.Context) error {
		var out bytes.Buffer
		err := client.Run(ctx, stopScript, &out, &out)
		if err != nil {
			slog.Warn("remote stop failed", "err", err, "out", out.String())
		}
		return err
	}

	d := &Daemon{
		Port:      port,
		Secret:    secret,
		RemoteDir: remoteDir,
		stopFunc:  stop,
	}
	return d, nil
}
