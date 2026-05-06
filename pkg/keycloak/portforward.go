// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package keycloak

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForward forwards a free local port to servicePort on the
// first Ready pod behind the named Service. Returns the local
// http URL and a stop function the caller must call to release
// the forward.
func PortForward(
	ctx context.Context,
	restConfig *rest.Config,
	namespace, serviceName string,
	servicePort int,
) (string, func(), error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return "", nil, fmt.Errorf("constructing kubernetes clientset: %w", err)
	}

	pod, err := pickPodBehindService(ctx, clientset, namespace, serviceName)
	if err != nil {
		return "", nil, err
	}

	localPort, err := pickFreeLocalPort()
	if err != nil {
		return "", nil, fmt.Errorf("allocating local port: %w", err)
	}

	dialer, err := newPortforwardDialer(restConfig, namespace, pod.Name)
	if err != nil {
		return "", nil, err
	}

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	fw, err := portforward.New(
		dialer,
		[]string{fmt.Sprintf("%d:%d", localPort, servicePort)},
		stopCh,
		readyCh,
		io.Discard,
		io.Discard,
	)
	if err != nil {
		return "", nil, fmt.Errorf("constructing port-forwarder: %w", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- fw.ForwardPorts() }()

	select {
	case <-readyCh:
	case err := <-errCh:
		return "", nil, fmt.Errorf("port-forward failed before ready: %w", err)
	case <-ctx.Done():
		close(stopCh)
		return "", nil, ctx.Err()
	}

	stop := func() {
		close(stopCh)
		<-errCh // drain so the ForwardPorts goroutine exits
	}
	return fmt.Sprintf("http://localhost:%d", localPort), stop, nil
}

func pickPodBehindService(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace, serviceName string,
) (*coreV1.Pod, error) {
	svc, err := clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metaV1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("reading Service %s/%s: %w", namespace, serviceName, err)
	}
	if len(svc.Spec.Selector) == 0 {
		return nil, fmt.Errorf("service %s/%s has no selector", namespace, serviceName)
	}

	selector := labels.SelectorFromSet(svc.Spec.Selector).String()
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metaV1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing pods for Service %s/%s: %w", namespace, serviceName, err)
	}

	for i := range pods.Items {
		if isPodReady(&pods.Items[i]) {
			return &pods.Items[i], nil
		}
	}
	return nil, fmt.Errorf(
		"no Ready pod found behind Service %s/%s (selector %q)",
		namespace, serviceName, selector,
	)
}

func isPodReady(p *coreV1.Pod) bool {
	if p.Status.Phase != coreV1.PodRunning {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == coreV1.PodReady && c.Status == coreV1.ConditionTrue {
			return true
		}
	}
	return false
}

// pickFreeLocalPort relies on the kernel's :0 allocation. Same
// approach as kubectl port-forward; the brief window between
// close and rebind is acceptable.
func pickFreeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = listener.Close() }()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("listener address is not TCP")
	}
	return addr.Port, nil
}

func newPortforwardDialer(
	restConfig *rest.Config,
	namespace, podName string,
) (httpstream.Dialer, error) {
	roundTripper, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("constructing SPDY round-tripper: %w", err)
	}

	host, err := url.Parse(restConfig.Host)
	if err != nil {
		return nil, fmt.Errorf("parsing rest.Config.Host %q: %w", restConfig.Host, err)
	}

	subresourcePath := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	endpoint := &url.URL{
		Scheme: host.Scheme,
		Host:   host.Host,
		Path:   host.Path + subresourcePath,
	}
	return spdy.NewDialer(
		upgrader,
		&http.Client{Transport: roundTripper},
		http.MethodPost,
		endpoint,
	), nil
}
