package main

import (
	"fmt"
	"os"
	"os/signal"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/config"
	"tokamak-sybil-resistance/database"
	"tokamak-sybil-resistance/log"
	"tokamak-sybil-resistance/node"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
)

const (
	flagCfg = "cfg"
	// flagMode    = "mode"
	// flagSK      = "privatekey"
	// flagYes     = "yes"
	// flagBlock   = "block"
	// modeSync    = "sync"
	// modeCoord   = "coord"
	// nMigrations = "nMigrations"
	// flagAccount = "account"
	// flagPath    = "path"
)

// Config is the configuration of the node execution
type Config struct {
	// mode node.Mode
	node *config.Node
}

func parseCli(c *cli.Context) (*Config, error) {
	cfg, err := getConfig(c)
	if err != nil {
		if err := cli.ShowAppHelp(c); err != nil {
			panic(err)
		}
		return nil, common.Wrap(err)
	}
	return cfg, nil
}

func getConfig(c *cli.Context) (*Config, error) {
	var cfg Config
	// mode := c.String(flagMode)
	nodeCfgPath := c.String(flagCfg)
	var err error
	// switch mode {
	// case modeSync:
	// 	// cfg.mode = node.ModeSynchronizer
	// 	cfg.node, err = config.LoadNode(nodeCfgPath, false)
	// 	if err != nil {
	// 		return nil, common.Wrap(err)
	// 	}
	// case modeCoord:
	// 	cfg.mode = node.ModeCoordinator
	cfg.node, err = config.LoadNode(nodeCfgPath /*, true*/)
	if err != nil {
		return nil, common.Wrap(err)
	}
	// default:
	// 	return nil, common.Wrap(fmt.Errorf("invalid mode \"%v\"", mode))
	// }

	return &cfg, nil
}

func waitSigInt() {
	stopCh := make(chan interface{})

	// catch ^C to send the stop signal
	ossig := make(chan os.Signal, 1)
	signal.Notify(ossig, os.Interrupt)
	const forceStopCount = 3
	go func() {
		n := 0
		for sig := range ossig {
			if sig == os.Interrupt {
				log.Info("Received Interrupt Signal")
				stopCh <- nil
				n++
				if n == forceStopCount {
					log.Fatalf("Received %v Interrupt Signals", forceStopCount)
				}
			}
		}
	}()
	<-stopCh
}

func cmdRun(c *cli.Context) error {
	cfg, err := parseCli(c)
	if err != nil {
		return common.Wrap(fmt.Errorf("error parsing flags and config: %w", err))
	}
	// TODO: Initialize lof library
	// log.Init(cfg.node.Log.Level, cfg.node.Log.Out)
	innerNode, err := node.NewNode(cfg.node, c.App.Version)
	if err != nil {
		return common.Wrap(fmt.Errorf("error starting node: %w", err))
	}
	innerNode.Start()
	waitSigInt()
	innerNode.Stop()

	return nil
}

func runMigrations(c *cli.Context) error {
	db, err := database.ConnectSQLDB()
	if err != nil {
		return common.Wrap(fmt.Errorf("error running migrations: %w", err))
	}
	defer db.Close()

	if err := database.MigrationsDown(db.DB, 0); err != nil {
		return common.Wrap(fmt.Errorf("error running migrations: %w", err))
	}

	if err := database.MigrationsUp(db.DB); err != nil {
		return common.Wrap(fmt.Errorf("error running migrations: %w", err))
	}

	os.Exit(0)

	return nil
}

func RunApp() error {
	app := cli.NewApp()
	app.Name = "tokamak-node"
	app.Version = "v1"

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:     flagCfg,
			Usage:    "Node configuration `FILE`",
			Required: false,
		},
	}

	app.Commands = []cli.Command{
		{
			Name:    "run",
			Aliases: []string{},
			Usage:   "Run the tokamak-node",
			Action:  cmdRun,
			Flags:   flags,
		},
		{
			Name:    "migrate",
			Aliases: []string{},
			Usage:   "Run the migrations down & up",
			Action:  runMigrations,
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		return common.Wrap(err)
	}

	router := gin.Default()
	err = router.Run("localhost:8080")
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
	return nil
}

func main() {
	if err := RunApp(); err != nil {
		fmt.Printf("\nError: %v\n", common.Wrap(err))
		os.Exit(1)
	}
}
