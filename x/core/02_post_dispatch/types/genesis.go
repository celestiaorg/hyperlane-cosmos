package types

import (
	"fmt"
)

func NewGenesisState() *GenesisState {
	return &GenesisState{
		Igps:             []InterchainGasPaymaster{},
		IgpGasConfigs:    []GenesisDestinationGasConfigWrapper{},
		MerkleTreeHooks:  []MerkleTreeHook{},
		NoopHooks:        []NoopHook{},
		AggregationHooks: []AggregationHook{},
		RateLimitedHooks: []RateLimitedHook{},
		RateLimitBuckets: []RateLimitBucket{},
	}
}

func (gs *GenesisState) Validate() error {
	igpMap := make(map[uint64]struct{})
	for _, igp := range gs.Igps {
		if _, ok := igpMap[igp.Id.GetInternalId()]; ok {
			return fmt.Errorf("duplicate igp: %s", igp.Id)
		}
		igpMap[igp.Id.GetInternalId()] = struct{}{}
	}

	for _, config := range gs.IgpGasConfigs {
		if _, ok := igpMap[config.IgpId]; !ok {
			return fmt.Errorf("igp does not exist: %d", config.IgpId)
		}
	}

	aggregationHookMap := make(map[uint64]struct{})
	for _, aggregationHook := range gs.AggregationHooks {
		if _, ok := aggregationHookMap[aggregationHook.Id.GetInternalId()]; ok {
			return fmt.Errorf("duplicate aggregation hook: %s", aggregationHook.Id)
		}
		aggregationHookMap[aggregationHook.Id.GetInternalId()] = struct{}{}
	}

	rateLimitedHookMap := make(map[uint64]struct{})
	for _, hook := range gs.RateLimitedHooks {
		if _, ok := rateLimitedHookMap[hook.Id.GetInternalId()]; ok {
			return fmt.Errorf("duplicate rate limited hook: %s", hook.Id)
		}
		rateLimitedHookMap[hook.Id.GetInternalId()] = struct{}{}
	}

	bucketMap := make(map[string]struct{})
	for _, bucket := range gs.RateLimitBuckets {
		if _, ok := rateLimitedHookMap[bucket.HookId.GetInternalId()]; !ok {
			return fmt.Errorf("rate limited hook does not exist: %s", bucket.HookId)
		}
		key := bucket.HookId.String() + "/" + bucket.TokenId.String()
		if _, ok := bucketMap[key]; ok {
			return fmt.Errorf("duplicate rate limit bucket: %s", key)
		}
		bucketMap[key] = struct{}{}
	}

	return nil
}
