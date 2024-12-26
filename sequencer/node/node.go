/*
Package node does the initialization of all the required objects to run both
the synchronizer and the coordinator.

The Node contains several goroutines that run in the background or that
periodically perform tasks.  One of this goroutines periodically calls the
`Synchronizer.Sync` function, allowing the synchronization of one block at a
time.  After every call to `Synchronizer.Sync`, the Node sends a message to the
Coordinator to notify it about the new synced block (and associated state) or
reorg (and resetted state) in case one happens.
*/
package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
	"tokamak-sybil-resistance/api/stateapiupdater"
	"tokamak-sybil-resistance/batchbuilder"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/config"
	"tokamak-sybil-resistance/coordinator"
	dbUtils "tokamak-sybil-resistance/database"
	"tokamak-sybil-resistance/database/historydb"
	"tokamak-sybil-resistance/database/statedb"
	"tokamak-sybil-resistance/eth"
	"tokamak-sybil-resistance/etherscan"
	"tokamak-sybil-resistance/log"
	"tokamak-sybil-resistance/synchronizer"
	"tokamak-sybil-resistance/txprocessor"
	"tokamak-sybil-resistance/txselector"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jmoiron/sqlx"
	"github.com/russross/meddler"
)

const SyncTime = 24 * 60 * time.Minute

// Mode sets the working mode of the node (synchronizer or coordinator)
// type Mode string

// const (
// 	// ModeCoordinator defines the mode of the HermezNode as Coordinator, which
// 	// means that the node is set to forge (which also will be synchronizing with
// 	// the L1 blockchain state)
// 	ModeCoordinator Mode = "coordinator"

// 	// ModeSynchronizer defines the mode of the HermezNode as Synchronizer, which
// 	// means that the node is set to only synchronize with the L1 blockchain state
// 	// and will not forge
// 	ModeSynchronizer Mode = "synchronizer"
// )

// Node is the Hermez Node
type Node struct {
	// nodeAPI         *NodeAPI
	stateAPIUpdater *stateapiupdater.Updater
	// debugAPI        *debugapi.DebugAPI
	// Coordinator
	coord *coordinator.Coordinator

	// Synchronizer
	sync *synchronizer.Synchronizer

	// General
	cfg *config.Node
	// mode         Mode
	sqlConnRead  *sqlx.DB
	sqlConnWrite *sqlx.DB
	historyDB    *historydb.HistoryDB
	ctx          context.Context
	wg           sync.WaitGroup
	cancel       context.CancelFunc
}

// APIServer is a server that only runs the API
// type APIServer struct {
// 	nodeAPI *NodeAPI
// 	mode    Mode
// 	ctx     context.Context
// 	wg      sync.WaitGroup
// 	cancel  context.CancelFunc
// }

// NodeAPI holds the node http API
// type NodeAPI struct { //nolint:golint
// 	api                                     *api.API
// 	engine                                  *gin.Engine
// 	addr                                    string
// 	coordinatorNetwork                      bool
// 	coordinatorNetworkFindMorePeersInterval time.Duration
// 	readtimeout                             time.Duration
// 	writetimeout                            time.Duration
// }

// NewNodeAPI creates a new NodeAPI (which internally calls api.NewAPI)
// func NewNodeAPI(
// 	addr string,
// 	cfgAPI config.APIConfigParameters,
// 	apiConfig api.Config,
// 	coordinatorNetwork bool,
// 	coordinatorNetworkFindMorePeersInterval time.Duration,
// ) (*NodeAPI, error) {
// 	_api, err := api.NewAPI(apiConfig)
// 	if err != nil {
// 		return nil, common.Wrap(err)
// 	}
// 	return &NodeAPI{
// 		addr:                                    addr,
// 		api:                                     _api,
// 		engine:                                  apiConfig.Server,
// 		coordinatorNetwork:                      coordinatorNetwork,
// 		coordinatorNetworkFindMorePeersInterval: coordinatorNetworkFindMorePeersInterval,
// 		readtimeout:                             cfgAPI.Readtimeout.Duration,
// 		writetimeout:                            cfgAPI.Writetimeout.Duration,
// 	}, nil
// }

// Check if a directory exists and is empty
func isDirectoryEmpty(path string) (bool, error) {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil // Directory doesn't exist, treat as empty
		}
		return false, err
	}
	return len(dirEntries) == 0, nil
}

// NewNode creates a Node
func NewNode(cfg *config.Node, version string) (*Node, error) {
	meddler.Debug = cfg.Debug.MeddlerLogs

	// Establish DB connection
	db, err := dbUtils.InitSQLDB()
	if err != nil {
		return nil, common.Wrap(fmt.Errorf("dbUtils.InitSQLDB: %w", err))
	}

	historyDB := historydb.NewHistoryDB(db, db)

	ethClient, err := ethclient.Dial(cfg.Web3.URL)
	if err != nil {
		return nil, common.Wrap(err)
	}
	var ethCfg eth.EthereumConfig
	var forgerAccount *accounts.Account
	var keyStore *keystore.KeyStore
	ethCfg = eth.EthereumConfig{
		CallGasLimit: 0, // cfg.Coordinator.EthClient.CallGasLimit,
		GasPriceDiv:  0, // cfg.Coordinator.EthClient.GasPriceDiv,
	}

	scryptN := keystore.StandardScryptN
	scryptP := keystore.StandardScryptP
	if cfg.Coordinator.Debug.LightScrypt {
		scryptN = keystore.LightScryptN
		scryptP = keystore.LightScryptP
	}
	keyStore = keystore.NewKeyStore(cfg.Coordinator.EthClient.Keystore.Path, scryptN, scryptP)

	isEmpty, err := isDirectoryEmpty(cfg.Coordinator.EthClient.Keystore.Path)
	if err != nil {
		return nil, common.Wrap(err)
	}

	if isEmpty {
		// Create a new account if keystore is empty
		account, err := keyStore.NewAccount(cfg.Coordinator.EthClient.Keystore.Password)
		if err != nil {
			return nil, common.Wrap(err)
		}
		log.Infof("New account created: %s", account.Address.Hex())

		// Necessary to pass the github actions Node Run test
		cfg.Coordinator.ForgerAddress = account.Address
	} else {
		log.Infof("Keystore already initialized, skipping account creation.")
	}

	forgerBalance, err := ethClient.BalanceAt(context.TODO(), cfg.Coordinator.ForgerAddress, nil)
	if err != nil {
		return nil, common.Wrap(err)
	}

	minForgeBalance := cfg.Coordinator.MinimumForgeAddressBalance
	if minForgeBalance != nil && forgerBalance.Cmp(minForgeBalance) == -1 {
		return nil, common.Wrap(fmt.Errorf(
			"forger account balance is less than cfg.Coordinator.MinimumForgeAddressBalance: %v < %v",
			forgerBalance, minForgeBalance))
	}
	log.Infow("forger ethereum account balance",
		"addr", cfg.Coordinator.ForgerAddress,
		"balance", forgerBalance,
		"minForgeBalance", minForgeBalance,
	)

	// Unlock Coordinator ForgerAddr in the keystore to make calls
	// to ForgeBatch in the smart contract
	if !keyStore.HasAddress(cfg.Coordinator.ForgerAddress) {
		return nil, common.Wrap(fmt.Errorf(
			"ethereum keystore doesn't have the key for address %v",
			cfg.Coordinator.ForgerAddress))
	}
	forgerAccount = &accounts.Account{
		Address: cfg.Coordinator.ForgerAddress,
	}
	if err := keyStore.Unlock(
		*forgerAccount,
		cfg.Coordinator.EthClient.Keystore.Password,
	); err != nil {
		return nil, common.Wrap(err)
	}
	log.Infow("Forger ethereum account unlocked in the keystore",
		"addr", cfg.Coordinator.ForgerAddress)
	client, err := eth.NewClient(ethClient, forgerAccount, keyStore, &eth.ClientConfig{
		Ethereum: ethCfg,
		Rollup: eth.RollupConfig{
			Address: cfg.SmartContracts.Rollup,
		},
	})
	if err != nil {
		return nil, common.Wrap(err)
	}

	chainID, err := client.EthChainID()

	if err != nil {
		return nil, common.Wrap(err)
	}
	if !chainID.IsUint64() {
		return nil, common.Wrap(fmt.Errorf("chainID cannot be represented as uint64"))
	}

	chainIDU64 := chainID.Uint64()

	// const maxUint16 uint64 = 0xffff
	// if chainIDU64 > maxUint16 {
	// 	return nil, common.Wrap(fmt.Errorf("chainID overflows uint16"))
	// }
	// chainIDU16 := uint16(chainIDU64)

	stateDB, err := statedb.NewStateDB(statedb.Config{
		Path:    cfg.StateDB.Path,
		Keep:    cfg.StateDB.Keep,
		Type:    statedb.TypeSynchronizer,
		NLevels: statedb.MaxNLevels,
	})
	if err != nil {
		return nil, common.Wrap(err)
	}

	sync, err := synchronizer.NewSynchronizer(
		client,
		historyDB,
		stateDB,
		synchronizer.Config{
			StatsUpdateBlockNumDiffThreshold: cfg.Synchronizer.StatsUpdateBlockNumDiffThreshold,
			StatsUpdateFrequencyDivider:      cfg.Synchronizer.StatsUpdateFrequencyDivider,
			ChainID:                          chainIDU64,
		})
	if err != nil {
		return nil, common.Wrap(err)
	}
	initSCVars := sync.SCVars()

	scConsts := common.SCConsts{
		Rollup: *sync.RollupConstants(),
	}


	// TODO: rename node configs or remove unnecessary configs if not needed
	hdbNodeCfg := historydb.NodeConfig{
		MaxPoolTxs: cfg.Coordinator.L2DB.MaxTxs,
		MinFeeUSD:  cfg.Coordinator.L2DB.MinFeeUSD,
		MaxFeeUSD:  cfg.Coordinator.L2DB.MaxFeeUSD,
		ForgeDelay: cfg.Coordinator.ForgeDelay.Duration.Seconds(),
	}
	if err := historyDB.SetNodeConfig(&hdbNodeCfg); err != nil {
		return nil, common.Wrap(err)
	}
	hdbConsts := historydb.Constants{
		SCConsts: common.SCConsts{
			Rollup: scConsts.Rollup,
		},
		ChainID:       chainIDU64,
		HermezAddress: cfg.SmartContracts.Rollup,
	}
	if err := historyDB.SetConstants(&hdbConsts); err != nil {
		return nil, common.Wrap(err)
	}
	var etherScanService *etherscan.Service
	if cfg.Coordinator.Etherscan.URL != "" && cfg.Coordinator.Etherscan.APIKey != "" {
		log.Info("EtherScan method detected in cofiguration file")
		etherScanService, _ = etherscan.NewEtherscanService(cfg.Coordinator.Etherscan.URL,
			cfg.Coordinator.Etherscan.APIKey)
	} else {
		log.Info("EtherScan method not configured in config file")
		etherScanService = nil
	}
	stateAPIUpdater, err := stateapiupdater.NewUpdater(
		historyDB,
		&hdbNodeCfg,
		initSCVars,
		&hdbConsts,
		&cfg.RecommendedFeePolicy,
		cfg.Coordinator.Circuit.MaxTx,
	)
	if err != nil {
		return nil, common.Wrap(err)
	}

	var coord *coordinator.Coordinator
	// if mode == ModeCoordinator {
	// Unlock FeeAccount EthAddr in the keystore to generate the
	// account creation authorization
	// if !keyStore.HasAddress(cfg.Coordinator.FeeAccount.Address) {
	// 	return nil, common.Wrap(fmt.Errorf(
	// 		"ethereum keystore doesn't have the key for address %v",
	// 		cfg.Coordinator.FeeAccount.Address))
	// }
	// feeAccount := accounts.Account{
	// 	Address: cfg.Coordinator.FeeAccount.Address,
	// }
	// if err := keyStore.Unlock(feeAccount,
	// 	cfg.Coordinator.EthClient.Keystore.Password); err != nil {
	// 	return nil, common.Wrap(err)
	// }
	// //Swap bjj endianness
	// decodedBjjPubKey, err := hex.DecodeString(cfg.Coordinator.FeeAccount.BJJ.String())
	// if err != nil {
	// 	log.Error("Error decoding BJJ public key from config file. Error: ", err.Error())
	// 	return nil, common.Wrap(err)
	// }
	// bSwapped := common.SwapEndianness(decodedBjjPubKey)
	// var bjj babyjub.PublicKeyComp
	// copy(bjj[:], bSwapped[:])

	// auth := &common.AccountCreationAuth{
	// 	EthAddr: cfg.Coordinator.FeeAccount.Address,
	// 	BJJ:     bjj,
	// }

	//TODO: Check and complete this auth signing functionality

	// if err := auth.Sign(func(msg []byte) ([]byte, error) {
	// 	return keyStore.SignHash(feeAccount, msg)
	// }, chainIDU16, cfg.SmartContracts.Rollup); err != nil {
	// 	return nil, common.Wrap(err)
	// }

	// coordAccount := txselector.CoordAccount{
	// 	Addr: cfg.Coordinator.FeeAccount.Address,
	// 	BJJ:  bjj,
	// 	// AccountCreationAuth: auth.Signature,
	// }

	txSelector, err := txselector.NewTxSelector(
		// &coordAccount,
		cfg.Coordinator.TxSelector.Path,
		stateDB,
	)
	if err != nil {
		return nil, common.Wrap(err)
	}
	batchBuilder, err := batchbuilder.NewBatchBuilder(
		cfg.Coordinator.BatchBuilder.Path,
		stateDB,
		0,
		uint64(cfg.Coordinator.Circuit.NLevels),
	)
	if err != nil {
		return nil, common.Wrap(err)
	}

	//TODO: Initialize server proofs
	// serverProofs := make([]prover.Client, len(cfg.Coordinator.ServerProofs.URLs))
	// for i, serverProofCfg := range cfg.Coordinator.ServerProofs.URLs {
	// 	serverProofs[i] = prover.NewProofServerClient(serverProofCfg,
	// 		cfg.Coordinator.ProofServerPollInterval.Duration)
	// }

	txProcessorCfg := txprocessor.Config{
		NLevels: uint32(cfg.Coordinator.Circuit.NLevels),
		MaxTx:   uint32(cfg.Coordinator.Circuit.MaxTx),
		ChainID: chainIDU64,
		// MaxFeeTx: common.RollupConstMaxFeeIdxCoordinator,
		MaxL1Tx: common.RollupConstMaxL1Tx,
	}
	var verifierIdx int
	if cfg.Coordinator.Debug.RollupVerifierIndex == nil {
		verifierIdx, err = scConsts.Rollup.FindVerifierIdx(
			cfg.Coordinator.Circuit.MaxTx,
			cfg.Coordinator.Circuit.NLevels,
		)
		if err != nil {
			return nil, common.Wrap(err)
		}
		log.Infow("Found verifier that matches circuit config", "verifierIdx", verifierIdx)
	} else {
		verifierIdx = *cfg.Coordinator.Debug.RollupVerifierIndex
		log.Infow("Using debug verifier index from config", "verifierIdx", verifierIdx)
		if verifierIdx >= len(scConsts.Rollup.Verifiers) {
			return nil, common.Wrap(
				fmt.Errorf("verifierIdx (%v) >= "+
					"len(scConsts.Rollup.Verifiers) (%v)",
					verifierIdx, len(scConsts.Rollup.Verifiers)))
		}
		verifier := scConsts.Rollup.Verifiers[verifierIdx]
		if verifier.MaxTx != cfg.Coordinator.Circuit.MaxTx ||
			verifier.NLevels != cfg.Coordinator.Circuit.NLevels {
			return nil, common.Wrap(
				fmt.Errorf("circuit config and verifier params don't match.  "+
					"circuit.MaxTx = %v, circuit.NLevels = %v, "+
					"verifier.MaxTx = %v, verifier.NLevels = %v",
					cfg.Coordinator.Circuit.MaxTx, cfg.Coordinator.Circuit.NLevels,
					verifier.MaxTx, verifier.NLevels,
				))
		}
	}

	coord, err = coordinator.NewCoordinator(
		coordinator.Config{
			ForgerAddress:           cfg.Coordinator.ForgerAddress,
			ConfirmBlocks:           cfg.Coordinator.ConfirmBlocks,
			L1BatchTimeoutPerc:      cfg.Coordinator.L1BatchTimeoutPerc,
			ForgeRetryInterval:      cfg.Coordinator.ForgeRetryInterval.Duration,
			ForgeDelay:              cfg.Coordinator.ForgeDelay.Duration,
			MustForgeAtSlotDeadline: cfg.Coordinator.MustForgeAtSlotDeadline,
			IgnoreSlotCommitment:    cfg.Coordinator.IgnoreSlotCommitment,
			ForgeOncePerSlotIfTxs:   cfg.Coordinator.ForgeOncePerSlotIfTxs,
			ForgeNoTxsDelay:         cfg.Coordinator.ForgeNoTxsDelay.Duration,
			SyncRetryInterval:       cfg.Coordinator.SyncRetryInterval.Duration,
			EthClientAttempts:       cfg.Coordinator.EthClient.Attempts,
			EthClientAttemptsDelay:  cfg.Coordinator.EthClient.AttemptsDelay.Duration,
			EthNoReuseNonce:         cfg.Coordinator.EthClient.NoReuseNonce,
			EthTxResendTimeout:      cfg.Coordinator.EthClient.TxResendTimeout.Duration,
			MaxGasPrice:             cfg.Coordinator.EthClient.MaxGasPrice,
			MinGasPrice:             cfg.Coordinator.EthClient.MinGasPrice,
			GasPriceIncPerc:         cfg.Coordinator.EthClient.GasPriceIncPerc,
			TxManagerCheckInterval:  cfg.Coordinator.EthClient.CheckLoopInterval.Duration,
			DebugBatchPath:          cfg.Coordinator.Debug.BatchPath,
			ForgeBatchGasCost: cfg.Coordinator.EthClient.ForgeBatchGasCost,
			// VerifierIdx:       uint8(verifierIdx),
			TxProcessorConfig: txProcessorCfg,
			ProverReadTimeout: cfg.Coordinator.ProverWaitReadTimeout.Duration,
		},
		historyDB,
		txSelector,
		batchBuilder,
		nil, //serverProofs
		client,
		&scConsts,
		initSCVars,
		etherScanService,
	)
	if err != nil {
		return nil, common.Wrap(err)
	}
	// }
	// var nodeAPI *NodeAPI
	// if cfg.API.Address != "" {
	// 	if cfg.Debug.GinDebugMode {
	// 		gin.SetMode(gin.DebugMode)
	// 	} else {
	// 		gin.SetMode(gin.ReleaseMode)
	// 	}
	// 	server := gin.Default()
	// 	server.Use(cors.Default())
	// 	coord := false
	// 	var coordnetConfig *api.CoordinatorNetworkConfig
	// 	if mode == ModeCoordinator {
	// 		coord = cfg.Coordinator.API.Coordinator
	// 		if cfg.API.CoordinatorNetwork {
	// 			// Setup coordinators network configuration
	// 			// Get libp2p addresses of the registered coordinators
	// 			// to be used as bootstrap nodes for the p2p network

	// 			//TODO: Check if we'll need this
	// 			// bootstrapAddrs, err := client.GetCoordinatorsLibP2PAddrs()
	// 			if err != nil {
	// 				log.Warn("error getting registered addresses from the SMC or no addresses registered. error:", err.Error())
	// 			}
	// 			// Get Ethereum private key of the coordinator
	// 			keyJSON, err := keyStore.Export(*account, cfg.Coordinator.EthClient.Keystore.Password, cfg.Coordinator.EthClient.Keystore.Password)
	// 			if err != nil {
	// 				return nil, common.Wrap(err)
	// 			}
	// 			key, err := keystore.DecryptKey(keyJSON, cfg.Coordinator.EthClient.Keystore.Password)
	// 			if err != nil {
	// 				return nil, common.Wrap(err)
	// 			}
	// 			coordnetConfig = &api.CoordinatorNetworkConfig{
	// 				BootstrapPeers: nil,
	// 				EthPrivKey:     key.PrivateKey,
	// 			}
	// 		}
	// 	}
	// 	var err error
	// 	nodeAPI, err = NewNodeAPI(cfg.API.Address, cfg.API, api.Config{
	// 		Version:                  version,
	// 		ExplorerEndpoints:        cfg.API.Explorer,
	// 		CoordinatorEndpoints:     coord,
	// 		Server:                   server,
	// 		HistoryDB:                historyDB,
	// 		StateDB:                  stateDB,
	// 		EthClient:                ethClient,
	// 		ForgerAddress:            &cfg.Coordinator.ForgerAddress,
	// 		CoordinatorNetworkConfig: coordnetConfig,
	// 	}, cfg.API.CoordinatorNetwork, cfg.API.FindPeersCoordinatorNetworkInterval.Duration)
	// 	if err != nil {
	// 		return nil, common.Wrap(err)
	// 	}
	// }

	// TODO: Update debug API fields
	// var debugAPI *debugapi.DebugAPI
	// if cfg.Debug.APIAddress != "" {
	// 	debugAPI = debugapi.NewDebugAPI(cfg.Debug.APIAddress, stateDB, sync)
	// }
	ctx, cancel := context.WithCancel(context.Background())
	return &Node{
		stateAPIUpdater: stateAPIUpdater,
		coord:           coord,
		sync:            sync,
		cfg:             cfg,
		sqlConnRead:     db,
		sqlConnWrite:    db,
		historyDB:       historyDB,
		ctx:             ctx,
		cancel:          cancel,
	}, nil
}

func (n *Node) handleReorg(
	ctx context.Context,
	stats *synchronizer.Stats,
	vars *common.SCVariables,
) error {
	n.coord.SendMsg(ctx, coordinator.MsgSyncReorg{
		Stats: *stats,
		Vars:  *vars.AsPtr(),
	})
	n.stateAPIUpdater.SetSCVars(vars.AsPtr())
	n.stateAPIUpdater.UpdateNetworkInfoBlock(
		stats.Eth.LastBlock, stats.Sync.LastBlock,
	)
	if err := n.stateAPIUpdater.Store(); err != nil {
		return common.Wrap(err)
	}
	return nil
}

func (n *Node) syncLoopFn(ctx context.Context, lastBlock *common.Block) (*common.Block,
	time.Duration, error) {
	blockData, discarded, err := n.sync.Sync(ctx, lastBlock)
	stats := n.sync.Stats()
	if err != nil {
		// case: error
		return nil, n.cfg.Synchronizer.SyncLoopInterval.Duration, common.Wrap(err)
	} else if discarded != nil {
		// case: reorg
		log.Infow("Synchronizer.Sync reorg", "discarded", *discarded)
		vars := n.sync.SCVars()
		if err := n.handleReorg(ctx, stats, vars); err != nil {
			return nil, time.Duration(0), common.Wrap(err)
		}
		return nil, time.Duration(0), nil
	} else if blockData != nil {
		// case: new block
		vars := common.SCVariablesPtr{
			Rollup: blockData.Rollup.Vars,
		}
		if err := n.handleNewBlock(ctx, stats, &vars); err != nil {
			return nil, time.Duration(SyncTime), common.Wrap(err)
		}
		return &blockData.Block, time.Duration(SyncTime), nil
	} else {
		// case: no block
		return lastBlock, n.cfg.Synchronizer.SyncLoopInterval.Duration, nil
	}
}

// StartSynchronizer starts the synchronizer
func (n *Node) StartSynchronizer() {
	log.Info("Starting Synchronizer...")

	// Trigger a manual call to handleNewBlock with the loaded state of the
	// synchronizer in order to quickly activate the API and Coordinator
	// and avoid waiting for the next block.  Without this, the API and
	// Coordinator will not react until the following block (starting from
	// the last synced one) is synchronized
	stats := n.sync.Stats()
	vars := n.sync.SCVars()
	if err := n.handleNewBlock(n.ctx, stats, vars.AsPtr() /*, []common.BatchData{}*/); err != nil {
		log.Fatalw("Node.handleNewBlock", "err", err)
	}

	n.wg.Add(1)
	go func() {
		var err error
		var lastBlock *common.Block
		waitDuration := time.Duration(0)
		for {
			select {
			case <-n.ctx.Done():
				log.Info("Synchronizer done")
				n.wg.Done()
				return
			case <-time.After(waitDuration):
				if lastBlock, waitDuration, err = n.syncLoopFn(n.ctx,
					lastBlock); err != nil {
					if n.ctx.Err() != nil {
						continue
					}
					if errors.Is(err, eth.ErrBlockHashMismatchEvent) {
						log.Warnw("Synchronizer.Sync", "err", err)
					} else if errors.Is(err, synchronizer.ErrUnknownBlock) {
						log.Warnw("Synchronizer.Sync", "err", err)
					} else {
						log.Errorw("Synchronizer.Sync", "err", err)
					}
				}
			}
		}
	}()

}

// Start the sequencer node
func (n *Node) Start() {
	log.Info("Starting node...")
	n.coord.Start()
	n.StartSynchronizer()
}

// Stop the node
func (n *Node) Stop() {
	log.Infow("Stopping node...")
	n.cancel()
	n.wg.Wait()
	log.Info("Stopping Coordinator...")
	n.coord.Stop()

	// Close kv DBs
	n.sync.StateDB().Close()

	n.coord.TxSelector().LocalAccountsDB().Close()
	n.coord.BatchBuilder().LocalStateDB().Close()
}

func (n *Node) handleNewBlock(
	ctx context.Context,
	stats *synchronizer.Stats,
	vars *common.SCVariablesPtr,
	/*, batches []common.BatchData*/
) error {
	n.coord.SendMsg(ctx, coordinator.MsgSyncBlock{
		Stats: *stats,
		Vars:  *vars,
	})
	n.stateAPIUpdater.SetSCVars(vars)

	/*
		When the state is out of sync, which means, the last block synchronized by the node is
		different/smaller from the last block provided by the ethereum, the network info in the state
		will not be updated. So, in order to get some information on the node state, we need
		to wait until the node finish the synchronization with the ethereum network.

		Side effects are information like lastBatch, nextForgers, metrics with zeros, defaults or null values
	*/
	if stats.Synced() {
		if err := n.stateAPIUpdater.UpdateNetworkInfo(
			stats.Eth.LastBlock, stats.Sync.LastBlock,
			common.BatchNum(stats.Eth.LastBatchNum),
			// stats.Sync.Auction.CurrentSlot.SlotNum,
		); err != nil {
			log.Errorw("ApiStateUpdater.UpdateNetworkInfo", "err", err)
		}
	} else {
		n.stateAPIUpdater.UpdateNetworkInfoBlock(
			stats.Eth.LastBlock, stats.Sync.LastBlock,
		)
	}
	if err := n.stateAPIUpdater.Store(); err != nil {
		return common.Wrap(err)
	}
	return nil
}
