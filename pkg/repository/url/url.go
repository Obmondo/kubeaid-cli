// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package url

import (
	"net/url"
	"strings"

	"github.com/samber/oops"

	"github.com/Obmondo/kubeaid-cli/pkg/urlprotocol"
)

// Parsed is defined in pkg/urlprotocol so pkg/config can embed it without
// pulling this package's parsing machinery. Aliased so callers and struct
// literals here are unchanged.
type Parsed = urlprotocol.Parsed

func Parse(unparsed string) (*Parsed, error) {
	// Trim any leading or trailing whitespaces.
	unparsed = strings.TrimSpace(unparsed)
	if unparsed == "" {
		return nil, oops.Errorf("Git Parsed is empty")
	}

	var (
		parsed *Parsed
		err    error
	)

	protocol := DetectProtocol(unparsed)
	switch protocol {
	case ProtocolSCP:
		parsed, err = parseSCPSchemed(unparsed, protocol)
		if err != nil {
			return nil, oops.
				With("url", unparsed).
				Wrapf(err, "Failed parsing SCP schemed repository URL")
		}

	case ProtocolHTTP, ProtocolHTTPs, ProtocolSSH:
		parsed, err = parseNonSCPSchemed(unparsed, protocol)
		if err != nil {
			return nil, oops.
				With("url", unparsed).
				Wrapf(err, "Failed parsing non-SCP schemed repository URL")
		}
	}

	return parsed, nil
}

func parseNonSCPSchemed(unparsed string, protocol Protocol) (*Parsed, error) {
	parsedURL, err := url.Parse(unparsed)
	if err != nil {
		return nil, oops.Wrapf(err, "Failed parsing non-SCP schemed URL")
	}

	owner, repository, err := parseRepositoryPath(parsedURL.Path)
	if err != nil {
		return nil, oops.Wrapf(err, "Failed parsing repository path")
	}

	parsedRepositoryURL := &Parsed{
		Protocol:   protocol,
		Host:       parsedURL.Host,
		Owner:      owner,
		Repository: repository,
	}
	return parsedRepositoryURL, nil
}

func parseSCPSchemed(unparsed string, protocol Protocol) (*Parsed, error) {
	// Trim the scheme, if exists.
	unparsed = strings.TrimPrefix(unparsed, string(SchemeSCP))

	parts := strings.Split(unparsed, ":")
	if (len(parts) != 2) || (len(parts[0]) == 0) || (len(parts[1]) == 0) {
		return nil, oops.Errorf("Wrong SCP schemed repository URL")
	}

	sshEndpoint := parts[0]
	path := parts[1]

	host, err := getSSHEndpointHost(sshEndpoint)
	if err != nil {
		return nil, oops.Wrapf(err, "Failed getting host from SSH endpoint")
	}

	owner, repository, err := parseRepositoryPath(path)
	if err != nil {
		return nil, oops.Wrapf(err, "Failed parsing repository path")
	}

	parsed := &Parsed{
		Protocol:   protocol,
		Host:       host,
		Owner:      owner,
		Repository: repository,
	}
	return parsed, nil
}

func parseRepositoryPath(path string) (owner, repository string, err error) {
	// Trim .git suffix, if exists.
	path = strings.TrimSuffix(path, ".git")

	// Remove the leading slash, representing root path, if exists.
	path = strings.TrimPrefix(path, "/")

	parts := strings.SplitN(path, "/", 3)
	if (len(parts) < 2) || (len(parts[0]) == 0) || (len(parts[1]) == 0) {
		err = oops.
			With("path", path).
			Errorf("Repository path must be in this format : owner/repository(.git)")
		return
	}

	owner = parts[0]
	repository = parts[1]

	return
}

func getSSHEndpointHost(sshEndpoint string) (host string, err error) {
	sshEndpointParts := strings.Split(sshEndpoint, "@")

	host = sshEndpointParts[len(sshEndpointParts)-1]
	if (len(sshEndpointParts) > 2) || (len(host) == 0) {
		err = oops.Errorf("Wrong SSH endpoint format")
		return
	}

	return
}
