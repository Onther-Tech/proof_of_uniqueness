package config

import (
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"
	"tokamak-sybil-resistance/api/stateapiupdater"
	"tokamak-sybil-resistance/common"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/go-playground/validator"
	"github.com/iden3/go-iden3-crypto/babyjub"
)

// Duration is a wrapper type that parses time duration from text.
type Duration struct {
	time.Duration `validate:"required" toml:"timeout"`
}

// ForgeBatchGasCost is the costs associated to a ForgeBatch transaction, split
// into different parts to be used in a formula.
type ForgeBatchGasCost struct {
	Fixed     uint64 `validate:"required" env:"TONNODE_FORGEBATCHGASCOST_FIXED"`
	L1UserTx  uint64 `validate:"required" env:"TONNODE_FORGEBATCHGASCOST_L1USERTX"`
	L1CoordTx uint64 `validate:"required" env:"TONNODE_FORGEBATCHGASCOST_L1COORDTX"`
	L2Tx      uint64 `validate:"required" env:"TONNODE_FORGEBATCHGASCOST_L2TX"`
}

// CoordinatorAPI specifies the configuration parameters of the API in mode
// coordinator
type CoordinatorAPI struct {
	// Coordinator enables the coordinator API endpoints
	Coordinator bool `env:"TONNODE_COORDINATORAPI_COORDINATOR"`
}

// Coordinator is the coordinator specific configuration.
type Coordinator struct {
	// ForgerAddress is the address under which this coordinator is forging
	ForgerAddress ethCommon.Address `validate:"required" env:"TONNODE_COORDINATOR_FORGERADDRESS"`
	// MinimumForgeAddressBalance is the minimum balance the forger address
	// needs to start the coordinator in wei. Of set to 0, the coordinator
	// will not check the balance before starting.
	MinimumForgeAddressBalance *big.Int `env:"TONNODE_COORDINATOR_MINIMUMFORGEADDRESSBALANCE"`
	// FeeAccount is the Hermez account that the coordinator uses to receive fees
	FeeAccount struct {
		// Address is the ethereum address of the account to receive fees
		Address ethCommon.Address `validate:"required" env:"TONNODE_FEEACCOUNT_ADDRESS"`
		// BJJ is the baby jub jub public key of the account to receive fees
		BJJ babyjub.PublicKeyComp `validate:"required" env:"TONNODE_FEEACCOUNT_BJJ"`
	} `validate:"required"`
	// ConfirmBlocks is the number of confirmation blocks to wait for sent
	// ethereum transactions before forgetting about them
	ConfirmBlocks int64 `validate:"required,gte=0" env:"TONNODE_COORDINATOR_CONFIRMBLOCKS"`
	// L1BatchTimeoutPerc is the portion of the range before the L1Batch
	// timeout that will trigger a schedule to forge an L1Batch
	L1BatchTimeoutPerc float64 `validate:"required,lte=1.0,gte=0.0" env:"TONNODE_COORDINATOR_L1BATCHTIMEOUTPERC"`
	// StartSlotBlocksDelay is the number of blocks of delay to wait before
	// starting the pipeline when we reach a slot in which we can forge.
	StartSlotBlocksDelay int64 `validate:"gte=0" env:"TONNODE_COORDINATOR_STARTSLOTBLOCKSDELAY"`
	// ScheduleBatchBlocksAheadCheck is the number of blocks ahead in which
	// the forger address is checked to be allowed to forge (apart from
	// checking the next block), used to decide when to stop scheduling new
	// batches (by stopping the pipeline).
	// For example, if we are at block 10 and ScheduleBatchBlocksAheadCheck
	// is 5, even though at block 11 we canForge, the pipeline will be
	// stopped if we can't forge at block 15.
	// This value should be the expected number of blocks it takes between
	// scheduling a batch and having it mined.
	ScheduleBatchBlocksAheadCheck int64 `validate:"gte=0" env:"TONNODE_COORDINATOR_SCHEDULEBATCHBLOCKSAHEADCHECK"`
	// SendBatchBlocksMarginCheck is the number of margin blocks ahead in
	// which the coordinator is also checked to be allowed to forge, apart
	// from the next block; used to decide when to stop sending batches to
	// the smart contract.
	// For example, if we are at block 10 and SendBatchBlocksMarginCheck is
	// 5, even though at block 11 we canForge, the batch will be discarded
	// if we can't forge at block 15.
	SendBatchBlocksMarginCheck int64 `validate:"gte=0" env:"TONNODE_COORDINATOR_SENDBATCHBLOCKSMARGINCHECK"`
	// ProofServerPollInterval is the waiting interval between polling the
	// ProofServer while waiting for a particular status
	ProofServerPollInterval Duration `validate:"required" env:"TONNODE_COORDINATOR_PROOFSERVERPOLLINTERVAL"`
	// ForgeRetryInterval is the waiting interval between calls forge a
	// batch after an error
	ForgeRetryInterval Duration `validate:"required" env:"TONNODE_COORDINATOR_FORGERETRYINTERVAL"`
	// ForgeDelay is the delay after which a batch is forged if the slot is
	// already committed.  If set to 0s, the coordinator will continuously
	// forge at the maximum rate.
	ForgeDelay Duration `validate:"-" env:"TONNODE_COORDINATOR_FORGEDELAY"`
	// ForgeNoTxsDelay is the delay after which a batch is forged even if
	// there are no txs to forge if the slot is already committed.  If set
	// to 0s, the coordinator will continuously forge even if the batches
	// are empty.
	ForgeNoTxsDelay Duration `validate:"-" env:"TONNODE_COORDINATOR_FORGENOTXSDELAY"`
	// MustForgeAtSlotDeadline enables the coordinator to forge slots if
	// the empty slots reach the slot deadline.
	MustForgeAtSlotDeadline bool `env:"TONNODE_COORDINATOR_MUSTFORGEATSLOTDEADLINE"`
	// IgnoreSlotCommitment disables forcing the coordinator to forge a
	// slot immediately when the slot is not committed. If set to false,
	// the coordinator will immediately forge a batch at the beginning of a
	// slot if it's the slot winner.
	IgnoreSlotCommitment bool `env:"TONNODE_COORDINATOR_IGNORESLOTCOMMITMENT"`
	// ForgeOncePerSlotIfTxs will make the coordinator forge at most one
	// batch per slot, only if there are included txs in that batch, or
	// pending l1UserTxs in the smart contract.  Setting this parameter
	// overrides `ForgeDelay`, `ForgeNoTxsDelay`, `MustForgeAtSlotDeadline`
	// and `IgnoreSlotCommitment`.
	ForgeOncePerSlotIfTxs bool `env:"TONNODE_COORDINATOR_FORGEONCEPERSLOTIFTXS"`
	// SyncRetryInterval is the waiting interval between calls to the main
	// handler of a synced block after an error
	SyncRetryInterval Duration `validate:"required" env:"TONNODE_COORDINATOR_SYNCRETRYINTERVAL"`
	// ProverWaitReadTimeout
	ProverWaitReadTimeout Duration `env:"TONNODE_COORDINATOR_PROVERWAITREADTIMEOUT"`
	L2DB                  struct {
		// SafetyPeriod is the number of batches after which
		// non-pending L2Txs are deleted from the pool
		SafetyPeriod common.BatchNum `validate:"required" env:"TONNODE_L2DB_SAFETYPERIOD"`
		// MaxTxs is the maximum number of pending L2Txs that can be
		// stored in the pool.  Once this number of pending L2Txs is
		// reached, inserts to the pool will be denied until some of
		// the pending txs are forged.
		MaxTxs uint32 `validate:"required" env:"TONNODE_L2DB_MAXTXS"`
		// MinFeeUSD is the minimum fee in USD that a tx must pay in
		// order to be accepted into the pool.  Txs with lower than
		// minimum fee will be rejected at the API level.
		MinFeeUSD float64 `validate:"gte=0" env:"TONNODE_L2DB_MINFEEUSD"`
		// MaxFeeUSD is the maximum fee in USD that a tx must pay in
		// order to be accepted into the pool.  Txs with greater than
		// maximum fee will be rejected at the API level.
		MaxFeeUSD float64 `validate:"required,gte=0" env:"TONNODE_L2DB_MAXFEEUSD"`
		// TTL is the Time To Live for L2Txs in the pool. L2Txs older
		// than TTL will be deleted.
		TTL Duration `validate:"required" env:"TONNODE_L2DB_TTL"`
		// PurgeBatchDelay is the delay between batches to purge
		// outdated transactions. Outdated L2Txs are those that have
		// been forged or marked as invalid for longer than the
		// SafetyPeriod and pending L2Txs that have been in the pool
		// for longer than TTL once there are MaxTxs.
		PurgeBatchDelay int64 `validate:"required,gte=0" env:"TONNODE_L2DB_PURGEBATCHDELAY"`
		// InvalidateBatchDelay is the delay between batches to mark
		// invalid transactions due to nonce lower than the account
		// nonce.
		InvalidateBatchDelay int64 `validate:"required" env:"TONNODE_L2DB_INVALIDATEBATCHDELAY"`
		// PurgeBlockDelay is the delay between blocks to purge
		// outdated transactions. Outdated L2Txs are those that have
		// been forged or marked as invalid for longer than the
		// SafetyPeriod and pending L2Txs that have been in the pool
		// for longer than TTL once there are MaxTxs.
		PurgeBlockDelay int64 `validate:"required,gte=0" env:"TONNODE_L2DB_PURGEBLOCKDELAY"`
		// InvalidateBlockDelay is the delay between blocks to mark
		// invalid transactions due to nonce lower than the account
		// nonce.
		InvalidateBlockDelay int64 `validate:"required,gte=0" env:"TONNODE_L2DB_INVALIDATEBLOCKDELAY"`
	} `validate:"required"`
	TxSelector struct {
		// Path where the TxSelector StateDB is stored
		Path string `validate:"required" env:"TONNODE_TXSELECTOR_PATH"`
	} `validate:"required"`
	BatchBuilder struct {
		// Path where the BatchBuilder StateDB is stored
		Path string `validate:"required" env:"TONNODE_BATCHBUILDER_PATH"`
	} `validate:"required"`
	ServerProofs struct {
		URLs []string `validate:"required" env:"TONNODE_SERVERPROOF_URLS" envSeparator:","`
	} `validate:"required"`
	Circuit struct {
		// MaxTx is the maximum number of txs supported by the circuit
		MaxTx int64 `validate:"required,gte=0" env:"TONNODE_CIRCUIT_MAXTX"`
		// NLevels is the maximum number of merkle tree levels
		// supported by the circuit
		NLevels int64 `validate:"required,gte=0" env:"TONNODE_CIRCUIT_NLEVELS"`
	} `validate:"required"`
	EthClient struct {
		// MaxGasPrice is the maximum gas price allowed for ethereum
		// transactions
		MaxGasPrice int64 `validate:"required" env:"TONNODE_ETHCLIENT_MAXGASPRICE"`
		// MinGasPrice is the minimum gas price in gwei allowed for ethereum
		MinGasPrice int64 `validate:"required" env:"TONNODE_ETHCLIENT_MINGASPRICE"`
		// GasPriceIncPerc is the percentage increase of gas price set
		// in an ethereum transaction from the suggested gas price by
		// the ethereum node
		GasPriceIncPerc int64 `validate:"gte=0" env:"TONNODE_ETHCLIENT_GASPRICEINCPERC"`
		// CheckLoopInterval is the waiting interval between receipt
		// checks of ethereum transactions in the TxManager
		CheckLoopInterval Duration `validate:"required" env:"TONNODE_ETHCLIENT_CHECKLOOPINTERVAL"`
		// Attempts is the number of attempts to do an eth client RPC
		// call before giving up
		Attempts int `validate:"required,gte=1" env:"TONNODE_ETHCLIENT_ATTEMPTS"`
		// AttemptsDelay is delay between attempts do do an eth client
		// RPC call
		AttemptsDelay Duration `validate:"required" env:"TONNODE_ETHCLIENT_ATTEMPTSDELAY"`
		// TxResendTimeout is the timeout after which a non-mined
		// ethereum transaction will be resent (reusing the nonce) with
		// a newly calculated gas price
		TxResendTimeout Duration `validate:"required" env:"TONNODE_ETHCLIENT_TXRESENDTIMEOUT"`
		// NoReuseNonce disables reusing nonces of pending transactions for
		// new replacement transactions
		NoReuseNonce bool `env:"TONNODE_ETHCLIENT_NOREUSENONCE"`
		// Keystore is the ethereum keystore where private keys are kept
		Keystore struct {
			// Path to the keystore
			Path string `validate:"required" env:"TONNODE_KEYSTORE_PATH"`
			// Password used to decrypt the keys in the keystore
			Password string `validate:"required" env:"TONNODE_KEYSTORE_PASSWORD"`
		} `validate:"required"`
		// ForgeBatchGasCost contains the cost of each action in the
		// ForgeBatch transaction.
		ForgeBatchGasCost ForgeBatchGasCost `validate:"required"`
	} `validate:"required"`
	API   CoordinatorAPI `validate:"required"`
	Debug struct {
		// BatchPath if set, specifies the path where batchInfo is stored
		// in JSON in every step/update of the pipeline
		BatchPath string `env:"TONNODE_COORDINATORDEBUG_BATCHPATH"`
		// LightScrypt if set, uses light parameters for the ethereum
		// keystore encryption algorithm.
		LightScrypt bool `env:"TONNODE_COORDINATORDEBUG_LIGHTSCRYPT"`
		// RollupVerifierIndex is the index of the verifier to use in
		// the Rollup smart contract.  The verifier chosen by index
		// must match with the Circuit parameters.
		RollupVerifierIndex *int
	}
	Etherscan struct {
		// URL if set, specifies the etherscan endpoint to get
		// the gas estimations for that moment.
		URL string `env:"TONNODE_ETHERSCAN_URL"`
		// APIKey allow access to etherscan services
		APIKey string `env:"TONNODE_ETHERSCAN_APIKEY"`
	}
}

// PostgreSQL is the postgreSQL configuration parameters.  It's possible to use
// differentiated SQL connections for read/write.  If the read configuration is
// not provided, the write one it's going to be used for both reads and writes
type PostgreSQL struct {
	// Port of the PostgreSQL write server
	PortWrite int `validate:"required" env:"TONNODE_POSTGRESQL_PORTWRITE"`
	// Host of the PostgreSQL write server
	HostWrite string `validate:"required" env:"TONNODE_POSTGRESQL_HOSTWRITE"`
	// User of the PostgreSQL write server
	UserWrite string `validate:"required" env:"TONNODE_POSTGRESQL_USERWRITE"`
	// Password of the PostgreSQL write server
	PasswordWrite string `validate:"required" env:"TONNODE_POSTGRESQL_PASSWORDWRITE"`
	// Name of the PostgreSQL write server database
	NameWrite string `validate:"required" env:"TONNODE_POSTGRESQL_NAMEWRITE"`
	// Port of the PostgreSQL read server
	PortRead int `env:"TONNODE_POSTGRESQL_PORTREAD"`
	// Host of the PostgreSQL read server
	HostRead string `validate:"nefield=HostWrite" env:"TONNODE_POSTGRESQL_HOSTREAD"`
	// User of the PostgreSQL read server
	UserRead string `env:"TONNODE_POSTGRESQL_USERREAD"`
	// Password of the PostgreSQL read server
	PasswordRead string `env:"TONNODE_POSTGRESQL_PASSWORDREAD"`
	// Name of the PostgreSQL read server database
	NameRead string `env:"TONNODE_POSTGRESQL_NAMEREAD"`
}

// NodeDebug specifies debug configuration parameters
type NodeDebug struct {
	// APIAddress is the address where the debugAPI will listen if
	// set
	APIAddress string `env:"TONNODE_DEBUG_APIADDRESS"`
	// MeddlerLogs enables meddler debug mode, where unused columns and struct
	// fields will be logged
	MeddlerLogs bool `env:"TONNODE_DEBUG_MEDDLERLOGS"`
	// GinDebugMode sets Gin-Gonic (the web framework) to run in
	// debug mode
	GinDebugMode bool `env:"TONNODE_DEBUG_GINDEBUGMODE"`
}

// Node is the hermez node configuration.
type Node struct {
	StateDB struct {
		// Path where the synchronizer StateDB is stored
		Path string `validate:"required" env:"TONNODE_STATEDB_PATH"`
		// Keep is the number of checkpoints to keep
		Keep int `validate:"required,gte=128" env:"TONNODE_STATEDB_KEEP"`
	} `validate:"required"`
	PostgreSQL PostgreSQL `validate:"required"`
	Web3       struct {
		// URL is the URL of the web3 ethereum-node RPC server.  Only
		// geth is officially supported.
		URL string `validate:"required,url" env:"TONNODE_WEB3_URL"`
	} `validate:"required"`
	Synchronizer struct {
		// SyncLoopInterval is the interval between attempts to
		// synchronize a new block from an ethereum node
		SyncLoopInterval Duration `validate:"required" env:"TONNODE_SYNCHRONIZER_SYNCLOOPINTERVAL"`
		// StatsUpdateBlockNumDiffThreshold is a threshold of a number of
		// Ethereum blocks left to synchronize, such that if there are more
		// blocks to sync than the defined value synchronizer can aggressively
		// skip calling UpdateEth to save network bandwidth and time.
		// After reaching the threshold UpdateEth is called on each block.
		// This value only affects the reported % of synchronization of
		// blocks and batches, nothing else.
		StatsUpdateBlockNumDiffThreshold uint16 `validate:"required,gt=32" env:"TONNODE_SYNCHRONIZER_STATSUPDATEBLOCKSNUMDIFFTHRESHOLD"`
		// StatsUpdateFrequencyDivider - While having more blocks to sync than
		// updateEthBlockNumThreshold, UpdateEth will be called once in a
		// defined number of blocks. This value only affects the reported % of
		// synchronization of blocks and batches, nothing else.
		StatsUpdateFrequencyDivider uint16 `validate:"required,gt=1" env:"TONNODE_SYNCHRONIZER_STATSUPDATEFREQUENCYDIVIDER"`
	} `validate:"required"`
	SmartContracts struct {
		// Rollup is the address of the Hermez.sol smart contract
		Rollup ethCommon.Address `validate:"required" env:"TONNODE_SMARTCONTRACTS_ROLLUP"`
	} `validate:"required"`
	API                  APIConfigParameters                  `validate:"required"`
	RecommendedFeePolicy stateapiupdater.RecommendedFeePolicy `validate:"required"`
	Debug                NodeDebug                            `validate:"required"`
	Coordinator          Coordinator                          `validate:"-"`
	Log                  LogConf                              `validate:"-"`
}

// APIConfigParameters specifies the configuration parameters of the API
type APIConfigParameters struct {
	// Address where the API will listen if set
	Address string `env:"TONNODE_API_ADDRESS"`
	// Explorer enables the Explorer API endpoints
	Explorer bool `env:"TONNODE_API_EXPLORER"`
	// ReadTimeout is the maximum duration for reading the entire
	// request, including the body.
	Readtimeout Duration `env:"TONNODE_API_READTIMEOUT"`
	// WriteTimeout is the maximum duration before timing out
	// writes of the response.
	Writetimeout Duration `env:"TONNODE_API_WRITETIMEOUT"`
	// Maximum concurrent connections allowed between API and SQL
	MaxSQLConnections int `validate:"required,gte=1" env:"TONNODE_API_MAXSQLCONNECTIONS"`
	// SQLConnectionTimeout is the maximum amount of time that an API request
	// can wait to establish a SQL connection
	SQLConnectionTimeout Duration `env:"TONNODE_API_SQLCONNECTIONTIMEOUT"`
	// UpdateMetricsInterval is the interval between updates of the metrics
	UpdateMetricsInterval Duration `validate:"required" env:"TONNODE_API_UPDATEMETRICSINTERVAL"`
	// UpdateRecommendedFeeInterval is the interval between updates of the recommended fees
	UpdateRecommendedFeeInterval Duration `validate:"required" env:"TONNODE_API_UPDATERECOMMENDEDFEEINTERVAL"`
	// FindPeersCoordinatorNetworkInterval time elapsed between peer discovery process for the coordinators p2p network
	FindPeersCoordinatorNetworkInterval Duration `env:"TONNODE_API_COORDINATORNETWORK_FINDPEERSINTERVAL"`
}

// APIServer is the api server configuration parameters
type APIServer struct {
	// NodeAPI specifies the configuration parameters of the API
	Log         LogConf             `validate:"-"`
	API         APIConfigParameters `validate:"required"`
	PostgreSQL  PostgreSQL          `validate:"required"`
	Coordinator struct {
		// ForgerAddress is the address under which this coordinator is forging
		ForgerAddress ethCommon.Address `validate:"required" env:"TONNODE_COORDINATOR_FORGERADDRESS"`
		API           struct {
			// Coordinator enables the coordinator API endpoints
			Coordinator bool `env:"TONNODE_COORDINATORAPI_COORDINATOR"`
		} `validate:"required"`
		L2DB struct {
			// MaxTxs is the maximum number of pending L2Txs that can be
			// stored in the pool.  Once this number of pending L2Txs is
			// reached, inserts to the pool will be denied until some of
			// the pending txs are forged.
			MaxTxs uint32 `validate:"required" env:"TONNODE_L2DB_MAXTXS"`
			// MinFeeUSD is the minimum fee in USD that a tx must pay in
			// order to be accepted into the pool.  Txs with lower than
			// minimum fee will be rejected at the API level.
			MinFeeUSD float64 `validate:"gte=0" env:"TONNODE_L2DB_MINFEEUSD"`
			// MaxFeeUSD is the maximum fee in USD that a tx must pay in
			// order to be accepted into the pool.  Txs with greater than
			// maximum fee will be rejected at the API level.
			MaxFeeUSD float64 `validate:"required,gte=0" env:"TONNODE_L2DB_MAXFEEUSD"`
		} `validate:"required"`
		// Keystore is the ethereum keystore where private keys are kept.
		// Required if API.CoordinatorNetwork == true
		Keystore struct {
			// Path to the keystore
			Path string `env:"TONNODE_KEYSTORE_PATH"`
			// Password used to decrypt the keys in the keystore
			Password string `env:"TONNODE_KEYSTORE_PASSWORD"`
			// LightScrypt if set, uses light parameters for the ethereum
			// keystore encryption algorithm.
			LightScrypt bool `env:"TONNODE_COORDINATORDEBUG_LIGHTSCRYPT"`
		}
		// Rollup is the address of the Hermez.sol smart contract.
		// Required if API.CoordinatorNetwork == true
		Rollup ethCommon.Address `env:"TONNODE_SMARTCONTRACTS_ROLLUP"`
	}
	Web3 struct {
		// URL is the URL of the web3 ethereum-node RPC server.  Only
		// geth is officially supported.
		URL string `validate:"required,url" env:"TONNODE_WEB3_URL"`
	}
	Debug NodeDebug `validate:"required"`
}

// LogConf specifies the log configuration parameters
type LogConf struct {
	Level string   `env:"TONNODE_LOG_LEVEL"`
	Out   []string `env:"TONNODE_LOG_OUT" envSeparator:","`
}

// LoadNode loads the Node configuration from path.
func LoadNode(path string /*, coordinator bool*/) (*Node, error) {
	var cfg, aux Node
	err := SourceParamsNode(path, &cfg, &aux)
	if err != nil {
		log.Println("Error SourceParamsNode: ", err.Error())
	}
	err = LoadConfig(path, DefaultValues, &cfg)
	if err != nil {
		//Split errors depending on if there is a file error, a env error or a default error
		if strings.Contains(err.Error(), "default") {
			return nil, err
		}
		log.Println(err.Error())
	}
	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		return nil, common.Wrap(fmt.Errorf("error validating configuration file: %w", err))
	}
	// if coordinator {
	if err := validate.Struct(cfg.Coordinator); err != nil {
		return nil, common.Wrap(fmt.Errorf("error validating configuration file: %w", err))
		// }
	}
	log.Printf("Loaded Configuration: %+v", cfg)
	return &cfg, nil
}

func (d *Duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}
