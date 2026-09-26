// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package velociraptor

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
)

// Velociraptor's Query RPC (service proto.API, method Query) takes a
// VQLCollectorArgs and streams VQLResponse messages. Velociraptor is
// AGPL-3.0 licensed, so instead of vendoring its .proto files and
// generated code this package encodes the few fields it needs by hand
// with protowire. Field numbers are from actions/proto/vql.proto:
//
//	message VQLCollectorArgs {
//	  repeated VQLRequest Query = 2;   // VQLRequest { string VQL = 1; string Name = 2; }
//	  repeated VQLEnv env = 3;         // VQLEnv { string key = 1; string value = 2; }
//	  uint64 max_row = 4;
//	  uint64 max_wait = 6;
//	  uint64 timeout = 25;
//	  string org_id = 35;
//	}
//	message VQLResponse {
//	  string Response = 1;             // JSON array of rows
//	  string log = 9;
//	}
const (
	queryMethod = "/proto.API/Query"

	argsFieldQuery   = 2
	argsFieldEnv     = 3
	argsFieldMaxRow  = 4
	argsFieldMaxWait = 6
	argsFieldTimeout = 25
	argsFieldOrgID   = 35

	requestFieldVQL  = 1
	requestFieldName = 2

	envFieldKey   = 1
	envFieldValue = 2

	responseFieldResponse = 1
	responseFieldLog      = 9
)

// queryArgs is the subset of VQLCollectorArgs this package sends.
type queryArgs struct {
	Name    string
	VQL     string
	Env     [][2]string
	OrgID   string
	MaxRow  uint64
	MaxWait uint64
	Timeout uint64
}

func (a queryArgs) marshal() []byte {
	var req []byte
	req = protowire.AppendTag(req, requestFieldVQL, protowire.BytesType)
	req = protowire.AppendString(req, a.VQL)
	if a.Name != "" {
		req = protowire.AppendTag(req, requestFieldName, protowire.BytesType)
		req = protowire.AppendString(req, a.Name)
	}

	var b []byte
	b = protowire.AppendTag(b, argsFieldQuery, protowire.BytesType)
	b = protowire.AppendBytes(b, req)
	for _, kv := range a.Env {
		var env []byte
		env = protowire.AppendTag(env, envFieldKey, protowire.BytesType)
		env = protowire.AppendString(env, kv[0])
		env = protowire.AppendTag(env, envFieldValue, protowire.BytesType)
		env = protowire.AppendString(env, kv[1])
		b = protowire.AppendTag(b, argsFieldEnv, protowire.BytesType)
		b = protowire.AppendBytes(b, env)
	}
	for _, f := range []struct {
		num protowire.Number
		v   uint64
	}{{argsFieldMaxRow, a.MaxRow}, {argsFieldMaxWait, a.MaxWait}, {argsFieldTimeout, a.Timeout}} {
		if f.v != 0 {
			b = protowire.AppendTag(b, f.num, protowire.VarintType)
			b = protowire.AppendVarint(b, f.v)
		}
	}
	if a.OrgID != "" {
		b = protowire.AppendTag(b, argsFieldOrgID, protowire.BytesType)
		b = protowire.AppendString(b, a.OrgID)
	}
	return b
}

// queryResponse is the subset of VQLResponse this package reads.
type queryResponse struct {
	Response string
	Log      string
}

func unmarshalResponse(b []byte) (queryResponse, error) {
	var out queryResponse
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return out, fmt.Errorf("decoding VQLResponse tag: %w", protowire.ParseError(n))
		}
		b = b[n:]
		if typ == protowire.BytesType && (num == responseFieldResponse || num == responseFieldLog) {
			v, m := protowire.ConsumeString(b)
			if m < 0 {
				return out, fmt.Errorf("decoding VQLResponse field %d: %w", num, protowire.ParseError(m))
			}
			if num == responseFieldResponse {
				out.Response = v
			} else {
				out.Log = v
			}
			b = b[m:]
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			return out, fmt.Errorf("skipping VQLResponse field %d: %w", num, protowire.ParseError(m))
		}
		b = b[m:]
	}
	return out, nil
}

// frame is a pre-encoded protobuf message passed through rawCodec.
type frame struct {
	data []byte
}

// rawCodec hands pre-encoded bytes to gRPC. Its name is "proto" so
// the content-subtype matches what the server expects.
type rawCodec struct{}

func (rawCodec) Name() string { return "proto" }

func (rawCodec) Marshal(v any) ([]byte, error) {
	f, ok := v.(*frame)
	if !ok {
		return nil, fmt.Errorf("rawCodec cannot marshal %T", v)
	}
	return f.data, nil
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	f, ok := v.(*frame)
	if !ok {
		return fmt.Errorf("rawCodec cannot unmarshal into %T", v)
	}
	f.data = append([]byte(nil), data...)
	return nil
}
