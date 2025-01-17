package gossip

import (
	hiveproto "github.com/iotaledger/hive.go/protocol/message"
	"github.com/iotaledger/hive.go/protocol/tlv"
)

var (
	// contains the definition for gossip messages
	gossipMessageRegistry *hiveproto.Registry
)

func init() {
	definitions := []*hiveproto.Definition{
		tlv.HeaderMessageDefinition,
		milestoneRequestMessageDefinition,
		blockMessageDefinition,
		blockRequestMessageDefinition,
		heartbeatMessageDefinition,
	}
	gossipMessageRegistry = hiveproto.NewRegistry(definitions)
}
