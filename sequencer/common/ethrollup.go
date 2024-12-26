package common

import (
	"fmt"
	"math/big"

	ethCommon "github.com/ethereum/go-ethereum/common"
)

const (
	// RollupConstMaxFeeIdxCoordinator is the maximum number of tokens the
	// coordinator can use to collect fees (determines the number of tokens
	// that the coordinator can collect fees from).  This value is
	// determined by the circuit.
	// RollupConstMaxFeeIdxCoordinator = 64
	// RollupConstReservedIDx First 256 indexes reserved, first user index will be the 256
	RollupConstReservedIDx = 255
	// RollupConstExitIDx IDX 1 is reserved for exits
	RollupConstExitIDx = 1
	// RollupConstLimitTokens Max number of tokens allowed to be registered inside the rollup
	RollupConstLimitTokens = (1 << 32) //nolint:gomnd
	// RollupConstL1CoordinatorTotalBytes [4 bytes] token + [32 bytes] babyjub + [65 bytes]
	// compressedSignature
	RollupConstL1CoordinatorTotalBytes = 101
	// RollupConstL1UserTotalBytes [20 bytes] fromEthAddr + [32 bytes] fromBjj-compressed + [6
	// bytes] fromIdx + [5 bytes] depositAmountFloat40 + [5 bytes] amountFloat40 + [4 bytes]
	// tokenId + [6 bytes] toIdx
	RollupConstL1UserTotalBytes = 78
	// RollupConstMaxL1UserTx Maximum L1-user transactions allowed to be queued in a batch
	RollupConstMaxL1UserTx = 128
	// RollupConstMaxL1Tx Maximum L1 transactions allowed to be queued in a batch
	RollupConstMaxL1Tx = 256
	// RollupConstInputSHAConstantBytes [6 bytes] lastIdx + [6 bytes] newLastIdx  + [32 bytes]
	// stateRoot  + [32 bytes] newStRoot  + [32 bytes] newExitRoot + [_MAX_L1_TX *
	// _L1_USER_TOTALBYTES bytes] l1TxsData + totalL2TxsDataLength + feeIdxCoordinatorLength +
	// [2 bytes] chainID = 18542 bytes +  totalL2TxsDataLength + feeIdxCoordinatorLength
	RollupConstInputSHAConstantBytes = 18546
	// RollupConstMaxWithdrawalDelay max withdrawal delay in seconds
	RollupConstMaxWithdrawalDelay = 2 * 7 * 24 * 60 * 60
	// RollupConstExchangeMultiplier exchange multiplier
	RollupConstExchangeMultiplier = 1e14
)

// TODO: Check and Set the following
var (
	// RollupConstLimitDepositAmount Max deposit amount allowed (depositAmount: L1 --> L2)
	RollupConstLimitDepositAmount, _ = new(big.Int).SetString(
		"", 10)
	// RollupConstLimitL2TransferAmount Max amount allowed (amount L2 --> L2)
	RollupConstLimitL2TransferAmount, _ = new(big.Int).SetString(
		"", 10)
	// RollupConstEthAddressInternalOnly This ethereum address is used internally for rollup
	// accounts that don't have ethereum address, only Babyjubjub.
	// This non-ethereum accounts can be created by the coordinator and allow users to have a
	// rollup account without needing an ethereum address
	RollupConstEthAddressInternalOnly = ethCommon.HexToAddress(
		"0xFFfFfFffFFfffFFfFFfFFFFFffFFFffffFfFFFfF")
)

// BucketParams are the parameter variables of each Bucket of Rollup Smart
// Contract
type BucketParams struct {
	CeilUSD         *big.Int
	BlockStamp      *big.Int
	Withdrawals     *big.Int
	RateBlocks      *big.Int
	RateWithdrawals *big.Int
	MaxWithdrawals  *big.Int
}

// BucketUpdate are the bucket updates (tracking the withdrawals value changes)
// in Rollup Smart Contract
type BucketUpdate struct {
	EthBlockNum int64    `meddler:"eth_block_num"`
	NumBucket   int      `meddler:"num_bucket"`
	BlockStamp  int64    `meddler:"block_stamp"`
	Withdrawals *big.Int `meddler:"withdrawals,bigint"`
}

// RollupVariables are the variables of the Rollup Smart Contract
type RollupVariables struct {
	EthBlockNum           int64          `meddler:"eth_block_num"`
	ForgeL1L2BatchTimeout int64          `meddler:"forge_l1_timeout" validate:"required"`
	Buckets               []BucketParams `meddler:"buckets,json"`
	SafeMode              bool           `meddler:"safe_mode"`
}

// RollupVerifierStruct is the information about verifiers of the Rollup Smart Contract
type RollupVerifierStruct struct {
	MaxTx   int64 `json:"maxTx"`
	NLevels int64 `json:"nlevels"`
}

// RollupConstants are the constants of the Rollup Smart Contract
type RollupConstants struct {
	AbsoluteMaxL1L2BatchTimeout int64                  `json:"absoluteMaxL1L2BatchTimeout"`
	Verifiers                   []RollupVerifierStruct `json:"verifiers"`
	TokamakGovernanceAddress    ethCommon.Address      `json:"tokamakGovernanceAddress"`
	// First block where the first slot begins
	GenesisBlockNum int64 `json:"genesisBlockNum"`
}

// FindVerifierIdx tries to find a matching verifier in the RollupConstants and
// returns its index
func (c *RollupConstants) FindVerifierIdx(MaxTx, NLevels int64) (int, error) {
	for i, verifier := range c.Verifiers {
		if verifier.MaxTx == MaxTx && verifier.NLevels == NLevels {
			return i, nil
		}
	}
	return 0, Wrap(fmt.Errorf("verifier not found for MaxTx: %v, NLevels: %v",
		MaxTx, NLevels))
}

// Copy returns a deep copy of the Variables
func (v *RollupVariables) Copy() *RollupVariables {
	vCpy := *v
	return &vCpy
}
