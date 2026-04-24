package main

import (
	"context"
	"fmt"
	"os"

	"neo-code/internal/cli"
)

func main() {
	if err := cli.ExecuteGatewayServer(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "neocode-gateway: %v\n", err)
		os.Exit(1)
	}
}
