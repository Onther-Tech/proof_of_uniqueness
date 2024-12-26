package common

import (
	"encoding/binary"
	"fmt"
	"math/big"

	ethCommon "github.com/ethereum/go-ethereum/common"
)

const batchNumBytesLen = 8 //TODO: Need to check if this needs to be updated

// Batch is a struct that represents Hermez network batch
type Batch struct {
	BatchNum  BatchNum       `meddler:"batch_num"`
	EthTxHash ethCommon.Hash `meddler:"eth_tx_hash"`
	// Ethereum block in which the batch is forged
	EthBlockNum int64             `meddler:"eth_block_num"`
	ForgerAddr  ethCommon.Address `meddler:"forger_addr"`

	AccountRoot *big.Int `meddler:"account_root,bigint"`
	VouchRoot   *big.Int `meddler:"vouch_root,bigint"`
	ScoreRoot   *big.Int `meddler:"score_root,bigint"`
	ExitRoot    *big.Int `meddler:"exit_root,bigint"`

	NumAccounts   int      `meddler:"num_accounts"`
	LastIdx       int64    `meddler:"last_idx"`
	GasUsed       uint64   `meddler:"gas_used"`
	GasPrice      *big.Int `meddler:"gas_price,bigint"`
	EtherPriceUSD float64  `meddler:"ether_price_usd"`
	// ForgeL1TxsNum is optional, Only when the batch forges L1 txs. Identifier that corresponds
	// to the group of L1 txs forged in the current batch.
	ForgeL1TxsNum *int64   `meddler:"forge_l1_txs_num"`
	SlotNum       int64    `meddler:"slot_num"` // Slot in which the batch is forged
	TotalFeesUSD  *float64 `meddler:"total_fees_usd"`
}

type BatchNum uint32

// Bytes returns a byte array of length 4 representing the BatchNum
func (bn BatchNum) Bytes() []byte {
	var batchNumBytes [batchNumBytesLen]byte
	binary.BigEndian.PutUint32(batchNumBytes[:], uint32(bn))
	return batchNumBytes[:]
}

// BatchNumFromBytes returns BatchNum from a []byte
func BatchNumFromBytes(b []byte) (BatchNum, error) {
	if len(b) != batchNumBytesLen {
		return 0,
			Wrap(fmt.Errorf("can not parse BatchNumFromBytes, bytes len %d, expected %d",
				len(b), batchNumBytesLen))
	}
	batchNum := binary.BigEndian.Uint32(b[:batchNumBytesLen])
	return BatchNum(batchNum), nil
}

// BigInt returns a *big.Int representing the BatchNum
func (bn BatchNum) BigInt() *big.Int {
	return big.NewInt(int64(bn))
}

// BatchData contains the information of a Batch
type BatchData struct {
	L1Batch bool
	// L1UserTxs that were forged in the batch
	L1UserTxs []L1Tx
	CreatedAccounts []Account
	UpdatedAccounts []AccountUpdate
	ExitTree        []ExitInfo
	Batch           Batch
}

// NewBatchData creates an empty BatchData with the slices initialized.
func NewBatchData() *BatchData {
	return &BatchData{
		L1Batch:   false,
		L1UserTxs: make([]L1Tx, 0),
		CreatedAccounts: make([]Account, 0),
		ExitTree:        make([]ExitInfo, 0),
		Batch:           Batch{},
	}
}
