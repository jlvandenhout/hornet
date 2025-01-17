package testsuite

import (
	"context"
	"math/rand"

	"github.com/stretchr/testify/require"

	"github.com/iotaledger/hive.go/serializer/v2"
	"github.com/iotaledger/hornet/pkg/model/storage"
	"github.com/iotaledger/hornet/pkg/model/utxo"
	"github.com/iotaledger/hornet/pkg/testsuite/utils"
	iotago "github.com/iotaledger/iota.go/v3"
	"github.com/iotaledger/iota.go/v3/builder"
)

type BlockBuilder struct {
	te      *TestEnvironment
	tag     string
	tagData []byte

	parents iotago.BlockIDs

	fromWallet *utils.HDWallet
	toWallet   *utils.HDWallet

	amount uint64

	fakeInputs  bool
	outputToUse *utxo.Output
}

type Block struct {
	builder *BlockBuilder
	block   *storage.Block

	consumedOutputs []*utxo.Output
	createdOutputs  []*utxo.Output

	booked        bool
	storedBlockID iotago.BlockID
}

func (te *TestEnvironment) NewBlockBuilder(optionalTag ...string) *BlockBuilder {
	tag := ""
	if len(optionalTag) > 0 {
		tag = optionalTag[0]
	}
	return &BlockBuilder{
		te:  te,
		tag: tag,
	}
}

func (b *BlockBuilder) LatestMilestoneAsParents() *BlockBuilder {
	return b.Parents(iotago.BlockIDs{b.te.coo.lastMilestoneBlockID})
}

func (b *BlockBuilder) Parents(parents iotago.BlockIDs) *BlockBuilder {
	b.parents = parents
	return b
}

func (b *BlockBuilder) FromWallet(wallet *utils.HDWallet) *BlockBuilder {
	b.fromWallet = wallet
	return b
}

func (b *BlockBuilder) Amount(amount uint64) *BlockBuilder {
	b.amount = amount
	return b
}

func (b *BlockBuilder) FakeInputs() *BlockBuilder {
	b.fakeInputs = true
	return b
}

func (b *BlockBuilder) UsingOutput(output *utxo.Output) *BlockBuilder {
	b.outputToUse = output
	return b
}

func (b *BlockBuilder) TagData(data []byte) *BlockBuilder {
	b.tagData = data
	return b
}

func (b *BlockBuilder) fromWalletSigner() iotago.AddressSigner {
	require.NotNil(b.te.TestInterface, b.fromWallet)

	inputPrivateKey, _ := b.fromWallet.KeyPair()
	return iotago.NewInMemoryAddressSigner(iotago.AddressKeys{Address: b.fromWallet.Address(), Keys: inputPrivateKey})
}

func (b *BlockBuilder) fromWalletOutputs() ([]*utxo.Output, uint64) {
	require.NotNil(b.te.TestInterface, b.fromWallet)
	require.Greaterf(b.te.TestInterface, b.amount, uint64(0), "trying to send a transaction with no value")

	var outputsThatCanBeConsumed []*utxo.Output

	if b.outputToUse != nil {
		// Only use the given output
		outputsThatCanBeConsumed = append(outputsThatCanBeConsumed, b.outputToUse)
	} else {
		if b.fakeInputs {
			// Add a fake output with enough balance to create a valid transaction
			fakeInputID := iotago.OutputID{}
			copy(fakeInputID[:], randBytes(iotago.TransactionIDLength))
			fakeInput := &iotago.BasicOutput{
				Amount: b.amount,
				Conditions: iotago.UnlockConditions{
					&iotago.AddressUnlockCondition{
						Address: b.fromWallet.Address(),
					},
				},
			}
			outputsThatCanBeConsumed = append(outputsThatCanBeConsumed, utxo.CreateOutput(fakeInputID, iotago.EmptyBlockID(), 0, 0, fakeInput))
		} else {
			outputsThatCanBeConsumed = b.fromWallet.Outputs()
		}
	}

	require.NotEmptyf(b.te.TestInterface, outputsThatCanBeConsumed, "no outputs available on the wallet")

	outputsBalance := uint64(0)
	for _, output := range outputsThatCanBeConsumed {
		outputsBalance += output.Deposit()
	}

	require.GreaterOrEqualf(b.te.TestInterface, outputsBalance, b.amount, "not enough balance in the selected outputs to send the requested amount")

	return outputsThatCanBeConsumed, outputsBalance
}

func (b *BlockBuilder) txBuilderFromWalletSendingOutputs(outputs ...iotago.Output) (txBuilder *builder.TransactionBuilder, consumedInputs []*utxo.Output) {
	require.Greater(b.te.TestInterface, len(outputs), 0)

	txBuilder = builder.NewTransactionBuilder(b.te.protoParas.NetworkID())

	fromAddr := b.fromWallet.Address()

	outputsThatCanBeConsumed, _ := b.fromWalletOutputs()

	var consumedAmount uint64
	for _, output := range outputsThatCanBeConsumed {
		txBuilder.AddInput(&builder.TxInput{UnlockTarget: fromAddr, InputID: output.OutputID(), Input: output.Output()})
		consumedInputs = append(consumedInputs, output)
		consumedAmount += output.Deposit()

		if consumedAmount >= b.amount {
			break
		}
	}

	for _, output := range outputs {
		txBuilder.AddOutput(output)
	}

	if b.amount < consumedAmount {
		// Send remainder back to fromWallet
		remainderAmount := consumedAmount - b.amount
		remainderOutput := &iotago.BasicOutput{Conditions: iotago.UnlockConditions{&iotago.AddressUnlockCondition{Address: fromAddr}}, Amount: remainderAmount}
		txBuilder.AddOutput(remainderOutput)
	}

	return
}

func (b *BlockBuilder) BuildTaggedData() *Block {

	require.NotEmpty(b.te.TestInterface, b.tag)

	iotaBlock, err := builder.NewBlockBuilder().
		ProtocolVersion(b.te.protoParas.Version).
		Parents(b.parents).
		Payload(&iotago.TaggedData{Tag: []byte(b.tag), Data: b.tagData}).
		Build()
	require.NoError(b.te.TestInterface, err)

	_, err = b.te.PoWHandler.DoPoW(context.Background(), iotaBlock, 1)
	require.NoError(b.te.TestInterface, err)

	block, err := storage.NewBlock(iotaBlock, serializer.DeSeriModePerformValidation, b.te.protoParas)
	require.NoError(b.te.TestInterface, err)

	return &Block{
		builder: b,
		block:   block,
	}
}

func (b *BlockBuilder) BuildTransactionUsingOutputs(outputs ...iotago.Output) *Block {
	txBuilder, consumedInputs := b.txBuilderFromWalletSendingOutputs(outputs...)

	if len(b.tag) > 0 {
		txBuilder.AddTaggedDataPayload(&iotago.TaggedData{Tag: []byte(b.tag), Data: b.tagData})
	}

	require.NotNil(b.te.TestInterface, b.parents)

	iotaBlock, err := txBuilder.BuildAndSwapToBlockBuilder(b.te.protoParas, b.fromWalletSigner(), nil).
		Parents(b.parents).
		ProofOfWork(context.Background(), b.te.protoParas, float64(b.te.protoParas.MinPoWScore)).
		Build()
	require.NoError(b.te.TestInterface, err)

	block, err := storage.NewBlock(iotaBlock, serializer.DeSeriModePerformValidation, b.te.protoParas)
	require.NoError(b.te.TestInterface, err)

	var sentUTXO []*utxo.Output

	// Book the outputs in the wallets
	blockTx := block.Transaction()
	txEssence := blockTx.Essence
	for i := range txEssence.Outputs {
		utxoOutput, err := utxo.NewOutput(block.BlockID(), b.te.LastMilestoneIndex()+1, 0, blockTx, uint16(i))
		require.NoError(b.te.TestInterface, err)
		sentUTXO = append(sentUTXO, utxoOutput)
	}

	return &Block{
		builder:         b,
		block:           block,
		consumedOutputs: consumedInputs,
		createdOutputs:  sentUTXO,
	}
}

func (b *BlockBuilder) BuildAlias() *Block {
	require.NotNil(b.te.TestInterface, b.fromWallet)
	fromAddress := b.fromWallet.Address()

	aliasOutput := &iotago.AliasOutput{
		Amount:         0,
		NativeTokens:   nil,
		AliasID:        iotago.AliasID{},
		StateIndex:     0,
		StateMetadata:  nil,
		FoundryCounter: 0,
		Conditions: iotago.UnlockConditions{
			&iotago.StateControllerAddressUnlockCondition{Address: fromAddress},
			&iotago.GovernorAddressUnlockCondition{Address: fromAddress},
		},
		Features: nil,
		ImmutableFeatures: iotago.Features{
			&iotago.IssuerFeature{Address: fromAddress},
		},
	}

	if b.amount == 0 {
		b.amount = b.te.protoParas.RentStructure.MinRent(aliasOutput)
		aliasOutput.Amount = b.amount
	}
	require.Greater(b.te.TestInterface, b.amount, uint64(0), "trying to send a transaction with no value")

	return b.BuildTransactionUsingOutputs(aliasOutput)
}

func (b *BlockBuilder) BuildTransactionToWallet(wallet *utils.HDWallet) *Block {
	b.toWallet = wallet
	output := &iotago.BasicOutput{Conditions: iotago.UnlockConditions{&iotago.AddressUnlockCondition{Address: b.toWallet.Address()}}, Amount: b.amount}
	return b.BuildTransactionUsingOutputs(output)
}

func (m *Block) Store() *Block {
	require.True(m.builder.te.TestInterface, m.storedBlockID.Empty())
	m.storedBlockID = m.builder.te.StoreBlock(m.block).Block().BlockID()
	return m
}

func (m *Block) BookOnWallets() *Block {
	require.False(m.builder.te.TestInterface, m.booked)
	m.builder.fromWallet.BookSpents(m.consumedOutputs)

	if m.builder.toWallet != nil {
		// Also book it in the toWallet because both addresses can have part ownership of the output.
		// Note: if there is a third wallet involved this will not catch it, and the third wallet will still hold a reference to it
		m.builder.toWallet.BookSpents(m.consumedOutputs)
	}

	for _, sentOutput := range m.createdOutputs {
		// Check if we should book the output to the toWallet or to the fromWallet
		switch output := sentOutput.Output().(type) {
		case *iotago.BasicOutput:
			if m.builder.toWallet != nil {
				if output.UnlockConditionSet().Address().Address.Equal(m.builder.toWallet.Address()) {
					m.builder.toWallet.BookOutput(sentOutput)
					continue
				}
			}
			// Note: we do not care about SDRUC here right now
			m.builder.fromWallet.BookOutput(sentOutput)

		case *iotago.AliasOutput:
			if m.builder.toWallet != nil {
				if output.UnlockConditionSet().GovernorAddress().Address.Equal(m.builder.toWallet.Address()) ||
					output.UnlockConditionSet().StateControllerAddress().Address.Equal(m.builder.toWallet.Address()) {
					m.builder.toWallet.BookOutput(sentOutput)
				}
			}
			if output.UnlockConditionSet().GovernorAddress().Address.Equal(m.builder.fromWallet.Address()) ||
				output.UnlockConditionSet().StateControllerAddress().Address.Equal(m.builder.fromWallet.Address()) {
				m.builder.fromWallet.BookOutput(sentOutput)
			}
		}
	}
	m.booked = true

	return m
}

func (m *Block) GeneratedUTXO() *utxo.Output {
	require.Greater(m.builder.te.TestInterface, len(m.createdOutputs), 0)
	return m.createdOutputs[0]
}

func (m *Block) IotaBlock() *iotago.Block {
	return m.block.Block()
}

func (m *Block) StoredBlock() *storage.Block {
	return m.block
}

func (m *Block) StoredBlockID() iotago.BlockID {
	require.NotNil(m.builder.te.TestInterface, m.storedBlockID)
	return m.storedBlockID
}

// returns length amount random bytes
func randBytes(length int) []byte {
	var b []byte
	for i := 0; i < length; i++ {
		b = append(b, byte(rand.Intn(256)))
	}
	return b
}
