// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

type (
	// PolicyDocument defines a policy document as a Go struct that can be serialized
	// to JSON.
	PolicyDocument struct {
		Version   string            `json:"Version"`
		Statement []PolicyStatement `json:"Statement"`
	}

	// PolicyStatement defines a statement in a policy document.
	PolicyStatement struct {
		Effect    string            `json:"Effect"`
		Action    []string          `json:"Action"`
		Principal map[string]string `json:"Principal,omitempty"`
		Resource  string            `json:"Resource,omitempty"`
	}
)

// CreateIAMRoleForPolicy creates an IAM role with the given IAM policy. It also links an IAM trust
// policy for the IAM role, so it can be assumed by the nodes of the provisioned Kubernetes
// cluster.
func CreateIAMRoleForPolicy(ctx context.Context,
	accountID string,
	iamClient *iam.Client,
	name string,
	policyDocument,
	assumePolicyDocument PolicyDocument,
) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("iam-role", name),
	})

	iamPath := fmt.Sprintf("/%s/", config.ParsedGeneralConfig.Cluster.Name)

	// Create the IAM policy.
	_, err := iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     &name,
		PolicyDocument: jsonMarshalIAMPolicyDocument(ctx, policyDocument),
		Path:           &iamPath,
	})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		slog.InfoContext(ctx, "IAM Policy already exists")
	} else {
		assert.AssertErrNil(ctx, err, "Failed creating IAM Policy")
		slog.InfoContext(ctx, "Created IAM Policy")
	}

	// Create IAM Role (with IAM Trust Policy, so it can be assumed).
	_, err = iamClient.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 &name,
		AssumeRolePolicyDocument: jsonMarshalIAMPolicyDocument(ctx, assumePolicyDocument),
		Path:                     &iamPath,
	})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		slog.InfoContext(ctx, "IAM Role already exists")
	} else {
		assert.AssertErrNil(ctx, err, "Failed creating IAM Role")
		slog.InfoContext(ctx, "Created IAM Role")
	}

	// Link the IAM Role and Policy
	_, err = iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  &name,
		PolicyArn: aws.String(fmt.Sprintf("arn:aws:iam::%s:policy%s%s", accountID, iamPath, name)),
	})
	assert.AssertErrNil(ctx, err, "Failed attaching IAM Role and Policy")
	slog.InfoContext(ctx, "Attached IAM Role and Policy")
}

// JSON marshals the given IAM Policy document and returns the result.
func jsonMarshalIAMPolicyDocument(ctx context.Context, policyDocument PolicyDocument) *string {
	policyDocumentAsBytes, err := json.Marshal(policyDocument)
	assert.AssertErrNil(ctx, err, "Failed JSON marshalling IAM Policy document")

	return aws.String(string(policyDocumentAsBytes))
}
