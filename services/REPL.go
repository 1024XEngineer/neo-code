package services

import (
	"context"
	"fmt"
	"strings"

	"neo-code/ai"
)

func HandleCommand(ctx context.Context, input string, activeModel *string, historyChanged *bool) (bool, error) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return false, nil
	}

	switch fields[0] {
	case "/switch":
		if len(fields) < 2 {
			printAvailableModels()
			return false, fmt.Errorf("Usage: /switch <model>")
		}
		target := fields[1]
		if !ai.IsSupportedModel(target) {
			printAvailableModels()
			return false, fmt.Errorf("Model %q is not supported", target)
		}
		*activeModel = target
		fmt.Printf("Switched to model %s\n", target)
	case "/models":
		printAvailableModels()
	case "/memory":
		stats, err := GetMemoryStats(ctx)
		if err != nil {
			return false, err
		}
		fmt.Printf("memory items: %d, topK: %d, minScore: %.2f, file: %s\n", stats.Items, stats.TopK, stats.MinScore, stats.Path)
	case "/clear-memory":
		if err := ClearMemory(ctx); err != nil {
			return false, err
		}
		fmt.Println("Cleared local long-term memory")
	case "/clear-context":
		if historyChanged != nil {
			*historyChanged = true
		}
		fmt.Println("Cleared current conversation context")
	case "/help":
		printHelp()
	case "/exit":
		return true, nil
	default:
		fmt.Printf("Unrecognized command %s, try /help\n", fields[0])
	}
	return false, nil
}

func printAvailableModels() {
	fmt.Println("Available models:")
	for _, model := range ai.SupportedModels {
		fmt.Printf("  %s\n", model)
	}
}

func printHelp() {
	fmt.Println("Commands:")
	fmt.Println("  /switch <model>  Switch the active model")
	fmt.Println("  /models          List supported models")
	fmt.Println("  /memory          Show local memory stats")
	fmt.Println("  /clear-memory    Clear local long-term memory")
	fmt.Println("  /clear-context   Clear current short-term context")
	fmt.Println("  /exit            Exit the program")
	fmt.Println("  /help            Show this help text")
	fmt.Println("All other input is treated as a prompt sent to the model.")
	fmt.Println("Relevant memories are retrieved from local JSON storage automatically.")
}
