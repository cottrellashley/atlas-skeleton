package assemble

import (
	"context"
	"errors"
	"fmt"
	"time"

	"atlas.local/gateway/core/access"
	"atlas.local/gateway/core/registry"
	"atlas.local/gateway/ports"
)

// startPolicyTimeout bounds the policy read a gateway makes as it starts.
const startPolicyTimeout = 30 * time.Second

// checkedPolicy is the deployment's policy source, each policy it answers
// held to the catalogue the gateway serves before it may decide (R040,
// 06-7 §6.7.10). A capability the deployment withholds may already be
// named, so its grant by name is in place when it is added (A-180).
func checkedPolicy(p Parts, reg *registry.Registry) ports.PolicySource {
	return access.Checked(p.Policy, func() access.Catalogue {
		return access.Catalogue{Served: reg.Snapshot(), Withheld: p.Exclude}
	}, p.Log)
}

// startPolicy reads the policy once the catalogue is registered. A policy
// that names what the gateway does not serve stops it starting, with what
// to fix. A source that cannot answer yet does not: the pipeline keeps
// trying, refuses calls meanwhile, and its PolicyStatus says why, so a
// brief outage of a policy store never stops a deploy for good.
func startPolicy(src ports.PolicySource) error {
	if src == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), startPolicyTimeout)
	defer cancel()
	_, err := src.Policy(ctx)
	var refused *access.Refused
	if errors.As(err, &refused) {
		return fmt.Errorf("assemble: the gateway's policy: %w", err)
	}
	return nil
}
