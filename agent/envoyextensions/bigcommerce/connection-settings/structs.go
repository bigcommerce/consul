package connectionsettings;

import(
	"fmt"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/envoyextensions/extensioncommon"
	"github.com/mitchellh/mapstructure"
)

type upstreamDefaultsConfig struct {
	RequestTimeout int
	MaxConnections int
}

func (c *upstreamDefaultsConfig) normalize() {
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 15
	}
	if c.MaxConnections == 0 {
		c.MaxConnections = 4096
	}
}

func (c *upstreamDefaultsConfig) validate() error {
	c.normalize()

	var resultErr error
	return resultErr
}

type connectionSettings struct {
	extensioncommon.BasicExtensionAdapter
	UpstreamDefaults upstreamDefaultsConfig
}

func newConnectionSettings(ext api.EnvoyExtension) (*connectionSettings, error) {
	c := &connectionSettings{}
	if ext.Name != api.BigCommerceConnectionSettingsExtension {
		return c, fmt.Errorf("expected extension name %q but got %q", api.BigCommerceConnectionSettingsExtension, ext.Name)
	}
	if err := c.fromArguments(ext.Arguments); err != nil {
		return c, err
	}
	return c, nil
}

func (c *connectionSettings) fromArguments(args map[string]interface{}) error {
	if err := mapstructure.Decode(args, c); err != nil {
		return fmt.Errorf("error decoding extension arguments: %v", err)
	}
	c.UpstreamDefaults.normalize()
	return c.validate()
}

func (c *connectionSettings) validate() error {
	var resultErr error
	// if a.DefaultUpstreamRequestTimeout == 0 {
	// 	resultErr = multierror.Append(resultErr, fmt.Errorf("DefaultUpstreamRequestTimeout is required"))
	// }
	return resultErr
}
