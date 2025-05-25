package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"github.com/xssnick/ton-provider-web/internal/backend/storage"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-storage-provider/pkg/transport"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/xssnick/ton-provider-web/internal/backend"
	"github.com/xssnick/ton-provider-web/internal/backend/db"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

type Config struct {
	DBPath             string `json:"db_path"`
	StorageDir         string `json:"storage_dir"`
	ServerAddr         string `json:"server_addr"`
	MaxFileSize        uint64 `json:"max_file_size"`
	PrivateKey         []byte `json:"private_key"`
	VerificationDomain string `json:"verification_domain"`
	TonConfigURL       string `json:"ton_config_url"`

	StorageApiAddr     string `json:"storage_api_addr"`
	StorageApiLogin    string `json:"storage_api_login"`
	StorageApiPassword string `json:"storage_api_password"`
	ProviderKeyHex     string `json:"provider_key_hex"`
}

const configFile = "./config.json"

func main() {
	// Configure logger
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

	// Load or generate configuration
	cfg, err := loadOrGenerateConfig(configFile, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load or generate configuration")
	}

	// Database initialization
	database, err := db.NewDatabase(cfg.DBPath, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize database")
	}
	defer func() {
		if err := database.Close(); err != nil {
			logger.Error().Err(err).Msg("Failed to close database")
		}
	}()

	lsCfg, err := liteclient.GetConfigFromUrl(context.Background(), cfg.TonConfigURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load TON config")
		return
	}

	// TON Connection
	client := liteclient.NewConnectionPool()
	err = client.AddConnectionsFromConfig(context.Background(), lsCfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to add connections from TON config")
		return
	}

	// Initialize TON API client with the given Proof Check Policy
	api := ton.NewAPIClient(client, ton.ProofCheckPolicyFast).WithRetry()

	dl, err := adnl.DefaultListener(":")
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create ADNL listener")
		return
	}
	netMgr := adnl.NewMultiNetReader(dl)

	_, adnlKey, _ := ed25519.GenerateKey(nil)
	_, dhtKey, _ := ed25519.GenerateKey(nil)

	gwDht := adnl.NewGatewayWithNetManager(dhtKey, netMgr)
	if err = gwDht.StartClient(2); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start DHT ADNL client")
		return
	}

	dhtClient, err := dht.NewClientFromConfig(gwDht, lsCfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create DHT client")
		return
	}

	gw := adnl.NewGatewayWithNetManager(adnlKey, netMgr)
	if err = gw.StartClient(2); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start ADNL client")
		return
	}

	pcl := transport.NewClient(gw, dhtClient)

	storageClient := storage.NewClient(cfg.StorageApiAddr, &storage.Credentials{
		Login:    cfg.StorageApiLogin,
		Password: cfg.StorageApiPassword,
	}, logger)

	providerKey, err := hex.DecodeString(cfg.ProviderKeyHex)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to decode provider key")
		return
	}
	if len(providerKey) != 32 {
		logger.Fatal().Msg("Provider key must be 32 bytes long")
		return
	}

	// Service initialization
	service := backend.NewService(database, api, pcl, providerKey, storageClient, cfg.StorageDir, logger)

	// TON Connect Verifier initialization
	sessionDuration := 30 * time.Minute
	verifier := wallet.NewTonConnectVerifier(cfg.VerificationDomain, sessionDuration, api)

	// Server initialization
	go backend.Listen(ed25519.NewKeyFromSeed(cfg.PrivateKey), cfg.ServerAddr, cfg.MaxFileSize, service, verifier, logger)

	// Service is running
	logger.Info().Msg("Service initialized and server running")
	select {} // Keep the main thread alive
}

func loadOrGenerateConfig(path string, logger zerolog.Logger) (*Config, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		_, privateKey, err := ed25519.GenerateKey(nil)
		if err != nil {
			return nil, err
		}

		logger.Info().Msg("Config file not found, generating default config")
		defaultConfig := &Config{
			DBPath:             "./data/db",
			StorageDir:         "./data/storage",
			ServerAddr:         ":8080",
			MaxFileSize:        512 << 20,
			PrivateKey:         privateKey.Seed(),
			VerificationDomain: "example.com",
			TonConfigURL:       "https://ton-blockchain.github.io/global.config.json",
			StorageApiAddr:     "http://127.0.0.1:7711",
			StorageApiLogin:    "some_login",
			StorageApiPassword: "some_password",
			ProviderKeyHex:     "0000000000000000000000000000000000000000000000000000000000000000",
		}
		if err := saveConfig(path, defaultConfig, logger); err != nil {
			return nil, err
		}
		return defaultConfig, nil
	} else if err != nil {
		return nil, err
	}
	defer file.Close()

	config := &Config{}
	if err := json.NewDecoder(file).Decode(config); err != nil {
		return nil, err
	}

	return config, nil
}

func saveConfig(path string, config *Config, logger zerolog.Logger) error {
	file, err := os.Create(path)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create config file")
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		logger.Error().Err(err).Msg("Failed to encode config file")
		return err
	}
	return nil
}
