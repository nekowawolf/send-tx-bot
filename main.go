package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/nekowawolf/send-tx-bot/chain"

)

func main() {
	fmt.Println("\nSelect Chain:")
	fmt.Println("1. Monad")
	fmt.Print("\nEnter your choice: ")

	reader := bufio.NewReader(os.Stdin)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		chain.MonadSend()
	default:
		fmt.Println("Invalid choice. Please select a valid option.")
		os.Exit(1)
	}
}