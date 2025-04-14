package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/joho/godotenv"
)

const (
	RPC_URL  = "https://testnet-rpc.monad.xyz"
	CHAIN_ID = 10143
)

type Config struct {
	PrivateKey      string
	ReceiverAddress string
	AmountMON       float64
	TxCount         int
	DelaySeconds    int
	MaxFeeMultiplier float64
	MaxAttempts     int
}

func isValidPrivateKey(key string) bool {
	match, _ := regexp.MatchString("^[a-fA-F0-9]{64}$", key)
	return match
}

func loadConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf("error loading .env file: %v", err)
	}

	privateKey := os.Getenv("PRIVATE_KEY")
	if privateKey == "" {
		return nil, fmt.Errorf("PRIVATE_KEY is required in .env")
	}

	if !isValidPrivateKey(privateKey) {
		return nil, fmt.Errorf("invalid PRIVATE_KEY format - must be 64-character hex string")
	}

	amount, err := strconv.ParseFloat(os.Getenv("AMOUNT_MON"), 64)
	if err != nil {
		return nil, fmt.Errorf("invalid AMOUNT_MON in .env: %v", err)
	}

	txCount := 1
	if tc := os.Getenv("TX_COUNT"); tc != "" {
		txCount, err = strconv.Atoi(tc)
		if err != nil {
			return nil, fmt.Errorf("invalid TX_COUNT in .env: %v", err)
		}
	}

	delay := 0
	if dl := os.Getenv("DELAY_SECONDS"); dl != "" {
		delay, err = strconv.Atoi(dl)
		if err != nil {
			return nil, fmt.Errorf("invalid DELAY_SECONDS in .env: %v", err)
		}
	}

	maxFeeMultiplier := 1.5 
	if mfm := os.Getenv("MAX_FEE_MULTIPLIER"); mfm != "" {
		maxFeeMultiplier, err = strconv.ParseFloat(mfm, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_FEE_MULTIPLIER in .env: %v", err)
		}
	}

	maxAttempts := 5
	if ma := os.Getenv("MAX_ATTEMPTS"); ma != "" {
		maxAttempts, err = strconv.Atoi(ma)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_ATTEMPTS in .env: %v", err)
		}
	}

	receiver := os.Getenv("RECEIVER_ADDRESS")
	if receiver == "" {
		return nil, fmt.Errorf("RECEIVER_ADDRESS is required in .env")
	}

	return &Config{
		PrivateKey:      privateKey,
		ReceiverAddress: receiver,
		AmountMON:       amount,
		TxCount:         txCount,
		DelaySeconds:    delay,
		MaxFeeMultiplier: maxFeeMultiplier,
		MaxAttempts:     maxAttempts,
	}, nil
}

func printTransactionDetails(txHash common.Hash, amount float64, receiver string, gasFeeCap, gasTipCap *big.Int) {
	fmt.Printf("%-20s: %-20.2f MON \n", "Amount Sent", amount)
	fmt.Printf("%-20s: %-20s \n", "To Address", receiver)
	fmt.Printf("%-20s: %-20s \n", "Max Fee (GasFeeCap)", fmt.Sprintf("%s Gwei", weiToGwei(gasFeeCap)))
	fmt.Printf("%-20s: %-20s \n", "Priority Fee (GasTipCap)", fmt.Sprintf("%s Gwei", weiToGwei(gasTipCap)))
	fmt.Printf("%-20s: %-20s \n", "Tx Hash", txHash.Hex())
	fmt.Printf("%-20s: %-20s \n", "Explorer Link", fmt.Sprintf("https://testnet.monadexplorer.com/tx/%s", txHash.Hex()))
	fmt.Println("──────────────────────────────────────────────")
}

func weiToGwei(wei *big.Int) string {
	gwei := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e9))
	return gwei.Text('f', 2)
}

func sendTransaction(client *ethclient.Client, privateKey *ecdsa.PrivateKey, toAddress common.Address, amountWei *big.Int, config *Config, nonce uint64) (common.Hash, error) {
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

		baseFee := new(big.Int).Mul(head.BaseFee, big.NewInt(int64(config.MaxFeeMultiplier * 100)))
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

		txHash = signedTx.Hash()
		printTransactionDetails(txHash, config.AmountMON, config.ReceiverAddress, gasFeeCap, gasTipCap)
		return txHash, nil
	}

	return txHash, fmt.Errorf("max attempts reached, last error: %v", lastErr)
}

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Config error: %v", err)
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

	amountWei := new(big.Int).Mul(
		big.NewInt(int64(config.AmountMON*1e6)),
		big.NewInt(1e12),
	)

	receiverAddress := common.HexToAddress(config.ReceiverAddress)

	fmt.Printf("Starting to send %d transactions of %.2f MON each\n", config.TxCount, config.AmountMON)
	fmt.Printf("From: %s\n", fromAddress.Hex())
	fmt.Printf("To: %s\n", config.ReceiverAddress)
	fmt.Printf("Max fee multiplier: %.2fx\n", config.MaxFeeMultiplier)
	fmt.Printf("Max attempts per tx: %d\n", config.MaxAttempts)
	fmt.Println("──────────────────────────────────────────────")

	for i := 1; i <= config.TxCount; i++ {
		nonce, err := client.PendingNonceAt(context.Background(), fromAddress)
		if err != nil {
			log.Printf("Failed to get nonce for transaction %d: %v", i, err)
			continue
		}

		_, err = sendTransaction(client, privateKey, receiverAddress, amountWei, config, nonce)
		if err != nil {
			log.Printf("Failed to send transaction %d: %v", i, err)
		} else {
			fmt.Printf("Transaction %d/%d completed successfully\n", i, config.TxCount)
		}

		if i < config.TxCount && config.DelaySeconds > 0 {
			fmt.Printf("Waiting %d seconds before next transaction...\n", config.DelaySeconds)
			time.Sleep(time.Duration(config.DelaySeconds) * time.Second)
		}
	}

	fmt.Println("\nAll transactions processed")
}