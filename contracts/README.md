## Foundry

**Foundry is a blazing fast, portable and modular toolkit for Ethereum application development written in Rust.**

Foundry consists of:

-   **Forge**: Ethereum testing framework (like Truffle, Hardhat and DappTools).
-   **Cast**: Swiss army knife for interacting with EVM smart contracts, sending transactions and getting chain data.
-   **Anvil**: Local Ethereum node, akin to Ganache, Hardhat Network.
-   **Chisel**: Fast, utilitarian, and verbose solidity REPL.

## Documentation

https://book.getfoundry.sh/

## Usage

### Build

```shell
$ forge build
```

### Test

```shell
$ forge test
```

### Format

```shell
$ forge fmt
```

### Gas Snapshots

```shell
$ forge snapshot
```

### Anvil

```shell
$ anvil
```

### Deploy

```shell
$ forge script script/Counter.s.sol:CounterScript --rpc-url <your_rpc_url> --private-key <your_private_key>
```

### Cast

```shell
$ cast <subcommand>
```

### Help

```shell
$ forge --help
$ anvil --help
$ cast --help
```

# Poseidon Contracts (Thanos)
Poseidon2Elements deployed at: 0xb84B26659fBEe08f36A2af5EF73671d66DDf83db
Poseidon3Elements deployed at: 0xFc50367cf2bA87627f99EDD8703FF49252473AED
Poseidon4Elements deployed at: 0xF8AB2781AA06A1c3eF41Bd379Ec1681a70A148e0

# deploy Poseidon
forge script script/DeployPoseidon.s.sol --broadcast --ffi

# deploy Sybil
forge script script/DeployVerifier.s.sol --rpc-url https://rpc.thanos-sepolia.tokamak.network --private-key <your_private_key> --broadcast

forge script script/DeploySybilMvp.s.sol --rpc-url https://rpc.thanos-sepolia.tokamak.network --private-key <your_private_key> --legacy  --broadcast

# deploy and verify (same time)
forge script script/DeploySybilMvp.s.sol \
  --rpc-url https://rpc.thanos-sepolia.tokamak.network \
  --private-key <your_private_key> \
  --broadcast \
  --verify \
  --verifier blockscout \
  --verifier-url https://explorer.thanos-sepolia.tokamak.network/api/

## Verify contract:

# Encode ABI
cast abi-encode "constructor(address[],uint256[],uint256[],uint8,address,address,address)" '[0xac47fbc2bf0f12455b8cc7cf46630decb7b26ffa]' '[100]' '[5]' 240 0xE323F085c404a72127E2eC68facA5d82D30B65Ad 0x117B65CD97f11745ABD50f362f7D227a106D6c33 0x3Bd67f3F82b59bcc524279ed4fCcD713F490715E

forge verify-contract --constructor-args --chain bsc-testnet 0x3dBC98f34b4C87688313f9364e9e32764c650Ce1 src/sybil.sol:Sybil --etherscan-api-key 123

forge script script/DeploySybilMvp.s.sol --rpc-url https://rpc.thanos-sepolia.tokamak.network --private-key <your_private_key> --resume --verify --verifier blockscout --verifier-url https://explorer.thanos-sepolia.tokamak.network/api/

