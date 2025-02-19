package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

type (
	// PolicyDocument defines a policy document as a Go struct that can be serialized
	// to JSON.
	PolicyDocument struct {
		Version   string
		Statement []PolicyStatement
	}

	// PolicyStatement defines a statement in a policy document.
	PolicyStatement struct {
		Effect    string
		Action    []string
		Principal map[string]string `json:",omitempty"`
		Resource  string            `json:",omitempty"`
	}
)

// CreateIAMRoleForPolicy creates an IAM role with the given IAM policy. It also links an IAM trust
// policy for the IAM role, so it can be assumed by the nodes of the provisioned Kubernetes
// cluster.
func CreateIAMRoleForPolicy(ctx context.Context,
	iamClient *iam.Client,
	name string,
	policyDocument,
	assumePolicyDocument PolicyDocument,
) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("iam-role", name),
	})

	// Create the IAM policy.

	iamPolicyPath := fmt.Sprintf("/%s/", name)

	_, err := iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     &name,
		PolicyDocument: jsonMarshalIAMPolicyDocument(ctx, policyDocument),
		Path:           &iamPolicyPath,
	})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		slog.InfoContext(ctx, "IAM Policy already exists")
	} else {
		assert.AssertErrNil(ctx, err, "Failed creating IAM Policy")
		slog.InfoContext(ctx, "Created IAM Policy")
	}

	// Create IAM Role (with IAM Trust Policy, so it can be assumed) and link the above IAM Policy
	// to it.
	_, err = iamClient.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 &name,
		AssumeRolePolicyDocument: jsonMarshalIAMPolicyDocument(ctx, assumePolicyDocument),
		Path:                     &iamPolicyPath,
	})
	if err != nil && strings.Contains(err.Error(), "already exists") {
		slog.InfoContext(ctx, "IAM Role already exists")
	} else {
		assert.AssertErrNil(ctx, err, "Failed creating IAM Role")
		slog.InfoContext(ctx, "Created IAM Role")
	}
}

// JSON marshals the given IAM Policy document and returns the result.
func jsonMarshalIAMPolicyDocument(ctx context.Context, policyDocument PolicyDocument) *string {
	policyDocumentAsBytes, err := json.Marshal(policyDocument)
	assert.AssertErrNil(ctx, err, "Failed JSON marshalling IAM Policy document")

	return aws.String(string(policyDocumentAsBytes))
}
