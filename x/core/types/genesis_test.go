package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
)

func TestGenesisStateValidateMessages(t *testing.T) {
	mailbox := func(id uint64, sent, received uint32) types.Mailbox {
		return types.Mailbox{
			Id:              util.GenerateHexAddress([20]byte{}, uint32(types.ModuleId), id),
			MessageSent:     sent,
			MessageReceived: received,
		}
	}
	message := func(mailboxId uint64, n int64) types.GenesisMailboxMessageWrapper {
		return types.GenesisMailboxMessageWrapper{MailboxId: mailboxId, MessageId: util.CreateMockHexAddress("message", n)}
	}

	tests := []struct {
		name      string
		mailboxes []types.Mailbox
		messages  []types.GenesisMailboxMessageWrapper
		err       string
	}{
		{
			name: "empty",
		},
		{
			name:      "dispatched and received messages",
			mailboxes: []types.Mailbox{mailbox(0, 2, 1), mailbox(1, 0, 1)},
			messages:  []types.GenesisMailboxMessageWrapper{message(0, 0), message(0, 1), message(0, 2), message(1, 0)},
		},
		{
			name:      "duplicated message",
			mailboxes: []types.Mailbox{mailbox(0, 1, 1)},
			messages:  []types.GenesisMailboxMessageWrapper{message(0, 0), message(0, 0)},
			err:       "duplicated message",
		},
		{
			name:      "received message dispatched again",
			mailboxes: []types.Mailbox{mailbox(0, 2, 1)},
			messages:  []types.GenesisMailboxMessageWrapper{message(0, 0), message(0, 1)},
		},
		{
			name:      "fewer messages than max(sent, received)",
			mailboxes: []types.Mailbox{mailbox(0, 3, 1)},
			messages:  []types.GenesisMailboxMessageWrapper{message(0, 0), message(0, 1)},
			err:       "messages (2) not in range [max(sent, received) (3), sent + received (4)]",
		},
		{
			name:      "message for unknown mailbox",
			mailboxes: []types.Mailbox{mailbox(0, 1, 0)},
			messages:  []types.GenesisMailboxMessageWrapper{message(0, 0), message(1, 1)},
			err:       "for unknown mailbox 1",
		},
		{
			name:      "too many messages",
			mailboxes: []types.Mailbox{mailbox(0, 0, 1)},
			messages:  []types.GenesisMailboxMessageWrapper{message(0, 0), message(0, 1)},
			err:       "messages (2) not in range [max(sent, received) (1), sent + received (1)]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := types.NewGenesisState()
			gs.Mailboxes = tc.mailboxes
			gs.Messages = tc.messages

			err := gs.Validate()
			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
		})
	}
}

func TestGenesisStateValidateSubmodules(t *testing.T) {
	gs := types.NewGenesisState()
	gs.IsmGenesis = nil
	require.NoError(t, gs.Validate())

	gs = types.NewGenesisState()
	gs.PostDispatchGenesis = nil
	require.ErrorContains(t, gs.Validate(), "post dispatch genesis: missing data")

	gs = types.NewGenesisState()
	gs.PostDispatchGenesis.Igps = nil
	require.ErrorContains(t, gs.Validate(), "post dispatch genesis: missing data")
}
