# Sequencer for SYB

SYB sequencer is a zk-rollup sequencer designed to compute uniqueness score for each user account based on the vouches made between accounts. This score will reflect the interactions and endorsements that one account provides to another. Acting as a backend middleware, sequencer facilitates communication between the smart contract and the Circuit, which is responsible for calculating the uniqueness score. 

By utilizing a zk-rollup architecture, we aim to significantly reduce the computational costs associated with calculating scores directly through smart contracts, which would otherwise be prohibitively high. This approach ensures efficiency and scalability while maintaining the integrity of our computations.

## Setup
```bash
cp .env.example .env
brew install go-task # if you are on different OS: https://taskfile.dev/installation/
brew install golangci-lint

# from the root directory
cp githooks/pre-commit .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

If you are using VSCode, install the [go extension](https://marketplace.visualstudio.com/items?itemName=golang.go) and add the following to your user settings.json file to lint `go` files on save (note that if you push unlinted go files, triggered actions will fail):
```json
"go.lintTool": "golangci-lint",
"go.lintFlags": [
    "--fast"
],
"go.lintOnSave": "file",
"go.formatTool": "gofmt",
"go.useLanguageServer": true,
"[go]": {
    "editor.formatOnSave": true,
    "editor.codeActionsOnSave": {
        "source.organizeImports": "always"
    }
}
```

## Run Sequencer
```bash
task run-seq
```

## Update keystore address
```bash
mkdir -p var/tokamak/ethkeystore  # create this directory inside sequencer

# Once file is created,New address will be given in terminal, Please update this address as forger address in cfg.toml file

cfg.Coordinator.ForgerAddress  # Update this address
```

## Run Tests
```bash
task test-<name> # for example: task test-historydb
```

## Architecture

### E2E flow
<img src="../doc/images/sequencer_e2e_flow.png" />

### Sync flow ([interactive link](https://viewer.diagrams.net/?tags=%7B%7D&lightbox=1&highlight=0000ff&edit=_blank&layers=1&nav=1#G10tKc2c3VyREzzdtekl2dcNMwI4HWOSfr#%7B%22pageId%22%3A%22mWZ3KBgQXANqmTgwyxpi%22%7D))
<img src="../doc/images/sequencer_sync_flow.png" />

## CMD to run sequencer in sync mode

```
go run main.go run --mode sync --cfg cfg.toml
```


### Coord flow ([interactive link](https://viewer.diagrams.net/?tags=%7B%7D&lightbox=1&highlight=0000ff&edit=_blank&layers=1&nav=1#G10tKc2c3VyREzzdtekl2dcNMwI4HWOSfr#%7B%22pageId%22%3A%22MOlNjBzEnPvgUMVi-x9F%22%7D))
<img src="../doc/images/sequencer_coord_flow.png" />

## CMD to run sequencer in coord mode

```
go run main.go run --mode coord --cfg cfg.toml
```
## To generate .go file of the contract
```
npm install solc@0.8.23
```
```
npx solc --bin --abi src/sybil.sol -o build/
```
```
abigen --abi=build/src_mvp_Sybil_sol_Sybil.abi --bin=build/src_mvp_Sybil_sol_Sybil.bin --pkg=sybil --out=Sybil.go
```
