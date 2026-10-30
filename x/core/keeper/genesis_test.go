package keeper_test

import (
	"math/big"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"

	i "github.com/bcp-innovations/hyperlane-cosmos/tests/integration"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/bcp-innovations/hyperlane-cosmos/x/core"
	"github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warpTypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

/*

TEST CASES - genesis.go

* ExportGenesis (valid) with dispatched and received messages passes ValidateGenesis
* ExportGenesis (valid) with a received message dispatched again passes ValidateGenesis

*/

var _ = Describe("genesis.go", Ordered, func() {
	var s *i.KeeperTestSuite
	var creator i.TestValidatorAddress
	var sender i.TestValidatorAddress

	BeforeEach(func() {
		s = i.NewCleanChain()
		creator = i.GenerateTestValidatorAddress("Creator")
		sender = i.GenerateTestValidatorAddress("Sender")
		err := s.MintBaseCoins(creator.Address, 1_000_000)
		Expect(err).To(BeNil())
		err = s.MintBaseCoins(sender.Address, 1_000_000)
		Expect(err).To(BeNil())
	})

	It("ExportGenesis (valid) with dispatched and received messages passes ValidateGenesis", func() {
		// Arrange
		mailboxId, _, _, ismId := createValidMailbox(s, creator.Address, "noop", 1)

		hexSender, _ := util.DecodeHexAddress(sender.Address)
		recipient, _ := util.DecodeHexAddress("0xd7194459d45619d04a5a0f9e78dc9594a0f37fd6da8382fe12ddda6f2f46d647")
		_, err := s.App().HyperlaneKeeper.DispatchMessage(
			s.Ctx(),
			mailboxId,
			hexSender,
			sdk.NewCoins(sdk.NewCoin("acoin", math.NewInt(1000000))),
			1,
			recipient,
			nil,
			util.StandardHookMetadata{
				GasLimit: math.NewInt(50000),
				Address:  sender.AccAddress,
			},
			nil,
		)
		Expect(err).To(BeNil())

		mockApp := i.CreateMockApp(s.App().HyperlaneKeeper.AppRouter())
		mockRecipient, err := mockApp.RegisterApp(s.Ctx(), ismId)
		Expect(err).To(BeNil())

		message := util.HyperlaneMessage{
			Version:     3,
			Nonce:       0,
			Origin:      0,
			Sender:      util.CreateMockHexAddress("sender", 0),
			Destination: 1,
			Recipient:   mockRecipient,
			Body:        nil,
		}
		_, err = s.RunTx(&types.MsgProcessMessage{
			MailboxId: mailboxId,
			Relayer:   sender.Address,
			Metadata:  "",
			Message:   message.String(),
		})
		Expect(err).To(BeNil())

		// Act
		coreModule := core.NewAppModule(s.App().AppCodec(), s.App().HyperlaneKeeper)
		genState := coreModule.ExportGenesis(s.Ctx(), s.App().AppCodec())

		// Assert
		Expect(coreModule.ValidateGenesis(s.App().AppCodec(), nil, genState)).To(Succeed())
	})

	It("ExportGenesis (valid) with a received message dispatched again passes ValidateGenesis", func() {
		// Arrange: a synthetic token enrolled as its own router on the local domain
		mailboxId, _, _, _ := createValidMailbox(s, creator.Address, "noop", 1)

		res, err := s.RunTx(&warpTypes.MsgCreateSyntheticToken{Owner: creator.Address, OriginMailbox: mailboxId})
		Expect(err).To(BeNil())
		var token warpTypes.MsgCreateSyntheticTokenResponse
		Expect(proto.Unmarshal(res.MsgResponses[0].Value, &token)).To(Succeed())

		_, err = s.RunTx(&warpTypes.MsgEnrollRemoteRouter{
			Owner:        creator.Address,
			TokenId:      token.Id,
			RemoteRouter: &warpTypes.RemoteRouter{ReceiverDomain: 1, ReceiverContract: token.Id, Gas: math.NewInt(50000)},
		})
		Expect(err).To(BeNil())

		var recipient util.HexAddress
		copy(recipient[12:], sender.AccAddress)
		payload, err := warpTypes.NewWarpPayload(recipient.Bytes(), *big.NewInt(100))
		Expect(err).To(BeNil())

		// Process a forged message equal to the next dispatch of the token
		message := util.HyperlaneMessage{
			Version:     3,
			Nonce:       0,
			Origin:      1,
			Sender:      token.Id,
			Destination: 1,
			Recipient:   token.Id,
			Body:        payload.Bytes(),
		}
		_, err = s.RunTx(&types.MsgProcessMessage{MailboxId: mailboxId, Relayer: sender.Address, Message: message.String()})
		Expect(err).To(BeNil())

		// Act
		_, err = s.RunTx(&warpTypes.MsgRemoteTransfer{
			Sender:            sender.Address,
			TokenId:           token.Id,
			DestinationDomain: 1,
			Recipient:         recipient,
			Amount:            math.NewInt(100),
			GasLimit:          math.ZeroInt(),
			MaxFee:            sdk.NewCoin("acoin", math.NewInt(1000000)),
		})
		Expect(err).To(BeNil())

		// Assert
		mailbox, err := s.App().HyperlaneKeeper.Mailboxes.Get(s.Ctx(), mailboxId.GetInternalId())
		Expect(err).To(BeNil())
		Expect(mailbox.MessageSent).To(Equal(uint32(1)))
		Expect(mailbox.MessageReceived).To(Equal(uint32(1)))

		coreModule := core.NewAppModule(s.App().AppCodec(), s.App().HyperlaneKeeper)
		genState := coreModule.ExportGenesis(s.Ctx(), s.App().AppCodec())
		var gs types.GenesisState
		Expect(s.App().AppCodec().UnmarshalJSON(genState, &gs)).To(Succeed())
		Expect(gs.Messages).To(HaveLen(1))
		Expect(coreModule.ValidateGenesis(s.App().AppCodec(), nil, genState)).To(Succeed())
	})
})
