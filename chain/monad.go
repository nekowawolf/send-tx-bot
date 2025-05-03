package chain

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/joho/godotenv"
)

const (
	RPC_URL   = "https://testnet-rpc.monad.xyz"
	CHAIN_ID  = 10143
	MAX_RECIPIENTS = 50
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
		log.Fatalf("Config error: %v", err)
	}

	config.AmountMON = amount
	config.TxPerAddress = txPerAddress

	addresses := getReceiverAddresses()
	if len(addresses) == 0 {
		log.Fatal("No receiver addresses found in .env (RECEIVER_ADDRESS1 to RECEIVER_ADDRESS50)")
	}

	client, err := ethclient.Dial(RPC_URL)
	if err != nil {
		log.Fatalf("Failed to connect to RPC: %v", err)
	}
	defer client.Close()

	privateKey, err := crypto.HexToECDSA(config.PrivateKey)
	if err != nil {
		log.Fatalf("Invalid private key: %v", err)
	}

	publicKey := privateKey.Public()
	publicKeyECDSA := publicKey.(*ecdsa.PublicKey)
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	amountWei := convertMONtoWei(amount)

	fmt.Printf("\nStarting Transfer\n")
	fmt.Printf("From: %s\n", fromAddress.Hex())
	fmt.Printf("Total Recipients: %d\n", len(addresses))
	fmt.Printf("Transactions per address: %d\n", config.TxPerAddress)
	fmt.Printf("Amount per transaction: %.4f MON\n", amount)
	fmt.Printf("Max fee multiplier: %.2fx\n", config.MaxFeeMultiplier)
	fmt.Printf("Max attempts per tx: %d\n", config.MaxAttempts)
	if config.DelaySeconds > 0 {
		fmt.Printf("Delay between txs: %d seconds\n", config.DelaySeconds)
	}
	fmt.Println("──────────────────────────────────────────────")

	totalSuccess := 0
	totalFailed := 0

	for _, receiver := range addresses {
		fmt.Printf("\nProcessing address: %s\n", receiver.Hex())

		for i := 1; i <= config.TxPerAddress; i++ {
			nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
			if err != nil {
				log.Printf("Failed to get nonce for transaction %d: %v", i, err)
				totalFailed++
				continue
			}

			txHash, err := sendTransaction(client, privateKey, receiver, amountWei, config, nonce)
			if err != nil {
				log.Printf("Failed to send transaction %d: %v", i, err)
				totalFailed++
			} else {
				printTransactionDetails(txHash, amount, receiver.Hex())
				fmt.Printf("Transaction %d/%d completed\n", i, config.TxPerAddress)
				totalSuccess++
			}

			if i < config.TxPerAddress && config.DelaySeconds > 0 {
				time.Sleep(time.Duration(config.DelaySeconds) * time.Second)
			}
		}
	}

	fmt.Printf("\nTransfer Summary\n")
	fmt.Printf("Total Success: %d\n", totalSuccess)
	fmt.Printf("Total Failed: %d\n", totalFailed)
	fmt.Println("──────────────────────────────────────────────")
}

func sendTransaction(client *ethclient.Client, privateKey *ecdsa.PrivateKey, toAddress common.Address, amountWei *big.Int, config *TxConfig, nonce uint64) (common.Hash, error) {
	var txHash common.Hash
	var lastErr error

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
			Gas:       21000,
			GasTipCap: gasTipCap,
			GasFeeCap: gasFeeCap,
		})

		signedTx, err := types.SignTx(tx, types.NewLondonSigner(big.NewInt(CHAIN_ID)), privateKey)
		if err != nil {
			lastErr = fmt.Errorf("failed to sign transaction (attempt %d): %v", attempt, err)
			continue
		}

		err = client.SendTransaction(context.Background(), signedTx)
		if err != nil {
			lastErr = fmt.Errorf("failed to send transaction (attempt %d): %v", attempt, err)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}

		return signedTx.Hash(), nil
	}

	return txHash, fmt.Errorf("max attempts reached, last error: %v", lastErr)
}

func convertMONtoWei(amountMON float64) *big.Int {
	amount := new(big.Float).Mul(big.NewFloat(amountMON), big.NewFloat(1e18))
	wei := new(big.Int)
	amount.Int(wei)
	return wei
}

func printTransactionDetails(txHash common.Hash, amount float64, receiver string) {
	fmt.Println("──────────────────────────────────────────────")
	fmt.Printf("%-20s: %.4f MON\n", "Amount Sent", amount)
	fmt.Printf("%-20s: %s\n", "To Address", receiver)
	fmt.Printf("%-20s: %s\n", "Tx Hash", txHash.Hex())
	fmt.Printf("%-20s: %s\n", "Explorer Link", fmt.Sprintf("https://testnet.monadexplorer.com/tx/%s", txHash.Hex()))
}