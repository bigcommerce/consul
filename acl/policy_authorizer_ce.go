// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

//go:build !consulent

package acl

import "github.com/hashicorp/go-hclog"

// enterprisePolicyAuthorizer stub
type enterprisePolicyAuthorizer struct{}

func (authz *enterprisePolicyAuthorizer) init(*Config) {
	// nothing to do
}

func (authz *enterprisePolicyAuthorizer) enforce(_ *EnterpriseRule, _ *AuthorizerContext) EnforcementDecision {
	return Default
}

// NewPolicyAuthorizer merges the policies and returns an Authorizer that will enforce them
func NewPolicyAuthorizer(policies []*Policy, entConfig *Config, logger hclog.Logger) (Authorizer, error) {
	return newPolicyAuthorizer(policies, entConfig, logger)
}

// NewPolicyAuthorizerWithDefaults will actually created a ChainedAuthorizer with
// the policies compiled into one Authorizer and the backup policy of the defaultAuthz
func NewPolicyAuthorizerWithDefaults(defaultAuthz Authorizer, policies []*Policy, entConfig *Config, logger hclog.Logger) (Authorizer, error) {
	authz, err := newPolicyAuthorizer(policies, entConfig, logger)
	if err != nil {
		return nil, err
	}

	return NewChainedAuthorizer([]Authorizer{authz, defaultAuthz}), nil
}
