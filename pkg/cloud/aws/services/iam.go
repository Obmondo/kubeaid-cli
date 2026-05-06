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

type IAMAPI interface {
	CreatePolicy(ctx context.Context, params *iam.CreatePolicyInput, optFns ...func(*iam.Options)) (*iam.CreatePolicyOutput, error)
	CreateRole(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	AttachRolePolicy(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error)
}

// CreateIAMRoleForPolicy creates an IAM role with the given IAM policy. It also links an IAM trust
// policy for the IAM role, so it can be assumed by the nodes of the provisioned Kubernetes
// cluster.
func CreateIAMRoleForPolicy(ctx context.Context,
	accountID string,
	iamClient IAMAPI,
	name string,
	policyDocument,
	assumePolicyDocument PolicyDocument,
) error {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("iam-role", name),
	})

	iamPath := fmt.Sprintf("/%s/", config.ParsedGeneralConfig.Cluster.Name)

	policyDocStr, err := jsonMarshalIAMPolicyDocument(policyDocument)
	if err != nil {
		return fmt.Errorf("marshalling IAM policy document: %w", err)
	}

	_, err = iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     &name,
		PolicyDocument: policyDocStr,
		Path:           &iamPath,
	})
	switch {
	case err != nil && strings.Contains(err.Error(), "already exists"):
		slog.InfoContext(ctx, "IAM Policy already exists")
	case err != nil:
		return fmt.Errorf("creating IAM Policy: %w", err)
	default:
		slog.InfoContext(ctx, "Created IAM Policy")
	}

	assumePolicyDocStr, err := jsonMarshalIAMPolicyDocument(assumePolicyDocument)
	if err != nil {
		return fmt.Errorf("marshalling IAM trust policy document: %w", err)
	}

	_, err = iamClient.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 &name,
		AssumeRolePolicyDocument: assumePolicyDocStr,
		Path:                     &iamPath,
	})
	switch {
	case err != nil && strings.Contains(err.Error(), "already exists"):
		slog.InfoContext(ctx, "IAM Role already exists")
	case err != nil:
		return fmt.Errorf("creating IAM Role: %w", err)
	default:
		slog.InfoContext(ctx, "Created IAM Role")
	}

	_, err = iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  &name,
		PolicyArn: aws.String(fmt.Sprintf("arn:aws:iam::%s:policy%s%s", accountID, iamPath, name)),
	})
	if err != nil {
		return fmt.Errorf("attaching IAM Role and Policy: %w", err)
	}
	slog.InfoContext(ctx, "Attached IAM Role and Policy")

	return nil
}

func jsonMarshalIAMPolicyDocument(policyDocument PolicyDocument) (*string, error) {
	policyDocumentAsBytes, err := json.Marshal(policyDocument)
	if err != nil {
		return nil, fmt.Errorf("marshalling IAM policy document: %w", err)
	}

	return aws.String(string(policyDocumentAsBytes)), nil
}
