// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package enrolment

import (
	"context"
	"strings"
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
	data := Render(bundle("001", ns), []byte("it's\n"), nil)
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
	// deb: the Wazuh apt repository (signature checked by apt), pinned
	// version; rpm: signature checked against the imported Wazuh key.
	assert.Contains(t, linux, `curl -fsSLo "$T/wazuh.asc" https://packages.wazuh.com/key/GPG-KEY-WAZUH`+"\n")
	assert.Contains(t, linux, `grep -q '^fpr:*0DCFCA5547B19D2A6099506096B3EE5F29111145:'`)
	assert.Contains(t, linux, `echo "deb [signed-by=$K] https://packages.wazuh.com/4.x/apt/ stable main" > "$L"`)
	assert.Contains(t, linux, `apt-get install -y "wazuh-agent=$V"`)
	assert.Contains(t, linux, "https://packages.wazuh.com/4.x/yum/$F")
	assert.Contains(t, linux, `rpm --import "$T/wazuh.asc"`)
	assert.Contains(t, linux, `S=$(rpm -K "$T/$F")`)
	assert.NotContains(t, linux, "WAZUH_REGISTRATION_CA", "no CA, no verification")

	win := string(data[KeyInstallWindows])
	assert.Contains(t, win, "https://packages.wazuh.com/4.x/windows/wazuh-agent-4.14.8-1.msi")
	assert.Contains(t, win, "https://packages.wazuh.com/4.x/checksums/wazuh/4.14.8/wazuh-agent-4.14.8-1.msi.sha512")
	assert.Contains(t, win, "(Get-FileHash -Algorithm SHA512 $msi).Hash -ne $sum")
	assert.Contains(t, win, "$sig.Status -ne 'Valid' -or $sig.SignerCertificate.Subject -notlike '*Wazuh, Inc*'")
	assert.NotContains(t, win, "WAZUH_REGISTRATION_CA")
	assert.Contains(t, win, `'WAZUH_MANAGER="agents.example.com"'`)
	assert.Contains(t, win, `'WAZUH_REGISTRATION_PASSWORD="it''s"'`)

	mac := string(data[KeyInstallMacOS])
	assert.Contains(t, mac, `'WAZUH_REGISTRATION_PASSWORD='\''it'\''\'\'''\''s'\'''`)
	assert.Contains(t, mac, "> /tmp/wazuh_envs\n")
	assert.Contains(t, mac, `F="wazuh-agent-4.14.8-1.$A.pkg"`)
	assert.Contains(t, mac, `"https://packages.wazuh.com/4.x/macos/$F"`)
	assert.Contains(t, mac, `"https://packages.wazuh.com/4.x/checksums/wazuh/4.14.8/$F.sha512"`)
	assert.Contains(t, mac, `shasum -a 512 "$T/$F"`)
	assert.Contains(t, mac, "grep -qF 'Developer ID Installer: Wazuh Inc (KLZK8P68R5)'")
	assert.NotContains(t, mac, "WAZUH_REGISTRATION_CA")
	assert.NotContains(t, data, KeyManagerCA)

	for k, v := range data {
		assert.NotContains(t, string(v), "WAZUH_AGENT_GROUP", k)
	}
}

const testCA = "-----BEGIN CERTIFICATE-----\nMIIBtest\n-----END CERTIFICATE-----"

func TestRenderWithCA(t *testing.T) {
	t.Parallel()
	data := Render(bundle("001", ns), []byte("pw"), []byte(testCA+"\n\n"))
	assert.Equal(t, testCA+"\n", string(data[KeyManagerCA]))

	caFile := "cat > /etc/wazuh-manager-ca.pem <<'WAZUH_MANAGER_CA'\n" + testCA + "\nWAZUH_MANAGER_CA\n"
	linux := string(data[KeyInstallLinux])
	assert.Contains(t, linux, caFile)
	assert.Contains(t, linux, "export WAZUH_REGISTRATION_CA=/etc/wazuh-manager-ca.pem\n")
	assert.Less(t, strings.Index(linux, "WAZUH_REGISTRATION_CA"), strings.Index(linux, "apt-get install"),
		"the CA is set before the package reads the variables")

	mac := string(data[KeyInstallMacOS])
	assert.Contains(t, mac, caFile)
	assert.Contains(t, mac, `'WAZUH_REGISTRATION_CA='\''/etc/wazuh-manager-ca.pem'\'''`)

	win := string(data[KeyInstallWindows])
	assert.Contains(t, win, "$ca = Join-Path $env:ProgramData 'wazuh-manager-ca.pem'\n")
	assert.Contains(t, win, "-Value @'\n"+testCA+"\n'@\n")
	assert.Contains(t, win, `('WAZUH_REGISTRATION_CA="{0}"' -f $ca)`)
}

func TestEnsureCA(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := bundle("001", ns)
	b.CASecretRef = &config.SecretRef{Namespace: ns, Name: "wazuh-manager-tls", Key: "ca.crt"}
	kube := fake.NewClientset(authd(ns, "pw"))

	res := Ensure(ctx, kube, []config.EnrolmentBundle{b}, false)
	require.Len(t, res, 1)
	assert.Equal(t, report.ActionCreate, res[0].Action, "a missing CA Secret is not an error")
	assert.Contains(t, res[0].Detail, "without manager CA")
	sec, err := kube.CoreV1().Secrets(ns).Get(ctx, bundleName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.NotContains(t, sec.Data, KeyManagerCA)

	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "wazuh-manager-tls"},
		Data:       map[string][]byte{"ca.crt": []byte(testCA)},
	}
	_, err = kube.CoreV1().Secrets(ns).Create(ctx, caSecret, metav1.CreateOptions{})
	require.NoError(t, err)
	res = Ensure(ctx, kube, []config.EnrolmentBundle{b}, false)
	assert.Equal(t, report.ActionUpdate, res[0].Action)
	assert.Equal(t, "keys install-linux.sh,install-macos.sh,install-windows.ps1,manager-ca.pem", res[0].Detail)

	caSecret.Data = map[string][]byte{"tls.crt": []byte("x")}
	_, err = kube.CoreV1().Secrets(ns).Update(ctx, caSecret, metav1.UpdateOptions{})
	require.NoError(t, err)
	res = Ensure(ctx, kube, []config.EnrolmentBundle{b}, false)
	assert.Equal(t, report.ActionError, res[0].Action, "a CA Secret without the key is an error")
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
