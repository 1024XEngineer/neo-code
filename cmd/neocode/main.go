package main

import (
	"context"
	"fmt"
	"os"

	"github.com/dust/neo-code/internal/app"
)

func main() {
	program, err := app.NewProgram(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "neocode: %v\n", err)
		os.Exit(1)
	}

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "neocode: %v\n", err)
		os.Exit(1)
	}
}
