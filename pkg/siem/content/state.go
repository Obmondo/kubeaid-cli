// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package content

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// State is the last content that went in cleanly on one target: the
// rollback source and the record of which files the content owns.
type State struct {
	Version string `json:"version"`
	Hash    string `json:"hash"`
	// Files maps kind/name to content (Wazuh), or artifact name to
	// definition (Velociraptor).
	Files map[string][]byte `json:"files"`
}

// StateStore keeps one State per target key ("wazuh-<code>",
// "velociraptor").
type StateStore interface {
	// Load returns nil when nothing was stored yet.
	Load(ctx context.Context, key string) (*State, error)
	Save(ctx context.Context, key string, st State) error
}

// Labels and keys of the state Secrets. The version label is what
// monitoring reads (kube-state-metrics kube_secret_labels, or kubectl).
const (
	stateDataKey      = "state.json.gz"
	versionLabel      = "kubesoc.io/content-version"
	hashAnnotation    = "kubesoc.io/content-hash"
	targetLabel       = "kubesoc.io/content-target"
	managedByLabel    = "app.kubernetes.io/managed-by"
	managedByValue    = "siem-reconciler"
	maxLabelValueSize = 63
)

var labelUnsafe = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// LabelValue makes a version usable as a label value.
func LabelValue(v string) string {
	v = labelUnsafe.ReplaceAllString(v, "_")
	if len(v) > maxLabelValueSize {
		v = v[:maxLabelValueSize]
	}
	return strings.Trim(v, "_.-")
}

// SecretStore keeps each State gzipped in Secret <Prefix><key>.
type SecretStore struct {
	Kube      kubernetes.Interface
	Namespace string
	Prefix    string
}

func (s SecretStore) name(key string) string { return s.Prefix + key }

// Load implements StateStore.
func (s SecretStore) Load(ctx context.Context, key string) (*State, error) {
	sec, err := s.Kube.CoreV1().Secrets(s.Namespace).Get(ctx, s.name(key), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading content state %s/%s: %w", s.Namespace, s.name(key), err)
	}
	raw := sec.Data[stateDataKey]
	if len(raw) == 0 {
		return nil, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("content state %s: %w", s.name(key), err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("content state %s: %w", s.name(key), err)
	}
	var st State
	if err := json.Unmarshal(plain, &st); err != nil {
		return nil, fmt.Errorf("content state %s: %w", s.name(key), err)
	}
	return &st, nil
}

// Save implements StateStore.
func (s SecretStore) Save(ctx context.Context, key string, st State) error {
	plain, err := json.Marshal(st)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(plain); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	client := s.Kube.CoreV1().Secrets(s.Namespace)
	labels := map[string]string{managedByLabel: managedByValue, targetLabel: LabelValue(key), versionLabel: LabelValue(st.Version)}
	cur, err := client.Get(ctx, s.name(key), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: s.name(key), Namespace: s.Namespace, Labels: labels,
				Annotations: map[string]string{hashAnnotation: st.Hash},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{stateDataKey: buf.Bytes()},
		}
		if _, err := client.Create(ctx, sec, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("creating content state %s/%s: %w", s.Namespace, s.name(key), err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading content state %s/%s: %w", s.Namespace, s.name(key), err)
	}
	if cur.Labels == nil {
		cur.Labels = map[string]string{}
	}
	for k, v := range labels {
		cur.Labels[k] = v
	}
	if cur.Annotations == nil {
		cur.Annotations = map[string]string{}
	}
	cur.Annotations[hashAnnotation] = st.Hash
	cur.Data = map[string][]byte{stateDataKey: buf.Bytes()}
	if _, err := client.Update(ctx, cur, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating content state %s/%s: %w", s.Namespace, s.name(key), err)
	}
	return nil
}
