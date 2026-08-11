// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func newApplyTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, coreV1.AddToScheme(s))
	return s
}

// applyCounter returns an interceptor.Funcs that counts Apply calls and
// succeeds unconditionally, simulating server-side apply behavior.
func applyCounter() (interceptor.Funcs, *int) {
	var mu sync.Mutex
	count := 0
	return interceptor.Funcs{
		Apply: func(_ context.Context, _ client.WithWatch, _ runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
			mu.Lock()
			count++
			mu.Unlock()
			return nil
		},
	}, &count
}

func TestApplyManifestFromReader(t *testing.T) {
	t.Parallel()

	scheme := newApplyTestScheme(t)

	tests := []struct {
		name           string
		yamlInput      string
		interceptApply func(ctx context.Context, cl client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error
		wantErr        bool
		wantErrSubstr  string
		wantApplyCount int
	}{
		{
			name: "applies single ConfigMap document",
			yamlInput: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
data:
  key: value
`,
			wantApplyCount: 1,
		},
		{
			name: "applies multi-document YAML",
			yamlInput: `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-one
  namespace: default
data:
  a: "1"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm-two
  namespace: default
data:
  b: "2"
`,
			wantApplyCount: 2,
		},
		{
			name:           "skips empty documents",
			yamlInput:      "---\n\n---\n",
			wantApplyCount: 0,
		},
		{
			name:          "returns error on invalid YAML",
			yamlInput:     "{{invalid yaml content}}",
			wantErr:       true,
			wantErrSubstr: "decoding YAML document",
		},
		{
			name: "returns error when Apply fails",
			yamlInput: `apiVersion: v1
kind: ConfigMap
metadata:
  name: fail-cm
  namespace: default
`,
			interceptApply: func(_ context.Context, _ client.WithWatch, _ runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
				return errors.New("conflict")
			},
			wantErr:       true,
			wantErrSubstr: "applying resource",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var applyCount *int
			var funcs interceptor.Funcs

			if tc.interceptApply != nil {
				funcs = interceptor.Funcs{
					Apply: tc.interceptApply,
				}
			} else {
				funcs, applyCount = applyCounter()
			}

			fakeClient := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithInterceptorFuncs(funcs).
				Build()

			err := ApplyManifestFromReader(context.Background(), fakeClient, strings.NewReader(tc.yamlInput))

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)

			if applyCount != nil {
				assert.Equal(t, tc.wantApplyCount, *applyCount)
			}
		})
	}
}

func TestApplyManifestFromFile(t *testing.T) {
	t.Parallel()

	scheme := newApplyTestScheme(t)

	tests := []struct {
		name           string
		fileContent    string
		useNonexistent bool
		wantErr        bool
		wantErrSubstr  string
		wantApplyCount int
	}{
		{
			name: "applies YAML from file",
			fileContent: `apiVersion: v1
kind: ConfigMap
metadata:
  name: from-file
  namespace: default
data:
  hello: world
`,
			wantApplyCount: 1,
		},
		{
			name:           "returns error for nonexistent file",
			useNonexistent: true,
			wantErr:        true,
			wantErrSubstr:  "opening manifest file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			funcs, applyCount := applyCounter()
			fakeClient := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithInterceptorFuncs(funcs).
				Build()

			var filePath string
			if tc.useNonexistent {
				filePath = filepath.Join(t.TempDir(), "nonexistent", "file.yaml")
			} else {
				dir := t.TempDir()
				filePath = filepath.Join(dir, "manifest.yaml")
				require.NoError(t, os.WriteFile(filePath, []byte(tc.fileContent), 0o600))
			}

			err := ApplyManifestFromFile(context.Background(), fakeClient, filePath)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantApplyCount, *applyCount)
		})
	}
}

func TestApplyManifestFromURL(t *testing.T) {
	t.Parallel()

	scheme := newApplyTestScheme(t)

	tests := []struct {
		name           string
		handler        http.HandlerFunc
		wantErr        bool
		wantErrSubstr  string
		wantApplyCount int
	}{
		{
			name: "applies YAML fetched from URL",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, `apiVersion: v1
kind: ConfigMap
metadata:
  name: from-url
  namespace: default
data:
  source: url
`)
			},
			wantApplyCount: 1,
		},
		{
			name: "returns error on non-200 status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:       true,
			wantErrSubstr: "unexpected status 404",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(tc.handler)
			t.Cleanup(server.Close)

			funcs, applyCount := applyCounter()
			fakeClient := crFake.NewClientBuilder().
				WithScheme(scheme).
				WithInterceptorFuncs(funcs).
				Build()

			err := ApplyManifestFromURL(context.Background(), fakeClient, server.URL)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantApplyCount, *applyCount)
		})
	}
}

func TestApplyManifestFromURL_ConnectionError(t *testing.T) {
	t.Parallel()

	scheme := newApplyTestScheme(t)
	fakeClient := crFake.NewClientBuilder().WithScheme(scheme).Build()

	err := ApplyManifestFromURL(context.Background(), fakeClient, "http://127.0.0.1:1/invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching manifest from")
}

func TestReplaceForceFromDir(t *testing.T) {
	t.Parallel()

	scheme := newApplyTestScheme(t)

	tests := []struct {
		name            string
		files           map[string]string
		preExist        []client.Object
		interceptCreate func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error
		interceptGet    func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
		wantErr         bool
		wantErrSubstr   string
		wantObjects     int
	}{
		{
			name: "creates resources from YAML files in directory",
			files: map[string]string{
				"cm.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: replaced-cm
  namespace: default
data:
  x: "1"
`,
			},
			wantObjects: 1,
		},
		{
			name: "skips non-YAML files",
			files: map[string]string{
				"readme.txt": "this is not yaml",
				"cm.yml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: only-yaml
  namespace: default
data:
  y: "2"
`,
			},
			wantObjects: 1,
		},
		{
			name:          "returns error for nonexistent directory",
			files:         nil,
			wantErr:       true,
			wantErrSubstr: "reading directory",
		},
		{
			name: "returns error when Create fails",
			files: map[string]string{
				"cm.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: fail-create
  namespace: default
`,
			},
			interceptCreate: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
				return errors.New("quota exceeded")
			},
			wantErr:       true,
			wantErrSubstr: "creating resource",
		},
		{
			name: "returns error when Get fails with non-NotFound error",
			files: map[string]string{
				"cm.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: get-fail
  namespace: default
`,
			},
			interceptGet: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return errors.New("internal server error")
			},
			wantErr:       true,
			wantErrSubstr: "getting existing resource",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var dirPath string
			if tc.files == nil {
				dirPath = filepath.Join(t.TempDir(), "nonexistent")
			} else {
				dirPath = t.TempDir()
				for name, content := range tc.files {
					require.NoError(t, os.WriteFile(filepath.Join(dirPath, name), []byte(content), 0o600))
				}
			}

			builder := crFake.NewClientBuilder().WithScheme(scheme)
			if tc.preExist != nil {
				builder = builder.WithObjects(tc.preExist...)
			}
			if tc.interceptCreate != nil {
				builder = builder.WithInterceptorFuncs(interceptor.Funcs{
					Create: tc.interceptCreate,
				})
			}
			if tc.interceptGet != nil {
				builder = builder.WithInterceptorFuncs(interceptor.Funcs{
					Get: tc.interceptGet,
				})
			}
			fakeClient := builder.Build()

			err := ReplaceForceFromDir(context.Background(), fakeClient, dirPath)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubstr)
				return
			}
			require.NoError(t, err)

			if tc.wantObjects > 0 {
				cmList := &coreV1.ConfigMapList{}
				require.NoError(t, fakeClient.List(context.Background(), cmList))
				assert.Equal(t, tc.wantObjects, len(cmList.Items))
			}
		})
	}
}

func TestReplaceForceFromDir_DeletesExisting(t *testing.T) {
	t.Parallel()

	scheme := newApplyTestScheme(t)

	dirPath := t.TempDir()
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: existing-cm
  namespace: default
data:
  version: "2"
`
	require.NoError(t, os.WriteFile(filepath.Join(dirPath, "cm.yaml"), []byte(manifest), 0o600))

	existingCM := &coreV1.ConfigMap{}
	existingCM.SetName("existing-cm")
	existingCM.SetNamespace("default")
	existingCM.Data = map[string]string{"version": "1"}

	fakeClient := crFake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingCM).
		Build()

	err := ReplaceForceFromDir(context.Background(), fakeClient, dirPath)
	require.NoError(t, err)

	got := &coreV1.ConfigMap{}
	require.NoError(t, fakeClient.Get(context.Background(),
		types.NamespacedName{Name: "existing-cm", Namespace: "default"}, got))
	assert.Equal(t, "2", got.Data["version"])
}
