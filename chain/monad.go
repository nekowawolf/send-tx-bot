package chain

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/fatih/color"
	"github.com/joho/godotenv"
)

const (
	RPC_URL                  = "https://testnet-rpc.monad.xyz"
	CHAIN_ID                 = 10143
	MAX_RECIPIENTS           = 50
	GAS_LIMIT_BUFFER_PERCENT = 10
)

var (
	cyan    = color.New(color.FgCyan).SprintFunc()
	yellow  = color.New(color.FgYellow).SprintFunc()
	green   = color.New(color.FgGreen).SprintFunc()
	red     = color.New(color.FgRed).SprintFunc()
	magenta = color.New(color.FgMagenta).SprintFunc()
	blue    = color.New(color.FgBlue).SprintFunc()
)

type TxConfig struct {
	PrivateKey       string
	AmountMON        float64
	TxPerAddress     int
	DelaySeconds     int
	MaxFeeMultiplier float64
	MaxAttempts      int
}

func loadConfig() (*TxConfig, error) {
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf("error loading .env file: %v", err)
	}

	privateKey := os.Getenv("PRIVATE_KEY")
	if privateKey == "" {
		return nil, fmt.Errorf("PRIVATE_KEY is required in .env")
	}

	delay := 2
	if dl := os.Getenv("DELAY_SECONDS"); dl != "" {
		var err error
		delay, err = strconv.Atoi(dl)
		if err != nil {
			return nil, fmt.Errorf("invalid DELAY_SECONDS in .env: %v", err)
		}
	}

	maxFeeMultiplier := 1.5
	if mfm := os.Getenv("MAX_FEE_MULTIPLIER"); mfm != "" {
		var err error
		maxFeeMultiplier, err = strconv.ParseFloat(mfm, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_FEE_MULTIPLIER in .env: %v", err)
		}
	}

	maxAttempts := 5
	if ma := os.Getenv("MAX_ATTEMPTS"); ma != "" {
		var err error
		maxAttempts, err = strconv.Atoi(ma)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_ATTEMPTS in .env: %v", err)
		}
	}

	return &TxConfig{
		PrivateKey:       privateKey,
		MaxFeeMultiplier: maxFeeMultiplier,
		MaxAttempts:      maxAttempts,
		DelaySeconds:     delay,
	}, nil
}

func MonadSend() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter amount to send to each address (in MON): ")
	amountInput, _ := reader.ReadString('\n')
	amountInput = strings.TrimSpace(amountInput)

	amount, err := strconv.ParseFloat(amountInput, 64)
	if err != nil || amount <= 0 {
		fmt.Println(red("Invalid amount. Please enter a positive number."))
		os.Exit(1)
	}

	fmt.Print("Enter number of transactions per address: ")
	countInput, _ := reader.ReadString('\n')
	countInput = strings.TrimSpace(countInput)

	count, err := strconv.Atoi(countInput)
	if err != nil || count < 1 {
		fmt.Println(red("Invalid number. Please enter a positive integer."))
		os.Exit(1)
	}

	fmt.Printf("\n%s %d transactions of %.4f MON to all addresses\n",
		cyan("Preparing to send"), count, amount)

	Monad(amount, count)
}

func getReceiverAddresses() []common.Address {
	var addresses []common.Address
	for i := 1; i <= MAX_RECIPIENTS; i++ {
		envVar := fmt.Sprintf("RECEIVER_ADDRESS%d", i)
		if addr := os.Getenv(envVar); addr != "" {
			addresses = append(addresses, common.HexToAddress(addr))
		}
	}
	return addresses
}

func Monad(amount float64, txPerAddress int) {
	config, err := loadConfig()
	if err != nil {
		log.Fatalf(red("Config error: %v"), err)
	}

	config.AmountMON = amount
	config.TxPerAddress = txPerAddress

	addresses := getReceiverAddresses()
	if len(addresses) == 0 {
		log.Fatal(red("No receiver addresses found in .env (RECEIVER_ADDRESS1 to RECEIVER_ADDRESS50)"))
	}

	client, err := ethclient.Dial(RPC_URL)
	if err != nil {
		log.Fatalf(red("Failed to connect to RPC: %v"), err)
	}
	defer client.Close()

	privateKey, err := crypto.HexToECDSA(config.PrivateKey)
	if err != nil {
		log.Fatalf(red("Invalid private key: %v"), err)
	}

	publicKey := privateKey.Public()
	publicKeyECDSA := publicKey.(*ecdsa.PublicKey)
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	amountWei := convertMONtoWei(amount)
	fmt.Printf("%s: %s\n", cyan("From"), fromAddress.Hex())
	fmt.Printf("%s: %d\n", cyan("Total Recipients"), len(addresses))
	fmt.Printf("%s: %d\n", cyan("Transactions per address"), config.TxPerAddress)
	fmt.Printf("%s: %.4f MON\n", cyan("Amount per transaction"), amount)
	if config.DelaySeconds > 0 {
		fmt.Printf("%s: %d seconds\n", cyan("Delay between txs"), config.DelaySeconds)
	}
	fmt.Println("\n▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔")

	totalSuccess := 0
	totalFailed := 0

	for _, receiver := range addresses {
		fmt.Printf("\n%s: %s\n", cyan("Processing address"), receiver.Hex())

		for i := 1; i <= config.TxPerAddress; i++ {
			nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
			if err != nil {
				log.Printf(red("Failed to get nonce for transaction %d: %v"), i, err)
				totalFailed++
				continue
			}

			txHash, err := sendTransaction(client, privateKey, receiver, amountWei, config, nonce)
			if err != nil {
				log.Printf(red("Failed to send transaction %d: %v"), i, err)
				totalFailed++
			} else {
				printTransactionDetails(client, txHash, amount, receiver.Hex())
				fmt.Printf(green("Transaction %d/%d completed\n"), i, config.TxPerAddress)
				totalSuccess++
				fmt.Println("\n▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔")
			}

			if i < config.TxPerAddress && config.DelaySeconds > 0 {
				time.Sleep(time.Duration(config.DelaySeconds) * time.Second)
			}
		}
	}

	fmt.Printf("\n%s\n", cyan("Transfer Summary"))
	fmt.Printf("%s: %d\n", green("Total Success"), totalSuccess)
	fmt.Printf("%s: %d\n", red("Total Failed"), totalFailed)
	fmt.Println("\nFollow X : 0xNekowawolf\n")
}

func estimateGasLimit(client *ethclient.Client, from common.Address, to common.Address, value *big.Int) (uint64, error) {
	msg := ethereum.CallMsg{
		From:  from,
		To:    &to,
		Value: value,
	}
	gasLimit, err := client.EstimateGas(context.Background(), msg)
	if err != nil {
		return 0, fmt.Errorf("failed to estimate gas: %v", err)
	}

	gasLimitWithBuffer := gasLimit * (100 + GAS_LIMIT_BUFFER_PERCENT) / 100
	return gasLimitWithBuffer, nil
}

func sendTransaction(client *ethclient.Client, privateKey *ecdsa.PrivateKey, toAddress common.Address, amountWei *big.Int, config *TxConfig, nonce uint64) (common.Hash, error) {
	var txHash common.Hash
	var lastErr error

	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	gasLimit, err := estimateGasLimit(client, fromAddress, toAddress, amountWei)
	if err != nil {
		return txHash, fmt.Errorf("failed to estimate gas limit: %v", err)
	}

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		gasTipCap, err := client.SuggestGasTipCap(context.Background())
		if err != nil {
			lastErr = fmt.Errorf("failed to get gas tip cap (attempt %d): %v", attempt, err)
			time.Sleep(2 * time.Second)
			continue
		}

		head, err := client.HeaderByNumber(context.Background(), nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to get chain head (attempt %d): %v", attempt, err)
			time.Sleep(2 * time.Second)
			continue
		}

		baseFee := new(big.Int).Mul(head.BaseFee, big.NewInt(int64(config.MaxFeeMultiplier*100)))
		baseFee = baseFee.Div(baseFee, big.NewInt(100))
		gasFeeCap := new(big.Int).Add(gasTipCap, baseFee)

		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   big.NewInt(CHAIN_ID),
			Nonce:     nonce,
			To:        &toAddress,
			Value:     amountWei,
			Gas:       gasLimit,
			GasTipCap: gasTipCap,
			GasFeeCap: gasFeeCap,
		})

		signedTx, err := types.SignTx(tx, types.NewLondonSigner(big.NewInt(CHAIN_ID)), privateKey)
		if err != nil {
			lastErr = fmt.Errorf("failed to sign transaction (attempt %d): %v", attempt, err)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}

		err = client.SendTransaction(context.Background(), signedTx)
		if err != nil {
			lastErr = fmt.Errorf("failed to send transaction (attempt %d): %v", attempt, err)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}

		txHash = signedTx.Hash()
		if err := waitForTransactionConfirmation(client, txHash, 3, 5); err != nil {
			lastErr = fmt.Errorf("transaction not mined (attempt %d): %v", attempt, err)
			continue
		}

		return txHash, nil
	}

	return txHash, fmt.Errorf("max attempts reached, last error: %v", lastErr)
}

func waitForTransactionConfirmation(client *ethclient.Client, txHash common.Hash, maxAttempts int, delaySeconds int) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, isPending, err := client.TransactionByHash(context.Background(), txHash)
		if err == nil && !isPending {
			return nil
		}

		if err != nil {
			log.Printf(yellow("Transaction status check failed (attempt %d): %v"), attempt, err)
		} else if isPending {
			log.Printf(yellow("Transaction still pending (attempt %d)"), attempt)
		}

		time.Sleep(time.Duration(delaySeconds) * time.Second)
	}
	return fmt.Errorf("transaction not confirmed after %d attempts", maxAttempts)
}

func convertMONtoWei(amountMON float64) *big.Int {
	amount := new(big.Float).Mul(big.NewFloat(amountMON), big.NewFloat(1e18))
	wei := new(big.Int)
	amount.Int(wei)
	return wei
}

func printTransactionDetails(client *ethclient.Client, txHash common.Hash, amount float64, receiver string) {
	tx, isPending, err := client.TransactionByHash(context.Background(), txHash)
	status := green("Confirmed")
	if err != nil {
		status = red(fmt.Sprintf("Error: %v", err))
	} else if isPending {
		status = yellow("Pending")
	}

	var feeStr string
	if tx != nil {
		receipt, err := bind.WaitMined(context.Background(), client, tx)
		if err == nil && receipt != nil {
			var gasPrice *big.Int
			if tx.Type() == types.DynamicFeeTxType {
				gasPrice = receipt.EffectiveGasPrice
			} else {
				gasPrice = tx.GasPrice()
			}

			fee := new(big.Float).Quo(
				new(big.Float).SetInt(
					new(big.Int).Mul(
						new(big.Int).SetUint64(receipt.GasUsed),
						gasPrice,
					),
				),
				new(big.Float).SetInt(big.NewInt(1e18)),
			)
			feeStr = fmt.Sprintf("%.6f MON", fee)
		} else {
			feeStr = yellow("Waiting for receipt...")
		}
	} else {
		feeStr = red("N/A")
	}

	fmt.Printf("%-20s: %s\n", cyan("Amount Sent"), magenta(fmt.Sprintf("%.4f MON", amount)))
	fmt.Printf("%-20s: %s\n", cyan("To Address"), receiver)
	fmt.Printf("%-20s: %s\n", cyan("Status"), status)
	fmt.Printf("%-20s: %s\n", cyan("Tx Hash"), yellow(txHash.Hex()))
	fmt.Printf("%-20s: %s\n", cyan("Fee"), yellow(feeStr))
	fmt.Printf("%-20s: %s\n", cyan("Explorer Link"), blue(fmt.Sprintf("https://testnet.monadexplorer.com/tx/%s", txHash.Hex())))
}
