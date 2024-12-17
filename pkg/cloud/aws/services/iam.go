package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/logger"
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

// CreateIAMPolicy creates an IAM policy.
func CreateIAMPolicy(ctx context.Context, iamClient *iam.Client, name string, document PolicyDocument) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("iam-policy", name),
	})

	documentAsBytes, err := json.Marshal(document)
	assert.AssertErrNil(ctx, err, "Failed JSON marshalling IAM Policy document")

	_, err = iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
		PolicyName:     aws.String(name),
		PolicyDocument: aws.String(string(documentAsBytes)),
	})
	switch {
	// IAM Policy already exists.
	case strings.Contains(err.Error(), "already exists"):
		slog.InfoContext(ctx, "IAM Policy already exists")

	default:
		assert.AssertErrNil(ctx, err, "Failed creating IAM Policy")
		slog.InfoContext(ctx, "Created IAM Policy")
	}
}
