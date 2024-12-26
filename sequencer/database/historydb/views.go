package historydb

import (
	"math/big"
	"time"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/common/apitypes"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/iden3/go-iden3-crypto/babyjub"
)

// txWrite is an representatiion that merges common.L1Tx and common.L2Tx
// in order to perform inserts into tx table
// EffectiveAmount and EffectiveDepositAmount are not set since they have default values in the DB
type txWrite struct {
	// Generic
	IsL1             bool               `meddler:"is_l1"`
	TxID             common.TxID        `meddler:"id"`
	Type             common.TxType      `meddler:"type"`
	Position         int                `meddler:"position"`
	FromIdx          *common.AccountIdx `meddler:"from_idx"`
	EffectiveFromIdx *common.AccountIdx `meddler:"effective_from_idx"`
	ToIdx            common.AccountIdx  `meddler:"to_idx"`
	Amount           *big.Int           `meddler:"amount,bigint"`
	AmountFloat      float64            `meddler:"amount_f"`
	// TokenID          common.TokenID     `meddler:"token_id"`
	BatchNum    *common.BatchNum `meddler:"batch_num"`     // batchNum in which this tx was forged. If the tx is L2, this must be != 0
	EthBlockNum int64            `meddler:"eth_block_num"` // Ethereum Block Number in which this L1Tx was added to the queue
	// L1
	ToForgeL1TxsNum    *int64                 `meddler:"to_forge_l1_txs_num"` // toForgeL1TxsNum in which the tx was forged / will be forged
	UserOrigin         *bool                  `meddler:"user_origin"`         // true if the tx was originated by a user, false if it was aoriginated by a coordinator. Note that this differ from the spec for implementation simplification purpposes
	FromEthAddr        *ethCommon.Address     `meddler:"from_eth_addr"`
	FromBJJ            *babyjub.PublicKeyComp `meddler:"from_bjj"`
	DepositAmount      *big.Int               `meddler:"deposit_amount,bigintnull"`
	DepositAmountFloat *float64               `meddler:"deposit_amount_f"`
	EthTxHash          *ethCommon.Hash        `meddler:"eth_tx_hash"`
	L1Fee              *big.Int               `meddler:"l1_fee,bigintnull"`
	// L2
	// Fee   *common.FeeSelector `meddler:"fee"`
	Nonce *common.Nonce       `meddler:"nonce"`
}

// TokenWithUSD add USD info to common.Token
type TokenWithUSD struct {
	ItemID      uint64            `json:"itemId" meddler:"item_id"`
	TokenID     common.TokenID    `json:"id" meddler:"token_id"`
	EthBlockNum int64             `json:"ethereumBlockNum" meddler:"eth_block_num"` // Ethereum block number in which this token was registered
	EthAddr     ethCommon.Address `json:"ethereumAddress" meddler:"eth_addr"`
	Name        string            `json:"name" meddler:"name"`
	Symbol      string            `json:"symbol" meddler:"symbol"`
	Decimals    uint64            `json:"decimals" meddler:"decimals"`
	USD         *float64          `json:"USD" meddler:"usd"`
	USDUpdate   *time.Time        `json:"fiatUpdate" meddler:"usd_update,utctime"`
	TotalItems  uint64            `json:"-" meddler:"total_items"`
	FirstItem   uint64            `json:"-" meddler:"first_item"`
	LastItem    uint64            `json:"-" meddler:"last_item"`
}

// MetricsAPI define metrics of the network
type MetricsAPI struct {
	TransactionsPerBatch   float64 `json:"transactionsPerBatch"`
	BatchFrequency         float64 `json:"batchFrequency"`
	TransactionsPerSecond  float64 `json:"transactionsPerSecond"`
	TokenAccounts          int64   `json:"tokenAccounts"`
	Wallets                int64   `json:"wallets"`
	AvgTransactionFee      float64 `json:"avgTransactionFee"`
	EstimatedTimeToForgeL1 float64 `json:"estimatedTimeToForgeL1" meddler:"estimated_time_to_forge_l1"`
}

// BucketUpdateAPI are the bucket updates (tracking the withdrawals value changes)
// in Rollup Smart Contract
type BucketUpdateAPI struct {
	EthBlockNum int64               `json:"ethereumBlockNum" meddler:"eth_block_num"`
	NumBucket   int                 `json:"numBucket" meddler:"num_bucket"`
	BlockStamp  int64               `json:"blockStamp" meddler:"block_stamp"`
	Withdrawals *apitypes.BigIntStr `json:"withdrawals" meddler:"withdrawals"`
}

// BucketParamsAPI are the parameter variables of each Bucket of Rollup Smart
// Contract
type BucketParamsAPI struct {
	CeilUSD         *apitypes.BigIntStr `json:"ceilUSD"`
	BlockStamp      *apitypes.BigIntStr `json:"blockStamp"`
	Withdrawals     *apitypes.BigIntStr `json:"withdrawals"`
	RateBlocks      *apitypes.BigIntStr `json:"rateBlocks"`
	RateWithdrawals *apitypes.BigIntStr `json:"rateWithdrawals"`
	MaxWithdrawals  *apitypes.BigIntStr `json:"maxWithdrawals"`
}

// RollupVariablesAPI are the variables of the Rollup Smart Contract
type RollupVariablesAPI struct {
	EthBlockNum int64 `json:"ethereumBlockNum" meddler:"eth_block_num"`
	// FeeAddToken           *apitypes.BigIntStr `json:"feeAddToken" meddler:"fee_add_token" validate:"required"`
	ForgeL1L2BatchTimeout int64 `json:"forgeL1L2BatchTimeout" meddler:"forge_l1_timeout" validate:"required"`
	// WithdrawalDelay       uint64              `json:"withdrawalDelay" meddler:"withdrawal_delay" validate:"required"`
	Buckets  []BucketParamsAPI `json:"buckets" meddler:"buckets,json"`
	SafeMode bool              `json:"safeMode" meddler:"safe_mode"`
}

// CoordinatorAPI is a representation of a coordinator with additional information
// required by the API
type CoordinatorAPI struct {
	ItemID      uint64            `json:"itemId" meddler:"item_id"`
	Bidder      ethCommon.Address `json:"bidderAddr" meddler:"bidder_addr"`
	Forger      ethCommon.Address `json:"forgerAddr" meddler:"forger_addr"`
	EthBlockNum int64             `json:"ethereumBlock" meddler:"eth_block_num"`
	URL         string            `json:"URL" meddler:"url"`
	TotalItems  uint64            `json:"-" meddler:"total_items"`
	FirstItem   uint64            `json:"-" meddler:"first_item"`
	LastItem    uint64            `json:"-" meddler:"last_item"`
}

// BatchAPI is a representation of a batch with additional information
// required by the API, and extracted by joining block table
type BatchAPI struct {
	ItemID           uint64                      `json:"itemId" meddler:"item_id"`
	BatchNum         common.BatchNum             `json:"batchNum" meddler:"batch_num"`
	EthereumTxHash   ethCommon.Hash              `json:"ethereumTxHash" meddler:"eth_tx_hash"`
	EthBlockNum      int64                       `json:"ethereumBlockNum" meddler:"eth_block_num"`
	EthBlockHash     ethCommon.Hash              `json:"ethereumBlockHash" meddler:"hash"`
	Timestamp        time.Time                   `json:"timestamp" meddler:"timestamp,utctime"`
	ForgerAddr       ethCommon.Address           `json:"forgerAddr" meddler:"forger_addr"`
	CollectedFeesDB  map[common.TokenID]*big.Int `json:"-" meddler:"fees_collected,json"`
	CollectedFeesAPI apitypes.CollectedFeesAPI   `json:"collectedFees" meddler:"-"`
	TotalFeesUSD     *float64                    `json:"historicTotalCollectedFeesUSD" meddler:"total_fees_usd"`
	AccountRoot      apitypes.BigIntStr          `json:"accountRoot" meddler:"account_root"`
	VouchRoot        apitypes.BigIntStr          `json:"vouchRoot" meddler:"vouch_root"`
	ScoreRoot        apitypes.BigIntStr          `json:"scoreRoot" meddler:"score_root"`
	NumAccounts      int                         `json:"numAccounts" meddler:"num_accounts"`
	ExitRoot         apitypes.BigIntStr          `json:"exitRoot" meddler:"exit_root"`
	ForgeL1TxsNum    *int64                      `json:"forgeL1TransactionsNum" meddler:"forge_l1_txs_num"`
	SlotNum          int64                       `json:"slotNum" meddler:"slot_num"`
	ForgedTxs        int                         `json:"forgedTransactions" meddler:"forged_txs"`
	TotalItems       uint64                      `json:"-" meddler:"total_items"`
	FirstItem        uint64                      `json:"-" meddler:"first_item"`
	LastItem         uint64                      `json:"-" meddler:"last_item"`
}

// NewRollupVariablesAPI creates a RollupVariablesAPI from common.RollupVariables
func NewRollupVariablesAPI(rollupVariables *common.RollupVariables) *RollupVariablesAPI {
	buckets := make([]BucketParamsAPI, len(rollupVariables.Buckets))
	rollupVars := RollupVariablesAPI{
		EthBlockNum: rollupVariables.EthBlockNum,
		// FeeAddToken:           apitypes.NewBigIntStr(rollupVariables.FeeAddToken),
		ForgeL1L2BatchTimeout: rollupVariables.ForgeL1L2BatchTimeout,
		// WithdrawalDelay:       rollupVariables.WithdrawalDelay,
		SafeMode: rollupVariables.SafeMode,
		Buckets:  buckets,
	}
	for i, bucket := range rollupVariables.Buckets {
		rollupVars.Buckets[i] = BucketParamsAPI{
			CeilUSD:         apitypes.NewBigIntStr(bucket.CeilUSD),
			BlockStamp:      apitypes.NewBigIntStr(bucket.BlockStamp),
			Withdrawals:     apitypes.NewBigIntStr(bucket.Withdrawals),
			RateBlocks:      apitypes.NewBigIntStr(bucket.RateBlocks),
			RateWithdrawals: apitypes.NewBigIntStr(bucket.RateWithdrawals),
			MaxWithdrawals:  apitypes.NewBigIntStr(bucket.MaxWithdrawals),
		}
	}
	return &rollupVars
}
