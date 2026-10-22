package keeper

import (
	"context"
	"fmt"
	"math"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/errors"
	gastypes "cosmossdk.io/store/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/keeper"
	postdispatchkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/keeper"
	"github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type Keeper struct {
	cdc          codec.BinaryCodec
	addressCodec address.Codec

	// authority is the address capable of executing a MsgUpdateParams and other authority-gated messages.
	// Typically this should be the x/gov module account.
	authority string

	// Mailboxes is a map of mailbox IDs to mailboxes
	Mailboxes collections.Map[uint64, types.Mailbox]
	// Messages is a set of tuples. The first key is the mailbox ID, second key is the message ID.
	Messages collections.KeySet[collections.Pair[uint64, []byte]]
	// MailboxesSequence is a monotonically increasing number of mailboxes. The
	// internal ID for a mailbox is the sequence number when it was created.
	MailboxesSequence collections.Sequence

	Schema collections.Schema

	bankKeeper types.BankKeeper

	IsmKeeper          ismkeeper.Keeper
	PostDispatchKeeper postdispatchkeeper.Keeper

	ismRouter          *util.Router[util.InterchainSecurityModule]
	postDispatchRouter *util.Router[util.PostDispatchModule]
	appRouter          *util.Router[util.HyperlaneApp]
}

// NewKeeper creates a new Keeper instance
func NewKeeper(cdc codec.BinaryCodec, addressCodec address.Codec, storeService storetypes.KVStoreService, authority string, bankKeeper types.BankKeeper) Keeper {
	if _, err := addressCodec.StringToBytes(authority); err != nil {
		panic(fmt.Errorf("invalid authority address: %w", err))
	}

	sb := collections.NewSchemaBuilder(storeService)
	k := Keeper{
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,

		Mailboxes:         collections.NewMap(sb, types.MailboxesKey, "mailboxes", collections.Uint64Key, codec.CollValue[types.Mailbox](cdc)),
		Messages:          collections.NewKeySet(sb, types.MessagesKey, "messages", collections.PairKeyCodec(collections.Uint64Key, collections.BytesKey)),
		MailboxesSequence: collections.NewSequence(sb, types.MailboxesSequenceKey, "mailboxes_sequence"),

		bankKeeper: bankKeeper,

		IsmKeeper:          ismkeeper.NewKeeper(cdc, storeService),
		PostDispatchKeeper: postdispatchkeeper.NewKeeper(cdc, storeService, bankKeeper),

		ismRouter:          util.NewRouter[util.InterchainSecurityModule](types.IsmRouterKey, "router_ism", sb),
		postDispatchRouter: util.NewRouter[util.PostDispatchModule](types.PostDispatchRouterKey, "router_post_dispatch", sb),
		appRouter:          util.NewRouter[util.HyperlaneApp](types.AppRouterKey, "router_app", sb),
	}

	k.IsmKeeper.SetCoreKeeper(k)
	k.PostDispatchKeeper.SetCoreKeeper(k)

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	k.Schema = schema

	return k
}

func (k Keeper) AppRouter() *util.Router[util.HyperlaneApp] {
	return k.appRouter
}

func (k *Keeper) ReceiverIsmId(ctx context.Context, recipient util.HexAddress) (util.HexAddress, error) {
	handler, err := k.appRouter.GetModule(recipient)
	if err != nil {
		return util.HexAddress{}, err
	}
	ism, err := (*handler).ReceiverIsmId(ctx, recipient)
	if err != nil {
		return util.HexAddress{}, err
	}
	return *ism, nil
}

func (k *Keeper) Handle(ctx context.Context, mailboxId util.HexAddress, message util.HyperlaneMessage) error {
	handler, err := k.appRouter.GetModule(message.Recipient)
	if err != nil {
		return err
	}
	return (*handler).Handle(ctx, mailboxId, message)
}

func (k Keeper) IsmRouter() *util.Router[util.InterchainSecurityModule] {
	return k.ismRouter
}

// Verify verifies message with the ISM ismId.
//
// Routing ISMs delegate verification to exactly one other ISM. Verify follows
// them in a loop. Every hop charges the same gas, in the same order, as the
// recursive implementation in v1.3.0, so the result, error and gas consumed
// are identical.
func (k Keeper) Verify(ctx context.Context, ismId util.HexAddress, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	recorder := &gasRecorder{GasMeter: sdkCtx.GasMeter()}
	routeCtx := sdkCtx.WithGasMeter(recorder)

	// visited maps each ISM on the walk to the index in recorder.charges where its hop starts.
	visited := make(map[util.HexAddress]int)
	for {
		if start, ok := visited[ismId]; ok && !recorder.refunded {
			return verifyRoutingCycle(sdkCtx.GasMeter(), ismId, recorder.charges[start:])
		}
		visited[ismId] = len(recorder.charges)

		// Consume a fixed amount of gas to prevent DoS attacks and limit routing hops.
		recorder.ConsumeGas(10000, "ism verification")

		handler, err := k.ismRouter.GetModule(ismId)
		if err != nil {
			return false, err
		}
		routing, ok := (*handler).(util.RoutingInterchainSecurityModule)
		if !ok {
			return (*handler).Verify(ctx, ismId, metadata, message)
		}
		ismId, err = routing.Route(routeCtx, ismId, message)
		if err != nil {
			return false, err
		}
	}
}

// verifyRoutingCycle handles a walk that reached ismId a second time. Routing
// is a pure function of the stored ISMs and the message, so the walk would
// repeat cycle forever and the recursive implementation ran out of gas. This
// charges whole laps of the cycle at once and then replays it charge by charge,
// so the meter panics with exactly the gas consumed of the recursive
// implementation.
func verifyRoutingCycle(meter gastypes.GasMeter, ismId util.HexAddress, cycle []gasCharge) (bool, error) {
	limit := meter.Limit()
	if limit == math.MaxUint64 {
		// Without a gas limit the walk would never end.
		return false, errors.Wrapf(types.ErrIsmRoutingCycle, "ISM %s is part of a routing cycle", ismId.String())
	}

	var lap uint64
	for _, c := range cycle {
		lap += c.amount
	}
	if consumed := meter.GasConsumed(); consumed < limit {
		if laps := (limit - consumed) / lap; laps > 0 {
			meter.ConsumeGas(laps*lap, "ism verification")
		}
	}

	// Less than one lap of gas is left, so one more lap panics with out of gas.
	for _, c := range cycle {
		meter.ConsumeGas(c.amount, c.descriptor)
	}
	// Unreachable with the SDK gas meter; a meter that does not panic still reports the cycle.
	return false, errors.Wrapf(types.ErrIsmRoutingCycle, "ISM %s is part of a routing cycle", ismId.String())
}

func (k Keeper) IsmExists(ctx context.Context, ismId util.HexAddress) (bool, error) {
	handler, err := k.ismRouter.GetModule(ismId)
	if err != nil {
		return false, err
	}
	return (*handler).Exists(ctx, ismId)
}

func (k *Keeper) AssertIsmExists(ctx context.Context, id util.HexAddress) error {
	ismExists, err := k.IsmExists(ctx, id)
	if err != nil || !ismExists {
		return fmt.Errorf("ism with id %s does not exist", id.String())
	}

	return nil
}

func (k Keeper) PostDispatchRouter() *util.Router[util.PostDispatchModule] {
	return k.postDispatchRouter
}

func (k *Keeper) PostDispatch(ctx context.Context, mailboxId, hookId util.HexAddress, metadata util.StandardHookMetadata, message util.HyperlaneMessage, maxFee sdk.Coins) (sdk.Coins, error) {
	handler, err := k.postDispatchRouter.GetModule(hookId)
	if err != nil {
		return sdk.NewCoins(), err
	}
	return (*handler).PostDispatch(ctx, mailboxId, hookId, metadata, message, maxFee)
}

func (k *Keeper) PostDispatchHookExists(ctx context.Context, hookId util.HexAddress) (bool, error) {
	handler, err := k.postDispatchRouter.GetModule(hookId)
	if err != nil {
		return false, err
	}
	return (*handler).Exists(ctx, hookId)
}

func (k *Keeper) QuoteDispatch(ctx context.Context, mailboxId, overwriteHookId util.HexAddress, metadata util.StandardHookMetadata, message util.HyperlaneMessage) (sdk.Coins, error) {
	mailbox, err := k.Mailboxes.Get(ctx, mailboxId.GetInternalId())
	if err != nil {
		return sdk.NewCoins(), fmt.Errorf("failed to find mailbox with id %s", mailboxId.String())
	}

	// check for valid mailbox state
	if mailbox.RequiredHook == nil {
		return sdk.NewCoins(), types.ErrRequiredHookNotSet
	}
	if mailbox.DefaultHook == nil {
		return sdk.NewCoins(), types.ErrDefaultHookNotSet
	}

	calculateGasPayment := func(hookId util.HexAddress) (sdk.Coins, error) {
		handler, err := k.postDispatchRouter.GetModule(hookId)
		if err != nil {
			return sdk.NewCoins(), err
		}

		return (*handler).QuoteDispatch(ctx, mailboxId, hookId, metadata, message)
	}

	requiredGasPayment, err := calculateGasPayment(*mailbox.RequiredHook)
	if err != nil {
		return sdk.NewCoins(), err
	}

	var defaultHookId util.HexAddress
	if overwriteHookId.IsZeroAddress() {
		defaultHookId = *mailbox.DefaultHook
	} else {
		defaultHookId = overwriteHookId
	}

	defaultGasPayment, err := calculateGasPayment(defaultHookId)
	if err != nil {
		return sdk.NewCoins(), err
	}

	return sdk.Coins.Add(requiredGasPayment, defaultGasPayment...), nil
}

func (k *Keeper) AssertPostDispatchHookExists(ctx context.Context, id util.HexAddress) error {
	hookExists, err := k.PostDispatchHookExists(ctx, id)
	if err != nil || !hookExists {
		return fmt.Errorf("hook with id %s does not exist", id.String())
	}
	return nil
}

func (k Keeper) LocalDomain(ctx context.Context, mailboxId util.HexAddress) (uint32, error) {
	mailbox, err := k.Mailboxes.Get(ctx, mailboxId.GetInternalId())
	if err != nil {
		return 0, err
	}
	return mailbox.LocalDomain, err
}

func (k Keeper) MailboxIdExists(ctx context.Context, mailboxId util.HexAddress) (bool, error) {
	mailbox, err := k.Mailboxes.Has(ctx, mailboxId.GetInternalId())
	if err != nil {
		return false, err
	}
	return mailbox, nil
}

func (k Keeper) GetMailbox(ctx context.Context, mailboxId util.HexAddress) (types.Mailbox, error) {
	mailbox, err := k.Mailboxes.Get(ctx, mailboxId.GetInternalId())
	if err != nil {
		return types.Mailbox{}, err
	}
	return mailbox, nil
}
