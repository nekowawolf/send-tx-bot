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
	fmt.Println("2. MegaETH")
	fmt.Print("\nEnter your choice: ")

	reader := bufio.NewReader(os.Stdin)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		chain.MonadSend()
	case "2":
		chain.MegaETH()
	default:
		fmt.Println("Invalid choice. Please select a valid option.")
		os.Exit(1)
	}
}