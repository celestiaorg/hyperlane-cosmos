package keeper_test

import (
	"context"
	"fmt"
	"math"

	storetypes "cosmossdk.io/store/types"
	i "github.com/bcp-innovations/hyperlane-cosmos/tests/integration"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	corekeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/keeper"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

/*

TEST CASES - routing_ism_verify_equivalence_test.go

Keeper.Verify walks routing ISMs in a loop and short-circuits cycles. These
tests prove that it is observably identical to the recursive implementation it
replaces (legacyVerify below): same result, same error, same panic and the
exact same gas consumed.

* Verify matches legacy recursion for acyclic and cyclic routing graphs
* Verify matches legacy recursion with an infinite gas meter (acyclic only)
* Verify (invalid) cycle with an infinite gas meter returns an error
* Verify (invalid) cycle with a large gas limit runs out of gas
* VerifyDryRun (invalid) gas limit above the maximum

*/

// legacyVerify is the recursive Keeper.Verify from hyperlane-cosmos v1.3.0.
// The routing step is RoutingISMHandler.Route, which is the body of the old
// RoutingISMHandler.Verify minus its tail call into Keeper.Verify.
func legacyVerify(ctx context.Context, router *util.Router[util.InterchainSecurityModule], ismId util.HexAddress, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	sdk.UnwrapSDKContext(ctx).GasMeter().ConsumeGas(10000, "ism verification")
	handler, err := router.GetModule(ismId)
	if err != nil {
		return false, err
	}
	if routing, ok := (*handler).(util.RoutingInterchainSecurityModule); ok {
		next, err := routing.Route(ctx, ismId, message)
		if err != nil {
			return false, err
		}
		return legacyVerify(ctx, router, next, metadata, message)
	}
	return (*handler).Verify(ctx, ismId, metadata, message)
}

type verifyOutcome struct {
	result   bool
	err      string
	panicked string
	gas      uint64
}

func (o verifyOutcome) String() string {
	return fmt.Sprintf("result=%v err=%q panic=%q gas=%d", o.result, o.err, o.panicked, o.gas)
}

func runVerify(ctx sdk.Context, meter storetypes.GasMeter, verify func(ctx sdk.Context) (bool, error)) (o verifyOutcome) {
	ctx = ctx.WithGasMeter(meter)
	defer func() {
		if r := recover(); r != nil {
			o.panicked = fmt.Sprintf("%T %v", r, r)
		}
		o.gas = meter.GasConsumed()
	}()
	result, err := verify(ctx)
	o.result = result
	if err != nil {
		o.err = err.Error()
	}
	return o
}

var _ = Describe("routing_ism_verify_equivalence_test.go", Ordered, func() {
	var s *i.KeeperTestSuite
	var creator i.TestValidatorAddress

	type scenario struct {
		name   string
		ism    util.HexAddress
		origin uint32
		cyclic bool
	}

	BeforeEach(func() {
		s = i.NewCleanChain()
		creator = i.GenerateTestValidatorAddress("Creator")
		Expect(s.MintBaseCoins(creator.Address, 1_000_000)).To(Succeed())
	})

	createRoutingIsm := func() util.HexAddress {
		res, err := s.RunTx(&types.MsgCreateRoutingIsm{Creator: creator.Address})
		Expect(err).To(BeNil())
		var resp types.MsgCreateRoutingIsmResponse
		Expect(proto.Unmarshal(res.MsgResponses[0].Value, &resp)).To(Succeed())
		return resp.Id
	}

	createNoopIsm := func() util.HexAddress {
		res, err := s.RunTx(&types.MsgCreateNoopIsm{Creator: creator.Address})
		Expect(err).To(BeNil())
		var resp types.MsgCreateNoopIsmResponse
		Expect(proto.Unmarshal(res.MsgResponses[0].Value, &resp)).To(Succeed())
		return resp.Id
	}

	setRoute := func(routingIsm, ism util.HexAddress, domain uint32) {
		_, err := s.RunTx(&types.MsgSetRoutingIsmDomain{
			Owner: creator.Address,
			IsmId: routingIsm,
			Route: types.Route{Ism: ism, Domain: domain},
		})
		Expect(err).To(BeNil())
	}

	buildScenarios := func() []scenario {
		noop := createNoopIsm()

		direct := createRoutingIsm()
		setRoute(direct, noop, 1)

		outer := createRoutingIsm()
		setRoute(outer, direct, 1)

		self := createRoutingIsm()
		setRoute(self, self, 1)

		a, b := createRoutingIsm(), createRoutingIsm()
		setRoute(a, b, 1)
		setRoute(b, a, 1)

		c1, c2, c3 := createRoutingIsm(), createRoutingIsm(), createRoutingIsm()
		setRoute(c1, c2, 1)
		setRoute(c2, c3, 1)
		setRoute(c3, c1, 1)

		// a tail of two hops into a two hop cycle: t1 -> t2 -> l1 -> l2 -> l1
		t1, t2, l1, l2 := createRoutingIsm(), createRoutingIsm(), createRoutingIsm(), createRoutingIsm()
		setRoute(t1, t2, 1)
		setRoute(t2, l1, 1)
		setRoute(l1, l2, 1)
		setRoute(l2, l1, 1)

		// the same ISM routes domain 1 into a cycle and domain 2 to a leaf
		mixed := createRoutingIsm()
		setRoute(mixed, mixed, 1)
		setRoute(mixed, noop, 2)

		// a routing ISM id with a valid module type that is not in the store
		unknown := direct
		unknown[len(unknown)-1] ^= 0xff

		return []scenario{
			{name: "leaf", ism: noop, origin: 1},
			{name: "routing -> leaf", ism: direct, origin: 1},
			{name: "routing -> routing -> leaf", ism: outer, origin: 1},
			{name: "self route", ism: self, origin: 1, cyclic: true},
			{name: "two hop cycle", ism: a, origin: 1, cyclic: true},
			{name: "three hop cycle", ism: c1, origin: 1, cyclic: true},
			{name: "tail into cycle", ism: t1, origin: 1, cyclic: true},
			{name: "mixed ism into cycle", ism: mixed, origin: 1, cyclic: true},
			{name: "mixed ism to leaf", ism: mixed, origin: 2},
			{name: "missing route", ism: direct, origin: 2},
			{name: "missing route on second hop", ism: outer, origin: 2},
			{name: "unknown routing ism", ism: unknown, origin: 1},
		}
	}

	gasLimits := func() []uint64 {
		limits := []uint64{0, 1, 9_999, 10_000, 10_001}
		// every gas limit around the first few hop boundaries
		for l := uint64(10_500); l < 60_000; l++ {
			limits = append(limits, l)
		}
		for l := uint64(60_000); l < 2_000_000; l += 7_919 {
			limits = append(limits, l)
		}
		// large gas limits
		return append(limits, 10_000_003, 123_456_789, 500_000_001)
	}

	It("Verify matches legacy recursion for acyclic and cyclic routing graphs", func() {
		router := s.App().HyperlaneKeeper.IsmRouter()
		for _, sc := range buildScenarios() {
			message := util.HyperlaneMessage{Version: 3, Origin: sc.origin}
			for _, limit := range gasLimits() {
				legacy := runVerify(s.Ctx(), storetypes.NewGasMeter(limit), func(ctx sdk.Context) (bool, error) {
					return legacyVerify(ctx, router, sc.ism, []byte{}, message)
				})
				got := runVerify(s.Ctx(), storetypes.NewGasMeter(limit), func(ctx sdk.Context) (bool, error) {
					return s.App().HyperlaneKeeper.Verify(ctx, sc.ism, []byte{}, message)
				})
				Expect(got).To(Equal(legacy), "scenario %q gas limit %d", sc.name, limit)
			}
		}
	})

	It("Verify matches legacy recursion with an infinite gas meter (acyclic only)", func() {
		router := s.App().HyperlaneKeeper.IsmRouter()
		for _, sc := range buildScenarios() {
			if sc.cyclic {
				continue
			}
			message := util.HyperlaneMessage{Version: 3, Origin: sc.origin}
			legacy := runVerify(s.Ctx(), storetypes.NewInfiniteGasMeter(), func(ctx sdk.Context) (bool, error) {
				return legacyVerify(ctx, router, sc.ism, []byte{}, message)
			})
			got := runVerify(s.Ctx(), storetypes.NewInfiniteGasMeter(), func(ctx sdk.Context) (bool, error) {
				return s.App().HyperlaneKeeper.Verify(ctx, sc.ism, []byte{}, message)
			})
			Expect(got).To(Equal(legacy), "scenario %q", sc.name)
		}
	})

	It("Verify (invalid) cycle with an infinite gas meter returns an error", func() {
		for _, sc := range buildScenarios() {
			if !sc.cyclic {
				continue
			}
			message := util.HyperlaneMessage{Version: 3, Origin: sc.origin}
			for _, meter := range []storetypes.GasMeter{storetypes.NewInfiniteGasMeter(), storetypes.NewGasMeter(math.MaxUint64)} {
				got := runVerify(s.Ctx(), meter, func(ctx sdk.Context) (bool, error) {
					return s.App().HyperlaneKeeper.Verify(ctx, sc.ism, []byte{}, message)
				})
				Expect(got.panicked).To(BeEmpty(), "scenario %q", sc.name)
				Expect(got.result).To(BeFalse())
				Expect(got.err).To(ContainSubstring(coretypes.ErrIsmRoutingCycle.Error()), "scenario %q", sc.name)
			}
		}
	})

	It("Verify (invalid) cycle with a large gas limit runs out of gas", func() {
		for _, sc := range buildScenarios() {
			if !sc.cyclic {
				continue
			}
			message := util.HyperlaneMessage{Version: 3, Origin: sc.origin}
			for _, limit := range []uint64{4_294_967_295, 20_000_000_000, 1 << 62, 1 << 63} {
				got := runVerify(s.Ctx(), storetypes.NewGasMeter(limit), func(ctx sdk.Context) (bool, error) {
					return s.App().HyperlaneKeeper.Verify(ctx, sc.ism, []byte{}, message)
				})
				Expect(got.panicked).To(HavePrefix("types.ErrorOutOfGas"), "scenario %q gas limit %d", sc.name, limit)
				Expect(got.gas).To(BeNumerically(">", limit))
				// the walk stops within one charge of the limit
				Expect(got.gas - limit).To(BeNumerically("<=", 10_000))
			}
		}
	})

	It("VerifyDryRun (invalid) gas limit above the maximum", func() {
		self := createRoutingIsm()
		setRoute(self, self, 1)
		message := util.HyperlaneMessage{Version: 3, Origin: 1}

		_, err := corekeeper.NewQueryServerImpl(s.App().HyperlaneKeeper).VerifyDryRun(s.Ctx(), &coretypes.QueryVerifyDryRunRequest{
			IsmId:    self.String(),
			Message:  util.EncodeEthHex(message.Bytes()),
			Metadata: "0x",
			GasLimit: "4294967295",
		})
		Expect(err).To(MatchError("gas limit 4294967295 exceeds maximum 10000000"))
	})
})
