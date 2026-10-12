// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package enrolment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Obmondo/kubeaid-cli/pkg/siem/config"
	"github.com/Obmondo/kubeaid-cli/pkg/siem/report"
)

const (
	ns        = "wazuh-001"
	authdName = "wazuh-authd-pass"
	authdKey  = "authd.pass"

	bundleName = "enrolment-bundle"
)

func bundle(tenant, namespace string) config.EnrolmentBundle {
	return config.EnrolmentBundle{
		Tenant: tenant, Namespace: namespace, Name: bundleName,
		ManagerHost: "agents.example.com", RegistrationPort: 21015, EventsPort: 21014,
		AuthdSecretRef: config.SecretRef{Namespace: namespace, Name: authdName, Key: authdKey},
		AgentVersion:   config.DefaultWazuhAgentVersion,
	}
}

func authd(namespace, value string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: authdName},
		Data:       map[string][]byte{authdKey: []byte(value)},
	}
}

func TestRender(t *testing.T) {
	t.Parallel()
	data := Render(bundle("001", ns), []byte("it's\n"))
	assert.Equal(t, "agents.example.com", string(data[KeyManagerHost]))
	assert.Equal(t, "21015", string(data[KeyRegistrationPort]))
	assert.Equal(t, "21014", string(data[KeyEventsPort]))
	assert.Equal(t, "it's\n", string(data[KeyAuthdPass]), "authd.pass is an exact copy")

	linux := string(data[KeyInstallLinux])
	assert.Contains(t, linux, "export WAZUH_MANAGER='agents.example.com'\n")
	assert.Contains(t, linux, "export WAZUH_MANAGER_PORT='21014'\n")
	assert.Contains(t, linux, "export WAZUH_REGISTRATION_SERVER='agents.example.com'\n")
	assert.Contains(t, linux, "export WAZUH_REGISTRATION_PORT='21015'\n")
	assert.Contains(t, linux, `export WAZUH_REGISTRATION_PASSWORD='it'\''s'`+"\n")
	assert.Contains(t, linux, "V=4.14.8-1\n")
	assert.Contains(t, linux, "https://packages.wazuh.com/4.x/apt/pool/main/w/wazuh-agent/$F")
	assert.Contains(t, linux, "https://packages.wazuh.com/4.x/yum/$F")

	win := string(data[KeyInstallWindows])
	assert.Contains(t, win, "https://packages.wazuh.com/4.x/windows/wazuh-agent-4.14.8-1.msi")
	assert.Contains(t, win, `'WAZUH_MANAGER="agents.example.com"'`)
	assert.Contains(t, win, `'WAZUH_REGISTRATION_PASSWORD="it''s"'`)

	mac := string(data[KeyInstallMacOS])
	assert.Contains(t, mac, `'WAZUH_REGISTRATION_PASSWORD='\''it'\''\'\'''\''s'\'''`)
	assert.Contains(t, mac, "> /tmp/wazuh_envs\n")
	assert.Contains(t, mac, "https://packages.wazuh.com/4.x/macos/wazuh-agent-4.14.8-1.$A.pkg")

	for k, v := range data {
		assert.NotContains(t, string(v), "WAZUH_AGENT_GROUP", k)
	}
}

func TestEnsure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kube := fake.NewClientset(authd(ns, "pw-1"))
	bundles := []config.EnrolmentBundle{bundle("002", "wazuh-002"), bundle("001", ns)}

	res := Ensure(ctx, kube, bundles, true)
	require.Len(t, res, 2)
	assert.Equal(t, report.ActionError, res[0].Action, "missing authd Secret")
	assert.Equal(t, report.ActionCreate, res[1].Action, "the other bundle continues")
	for _, a := range kube.Actions() {
		assert.Equal(t, "get", a.GetVerb(), "dry run only reads")
	}

	res = Ensure(ctx, kube, bundles, false)
	assert.Equal(t, report.ActionCreate, res[1].Action)
	sec, err := kube.CoreV1().Secrets(ns).Get(ctx, bundleName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "pw-1", string(sec.Data[KeyAuthdPass]))
	assert.Len(t, sec.Data, 7)

	res = Ensure(ctx, kube, bundles[1:], false)
	assert.Equal(t, report.ActionOK, res[0].Action)

	cur, err := kube.CoreV1().Secrets(ns).Get(ctx, authdName, metav1.GetOptions{})
	require.NoError(t, err)
	cur.Data[authdKey] = []byte("pw-2")
	_, err = kube.CoreV1().Secrets(ns).Update(ctx, cur, metav1.UpdateOptions{})
	require.NoError(t, err)
	res = Ensure(ctx, kube, bundles[1:], false)
	assert.Equal(t, report.ActionUpdate, res[0].Action)
	assert.Equal(t, "keys authd.pass,install-linux.sh,install-macos.sh,install-windows.ps1", res[0].Detail)
	sec, err = kube.CoreV1().Secrets(ns).Get(ctx, bundleName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "pw-2", string(sec.Data[KeyAuthdPass]))
}
