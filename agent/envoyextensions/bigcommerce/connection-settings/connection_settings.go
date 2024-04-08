package connectionsettings

import (
	"time"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/envoyextensions/extensioncommon"
	"google.golang.org/protobuf/types/known/durationpb"
	envoy_cluster_v3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
)

var _ extensioncommon.BasicExtension = (*connectionSettings)(nil)

// Constructor follows a specific function signature required for the extension registration.
func Constructor(ext api.EnvoyExtension) (extensioncommon.EnvoyExtender, error) {
	c, err := newConnectionSettings(ext)
	if err != nil {
		return nil, err
	}
	return &extensioncommon.BasicEnvoyExtender{
		Extension: c,
	}, nil
}

func (c *connectionSettings) CanApply(config *extensioncommon.RuntimeConfig) bool {
	return true
}

func (c *connectionSettings) PatchCluster(p extensioncommon.ClusterPayload) (*envoy_cluster_v3.Cluster, bool, error) {
	cluster := p.Message
	if p.IsInbound() {
		return cluster, false, nil
	}

	modified := false
	thresholds := cluster.CircuitBreakers.Thresholds

	if len(thresholds) == 0 {
		cluster.CircuitBreakers.Thresholds = []*envoy_cluster_v3.CircuitBreakers_Thresholds{{
			MaxConnections: makeUint32Value(c.UpstreamDefaults.MaxConnections),
		}}
		return cluster, true, nil
	}

	for _, threshold := range thresholds {
		if threshold.MaxConnections == nil {
			threshold.MaxConnections = makeUint32Value(c.UpstreamDefaults.MaxConnections)
			modified = true
		}
	}
	return cluster, modified, nil
}

func (c *connectionSettings) PatchRoute(p extensioncommon.RoutePayload) (*envoy_route_v3.RouteConfiguration, bool, error) {
	route := p.Message
	if p.IsInbound() {
		return route, false, nil
	}

	modified := false
	for _, virtualHost := range route.VirtualHosts {
		for _, route := range virtualHost.Routes {
			action, ok := route.Action.(*envoy_route_v3.Route_Route)

			if !ok || action.Route.Timeout.AsDuration() > 0 {
				continue
			}

			action.Route.Timeout = durationpb.New(time.Duration(c.UpstreamDefaults.RequestTimeout) * time.Second)
			modified = true
		}
	}

	return route, modified, nil
}

func makeUint32Value(n int) *wrappers.UInt32Value {
	return &wrappers.UInt32Value{Value: uint32(n)}
}
