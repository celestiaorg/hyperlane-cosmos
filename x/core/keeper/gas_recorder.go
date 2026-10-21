package keeper

import (
	storetypes "cosmossdk.io/store/types"
)

type gasCharge struct {
	amount     storetypes.Gas
	descriptor string
}

// gasRecorder is a GasMeter that records every charge before it passes it to
// the wrapped GasMeter.
type gasRecorder struct {
	storetypes.GasMeter
	charges  []gasCharge
	refunded bool
}

func (g *gasRecorder) ConsumeGas(amount storetypes.Gas, descriptor string) {
	g.charges = append(g.charges, gasCharge{amount: amount, descriptor: descriptor})
	g.GasMeter.ConsumeGas(amount, descriptor)
}

func (g *gasRecorder) RefundGas(amount storetypes.Gas, descriptor string) {
	g.refunded = true
	g.GasMeter.RefundGas(amount, descriptor)
}
