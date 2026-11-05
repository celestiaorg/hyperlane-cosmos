package types

import (
	"fmt"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismTypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	pdTypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
)

// NewGenesisState creates a new genesis state with default values.
func NewGenesisState() *GenesisState {
	return &GenesisState{
		IsmGenesis:           ismTypes.NewGenesisState(),
		PostDispatchGenesis:  pdTypes.NewGenesisState(),
		Mailboxes:            []Mailbox{},
		Messages:             []GenesisMailboxMessageWrapper{},
		IsmSequence:          0,
		PostDispatchSequence: 0,
		AppSequence:          0,
	}
}

// Validate performs basic genesis state validation returning an error upon any
func (gs *GenesisState) Validate() error {
	if err := gs.PostDispatchGenesis.Validate(); err != nil {
		return err
	}

	if err := gs.IsmGenesis.Validate(); err != nil {
		return err
	}

	messages := make(map[uint64]map[util.HexAddress]struct{})
	for _, m := range gs.Messages {
		// Mailbox ids are checked below to equal their index.
		if m.MailboxId >= uint64(len(gs.Mailboxes)) {
			return fmt.Errorf("message (%s) for unknown mailbox %d", m.MessageId, m.MailboxId)
		}
		if _, ok := messages[m.MailboxId][m.MessageId]; ok {
			return fmt.Errorf("duplicated message (%s) for mailbox %d, ", m.MessageId, m.MailboxId)
		}
		if messages[m.MailboxId] == nil {
			messages[m.MailboxId] = make(map[util.HexAddress]struct{})
		}
		messages[m.MailboxId][m.MessageId] = struct{}{}
	}

	for i, mailbox := range gs.Mailboxes {
		if mailbox.Id.GetInternalId() != uint64(i) {
			return fmt.Errorf("duplicated mailbox id %d, %d", mailbox.Id.GetInternalId(), i)
		}
		// Dispatched ids are unique by nonce and received ids by the replay check, but a received id can be dispatched again.
		messageCount := uint64(len(messages[mailbox.Id.GetInternalId()]))
		sent, received := uint64(mailbox.MessageSent), uint64(mailbox.MessageReceived)
		if messageCount < max(sent, received) || messageCount > sent+received {
			return fmt.Errorf("messages (%d) not in range [max(sent, received) (%d), sent + received (%d)] for mailbox %s", messageCount, max(sent, received), sent+received, mailbox.Id)
		}
	}

	return nil
}
