package main

import (
	"fmt"
	"os"

	"github.com/dhanuka84/hybrid-ai-platform/contracts"
)

func main() {
	if err := contracts.Check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("All versioned schemas and positive/negative fixtures passed")
}
