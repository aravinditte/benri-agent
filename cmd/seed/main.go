// Command seed loads the synthetic demo fixtures using the migrator database
// credential. It is a short-lived deployment step and is not part of the API.
package main

import (
	"fmt"
	"os"

	"github.com/aravinditte/benri-agent/internal/config"
	"github.com/aravinditte/benri-agent/internal/proc"
	"github.com/aravinditte/benri-agent/internal/seed"
	"github.com/aravinditte/benri-agent/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st, err := proc.OpenStore(cfg, store.RoleFull)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := seed.Seed(st, proc.FixturesDir(cfg), proc.Tokenizer(cfg)); err != nil {
		return err
	}
	fmt.Println("seeded synthetic demo fixtures")
	return nil
}
