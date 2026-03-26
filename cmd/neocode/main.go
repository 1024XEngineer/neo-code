package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"neocode/internal/app"
	"neocode/internal/config"
)

func main() {
	cfgPath := flag.String("config", config.DefaultPath(), "path to config file")
	flag.Parse()

	if err := app.Run(context.Background(), *cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
