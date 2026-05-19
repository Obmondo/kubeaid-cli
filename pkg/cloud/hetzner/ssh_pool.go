// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"os"
	"sync"
	"time"

	"k8c.io/kubeone/pkg/executor"
	"k8c.io/kubeone/pkg/ssh"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/progress"
)

// sshConnOpener is the indirection that lets tests inject a fake
// opener without standing up a real SSH server. Production wires it
// to kubeone's ssh.NewConnection (see realSSHOpen below).
type sshConnOpener func(ctx context.Context, address, privateKey string) (executor.Interface, error)

// sshConnPool caches one SSH connection per host-address for the
// duration of a Hetzner provider's prereq-infra phase. Different
// bootstrap operations targeting the same bare-metal server — the
// isHBMSReachable poll, then generateStoragePlan, plus anything we
// add later — all reuse the same authenticated channel. Net effect:
// one YubiKey touch per host instead of one per operation.
//
// Scope is intentionally narrow. The pool's only lifecycle hooks
// are getOrOpen + closeAll. ProvisionPrerequisiteInfrastructure
// defers closeAll at the top of the function so connections are
// reclaimed on every exit path (success or error). CAPH later
// in the bootstrap opens its own SSH from a Kubernetes controller —
// that's outside this pool and outside this process.
//
// Cross-process persistence (OpenSSH ControlMaster's `.sock` file)
// is deliberately NOT supported. kubeone's SSH library is pure-Go
// (golang.org/x/crypto/ssh) with no daemon / socket; persisting
// across kubeaid-cli invocations would require a separate mux
// daemon + IPC protocol, which isn't worth the engineering cost
// for the per-invocation touch reduction this pool already gives.
type sshConnPool struct {
	mu          sync.Mutex
	connections map[string]executor.Interface
	opener      sshConnOpener
}

// newSSHConnPool builds an empty pool wired to the real
// kubeone-SSH opener. Tests construct their own pools and swap the
// opener field directly.
func newSSHConnPool() *sshConnPool {
	return &sshConnPool{
		connections: map[string]executor.Interface{},
		opener:      realSSHOpen,
	}
}

// realSSHOpen is the production opener. Mirrors the auth shape
// waitForNATGatewaySSH (server.go) uses: hand the connection both
// the agent socket (yubikey-resident keys leave PrivateKey empty —
// see parser.hydrateSSHKeyPairFromAgent) and the supplied PrivateKey
// bytes (file-based key path). kubeone picks whichever is non-empty.
func realSSHOpen(ctx context.Context, address, privateKey string) (executor.Interface, error) {
	return ssh.NewConnection(ssh.NewConnector(ctx), ssh.Opts{
		Context:     ctx,
		Hostname:    address,
		Port:        22,
		Username:    "root",
		AgentSocket: os.Getenv(constants.EnvNameSSHAuthSock),
		PrivateKey:  []byte(privateKey),
		Timeout:     time.Second * 10,
	})
}

// getOrOpen returns the cached executor.Interface for address,
// opening a new connection (with a YubiKey-touch hint via
// progress.RequestYubiKeyTouch) if not yet pooled.
//
// Concurrency: the whole map lookup + open serialises under the
// pool's mutex. Two callers racing to add the same host can't both
// open — one wins the slot, the second finds the cached entry on
// retry. For different hosts, the second caller still waits behind
// the first under the same mutex; that's OK because the parallel
// InstallOSOnAllHBMS goroutines stagger naturally (TCP probes
// complete at different real-world times as each OS install
// finishes), and the per-host open is fast (single TCP+KEX, a few
// hundred ms at most). Holding a coarse lock is simpler than
// per-host mutexes and the contention is negligible.
//
// touchReason is the label surfaced by progress.RequestYubiKeyTouch
// when an open actually fires. Cached-hit calls don't raise a
// prompt.
func (p *sshConnPool) getOrOpen(ctx context.Context, address, privateKey, touchReason string) (executor.Interface, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if conn, ok := p.connections[address]; ok {
		return conn, nil
	}

	releaseTouchHint := progress.FromCtx(ctx).RequestYubiKeyTouch(touchReason)
	conn, err := p.opener(ctx, address, privateKey)
	releaseTouchHint()
	if err != nil {
		return nil, err
	}
	p.connections[address] = conn
	return conn, nil
}

// closeAll closes every cached connection and clears the map.
// Safe to call multiple times — the map is iterated and emptied
// in one critical section. Errors from individual Close calls are
// ignored: by the time closeAll runs the bootstrap phase is
// already finishing and propagating per-connection close failures
// would mask whatever real error (if any) the caller is returning.
func (p *sshConnPool) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for addr, conn := range p.connections {
		_ = conn.Close()
		delete(p.connections, addr)
	}
}
