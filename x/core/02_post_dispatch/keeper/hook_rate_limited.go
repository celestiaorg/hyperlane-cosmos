package keeper

import (
	"context"
	"math/big"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type RateLimitedHookHandler struct {
	k Keeper
}

var _ util.PostDispatchModule = RateLimitedHookHandler{}

// Exists returns whether a rate-limited hook exists for the given hook ID.
func (r RateLimitedHookHandler) Exists(ctx context.Context, hookId util.HexAddress) (bool, error) {
	has, err := r.k.rateLimitedHooks.Has(ctx, hookId.GetInternalId())
	if err != nil {
		return false, err
	}
	return has, nil
}

// HookType returns the Hyperlane post-dispatch hook type for rate-limited hooks.
func (r RateLimitedHookHandler) HookType() uint8 {
	return types.POST_DISPATCH_HOOK_TYPE_RATE_LIMITED
}

// QuoteDispatch returns the required fee for the hook.
// Rate limiting does not charge fees, so it always returns zero coins.
func (r RateLimitedHookHandler) QuoteDispatch(_ context.Context, _, _ util.HexAddress, _ util.StandardHookMetadata, _ util.HyperlaneMessage) (sdk.Coins, error) {
	return sdk.NewCoins(), nil
}

// PostDispatch validates and consumes rate-limit capacity for a Warp token message.
// It rejects messages from the wrong mailbox, non-latest messages, unconfigured token IDs,
// malformed token message bodies, and transfers above the token's current bucket level.
func (r RateLimitedHookHandler) PostDispatch(ctx context.Context, mailboxId, hookId util.HexAddress, _ util.StandardHookMetadata, message util.HyperlaneMessage, _ sdk.Coins) (sdk.Coins, error) {
	hook, err := r.k.rateLimitedHooks.Get(ctx, hookId.GetInternalId())
	if err != nil {
		return sdk.NewCoins(), err
	}

	if hook.MailboxId != mailboxId {
		return sdk.NewCoins(), errors.Wrapf(types.ErrSenderIsNotDesignatedMailbox, "required mailbox id: %s, sender mailbox id: %s", hook.MailboxId, mailboxId.String())
	}

	messageId := message.Id()
	isLatest, err := r.k.coreKeeper.IsLatestDispatchedMessage(ctx, mailboxId, message)
	if err != nil {
		return sdk.NewCoins(), errors.Wrap(types.ErrInvalidDispatchedMessage, err.Error())
	}
	if !isLatest {
		return sdk.NewCoins(), errors.Wrapf(types.ErrInvalidDispatchedMessage, "message %s is not latest dispatched message", messageId.String())
	}

	key := types.RateLimitBucketKey(hookId, message.Sender)
	bucket, err := r.k.rateLimitBuckets.Get(ctx, key)
	if err != nil {
		return sdk.NewCoins(), errors.Wrapf(types.ErrRateLimitNotConfigured, "hook: %s token: %s", hookId.String(), message.Sender.String())
	}

	amount, err := tokenMessageAmount(message.Body)
	if err != nil {
		return sdk.NewCoins(), err
	}

	currentLevel := r.k.CurrentRateLimitLevel(ctx, bucket)
	if currentLevel.LT(amount) {
		return sdk.NewCoins(), errors.Wrapf(types.ErrRateLimitExceeded, "amount %s exceeds current level %s", amount.String(), currentLevel.String())
	}

	now := currentBlockUnix(ctx)
	bucket.FilledLevel = currentLevel.Sub(amount)
	bucket.LastUpdated = now

	if err := r.k.rateLimitBuckets.Set(ctx, key, bucket); err != nil {
		return sdk.NewCoins(), err
	}

	_ = sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventConsumeRateLimit{
		RateLimitedHookId: hookId,
		TokenId:           message.Sender,
		MessageId:         messageId,
		Amount:            amount,
		FilledLevel:       bucket.FilledLevel,
		LastUpdated:       bucket.LastUpdated,
	})

	return sdk.NewCoins(), nil
}

// CurrentRateLimitLevel returns the lazily refilled bucket level at the current block time.
// The value is capped at the bucket's effective capacity, which is derived from the
// integer per-second refill rate over the fixed one-day refill duration.
func (k Keeper) CurrentRateLimitLevel(ctx context.Context, bucket types.RateLimitBucket) math.Int {
	effectiveCapacity := rateLimitEffectiveCapacity(bucket)
	if !effectiveCapacity.IsPositive() {
		return math.ZeroInt()
	}

	now := currentBlockUnix(ctx)
	if now > bucket.LastUpdated+types.RateLimitDurationSeconds {
		return effectiveCapacity
	}

	elapsed := uint64(0)
	if now > bucket.LastUpdated {
		elapsed = now - bucket.LastUpdated
	}

	currentLevel := bucket.FilledLevel.Add(bucket.RefillRate.Mul(math.NewInt(int64(elapsed))))
	if currentLevel.GT(effectiveCapacity) {
		return effectiveCapacity
	}

	return currentLevel
}

// currentBlockUnix returns the current block time as a non-negative Unix timestamp.
func currentBlockUnix(ctx context.Context) uint64 {
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if now < 0 {
		return 0
	}
	return uint64(now)
}

// rateLimitEffectiveCapacity returns the maximum level a bucket can refill to.
// This can be lower than MaxCapacity because RefillRate is rounded down when configured.
func rateLimitEffectiveCapacity(bucket types.RateLimitBucket) math.Int {
	return bucket.RefillRate.Mul(types.RateLimitDuration)
}

// tokenMessageAmount decodes the Hyperlane TokenMessage amount from bytes 32:64.
func tokenMessageAmount(body []byte) (math.Int, error) {
	if len(body) < 64 {
		return math.Int{}, errors.Wrapf(types.ErrInvalidRateLimitedHook, "token message body must be at least 64 bytes: %d", len(body))
	}

	amount := new(big.Int).SetBytes(body[32:64])
	return math.NewIntFromBigInt(amount), nil
}
