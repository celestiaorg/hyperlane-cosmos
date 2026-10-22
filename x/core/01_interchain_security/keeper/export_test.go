package keeper

import (
	"context"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
)

// SetStoredRoute writes route into a stored routing ISM without the msg server's
// checks, modelling routes stored before nested routing ISMs were rejected.
func (k *Keeper) SetStoredRoute(ctx context.Context, routingIsm util.HexAddress, route types.Route) error {
	ism, err := k.isms.Get(ctx, routingIsm.GetInternalId())
	if err != nil {
		return err
	}
	routing := ism.(*types.RoutingISM)
	routing.SetDomain(route)
	return k.isms.Set(ctx, routingIsm.GetInternalId(), routing)
}
