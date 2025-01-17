package utxo

import (
	"github.com/iotaledger/hive.go/marshalutil"
	"github.com/iotaledger/hornet/pkg/model/milestone"
	iotago "github.com/iotaledger/iota.go/v3"
)

func ParseOutputID(ms *marshalutil.MarshalUtil) (iotago.OutputID, error) {
	o := iotago.OutputID{}
	bytes, err := ms.ReadBytes(iotago.OutputIDLength)
	if err != nil {
		return o, err
	}
	copy(o[:], bytes)
	return o, nil
}

func parseTransactionID(ms *marshalutil.MarshalUtil) (iotago.TransactionID, error) {
	t := iotago.TransactionID{}
	bytes, err := ms.ReadBytes(iotago.TransactionIDLength)
	if err != nil {
		return t, err
	}
	copy(t[:], bytes)
	return t, nil
}

func ParseBlockID(ms *marshalutil.MarshalUtil) (iotago.BlockID, error) {
	bytes, err := ms.ReadBytes(iotago.BlockIDLength)
	if err != nil {
		return iotago.EmptyBlockID(), err
	}
	blockID := iotago.BlockID{}
	copy(blockID[:], bytes)
	return blockID, nil
}

func parseMilestoneIndex(ms *marshalutil.MarshalUtil) (milestone.Index, error) {
	index, err := ms.ReadUint32()
	if err != nil {
		return 0, err
	}
	return milestone.Index(index), nil
}
