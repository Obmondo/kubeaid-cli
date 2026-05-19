// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"k8c.io/kubeone/pkg/executor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExecutor satisfies executor.Interface for pool tests. Records
// whether Close has been called so tests can assert the pool actually
// drops every cached connection on closeAll.
type fakeExecutor struct {
	closed atomic.Bool
}

func (f *fakeExecutor) Exec(string) (string, string, int, error)                   { return "", "", 0, nil }
func (f *fakeExecutor) POpen(string, io.Reader, io.Writer, io.Writer) (int, error) { return 0, nil }
func (f *fakeExecutor) Close() error                                               { f.closed.Store(true); return nil }

func newFakePool() (*sshConnPool, *atomic.Int32) {
	var opens atomic.Int32
	p := &sshConnPool{
		connections: map[string]executor.Interface{},
		opener: func(_ context.Context, _, _ string) (executor.Interface, error) {
			opens.Add(1)
			return &fakeExecutor{}, nil
		},
	}
	return p, &opens
}

func TestSSHConnPoolCachesPerAddress(t *testing.T) {
	pool, opens := newFakePool()

	conn1, err := pool.getOrOpen(context.Background(), "1.2.3.4", "", "first call")
	require.NoError(t, err)
	require.NotNil(t, conn1)

	// Second call for the SAME address must hit the cache — same
	// pointer back, opener counter doesn't move.
	conn2, err := pool.getOrOpen(context.Background(), "1.2.3.4", "", "second call")
	require.NoError(t, err)
	assert.Same(t, conn1, conn2,
		"second call must return the cached connection, not open a new one")
	assert.Equal(t, int32(1), opens.Load(),
		"opener should have been called exactly once for a repeat lookup")

	// A different address opens fresh.
	conn3, err := pool.getOrOpen(context.Background(), "5.6.7.8", "", "different host")
	require.NoError(t, err)
	assert.NotSame(t, conn1, conn3, "different address must produce a different connection")
	assert.Equal(t, int32(2), opens.Load())
}

func TestSSHConnPoolCloseAll(t *testing.T) {
	pool, _ := newFakePool()

	conn1, err := pool.getOrOpen(context.Background(), "1.2.3.4", "", "")
	require.NoError(t, err)
	conn2, err := pool.getOrOpen(context.Background(), "5.6.7.8", "", "")
	require.NoError(t, err)

	pool.closeAll()

	fe1, ok := conn1.(*fakeExecutor)
	require.True(t, ok)
	fe2, ok := conn2.(*fakeExecutor)
	require.True(t, ok)
	assert.True(t, fe1.closed.Load(), "first conn must be closed")
	assert.True(t, fe2.closed.Load(), "second conn must be closed")
	assert.Empty(t, pool.connections, "map must be empty after closeAll")

	// Idempotent: calling closeAll again is a no-op (already empty).
	pool.closeAll()
}

func TestSSHConnPoolConcurrentSameAddress(t *testing.T) {
	// Race the opener: a dozen goroutines all asking for the same
	// address must converge on a single cached connection — the
	// whole point of the pool is to avoid N opens (and N yubikey
	// touches) for the same host. With the mutex this is the cheap
	// case to verify; without it the test would flake.
	pool, opens := newFakePool()

	const goroutines = 12
	results := make([]executor.Interface, goroutines)

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn, err := pool.getOrOpen(context.Background(), "1.2.3.4", "", "")
			require.NoError(t, err)
			results[i] = conn
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int32(1), opens.Load(),
		"concurrent callers must converge on a single open")
	for i := 1; i < goroutines; i++ {
		assert.Same(t, results[0], results[i],
			"every concurrent caller should observe the same cached connection")
	}
}

func TestSSHConnPoolPropagatesOpenerError(t *testing.T) {
	// When the opener fails (Robot creds wrong, sshd not up, ssh
	// host-key mismatch, …) the pool must NOT cache anything — a
	// later retry should fire a new open, not return the prior
	// nil-connection error from memory.
	failures := errors.New("ssh handshake failed")
	var opens atomic.Int32
	pool := &sshConnPool{
		connections: map[string]executor.Interface{},
		opener: func(_ context.Context, _, _ string) (executor.Interface, error) {
			opens.Add(1)
			if opens.Load() == 1 {
				return nil, failures
			}
			return &fakeExecutor{}, nil
		},
	}

	_, err := pool.getOrOpen(context.Background(), "1.2.3.4", "", "")
	require.ErrorIs(t, err, failures)
	assert.Empty(t, pool.connections, "failed open must not pollute the cache")

	conn, err := pool.getOrOpen(context.Background(), "1.2.3.4", "", "")
	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.Equal(t, int32(2), opens.Load(),
		"second call should retry the opener since nothing was cached")
}
