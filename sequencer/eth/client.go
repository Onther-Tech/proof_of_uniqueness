package eth

import (
	"tokamak-sybil-resistance/common"

	"github.com/ethereum/go-ethereum/accounts"
	ethKeystore "github.com/ethereum/go-ethereum/accounts/keystore"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ClientInterface is the eth Client interface used by hermez-node modules to
// interact with Ethereum Blockchain and smart contracts.
type ClientInterface interface {
	EthereumInterface
	RollupInterface
}

// RollupConfig is the configuration for the Rollup smart contract interface
type RollupConfig struct {
	Address ethCommon.Address
}

// Client is used to interact with Ethereum and the Hermez smart contracts.
type Client struct {
	EthereumClient
	RollupClient
}

// ClientConfig is the configuration of the Client
type ClientConfig struct {
	Ethereum EthereumConfig
	Rollup   RollupConfig
}

const (
	blocksPerDay = 0 //nolint:gomnd
)

// NewClient creates a new Client to interact with Ethereum and the Sybil smart contracts.
func NewClient(client *ethclient.Client, account *accounts.Account, ks *ethKeystore.KeyStore,
	cfg *ClientConfig) (*Client, error) {
	ethereumClient, err := NewEthereumClient(client, account, ks, &cfg.Ethereum)
	if err != nil {
		return nil, common.Wrap(err)
	}
	rollupClient, err := NewRollupClient(ethereumClient, cfg.Rollup.Address)
	if err != nil {
		return nil, common.Wrap(err)
	}
	return &Client{
		EthereumClient: *ethereumClient,
		RollupClient:   *rollupClient,
	}, nil
}
