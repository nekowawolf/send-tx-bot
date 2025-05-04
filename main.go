package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nekowawolf/send-tx-bot/chain"

)

func main() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter amount to send to each address (in MON): ")
	amountInput, _ := reader.ReadString('\n')
	amountInput = strings.TrimSpace(amountInput)

	amount, err := strconv.ParseFloat(amountInput, 64)
	if err != nil || amount <= 0 {
		fmt.Println("Invalid amount. Please enter a positive number.")
		os.Exit(1)
	}

	fmt.Print("Enter number of transactions per address: ")
	countInput, _ := reader.ReadString('\n')
	countInput = strings.TrimSpace(countInput)

	count, err := strconv.Atoi(countInput)
	if err != nil || count < 1 {
		fmt.Println("Invalid number. Please enter a positive integer.")
		os.Exit(1)
	}

	fmt.Printf("\nPreparing to send %d transactions of %.4f MON to all addresses\n", count, amount)

	chain.Monad(amount, count)
}