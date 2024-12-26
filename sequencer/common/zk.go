// Package common zk.go contains all the common data structures used at the
// hermez-node, zk.go contains the zkSnark inputs used to generate the proof
package common

import (
	"math/big"
)

// ZKInputs represents the inputs that will be used to generate the zkSNARK
// proof
type ZKInputs struct {
	// CurrentNumBatch is the current batch number processed
	CurrentNumBatch *uint32 `json:"currentNumBatch"` // uint32
	// inputs for final `hashGlobalInputs`
	// OldLastIdx is the last index assigned to an account
	OldLastIdx *uint32 `json:"oldLastIdx"` // uint32 (max nLevels bits)
	// OldStateRoot is the current account merkle tree root
	OldAccountRoot *big.Int `json:"oldAccountRoot"`

	//Vouches
	// OldVouchRoot is the current vouch merkle tree root
	OldVouchRoot *big.Int `json:"oldVouchRoot"`

	//Score
	// OldScoreRoot is the current score merkle tree root
	OldScoreRoot *big.Int `json:"oldScoreRoot"`

	// GlobalChainID is the blockchain ID (0 for Ethereum mainnet). This
	// value can be get from the smart contract.
	GlobalChainID *uint16 `json:"globalChainID"` // uint16
	//
	// Txs (L1&L2)
	//

	// transaction L1-L2
	// TxCompressedData, encodes the transactions fields together
	TxCompressedData []*big.Int `json:"txCompressedData"` // big.Int (max 251 bits), len: [maxTx]
	// MaxNumBatch is the maximum allowed batch number when the transaction
	// can be processed
	MaxNumBatch []*uint32 `json:"maxNumBatch"` // [uint32], len: [maxTx]

	// FromIdx
	FromIdx []*uint32 `json:"fromIdx"` // uint32 (max nLevels bits), len: [maxTx] index sender
	// AuxFromIdx is the AccountIdx of the new created account which is
	// consequence of a L1CreateAccountTx
	AuxFromIdx []*uint32 `json:"auxFromIdx"` // uint32 (max nLevels bits), len: [maxTx] auxilary index to create account

	// ToIdx
	ToIdx []*uint32 `json:"toIdx"` // uint32 (max nLevels bits), len: [maxTx] reciever index
	//auxillary index when signed index reciever is set to null
	AuxToIdx []*uint32 `json:"auxToIdx"` // uint32 (max nLevels bits), len: [maxTx]
	// ToBJJAy, bjj y coordinate reciever
	ToBJJAy []*big.Int `json:"toBjjAy"` // big.Int, len: [maxTx]
	// ToEthAddr, ethereum address reciever
	ToEthAddr []*big.Int `json:"toEthAddr"` // ethCommon.Address, len: [maxTx]
	// AmountF encoded as float40
	AmountF []*big.Int `json:"amountF"` // uint40 len: [maxTx]

	// OnChain determines if is L1 (1/true) or L2 (0/false)
	OnChain []*bool `json:"onChain"` // bool, len: [maxTx - 1]

	//
	// Txs/L1Txs
	//
	// NewAccount boolean (0/1) flag set 'true' when L1 tx creates a new account
	NewAccount []*bool `json:"newAccount"` // bool, len: [maxTx]
	// DepositAmountF encoded as float40
	DepositAmountF []*big.Int `json:"loadAmountF"` // uint40, len: [maxTx]
	// FromEthAddr, etherum address sender
	FromEthAddr []*big.Int `json:"fromEthAddr"` // ethCommon.Address, len: [maxTx]
	// FromBJJCompressed boolean encoded where each value is a *big.Int
	FromBJJCompressed [][256]*big.Int `json:"fromBjjCompressed"` // bool array, len: [maxTx][256]

	//
	// Txs/L2Txs
	//
	// transaction L2 signature
	// S, eddsa signature field s
	S []*big.Int `json:"s"` // big.Int, len: [maxTx]
	// R8x, eddsa signature field r8x
	R8x []*big.Int `json:"r8x"` // big.Int, len: [maxTx]
	// R8y, eddsa signature field r8y
	R8y []*big.Int `json:"r8y"` // big.Int, len: [maxTx]

	//
	// State MerkleTree Leafs transitions
	//

	// state 1, value of the sender (from) account leaf. The values at the
	// moment pre-smtprocessor of the update (before updating the Sender
	// leaf).
	// TokenID1  []*big.Int   `json:"tokenID1"`  // uint32, len: [maxTx]
	Nonce1    []*big.Int   `json:"nonce1"`    // uint64 (max 40 bits), len: [maxTx]
	Sign1     []*bool      `json:"sign1"`     // bool, len: [maxTx]
	Ay1       []*big.Int   `json:"ay1"`       // big.Int, len: [maxTx]
	Balance1  []*big.Int   `json:"balance1"`  // big.Int (max 192 bits), len: [maxTx]
	EthAddr1  []*big.Int   `json:"ethAddr1"`  // ethCommon.Address, len: [maxTx]
	Siblings1 [][]*big.Int `json:"siblings1"` // big.Int, len: [maxTx][nLevels + 1]
	// Required for inserts and deletes, values of the CircomProcessorProof
	// (smt insert proof)
	IsOld0_1  []*bool    `json:"isOld0_1"`  // bool, len: [maxTx]
	OldKey1   []*big.Int `json:"oldKey1"`   // uint64 (max 40 bits), len: [maxTx]
	OldValue1 []*big.Int `json:"oldValue1"` // Hash, len: [maxTx]

	// state 2, value of the receiver (to) account leaf. The values at the
	// moment pre-smtprocessor of the update (before updating the Receiver
	// leaf).
	// If Tx is an Exit (tx.ToIdx=1), state 2 is used for the Exit Merkle
	// Proof of the Exit MerkleTree.
	// TokenID2  []*big.Int   `json:"tokenID2"`  // uint32, len: [maxTx]
	Nonce2    []*big.Int   `json:"nonce2"`    // uint64 (max 40 bits), len: [maxTx]
	Sign2     []*bool      `json:"sign2"`     // bool, len: [maxTx]
	Ay2       []*big.Int   `json:"ay2"`       // big.Int, len: [maxTx]
	Balance2  []*big.Int   `json:"balance2"`  // big.Int (max 192 bits), len: [maxTx]
	EthAddr2  []*big.Int   `json:"ethAddr2"`  // ethCommon.Address, len: [maxTx]
	Siblings2 [][]*big.Int `json:"siblings2"` // big.Int, len: [maxTx][nLevels + 1]
	// Required for inserts and deletes, values of the CircomProcessorProof
	// (smt insert proof)
	IsOld0_2  []*bool    `json:"isOld0_2"`  // bool, len: [maxTx]
	OldKey2   []*big.Int `json:"oldKey2"`   // uint64 (max 40 bits), len: [maxTx]
	OldValue2 []*big.Int `json:"oldValue2"` // Hash, len: [maxTx]

	//Vouch
	VouchExists   []*bool      `json:"vouchExists"`   // bool, len: [maxTx]
	VouchSiblings [][]*big.Int `json:"vouchSiblings"` // len: [maxTx][2*nLevel + 1]

	//Score, 1. Sender, 2. Reciever
	Score1         []*uint32    `json:"score1"`         // uint32, len: [maxTx]
	ScoreSiblings1 [][]*big.Int `json:"scoreSiblings1"` // len: [maxTx][nLevel + 1]

	Score2         []*uint32    `json:"score2"`         // uint32, len: [maxTx]
	ScoreSiblings2 [][]*big.Int `json:"scoreSiblings2"` // len: [maxTx][nLevel + 1]

	//
	// Intermediate States
	//

	// Intermediate States to parallelize witness computation
	// Note: the Intermediate States (IS) of the last transaction does not
	// exist. Meaning that transaction 3 (4th) will fill the parameters
	// FromIdx[3] and ISOnChain[3], but last transaction (maxTx-1) will fill
	// FromIdx[maxTx-1] but will not fill ISOnChain. That's why IS have
	// length of maxTx-1, while the other parameters have length of maxTx.
	// Last transaction does not need intermediate state since its output
	// will not be used.

	// decode-tx
	// ISOnChain indicates if tx is L1 (true (1)) or L2 (false (0))
	ISOnChain []*bool `json:"imOnChain"` // bool, len: [maxTx - 1]
	// ISOutIdx current index account for each Tx
	// Contains the index of the created account in case that the tx is of
	// account creation type.
	ISOutIdx []*uint32 `json:"imOutIdx"` // uint64 (max nLevels bits), len: [maxTx - 1]
	// rollup-tx
	// ISStateRootAccount root at the moment of the Tx (once processed), the state
	// root value once the Tx is processed into the state tree
	ISStateRootAccount []*big.Int `json:"imAccountRoot"` // Hash, len: [maxTx - 1]
	// ISStateRootVouch root at the moment of the Tx (once processed), the state
	// root value once the Tx is processed into the state tree
	ISStateRootVouch []*big.Int `json:"imVouchRoot"` // Hash, len: [maxTx - 1]
	// ISStateRootScore root at the moment of the Tx (once processed), the state
	// root value once the Tx is processed into the state tree
	ISStateRootScore []*big.Int `json:"imScoreRoot"` // Hash, len: [maxTx - 1]
	// ISExitTree root at the moment (once processed) of the Tx the value
	// once the Tx is processed into the exit tree
	ISExitRoot []*big.Int `json:"imExitRoot"` // Hash, len: [maxTx - 1]
}

// NewZKInputs returns a pointer to an initialized struct of ZKInputs
func NewZKInputs(chainID uint64, maxTx, maxL1Tx, nLevels uint32,
	currentNumBatch *uint32) *ZKInputs {
	zki := &ZKInputs{}
	// General
	zki.CurrentNumBatch = currentNumBatch
	zki.OldLastIdx = new(uint32)
	zki.OldAccountRoot = big.NewInt(0)

	zki.OldVouchRoot = big.NewInt(0)
	zki.OldScoreRoot = big.NewInt(0)

	zki.GlobalChainID = new(uint16)

	// Txs
	zki.TxCompressedData = newSlice(maxTx)
	zki.MaxNumBatch = newSliceUint32(maxTx)
	zki.FromIdx = newSliceUint32(maxTx)
	zki.AuxFromIdx = newSliceUint32(maxTx)
	zki.ToIdx = newSliceUint32(maxTx)
	zki.AuxToIdx = newSliceUint32(maxTx)
	zki.ToBJJAy = newSlice(maxTx)
	zki.ToEthAddr = newSlice(maxTx)
	zki.AmountF = newSlice(maxTx)
	zki.OnChain = make([]*bool, maxTx) //Initialize with nil value, As marking 0 OR 1 would have meant L1 OR L2 transactions
	zki.NewAccount = make([]*bool, maxTx)

	// L1
	zki.DepositAmountF = newSlice(maxTx)
	zki.FromEthAddr = newSlice(maxTx)
	zki.FromBJJCompressed = make([][256]*big.Int, maxTx)
	for i := 0; i < len(zki.FromBJJCompressed); i++ {
		// zki.FromBJJCompressed[i] = newSlice(256)
		for j := 0; j < 256; j++ {
			zki.FromBJJCompressed[i][j] = big.NewInt(0)
		}
	}

	// L2
	zki.S = newSlice(maxTx)
	zki.R8x = newSlice(maxTx)
	zki.R8y = newSlice(maxTx)

	// State MerkleTree Leafs transitions
	// zki.TokenID1 = newSlice(maxTx)
	zki.Nonce1 = newSlice(maxTx)
	zki.Sign1 = make([]*bool, maxTx)
	zki.Ay1 = newSlice(maxTx)
	zki.Balance1 = newSlice(maxTx)
	zki.EthAddr1 = newSlice(maxTx)
	zki.Siblings1 = make([][]*big.Int, maxTx)
	for i := 0; i < len(zki.Siblings1); i++ {
		zki.Siblings1[i] = newSlice(nLevels + 1)
	}
	zki.IsOld0_1 = make([]*bool, maxTx)
	zki.OldKey1 = newSlice(maxTx)
	zki.OldValue1 = newSlice(maxTx)

	// zki.TokenID2 = newSlice(maxTx)
	zki.Nonce2 = newSlice(maxTx)
	zki.Sign2 = make([]*bool, maxTx)
	zki.Ay2 = newSlice(maxTx)
	zki.Balance2 = newSlice(maxTx)
	zki.EthAddr2 = newSlice(maxTx)
	zki.Siblings2 = make([][]*big.Int, maxTx)
	for i := 0; i < len(zki.Siblings2); i++ {
		zki.Siblings2[i] = newSlice(nLevels + 1)
	}
	zki.IsOld0_2 = make([]*bool, maxTx)
	zki.OldKey2 = newSlice(maxTx)
	zki.OldValue2 = newSlice(maxTx)

	//vouch
	zki.VouchExists = make([]*bool, maxTx)
	zki.VouchSiblings = make([][]*big.Int, maxTx)
	for i := 0; i < len(zki.VouchSiblings); i++ {
		zki.VouchSiblings[i] = newSlice(2*nLevels + 1)
	}

	//score 1. Sender, 2. Reciever
	zki.Score1 = newSliceUint32(maxTx)
	zki.ScoreSiblings1 = make([][]*big.Int, maxTx)
	for i := 0; i < len(zki.ScoreSiblings1); i++ {
		zki.ScoreSiblings1[i] = newSlice(nLevels + 1)
	}
	zki.Score2 = newSliceUint32(maxTx)
	zki.ScoreSiblings2 = make([][]*big.Int, maxTx)
	for i := 0; i < len(zki.ScoreSiblings2); i++ {
		zki.ScoreSiblings2[i] = newSlice(nLevels + 1)
	}

	// Intermediate States
	zki.ISOnChain = make([]*bool, maxTx-1)
	zki.ISOutIdx = newSliceUint32(maxTx - 1)
	zki.ISStateRootAccount = newSlice(maxTx - 1)
	zki.ISStateRootVouch = newSlice(maxTx - 1)
	zki.ISStateRootScore = newSlice(maxTx - 1)
	zki.ISExitRoot = newSlice(maxTx - 1)

	return zki
}

// newSlice returns a []*big.Int slice of length n with values initialized at
// 0.
// Is used to initialize all *big.Ints of the ZKInputs data structure, so when
// the transactions are processed and the ZKInputs filled, there is no need to
// set all the elements, and if a transaction does not use a parameter, can be
// leaved as it is in the ZKInputs, as will be 0, so later when using the
// ZKInputs to generate the zkSnark proof there is no 'nil'/'null' values.
func newSlice(n uint32) []*big.Int {
	s := make([]*big.Int, n)
	for i := 0; i < len(s); i++ {
		s[i] = big.NewInt(0)
	}
	return s
}

func newSliceUint32(n uint32) []*uint32 {
	s := make([]*uint32, n)
	for i := 0; i < len(s); i++ {
		s[i] = new(uint32)
	}
	return s
}
