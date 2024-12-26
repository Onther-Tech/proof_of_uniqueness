package api

import (
	"crypto/ecdsa"
	"errors"
	"tokamak-sybil-resistance/api/coordinatornetwork"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/database/historydb"
	"tokamak-sybil-resistance/database/statedb"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator"
	"github.com/multiformats/go-multiaddr"
)

// API serves HTTP requests to allow external interaction with the Hermez node
type API struct {
	historyDB     *historydb.HistoryDB
	config        *configAPI
	stateDB       *statedb.StateDB
	hermezAddress ethCommon.Address
	validate      *validator.Validate
	coordnet      *coordinatornetwork.CoordinatorNetwork
}

type CoordinatorNetworkConfig struct {
	BootstrapPeers []multiaddr.Multiaddr
	EthPrivKey     *ecdsa.PrivateKey
}

// Config wraps the parameters needed to start the API
type Config struct {
	Version                  string
	CoordinatorEndpoints     bool
	ExplorerEndpoints        bool
	Server                   *gin.Engine
	HistoryDB                *historydb.HistoryDB
	StateDB                  *statedb.StateDB
	EthClient                *ethclient.Client
	ForgerAddress            *ethCommon.Address
	CoordinatorNetworkConfig *CoordinatorNetworkConfig
}

// NewAPI sets the endpoints and the appropriate handlers, but doesn't start the server
func NewAPI(setup Config) (*API, error) {
	// Check input
	if setup.ExplorerEndpoints && setup.HistoryDB == nil {
		return nil, common.Wrap(errors.New("cannot serve Explorer endpoints without HistoryDB"))
	}
	consts, err := setup.HistoryDB.GetConstants()
	if err != nil {
		return nil, err
	}

	a := &API{
		historyDB: setup.HistoryDB,
		config: &configAPI{
			RollupConstants: *newRollupConstants(consts.Rollup),
			ChainID:         consts.ChainID,
		},
		stateDB:       setup.StateDB,
		hermezAddress: consts.HermezAddress,
		validate:      nil, //TODO: Add validations
	}

	// Setup coordinator network (libp2p interface) <=TODO
	// if setup.CoordinatorNetworkConfig != nil {
	// 	if setup.CoordinatorNetworkConfig.EthPrivKey == nil {
	// 		return nil, tracerr.New("EthPrivateKey is required to setup the coordinators network")
	// 	}
	// 	coordnet, err := coordinatornetwork.NewCoordinatorNetwork(
	// 		setup.CoordinatorNetworkConfig.EthPrivKey,
	// 		setup.CoordinatorNetworkConfig.BootstrapPeers,
	// 		consts.ChainID,
	// 		a.coordnetPoolTxHandler,
	// 	)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	a.coordnet = &coordnet
	// }

	// Setup http interface
	// middleware, err := metric.PrometheusMiddleware()
	// if err != nil {
	// 	return nil, err
	// }
	// setup.Server.Use(middleware)

	// setup.Server.NoRoute(a.noRoute)

	// v1 := setup.Server.Group("/v1")

	// v1.GET("/health", gin.WrapH(a.healthRoute(setup.Version, setup.EthClient, setup.ForgerAddress)))
	// // Add coordinator endpoints
	// if setup.CoordinatorEndpoints {
	// 	// Account creation authorization
	// 	v1.POST("/account-creation-authorization", a.postAccountCreationAuth)
	// 	v1.GET("/account-creation-authorization/:hezEthereumAddress", a.getAccountCreationAuth)
	// 	// Transaction
	// 	v1.POST("/transactions-pool", a.postPoolTx)
	// 	v1.PUT("/transactions-pool/:id", a.putPoolTx)
	// 	v1.PUT("/transactions-pool/accounts/:accountIndex/nonces/:nonce", a.putPoolTxByIdxAndNonce)
	// 	v1.GET("/transactions-pool/:id", a.getPoolTx)
	// 	v1.GET("/transactions-pool", a.getPoolTxs)
	// 	v1.POST("/atomic-pool", a.postAtomicPool)
	// 	v1.GET("/atomic-pool/:id", a.getAtomicGroup)
	// }

	// // Add explorer endpoints
	// if setup.ExplorerEndpoints {
	// 	// Account
	// 	v1.GET("/accounts", a.getAccounts)
	// 	v1.GET("/accounts/:accountIndex", a.getAccount)
	// 	v1.GET("/exits", a.getExits)
	// 	v1.GET("/exits/:batchNum/:accountIndex", a.getExit)
	// 	// Transaction
	// 	v1.GET("/transactions-history", a.getHistoryTxs)
	// 	v1.GET("/transactions-history/:id", a.getHistoryTx)
	// 	// Batches
	// 	v1.GET("/batches", a.getBatches)
	// 	v1.GET("/batches/:batchNum", a.getBatch)
	// 	v1.GET("/full-batches/:batchNum", a.getFullBatch)
	// 	// Slots
	// 	v1.GET("/slots", a.getSlots)
	// 	v1.GET("/slots/:slotNum", a.getSlot)
	// 	// Bids
	// 	v1.GET("/bids", a.getBids)
	// 	// State
	// 	v1.GET("/state", a.getState)
	// 	// Config
	// 	v1.GET("/config", a.getConfig)
	// 	// Tokens
	// 	v1.GET("/tokens", a.getTokens)
	// 	v1.GET("/tokens/:id", a.getToken)
	// 	// Fiat Currencies
	// 	v1.GET("/currencies", a.getFiatCurrencies)
	// 	v1.GET("/currencies/:symbol", a.getFiatCurrency)
	// 	// Coordinators
	// 	v1.GET("/coordinators", a.getCoordinators)
	// }

	return a, nil
}
