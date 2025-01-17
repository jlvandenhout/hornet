package utxo_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/iotaledger/hive.go/kvstore/mapdb"
	"github.com/iotaledger/hornet/pkg/model/milestone"
	"github.com/iotaledger/hornet/pkg/model/utxo"
	iotago "github.com/iotaledger/iota.go/v3"
)

func TestConfirmationApplyAndRollbackToEmptyLedger(t *testing.T) {

	manager := utxo.New(mapdb.NewMapDB())

	outputs := utxo.Outputs{
		RandUTXOOutput(iotago.OutputBasic),
		RandUTXOOutput(iotago.OutputBasic),
		RandUTXOOutput(iotago.OutputNFT),   // spent
		RandUTXOOutput(iotago.OutputBasic), // spent
		RandUTXOOutput(iotago.OutputAlias),
		RandUTXOOutput(iotago.OutputNFT),
		RandUTXOOutput(iotago.OutputFoundry),
	}

	msIndex := milestone.Index(756)
	msTimestamp := rand.Uint32()

	spents := utxo.Spents{
		RandUTXOSpent(outputs[3], msIndex, msTimestamp),
		RandUTXOSpent(outputs[2], msIndex, msTimestamp),
	}

	require.NoError(t, manager.ApplyConfirmationWithoutLocking(msIndex, outputs, spents, nil, nil))

	var outputCount int
	require.NoError(t, manager.ForEachOutput(func(_ *utxo.Output) bool {
		outputCount++
		return true
	}))
	require.Equal(t, 7, outputCount)

	var unspentCount int
	require.NoError(t, manager.ForEachUnspentOutput(func(_ *utxo.Output) bool {
		unspentCount++
		return true
	}))
	require.Equal(t, 5, unspentCount)

	var spentCount int
	require.NoError(t, manager.ForEachSpentOutput(func(_ *utxo.Spent) bool {
		spentCount++
		return true
	}))
	require.Equal(t, 2, spentCount)

	require.NoError(t, manager.RollbackConfirmationWithoutLocking(msIndex, outputs, spents, nil, nil))

	require.NoError(t, manager.ForEachOutput(func(_ *utxo.Output) bool {
		require.Fail(t, "should not be called")
		return true
	}))

	require.NoError(t, manager.ForEachUnspentOutput(func(_ *utxo.Output) bool {
		require.Fail(t, "should not be called")
		return true
	}))

	require.NoError(t, manager.ForEachSpentOutput(func(_ *utxo.Spent) bool {
		require.Fail(t, "should not be called")
		return true
	}))
}

func TestConfirmationApplyAndRollbackToPreviousLedger(t *testing.T) {

	manager := utxo.New(mapdb.NewMapDB())

	previousOutputs := utxo.Outputs{
		RandUTXOOutput(iotago.OutputBasic),
		RandUTXOOutput(iotago.OutputBasic), // spent
		RandUTXOOutput(iotago.OutputNFT),   // spent on 2nd confirmation
	}

	previousMsIndex := milestone.Index(48)
	previousMsTimestamp := rand.Uint32()
	previousSpents := utxo.Spents{
		RandUTXOSpent(previousOutputs[1], previousMsIndex, previousMsTimestamp),
	}
	require.NoError(t, manager.ApplyConfirmationWithoutLocking(previousMsIndex, previousOutputs, previousSpents, nil, nil))

	ledgerIndex, err := manager.ReadLedgerIndex()
	require.NoError(t, err)
	require.Equal(t, previousMsIndex, ledgerIndex)

	outputs := utxo.Outputs{
		RandUTXOOutput(iotago.OutputBasic),
		RandUTXOOutput(iotago.OutputFoundry),
		RandUTXOOutput(iotago.OutputBasic), // spent
		RandUTXOOutput(iotago.OutputAlias),
	}
	msIndex := milestone.Index(49)
	msTimestamp := rand.Uint32()
	spents := utxo.Spents{
		RandUTXOSpent(previousOutputs[2], msIndex, msTimestamp),
		RandUTXOSpent(outputs[2], msIndex, msTimestamp),
	}
	require.NoError(t, manager.ApplyConfirmationWithoutLocking(msIndex, outputs, spents, nil, nil))

	ledgerIndex, err = manager.ReadLedgerIndex()
	require.NoError(t, err)
	require.Equal(t, msIndex, ledgerIndex)

	// Prepare values to check
	outputByOutputID := make(map[string]struct{})
	unspentByOutputID := make(map[string]struct{})
	for _, output := range previousOutputs {
		outputByOutputID[output.MapKey()] = struct{}{}
		unspentByOutputID[output.MapKey()] = struct{}{}
	}
	for _, output := range outputs {
		outputByOutputID[output.MapKey()] = struct{}{}
		unspentByOutputID[output.MapKey()] = struct{}{}
	}

	spentByOutputID := make(map[string]struct{})
	for _, spent := range previousSpents {
		spentByOutputID[spent.MapKey()] = struct{}{}
		delete(unspentByOutputID, spent.MapKey())
	}
	for _, spent := range spents {
		spentByOutputID[spent.MapKey()] = struct{}{}
		delete(unspentByOutputID, spent.MapKey())
	}

	var outputCount int
	require.NoError(t, manager.ForEachOutput(func(output *utxo.Output) bool {
		outputCount++
		_, has := outputByOutputID[output.MapKey()]
		require.True(t, has)
		delete(outputByOutputID, output.MapKey())
		return true
	}))
	require.Empty(t, outputByOutputID)
	require.Equal(t, 7, outputCount)

	var unspentCount int
	require.NoError(t, manager.ForEachUnspentOutput(func(output *utxo.Output) bool {
		unspentCount++
		_, has := unspentByOutputID[output.MapKey()]
		require.True(t, has)
		delete(unspentByOutputID, output.MapKey())
		return true
	}))
	require.Equal(t, 4, unspentCount)
	require.Empty(t, unspentByOutputID)

	var spentCount int
	require.NoError(t, manager.ForEachSpentOutput(func(spent *utxo.Spent) bool {
		spentCount++
		_, has := spentByOutputID[spent.MapKey()]
		require.True(t, has)
		delete(spentByOutputID, spent.MapKey())
		return true
	}))
	require.Empty(t, spentByOutputID)
	require.Equal(t, 3, spentCount)

	require.NoError(t, manager.RollbackConfirmationWithoutLocking(msIndex, outputs, spents, nil, nil))

	ledgerIndex, err = manager.ReadLedgerIndex()
	require.NoError(t, err)
	require.Equal(t, previousMsIndex, ledgerIndex)

	// Prepare values to check
	outputByOutputID = make(map[string]struct{})
	unspentByOutputID = make(map[string]struct{})
	spentByOutputID = make(map[string]struct{})

	for _, output := range previousOutputs {
		outputByOutputID[output.MapKey()] = struct{}{}
		unspentByOutputID[output.MapKey()] = struct{}{}
	}

	for _, spent := range previousSpents {
		spentByOutputID[spent.MapKey()] = struct{}{}
		delete(unspentByOutputID, spent.MapKey())
	}

	require.NoError(t, manager.ForEachOutput(func(output *utxo.Output) bool {
		_, has := outputByOutputID[output.MapKey()]
		require.True(t, has)
		delete(outputByOutputID, output.MapKey())
		return true
	}))
	require.Empty(t, outputByOutputID)

	require.NoError(t, manager.ForEachUnspentOutput(func(output *utxo.Output) bool {
		_, has := unspentByOutputID[output.MapKey()]
		require.True(t, has)
		delete(unspentByOutputID, output.MapKey())
		return true
	}))
	require.Empty(t, unspentByOutputID)

	require.NoError(t, manager.ForEachSpentOutput(func(spent *utxo.Spent) bool {
		_, has := spentByOutputID[spent.MapKey()]
		require.True(t, has)
		delete(spentByOutputID, spent.MapKey())
		return true
	}))
	require.Empty(t, spentByOutputID)
}
