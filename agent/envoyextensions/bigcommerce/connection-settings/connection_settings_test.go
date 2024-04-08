package connectionsettings

import (
	"testing"
	"time"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/envoyextensions/extensioncommon"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	envoy_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
)

func TestConstructor(t *testing.T) {
	cases := map[string]struct {
		extensionName string
		arguments     map[string]interface{}
		expected      connectionSettings
		ok            bool
	}{
		"with no arguments uses defaults": {
			arguments: nil,
			ok:        true,
			expected: connectionSettings{
				UpstreamDefaults: upstreamDefaultsConfig{
					RequestTimeout: 15,
					MaxConnections: 4096,
				},
			},
		},
		"with an invalid name": {
			arguments:     map[string]any{},
			extensionName: "bad",
			ok:            false,
		},
		"invalid default upstream request timeout": {
			arguments: map[string]any{
				"UpstreamDefaults": map[string]any{
					"RequestTimeout": "invalid",
				},
			},
			ok: false,
		},
		"invalid default upstream max connections": {
			arguments: map[string]any{
				"UpstreamDefaults": map[string]any{
					"MaxConnections": "invalid",
				},
			},
			ok: false,
		},
		"valid everything": {
			arguments: map[string]any{
				"UpstreamDefaults": map[string]any{
					"RequestTimeout": 10,
					"MaxConnections": 1234,
				},
			},
			expected: connectionSettings{
				UpstreamDefaults: upstreamDefaultsConfig{
					RequestTimeout: 10,
					MaxConnections: 1234,
				},
			},
			ok: true,
		},
	}

	for n, tc := range cases {
		t.Run(n, func(t *testing.T) {

			extensionName := api.BigCommerceConnectionSettingsExtension
			if tc.extensionName != "" {
				extensionName = tc.extensionName
			}

			svc := api.CompoundServiceName{Name: "svc"}
			ext := extensioncommon.RuntimeConfig{
				ServiceName: svc,
				EnvoyExtension: api.EnvoyExtension{
					Name:      extensionName,
					Arguments: tc.arguments,
				},
			}

			e, err := Constructor(ext.EnvoyExtension)

			if tc.ok {
				require.NoError(t, err)
				require.Equal(t, &extensioncommon.BasicEnvoyExtender{Extension: &tc.expected}, e)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestPatchCluster(t *testing.T) {
	tests := map[string]struct {
		direction     extensioncommon.TrafficDirection
		cluster       *envoy_cluster_v3.Cluster
		expectCluster *envoy_cluster_v3.Cluster
		expectBool    bool
	}{
		"inbound clusters are unmodified": {
			direction: extensioncommon.TrafficDirectionInbound,
			cluster: &envoy_cluster_v3.Cluster{
				Name: "test-cluster",
				CircuitBreakers: &envoy_cluster_v3.CircuitBreakers{
					Thresholds: []*envoy_cluster_v3.CircuitBreakers_Thresholds{
						{
							MaxConnections: makeUint32Value(1),
						},
					},
				},
			},
			expectCluster: &envoy_cluster_v3.Cluster{
				Name: "test-cluster",
				CircuitBreakers: &envoy_cluster_v3.CircuitBreakers{
					Thresholds: []*envoy_cluster_v3.CircuitBreakers_Thresholds{
						{
							MaxConnections: makeUint32Value(1),
						},
					},
				},
			},
			expectBool: false,
		},
		"outbound clusters without limits are modified": {
			direction: extensioncommon.TrafficDirectionOutbound,
			cluster: &envoy_cluster_v3.Cluster{
				Name: "test-cluster",
				CircuitBreakers: &envoy_cluster_v3.CircuitBreakers{
					Thresholds: []*envoy_cluster_v3.CircuitBreakers_Thresholds{},
				},
			},
			expectCluster: &envoy_cluster_v3.Cluster{
				Name: "test-cluster",
				CircuitBreakers: &envoy_cluster_v3.CircuitBreakers{
					Thresholds: []*envoy_cluster_v3.CircuitBreakers_Thresholds{
						{
							MaxConnections: makeUint32Value(1234),
						},
					},
				},
			},
			expectBool: true,
		},
		"outbound clusters with other limits but no max conns are modified": {
			direction: extensioncommon.TrafficDirectionOutbound,
			cluster: &envoy_cluster_v3.Cluster{
				Name: "test-cluster",
				CircuitBreakers: &envoy_cluster_v3.CircuitBreakers{
					Thresholds: []*envoy_cluster_v3.CircuitBreakers_Thresholds{
						{
							MaxPendingRequests: makeUint32Value(100),
						},
					},
				},
			},
			expectCluster: &envoy_cluster_v3.Cluster{
				Name: "test-cluster",
				CircuitBreakers: &envoy_cluster_v3.CircuitBreakers{
					Thresholds: []*envoy_cluster_v3.CircuitBreakers_Thresholds{
						{
							MaxConnections:     makeUint32Value(1234),
							MaxPendingRequests: makeUint32Value(100),
						},
					},
				},
			},
			expectBool: true,
		},
		"outbound clusters with max connections are not modified": {
			direction: extensioncommon.TrafficDirectionOutbound,
			cluster: &envoy_cluster_v3.Cluster{
				Name: "test-cluster",
				CircuitBreakers: &envoy_cluster_v3.CircuitBreakers{
					Thresholds: []*envoy_cluster_v3.CircuitBreakers_Thresholds{
						{
							MaxConnections: makeUint32Value(1),
						},
					},
				},
			},
			expectCluster: &envoy_cluster_v3.Cluster{
				Name: "test-cluster",
				CircuitBreakers: &envoy_cluster_v3.CircuitBreakers{
					Thresholds: []*envoy_cluster_v3.CircuitBreakers_Thresholds{
						{
							MaxConnections: makeUint32Value(1),
						},
					},
				},
			},
			expectBool: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			l := connectionSettings{
				UpstreamDefaults: upstreamDefaultsConfig{
					MaxConnections: 1234,
				},
			}
			r, ok, err := l.PatchCluster(extensioncommon.ClusterPayload{
				TrafficDirection: tc.direction,
				Message:          tc.cluster,
			})
			require.NoError(t, err)
			require.Equal(t, tc.expectCluster, r)
			require.Equal(t, tc.expectBool, ok)
		})
	}
}

func TestPatchRoute(t *testing.T) {
	tests := map[string]struct {
		direction   extensioncommon.TrafficDirection
		route       *envoy_route_v3.RouteConfiguration
		expectRoute *envoy_route_v3.RouteConfiguration
		expectBool  bool
	}{
		"inbound routes are unmodified": {
			direction: extensioncommon.TrafficDirectionInbound,
			route: &envoy_route_v3.RouteConfiguration{
				VirtualHosts: []*envoy_route_v3.VirtualHost{
					{
						Routes: []*envoy_route_v3.Route{
							{
								Action: &envoy_route_v3.Route_Route{
									Route: &envoy_route_v3.RouteAction{
										Timeout: durationpb.New(1 * time.Second),
									},
								},
							},
						},
					},
				},
			},
			expectRoute: &envoy_route_v3.RouteConfiguration{
				VirtualHosts: []*envoy_route_v3.VirtualHost{
					{
						Routes: []*envoy_route_v3.Route{
							{
								Action: &envoy_route_v3.Route_Route{
									Route: &envoy_route_v3.RouteAction{
										Timeout: durationpb.New(1 * time.Second),
									},
								},
							},
						},
					},
				},
			},
			expectBool: false,
		},
		"outbound routes with a timeout are unmodified": {
			direction: extensioncommon.TrafficDirectionOutbound,
			route: &envoy_route_v3.RouteConfiguration{
				VirtualHosts: []*envoy_route_v3.VirtualHost{
					{
						Routes: []*envoy_route_v3.Route{
							{
								Action: &envoy_route_v3.Route_Route{
									Route: &envoy_route_v3.RouteAction{
										Timeout: durationpb.New(1 * time.Second),
									},
								},
							},
						},
					},
				},
			},
			expectRoute: &envoy_route_v3.RouteConfiguration{
				VirtualHosts: []*envoy_route_v3.VirtualHost{
					{
						Routes: []*envoy_route_v3.Route{
							{
								Action: &envoy_route_v3.Route_Route{
									Route: &envoy_route_v3.RouteAction{
										Timeout: durationpb.New(1 * time.Second),
									},
								},
							},
						},
					},
				},
			},
			expectBool: false,
		},
		"outbound routes without a timeout are modified": {
			direction: "outbound",
			route: &envoy_route_v3.RouteConfiguration{
				VirtualHosts: []*envoy_route_v3.VirtualHost{
					{
						Routes: []*envoy_route_v3.Route{
							{
								Action: &envoy_route_v3.Route_Route{
									Route: &envoy_route_v3.RouteAction{},
								},
							},
						},
					},
				},
			},
			expectRoute: &envoy_route_v3.RouteConfiguration{
				VirtualHosts: []*envoy_route_v3.VirtualHost{
					{
						Routes: []*envoy_route_v3.Route{
							{
								Action: &envoy_route_v3.Route_Route{
									Route: &envoy_route_v3.RouteAction{
										Timeout: durationpb.New(15 * time.Second),
									},
								},
							},
						},
					},
				},
			},
			expectBool: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			l := connectionSettings{
				UpstreamDefaults: upstreamDefaultsConfig{
					RequestTimeout: 15,
					MaxConnections: 4096,
				},
			}
			r, ok, err := l.PatchRoute(extensioncommon.RoutePayload{
				TrafficDirection: tc.direction,
				Message:          tc.route,
			})
			require.NoError(t, err)
			require.Equal(t, tc.expectRoute, r)
			require.Equal(t, tc.expectBool, ok)
		})
	}
}
