// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package secrets reads the Kubernetes Secrets the SIEM reconciler
// needs and creates missing generated Secrets. It never overwrites an
// existing value.
package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/randval"
)

// Component is the report component name.
const Component = "secrets"

// managedByLabel marks Secrets the reconciler created.
const (
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "siem-reconciler"
	sealedKind     = "SealedSecret"
	hex32Bytes     = 32
)

// Store reads secret values from the cluster.
type Store struct {
	Kube kubernetes.Interface
}

// Read returns the value of one Secret key. The value is never logged.
func (s Store) Read(ctx context.Context, ref config.SecretRef) (string, error) {
	sec, err := s.Kube.CoreV1().Secrets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("reading Secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	v, ok := sec.Data[ref.Key]
	if !ok || len(v) == 0 {
		return "", fmt.Errorf("secret %s/%s has no key %q", ref.Namespace, ref.Name, ref.Key)
	}
	return strings.TrimRight(string(v), "\r\n"), nil
}

// Ensure creates missing generated Secrets and adds missing keys to
// Secrets the reconciler owns or that are not SealedSecret-managed.
// Existing values are never changed. In dry-run mode nothing is
// written.
func Ensure(ctx context.Context, kube kubernetes.Interface, specs []config.GeneratedSecret, dryRun bool) []report.Result {
	results := make([]report.Result, 0, len(specs))
	for _, spec := range specs {
		results = append(results, ensureOne(ctx, kube, spec, dryRun))
	}
	return results
}

func ensureOne(ctx context.Context, kube kubernetes.Interface, spec config.GeneratedSecret, dryRun bool) report.Result {
	res := report.Result{Component: Component, Kind: "secret", Name: spec.Namespace + "/" + spec.Name, Action: report.ActionOK}
	fail := func(err error) report.Result {
		res.Action, res.Detail = report.ActionError, err.Error()
		return res
	}
	client := kube.CoreV1().Secrets(spec.Namespace)

	cur, err := client.Get(ctx, spec.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		res.Action = report.ActionCreate
		res.Detail = "keys " + strings.Join(keyNames(spec.Keys), ",")
		if dryRun {
			return res
		}
		data, err := generate(spec.Keys)
		if err != nil {
			return fail(err)
		}
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      spec.Name,
				Namespace: spec.Namespace,
				Labels:    map[string]string{managedByLabel: managedByValue},
			},
			Type: corev1.SecretTypeOpaque,
			Data: data,
		}
		if _, err := client.Create(ctx, sec, metav1.CreateOptions{}); err != nil {
			return fail(fmt.Errorf("creating Secret: %w", err))
		}
		return res
	}
	if err != nil {
		return fail(fmt.Errorf("reading Secret: %w", err))
	}

	var absent []config.GeneratedKey
	for _, k := range spec.Keys {
		if len(cur.Data[k.Key]) == 0 {
			absent = append(absent, k)
		}
	}
	if len(absent) == 0 {
		return res
	}
	res.Detail = "missing keys " + strings.Join(keyNames(absent), ",")
	if ownedBySealedSecret(cur) {
		// The sealed-secrets controller would revert an added key; the
		// SealedSecret must be fixed in git instead.
		return fail(fmt.Errorf("%s; Secret is managed by a SealedSecret, fix it there", res.Detail))
	}
	res.Action = report.ActionUpdate
	if dryRun {
		return res
	}
	data, err := generate(absent)
	if err != nil {
		return fail(err)
	}
	if cur.Data == nil {
		cur.Data = map[string][]byte{}
	}
	for k, v := range data {
		cur.Data[k] = v
	}
	if _, err := client.Update(ctx, cur, metav1.UpdateOptions{}); err != nil {
		return fail(fmt.Errorf("adding keys: %w", err))
	}
	return res
}

func ownedBySealedSecret(s *corev1.Secret) bool {
	for _, o := range s.OwnerReferences {
		if o.Kind == sealedKind {
			return true
		}
	}
	return false
}

func keyNames(keys []config.GeneratedKey) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k.Key)
	}
	sort.Strings(out)
	return out
}

func generate(keys []config.GeneratedKey) (map[string][]byte, error) {
	out := make(map[string][]byte, len(keys))
	for _, k := range keys {
		var (
			v   string
			err error
		)
		switch k.Generator {
		case config.GeneratorHex32:
			raw := make([]byte, hex32Bytes)
			if _, err = rand.Read(raw); err == nil {
				v = hex.EncodeToString(raw)
			}
		case config.GeneratorBase64:
			v, err = randval.Base64Key(hex32Bytes)
		default:
			v, err = randval.Password()
		}
		if err != nil {
			return nil, fmt.Errorf("generating %s: %w", k.Key, err)
		}
		out[k.Key] = []byte(v)
	}
	return out, nil
}

// Publish stores data under key in the named Secret, creating the
// Secret when missing and replacing the key when its value differs.
// Used by `siem-reconciler publish-api-client`, whose input (the
// Velociraptor API client) is re-minted on every server start.
func Publish(ctx context.Context, kube kubernetes.Interface, namespace, name, key string, data []byte) (report.Action, error) {
	client := kube.CoreV1().Secrets(namespace)
	cur, err := client.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{managedByLabel: managedByValue},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{key: data},
		}
		if _, err := client.Create(ctx, sec, metav1.CreateOptions{}); err != nil {
			return report.ActionError, fmt.Errorf("creating Secret %s/%s: %w", namespace, name, err)
		}
		return report.ActionCreate, nil
	}
	if err != nil {
		return report.ActionError, fmt.Errorf("reading Secret %s/%s: %w", namespace, name, err)
	}
	if bytes.Equal(cur.Data[key], data) {
		return report.ActionOK, nil
	}
	if cur.Data == nil {
		cur.Data = map[string][]byte{}
	}
	cur.Data[key] = data
	if _, err := client.Update(ctx, cur, metav1.UpdateOptions{}); err != nil {
		return report.ActionError, fmt.Errorf("updating Secret %s/%s: %w", namespace, name, err)
	}
	return report.ActionUpdate, nil
}
