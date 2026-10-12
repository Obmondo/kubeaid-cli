// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package velociraptor

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const (
	tenantA       = "Tenant A"
	tenantB       = "Tenant B"
	nameCol       = "Name"
	orgIDCol      = "OrgId"
	dryRunParam   = "DryRun"
	irisCollector = "Custom.Server.IrisCollector"
	keycloakSync  = "Custom.Server.KeycloakSync"
)

// decodeArgs is the server-side inverse of queryArgs.marshal.
func decodeArgs(t *testing.T, b []byte) (string, map[string]string) {
	t.Helper()
	env := map[string]string{}
	vql := ""
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		require.GreaterOrEqual(t, n, 0)
		b = b[n:]
		if typ != protowire.BytesType {
			m := protowire.ConsumeFieldValue(num, typ, b)
			b = b[m:]
			continue
		}
		v, m := protowire.ConsumeBytes(b)
		b = b[m:]
		fields := map[protowire.Number]string{}
		for len(v) > 0 {
			fn, _, k := protowire.ConsumeTag(v)
			v = v[k:]
			s, l := protowire.ConsumeString(v)
			v = v[l:]
			fields[fn] = s
		}
		if num == argsFieldQuery {
			vql = fields[requestFieldVQL]
		}
		if num == argsFieldEnv {
			env[fields[envFieldKey]] = fields[envFieldValue]
		}
	}
	return vql, env
}

func encodeResponse(rows any, log string) []byte {
	var b []byte
	if rows != nil {
		raw, _ := json.Marshal(rows)
		b = protowire.AppendTag(b, responseFieldResponse, protowire.BytesType)
		b = protowire.AppendString(b, string(raw))
	}
	b = protowire.AppendTag(b, 5, protowire.VarintType) // query_id: skipped by the client
	b = protowire.AppendVarint(b, 1)
	if log != "" {
		b = protowire.AppendTag(b, responseFieldLog, protowire.BytesType)
		b = protowire.AppendString(b, log)
	}
	return b
}

// fakeServer answers the reconciler's VQL like a Velociraptor server.
type fakeServer struct {
	mu         sync.Mutex
	orgs       []string
	artifacts  []string
	specs      map[string]map[string]string
	knownArtis map[string]bool
	writes     int
}

func (f *fakeServer) handle(t *testing.T) grpc.StreamHandler {
	return func(_ any, stream grpc.ServerStream) error {
		var in frame
		if err := stream.RecvMsg(&in); err != nil {
			return err
		}
		vql, env := decodeArgs(t, in.data)
		f.mu.Lock()
		defer f.mu.Unlock()
		var rows any
		log := "Starting query execution."
		switch vql {
		case vqlListOrgs:
			list := []map[string]any{{orgIDCol: "root", nameCol: "<root>"}}
			for i, o := range f.orgs {
				list = append(list, map[string]any{orgIDCol: "O" + string(rune('A'+i)), nameCol: o})
			}
			rows = list
		case vqlOrgClientConfigs:
			// The root org has no nonce here, so orgs() leaves its
			// _client_config unset.
			list := []map[string]any{{orgIDCol: "root", nameCol: "<root>"}}
			for i, o := range f.orgs {
				list = append(list, map[string]any{orgIDCol: "O" + string(rune('A'+i)), nameCol: o, "ClientConfig": "Client:\n  nonce: " + o + "\n"})
			}
			rows = list
		case vqlCreateOrg:
			f.orgs = append(f.orgs, env["OrgName"])
			f.writes++
			rows = []map[string]any{{"Org": map[string]any{"name": env["OrgName"]}}}
		case vqlGetMonitoring:
			specs := []map[string]any{}
			for a, p := range f.specs {
				envs := []map[string]any{}
				for k, v := range p {
					envs = append(envs, map[string]any{"key": k, "value": v})
				}
				specs = append(specs, map[string]any{"artifact": a, "parameters": map[string]any{"env": envs}})
			}
			rows = []map[string]any{{"State": map[string]any{"artifacts": f.artifacts, "specs": specs}}}
		case vqlAddMonitoring:
			name := env["ArtifactName"]
			if !f.knownArtis[name] {
				rows = []map[string]any{{"Result": nil}}
				log = "add_server_monitoring: artifact " + name + " not found"
				break
			}
			var p map[string]string
			_ = json.Unmarshal([]byte(env["Params"]), &p)
			found := false
			for _, a := range f.artifacts {
				found = found || a == name
			}
			if !found {
				f.artifacts = append(f.artifacts, name)
			}
			f.specs[name] = p
			f.writes++
			rows = []map[string]any{{"Result": map[string]any{"artifacts": f.artifacts}}}
		default:
			log = "unknown VQL"
		}
		if err := stream.SendMsg(&frame{data: encodeResponse(nil, log)}); err != nil {
			return err
		}
		return stream.SendMsg(&frame{data: encodeResponse(rows, "")})
	}
}

// testPKI is a CA with a server and a client certificate.
type testPKI struct {
	caPEM, serverCert, serverKey, clientCert, clientKey []byte
}

func newTestPKI(t *testing.T) testPKI {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Velociraptor CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	ca, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	issue := func(serial int64, cn string, usage x509.ExtKeyUsage, dns []string) ([]byte, []byte) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn}, DNSNames: dns,
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			ExtKeyUsage: []x509.ExtKeyUsage{usage}, KeyUsage: x509.KeyUsageDigitalSignature,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
		require.NoError(t, err)
		keyDER, err := x509.MarshalECPrivateKey(key)
		require.NoError(t, err)
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	}
	p := testPKI{caPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})}
	p.serverCert, p.serverKey = issue(2, defaultPinnedServerName, x509.ExtKeyUsageServerAuth, []string{defaultPinnedServerName})
	p.clientCert, p.clientKey = issue(3, "siem", x509.ExtKeyUsageClientAuth, nil)
	return p
}

// startServer runs the fake over mTLS and returns an api_client YAML.
func startServer(t *testing.T, f *fakeServer) []byte {
	t.Helper()
	pki := newTestPKI(t)
	cert, err := tls.X509KeyPair(pki.serverCert, pki.serverKey)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(pki.caPEM))
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert}, ClientCAs: pool,
		ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS12,
	})
	srv := grpc.NewServer(grpc.Creds(creds), grpc.ForceServerCodec(rawCodec{}), grpc.UnknownServiceHandler(f.handle(t)))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	indent := func(b []byte) string {
		out := ""
		for _, line := range splitLines(string(b)) {
			out += "  " + line + "\n"
		}
		return out
	}
	return []byte("ca_certificate: |\n" + indent(pki.caPEM) +
		"client_cert: |\n" + indent(pki.clientCert) +
		"client_private_key: |\n" + indent(pki.clientKey) +
		"api_connection_string: " + lis.Addr().String() + "\nname: siem\n")
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func spec() Spec {
	return Spec{
		Orgs: []string{tenantA, tenantB},
		Monitoring: []MonitoredArtifact{
			{Artifact: irisCollector},
			{Artifact: keycloakSync, Parameters: map[string]string{dryRunParam: "N"}},
		},
	}
}

func dial(t *testing.T, f *fakeServer) *Client {
	t.Helper()
	cfg, err := ParseAPIClient(startServer(t, f))
	require.NoError(t, err)
	c, err := Dial(cfg, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestReconcileOverMTLS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := &fakeServer{
		orgs:       []string{tenantA},
		artifacts:  []string{"Server.Monitor.Health", keycloakSync},
		specs:      map[string]map[string]string{keycloakSync: {dryRunParam: "Y", "PollSeconds": "300"}},
		knownArtis: map[string]bool{irisCollector: true, keycloakSync: true},
	}
	c := dial(t, f)

	got := map[string]report.Action{}
	for _, r := range Reconcile(ctx, c, spec(), true) {
		got[r.Kind+"/"+r.Name] = r.Action
	}
	assert.Equal(t, report.ActionOK, got["org/Tenant A"])
	assert.Equal(t, report.ActionCreate, got["org/Tenant B"])
	assert.Equal(t, report.ActionCreate, got["server-monitoring/"+irisCollector])
	assert.Equal(t, report.ActionUpdate, got["server-monitoring/"+keycloakSync])
	assert.Zero(t, f.writes, "dry run must not write")

	for _, r := range Reconcile(ctx, c, spec(), false) {
		assert.NotEqual(t, report.ActionError, r.Action, "%s %s: %s", r.Kind, r.Name, r.Detail)
	}
	assert.Equal(t, []string{tenantA, tenantB}, f.orgs)
	assert.Equal(t, map[string]string{dryRunParam: "N", "PollSeconds": "300"}, f.specs[keycloakSync],
		"unmanaged parameters are kept")
	assert.Contains(t, f.artifacts, "Server.Monitor.Health", "other artifacts are never removed")
	writes := f.writes

	for _, r := range Reconcile(ctx, c, spec(), false) {
		assert.Equal(t, report.ActionOK, r.Action, "%s %s", r.Kind, r.Name)
	}
	assert.Equal(t, writes, f.writes, "second run must not write")
}

func TestReconcileReportsServerRefusalAndDuplicates(t *testing.T) {
	t.Parallel()
	f := &fakeServer{
		orgs:       []string{tenantA, tenantA},
		specs:      map[string]map[string]string{},
		knownArtis: map[string]bool{},
	}
	c := dial(t, f)
	got := map[string]report.Result{}
	for _, r := range Reconcile(context.Background(), c, spec(), false) {
		got[r.Kind+"/"+r.Name] = r
	}
	assert.Equal(t, report.ActionError, got["org/Tenant A"].Action)
	assert.Equal(t, report.ActionError, got["server-monitoring/"+irisCollector].Action)
	assert.Contains(t, got["server-monitoring/"+irisCollector].Detail, "not found")
}

func TestParseAPIClientErrors(t *testing.T) {
	t.Parallel()
	_, err := ParseAPIClient([]byte("name: x\n"))
	require.Error(t, err)
	_, err = ParseAPIClient([]byte(":::"))
	require.Error(t, err)

	cfg := &APIClientConfig{CACertificate: "x", ClientCert: "y", ClientPrivateKey: "z"}
	_, err = cfg.TLSConfig()
	require.Error(t, err)
	_, err = Dial(cfg, "")
	require.Error(t, err, "no address")
}

func TestUnmarshalResponseRejectsGarbage(t *testing.T) {
	t.Parallel()
	_, err := unmarshalResponse([]byte{0xff, 0xff, 0xff})
	require.Error(t, err)
	_, err = rawCodec{}.Marshal("not a frame")
	require.Error(t, err)
	require.Error(t, rawCodec{}.Unmarshal(nil, "not a frame"))
}

// A VQL environment variable named Artifact is shadowed by the built-in
// Artifact namespace; add_server_monitoring then reports "artifact &{...}
// not found" (seen against a 0.77.1 server).
func TestAddMonitoringDoesNotUseArtifactVariable(t *testing.T) {
	t.Parallel()
	assert.NotContains(t, vqlAddMonitoring, "artifact=Artifact,")
	assert.Contains(t, vqlAddMonitoring, "artifact=ArtifactName")
}

func TestOrgClientConfigs(t *testing.T) {
	t.Parallel()
	f := &fakeServer{orgs: []string{tenantA, tenantB, tenantB}}
	got, err := OrgClientConfigs(context.Background(), dial(t, f))
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{
		tenantA: {"Client:\n  nonce: Tenant A\n"},
		tenantB: {"Client:\n  nonce: Tenant B\n", "Client:\n  nonce: Tenant B\n"},
	}, got, "orgs without a client config are left out; duplicates are kept for the caller")
	assert.Contains(t, vqlOrgClientConfigs, "_client_config AS ClientConfig", "hidden column must be selected explicitly")
}
