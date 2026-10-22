package keeper

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/stretchr/testify/require"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
)

// countingMeter counts the charges verifyRoutingCycle replays on the wrapped meter.
type countingMeter struct {
	storetypes.GasMeter
	calls int
}

func (m *countingMeter) ConsumeGas(amount storetypes.Gas, descriptor string) {
	m.calls++
	m.GasMeter.ConsumeGas(amount, descriptor)
}

// lenientMeter has a finite limit but never panics.
type lenientMeter struct {
	storetypes.GasMeter
	limit uint64
}

func (m *lenientMeter) Limit() storetypes.Gas { return m.limit }

func TestVerifyRoutingCycleRunsOutOfGasInOneLap(t *testing.T) {
	cycle := []gasCharge{
		{amount: 10_000, descriptor: "ism verification"},
		{amount: 1_234, descriptor: "route"},
		{amount: 10_000, descriptor: "ism verification"},
		{amount: 567, descriptor: "route"},
	}
	var lap uint64
	for _, c := range cycle {
		lap += c.amount
	}

	for _, limit := range []uint64{30_000, lap*3 + 1, 123_456_789, 1 << 62, 1 << 63} {
		meter := &countingMeter{GasMeter: storetypes.NewGasMeter(limit)}
		meter.GasMeter.ConsumeGas(lap+1, "walk so far")

		require.PanicsWithValue(t, storetypes.ErrorOutOfGas{Descriptor: "ism verification"}, func() {
			_, _ = verifyRoutingCycle(meter, util.HexAddress{}, cycle)
		}, "limit %d", limit)

		// one bulk charge, then at most one lap replayed charge by charge
		require.LessOrEqual(t, meter.calls, 1+len(cycle), "limit %d", limit)
		require.Greater(t, meter.GasConsumed(), limit, "limit %d", limit)
		require.LessOrEqual(t, meter.GasConsumed()-limit, uint64(10_000), "limit %d", limit)
	}
}

func TestVerifyRoutingCycleReturnsErrorWithoutPanic(t *testing.T) {
	cycle := []gasCharge{{amount: 10_000, descriptor: "ism verification"}}
	for _, meter := range []storetypes.GasMeter{
		storetypes.NewInfiniteGasMeter(),
		&lenientMeter{GasMeter: storetypes.NewInfiniteGasMeter(), limit: 50_000},
	} {
		ok, err := verifyRoutingCycle(meter, util.HexAddress{}, cycle)
		require.False(t, ok)
		require.ErrorIs(t, err, types.ErrIsmRoutingCycle)
	}
}
