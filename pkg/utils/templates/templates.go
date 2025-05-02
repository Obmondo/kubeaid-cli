package templates

import (
	"bytes"
	"context"
	"embed"
	"log/slog"
	"text/template"

	"github.com/go-sprout/sprout"
	"github.com/go-sprout/sprout/registry/encoding"
	"github.com/go-sprout/sprout/registry/strings"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

func ParseAndExecuteTemplate(
	ctx context.Context,
	embeddedFS *embed.FS,
	templateFileName string,
	values any,
) []byte {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("template", templateFileName),
	})

	contentsAsBytes, err := embeddedFS.ReadFile(templateFileName)
	assert.AssertErrNil(ctx, err, "Failed getting template from embedded file-system")

	sproutFuncs := sprout.New(sprout.WithRegistries(
		encoding.NewRegistry(),
		strings.NewRegistry(),
	)).Build()

	parsedTemplate, err := template.New(templateFileName).
		Funcs(sproutFuncs).
		Parse(string(contentsAsBytes))
	assert.AssertErrNil(ctx, err, "Failed parsing template")

	var executedTemplate bytes.Buffer
	err = parsedTemplate.Execute(&executedTemplate, values)
	assert.AssertErrNil(ctx, err, "Failed executing template")
	return executedTemplate.Bytes()
}
