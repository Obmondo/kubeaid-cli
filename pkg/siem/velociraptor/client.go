// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package velociraptor reconciles Velociraptor orgs (one per tenant)
// and the server monitoring table through the gRPC API, using an
// api_client config (mTLS). User grants are never touched: the
// KeycloakSync server artifact owns them.
package velociraptor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/yaml.v3"
)

// defaultPinnedServerName is the name Velociraptor's server
// certificate is issued for unless the config pins another one.
const defaultPinnedServerName = "VelociraptorServer"

// queryTimeoutSeconds bounds one VQL query on the server.
const queryTimeoutSeconds = 60

// APIClientConfig is the YAML `velociraptor config api_client` writes.
type APIClientConfig struct {
	CACertificate       string `yaml:"ca_certificate"`
	ClientCert          string `yaml:"client_cert"`
	ClientPrivateKey    string `yaml:"client_private_key"`
	APIConnectionString string `yaml:"api_connection_string"`
	Name                string `yaml:"name"`
	PinnedServerName    string `yaml:"pinned_server_name"`
}

// ParseAPIClient parses an api_client YAML.
func ParseAPIClient(data []byte) (*APIClientConfig, error) {
	var cfg APIClientConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing Velociraptor api_client config: %w", err)
	}
	if cfg.CACertificate == "" || cfg.ClientCert == "" || cfg.ClientPrivateKey == "" {
		return nil, errors.New("api_client config needs ca_certificate, client_cert and client_private_key")
	}
	return &cfg, nil
}

// TLSConfig builds the mTLS configuration: the client certificate,
// the Velociraptor CA as the only root and the pinned server name.
func (c *APIClientConfig) TLSConfig() (*tls.Config, error) {
	cert, err := tls.X509KeyPair([]byte(c.ClientCert), []byte(c.ClientPrivateKey))
	if err != nil {
		return nil, fmt.Errorf("loading api_client certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(c.CACertificate)) {
		return nil, errors.New("api_client ca_certificate holds no certificate")
	}
	serverName := c.PinnedServerName
	if serverName == "" {
		serverName = defaultPinnedServerName
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// Client runs VQL through the Query RPC.
type Client struct {
	conn *grpc.ClientConn
}

// Dial connects with the api_client config. address overrides
// api_connection_string when non-empty.
func Dial(cfg *APIClientConfig, address string) (*Client, error) {
	if address == "" {
		address = cfg.APIConnectionString
	}
	if address == "" {
		return nil, errors.New("no Velociraptor API address (api_connection_string or address)")
	}
	tlsCfg, err := cfg.TLSConfig()
	if err != nil {
		return nil, err
	}
	return NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
}

// NewClient connects with explicit dial options (tests).
func NewClient(address string, opts ...grpc.DialOption) (*Client, error) {
	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("connecting to Velociraptor API %s: %w", address, err)
	}
	return &Client{conn: conn}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Query runs one VQL statement with the given variables in scope and
// returns all rows plus the server's log lines. Values are passed as
// VQL environment variables, never spliced into the query text.
func (c *Client) Query(ctx context.Context, vql string, env map[string]string) ([]map[string]any, []string, error) {
	args := queryArgs{Name: "siem-reconciler", VQL: vql, Timeout: queryTimeoutSeconds}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args.Env = append(args.Env, [2]string{k, env[k]})
	}

	desc := &grpc.StreamDesc{StreamName: "Query", ServerStreams: true}
	stream, err := c.conn.NewStream(ctx, desc, queryMethod, grpc.ForceCodec(rawCodec{}))
	if err != nil {
		return nil, nil, fmt.Errorf("opening Velociraptor query stream: %w", err)
	}
	if err := stream.SendMsg(&frame{data: args.marshal()}); err != nil {
		return nil, nil, fmt.Errorf("sending VQL: %w", err)
	}
	if err := stream.CloseSend(); err != nil {
		return nil, nil, fmt.Errorf("closing VQL send: %w", err)
	}

	var rows []map[string]any
	var logs []string
	for {
		var f frame
		err := stream.RecvMsg(&f)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return rows, logs, fmt.Errorf("running VQL: %w", err)
		}
		resp, err := unmarshalResponse(f.data)
		if err != nil {
			return rows, logs, err
		}
		if resp.Log != "" {
			logs = append(logs, strings.TrimSpace(resp.Log))
		}
		if resp.Response == "" {
			continue
		}
		var part []map[string]any
		if err := json.Unmarshal([]byte(resp.Response), &part); err != nil {
			return rows, logs, fmt.Errorf("decoding VQL rows: %w", err)
		}
		rows = append(rows, part...)
	}
	return rows, logs, nil
}
