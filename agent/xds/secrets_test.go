// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package xds

import (
	"testing"

	envoy_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoy_listener_v3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_tls_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/hashicorp/consul/agent/proxycfg"
	"github.com/hashicorp/consul/agent/structs"
	"github.com/hashicorp/consul/agent/xds/config"
	"github.com/hashicorp/consul/lib"
	"github.com/hashicorp/consul/proto/private/prototest"
)

func TestSecretsFromSnapshot_NonTerminatingGatewayKindsReturnNil(t *testing.T) {
	s := &ResourceGenerator{Logger: hclog.NewNullLogger()}

	snap := proxycfg.TestConfigSnapshotTerminatingGateway(t, true, nil, nil)
	snap.Kind = structs.ServiceKindIngressGateway

	resources, err := s.secretsFromSnapshot(snap)
	require.NoError(t, err)
	require.Nil(t, resources)
}

func TestSecretsFromSnapshot_ConnectProxyReturnsLeafAndRootSecrets(t *testing.T) {
	s := &ResourceGenerator{Logger: hclog.NewNullLogger()}

	snap := proxycfg.TestConfigSnapshot(t, nil, nil)

	resources, err := s.secretsFromSnapshot(snap)
	require.NoError(t, err)
	require.Len(t, resources, 2)

	secrets := make(map[string]*envoy_tls_v3.Secret, len(resources))
	for _, resource := range resources {
		secret := resource.(*envoy_tls_v3.Secret)
		secrets[secret.Name] = secret
	}

	leafSecret := secrets[connectLeafSecretName]
	require.NotNil(t, leafSecret)
	leafCert, ok := leafSecret.Type.(*envoy_tls_v3.Secret_TlsCertificate)
	require.True(t, ok)
	require.Equal(t, lib.EnsureTrailingNewline(snap.Leaf().CertPEM), leafCert.TlsCertificate.CertificateChain.GetInlineString())
	require.Equal(t, lib.EnsureTrailingNewline(snap.Leaf().PrivateKeyPEM), leafCert.TlsCertificate.PrivateKey.GetInlineString())

	rootSecret := secrets[connectRootSecretName]
	require.NotNil(t, rootSecret)
	rootCA, ok := rootSecret.Type.(*envoy_tls_v3.Secret_ValidationContext)
	require.True(t, ok)
	require.Equal(t, snap.RootPEMs(), rootCA.ValidationContext.TrustedCa.GetInlineString())
}

// TestSecretsFromSnapshot_ConnectProxyEmitsEachSecretIndependently covers the
// degraded states where the snapshot is valid but only part of the mTLS
// material is present: a mesh gateway with no exported services (which cancels
// its leaf watch and sets Leaf to nil while keeping valid CA roots), and a root
// watch that has produced no CA roots.
//
// Each secret is emitted independently when its material is available.
// A nil leaf omits only connect-leaf, while empty roots omit only connect-root;
// neither case should panic or emit an invalid empty PEM.
func TestSecretsFromSnapshot_ConnectProxyEmitsEachSecretIndependently(t *testing.T) {
	s := &ResourceGenerator{Logger: hclog.NewNullLogger()}

	secretNames := func(t *testing.T, resources []proto.Message) []string {
		t.Helper()
		var names []string
		for _, res := range resources {
			secret, ok := res.(*envoy_tls_v3.Secret)
			require.True(t, ok)
			names = append(names, secret.Name)
		}
		return names
	}

	// A mesh gateway with no exported services cancels its leaf watch and sets
	// Leaf to nil while keeping valid CA roots. The root secret must still be
	// delivered in that state, and reading the nil leaf must not panic.
	t.Run("nil leaf still emits the root secret", func(t *testing.T) {
		snap := proxycfg.TestConfigSnapshot(t, nil, nil)
		snap.ConnectProxy.Leaf = nil
		require.NotEmpty(t, snap.RootPEMs())

		require.NotPanics(t, func() {
			resources, err := s.secretsFromSnapshot(snap)
			require.NoError(t, err)
			require.Equal(t, []string{connectRootSecretName}, secretNames(t, resources))
		})
	})

	t.Run("no CA roots still emits the leaf secret", func(t *testing.T) {
		snap := proxycfg.TestConfigSnapshot(t, nil, nil)
		snap.Roots.Roots = nil
		require.Empty(t, snap.RootPEMs())
		require.NotNil(t, snap.Leaf())

		resources, err := s.secretsFromSnapshot(snap)
		require.NoError(t, err)
		require.Equal(t, []string{connectLeafSecretName}, secretNames(t, resources))
	})

	// An empty trusted CA is rejected by Envoy, so the root secret must be
	// withheld rather than emitted with an empty inline string.
	t.Run("neither available yields no secrets", func(t *testing.T) {
		snap := proxycfg.TestConfigSnapshot(t, nil, nil)
		snap.ConnectProxy.Leaf = nil
		snap.Roots.Roots = nil

		resources, err := s.secretsFromSnapshot(snap)
		require.NoError(t, err)
		require.Empty(t, resources)
	})
}

// TestCreateDownstreamTransportSocketForConnectTLS_StableWhileMaterialMissing
// asserts that the public listener's transport socket is byte-identical
// whether or not the leaf and roots are currently available.
//
// This is what makes a transient certificate gap non-disruptive: the listener
// proto does not change, so its filter chain hash does not change and Envoy
// does not drain established connections. Gating listener generation on
// certificate readiness would break this property and reintroduce the drain
// that this SDS change exists to eliminate.
func TestCreateDownstreamTransportSocketForConnectTLS_StableWhileMaterialMissing(t *testing.T) {
	build := func(mutate func(*proxycfg.ConfigSnapshot)) *envoy_core_v3.TransportSocket {
		snap := proxycfg.TestConfigSnapshot(t, nil, nil)
		if mutate != nil {
			mutate(snap)
		}

		ts, err := createDownstreamTransportSocketForConnectTLS(snap, &config.ProxyConfig{Protocol: "tcp"}, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, ts, "public listener must still be given a TLS transport socket")
		return ts
	}

	ready := build(nil)

	t.Run("identical when leaf is missing", func(t *testing.T) {
		got := build(func(snap *proxycfg.ConfigSnapshot) { snap.ConnectProxy.Leaf = nil })
		prototest.AssertDeepEqual(t, ready, got)
	})

	t.Run("identical when CA roots are missing", func(t *testing.T) {
		got := build(func(snap *proxycfg.ConfigSnapshot) { snap.Roots.Roots = nil })
		prototest.AssertDeepEqual(t, ready, got)
	})
}

func TestSecretsFromSnapshot_InvalidKindReturnsError(t *testing.T) {
	s := &ResourceGenerator{Logger: hclog.NewNullLogger()}

	snap := proxycfg.TestConfigSnapshotTerminatingGateway(t, true, nil, nil)
	snap.Kind = "not-a-real-kind"

	_, err := s.secretsFromSnapshot(snap)
	require.Error(t, err)
}

// TestSecretsFromSnapshotTerminatingGateway_EmitsDownstreamConnectSecrets
// covers the gateway's own public listener. Each linked service presents its
// own Connect leaf, so there is one leaf secret per service plus a single
// shared root secret, and every name the listener references must be backed by
// an emitted secret.
func TestSecretsFromSnapshotTerminatingGateway_EmitsDownstreamConnectSecrets(t *testing.T) {
	s := &ResourceGenerator{Logger: hclog.NewNullLogger()}
	snap := proxycfg.TestConfigSnapshotTerminatingGateway(t, true, nil, nil)

	resources, err := s.secretsFromSnapshot(snap)
	require.NoError(t, err)

	emitted := make(map[string]*envoy_tls_v3.Secret)
	for _, res := range resources {
		secret, ok := res.(*envoy_tls_v3.Secret)
		require.True(t, ok)
		emitted[secret.Name] = secret
	}

	// One leaf per linked service, carrying that service's own certificate.
	services := snap.TerminatingGateway.ValidServices()
	require.NotEmpty(t, services)
	for _, svc := range services {
		name := terminatingGatewayLeafSecretName(svc)
		secret, ok := emitted[name]
		require.True(t, ok, "expected a leaf secret named %q", name)

		cert, ok := secret.Type.(*envoy_tls_v3.Secret_TlsCertificate)
		require.True(t, ok)
		leaf := snap.TerminatingGateway.ServiceLeaves[svc]
		require.Equal(t, leaf.CertPEM, cert.TlsCertificate.CertificateChain.GetInlineString())
		require.Equal(t, leaf.PrivateKeyPEM, cert.TlsCertificate.PrivateKey.GetInlineString())
	}

	// The roots are common to every chain, so exactly one shared secret backs them.
	root, ok := emitted[connectRootSecretName]
	require.True(t, ok, "expected a shared root secret")
	vc, ok := root.Type.(*envoy_tls_v3.Secret_ValidationContext)
	require.True(t, ok)
	require.Equal(t, snap.RootPEMs(), vc.ValidationContext.TrustedCa.GetInlineString())

	// Guard against a listener referring to a secret that is never delivered.
	listeners, err := s.listenersFromSnapshot(snap)
	require.NoError(t, err)
	referenced := 0
	for _, l := range listeners {
		for _, name := range sdsSecretNamesInListener(t, l) {
			require.Contains(t, emitted, name, "listener references undelivered secret %q", name)
			referenced++
		}
	}
	require.NotZero(t, referenced, "expected the gateway listener to reference SDS secrets")
}

// sdsSecretNamesInListener returns every SDS secret name referenced by a
// listener's downstream transport sockets.
func sdsSecretNamesInListener(t *testing.T, res proto.Message) []string {
	t.Helper()

	l, ok := res.(*envoy_listener_v3.Listener)
	if !ok {
		return nil
	}

	var names []string
	for _, fc := range l.FilterChains {
		ts := fc.GetTransportSocket()
		if ts == nil {
			continue
		}
		var downstream envoy_tls_v3.DownstreamTlsContext
		require.NoError(t, ts.GetTypedConfig().UnmarshalTo(&downstream))

		ctx := downstream.GetCommonTlsContext()
		for _, sc := range ctx.GetTlsCertificateSdsSecretConfigs() {
			names = append(names, sc.Name)
		}
		if vc := ctx.GetValidationContextSdsSecretConfig(); vc != nil {
			names = append(names, vc.Name)
		}
	}
	return names
}
