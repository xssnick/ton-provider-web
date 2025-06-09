package backend

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/xssnick/ton-provider-web/internal/backend/db"
	"github.com/xssnick/ton-provider-web/internal/backend/storage"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-storage-provider/pkg/contract"
	"github.com/xssnick/tonutils-storage-provider/pkg/transport"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Service struct {
	db             *db.Database
	storageBaseDir string
	stg            *storage.Client
	logger         zerolog.Logger
	api            ton.APIClientWrapped
	freeStore      time.Duration

	providerKey []byte
	provider    *transport.Client
}

func NewService(db *db.Database, api ton.APIClientWrapped, provider *transport.Client, providerKey []byte, stg *storage.Client, storageBaseDir string, logger zerolog.Logger) *Service {
	path, err := filepath.Abs(storageBaseDir)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to get absolute path to storage directory")
		return nil
	}
	logger.Info().Str("path", path).Msg("Storage directory")

	s := &Service{
		db:             db,
		stg:            stg,
		api:            api,
		storageBaseDir: path,
		provider:       provider,
		providerKey:    providerKey,
		freeStore:      15 * time.Minute,
		logger:         logger,
	}
	go s.worker()
	return s
}

type UserFileInfo struct {
	FileName  string    `json:"file_name"`
	CreatedAt time.Time `json:"created_at"`
	Size      string    `json:"size"`
	Status    string    `json:"status"`
	BagID     string    `json:"bag_id"`

	ExpireAt *time.Time `json:"expire_at"`

	PricePerDay     string `json:"price_per_day"`
	ProviderStatus  string `json:"provider_status"`
	ProviderReason  string `json:"provider_reason"`
	ContractBalance string `json:"contract_balance"`
	ContractAddr    string `json:"contract_addr"`
	TimeLeft        string `json:"time_left"`
}

func (s *Service) ListFilesByUser(userAddr string) ([]UserFileInfo, error) {
	// Retrieve file information from the database for the given user address
	files, err := s.db.GetFilesByUser(userAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve files for user %s: %w", userAddr, err)
	}

	fileKeys := make([]string, 0, len(files))
	userFiles := make([]UserFileInfo, 0, len(files))
	for _, file := range files {
		var expireAt *time.Time
		if file.State <= db.FileStateBag {
			// should be removed
			at := file.CreatedAt.Add(s.freeStore)
			if at.Before(time.Now()) {
				continue
			}
			expireAt = &at
		}

		// Convert db.FileInfo into UserFileInfo
		userFile := UserFileInfo{
			FileName:     file.FilePath,
			CreatedAt:    file.CreatedAt,
			Status:       map[int]string{0: "processing", 1: "deploy", 2: "stored"}[file.State],
			ContractAddr: file.ContractAddr,
			ExpireAt:     expireAt,
		}

		if file.State >= db.FileStateBag {
			userFile.Size = toSz(file.Bag.FullSize)
			userFile.BagID = hex.EncodeToString(file.Bag.RootHash)
		}

		if file.State >= db.FileStateStored {
			userFile.ProviderStatus = file.Provider.Status
			userFile.ProviderReason = file.Provider.Reason
			userFile.ContractBalance = file.Provider.Balance
			userFile.PricePerDay = file.Provider.PerDay
			userFile.TimeLeft = file.Provider.Left
		}
		userFiles = append(userFiles, userFile)
		fileKeys = append(fileKeys, file.FilePath)
	}

	if err := s.db.RefreshUserIfNeeded(userAddr, fileKeys, 5); err != nil {
		s.logger.Warn().Err(err).Msg("failed to refresh user files")
	}

	sort.Slice(userFiles, func(i, j int) bool {
		return userFiles[i].CreatedAt.After(userFiles[j].CreatedAt)
	})

	return userFiles, nil
}

type ContractDeployData struct {
	ContractAddr string `json:"contract_addr"`
	PerDay       string `json:"per_day"`
	PerProof     string `json:"per_proof"`
	ProofEvery   string `json:"proof_every"`
	StateInit    []byte `json:"state_init"`
	Body         []byte `json:"body"`
}

type ContractWithdrawData struct {
	ContractAddr string `json:"contract_addr"`
	Body         []byte `json:"body"`
}

type ContractTopupData struct {
	ContractAddr string `json:"contract_addr"`
}

func (s *Service) GetWithdrawData(ctx context.Context, userAddr, fileName string) (*ContractWithdrawData, error) {
	fi, err := s.db.GetFile(userAddr, fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	if fi.State != db.FileStateStored {
		return nil, fmt.Errorf("contract not yet deployed")
	}

	addr, body, err := s.getContractWithdrawData(fi.Bag, address.MustParseAddr(fi.OwnerAddr))
	if err != nil {
		return nil, fmt.Errorf("failed to get contract withdraw data: %w", err)
	}

	return &ContractWithdrawData{
		ContractAddr: addr.String(),
		Body:         body.ToBOC(),
	}, nil
}

func (s *Service) GetTopupData(ctx context.Context, userAddr, fileName string) (*ContractTopupData, error) {
	fi, err := s.db.GetFile(userAddr, fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	if fi.State != db.FileStateStored {
		return nil, fmt.Errorf("contract not yet deployed")
	}

	addr, _, err := s.getContractWithdrawData(fi.Bag, address.MustParseAddr(fi.OwnerAddr))
	if err != nil {
		return nil, fmt.Errorf("failed to get contract topup data: %w", err)
	}

	return &ContractTopupData{
		ContractAddr: addr.String(),
	}, nil
}

func (s *Service) GetDeployData(ctx context.Context, userAddr, fileName string) (*ContractDeployData, error) {
	fi, err := s.db.GetFile(userAddr, fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	if fi == nil {
		return nil, fmt.Errorf("file not found")
	}

	if fi.State != db.FileStateBag {
		return nil, fmt.Errorf("deploy not yet required")
	}

	off, addr, si, body, err := s.getContractDeployData(ctx, fi.Bag, address.MustParseAddr(fi.OwnerAddr), s.providerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get contract deploy data: %w", err)
	}

	return &ContractDeployData{
		ContractAddr: addr.String(),
		PerDay:       tlb.FromNanoTON(off.PerDayNano).String(),
		PerProof:     tlb.FromNanoTON(off.PerProofNano).String(),
		ProofEvery:   off.Every,
		StateInit:    si.ToBOC(),
		Body:         body.ToBOC(),
	}, nil
}

func (s *Service) RemoveFile(userAddr, fileName string) error {
	existingFile, err := s.db.GetFile(userAddr, fileName)
	if err == nil && existingFile == nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to check file existence: %w", err)
	}

	if existingFile.State >= db.FileStateStored {
		return fmt.Errorf("file is paid and stored at provider")
	}

	if err := s.db.CreateCleanTask(userAddr, fileName); err != nil {
		return fmt.Errorf("failed to store file metadata in database: %w", err)
	}

	return nil
}

func (s *Service) StoreFile(fileReader io.Reader, userAddr, fileName string) error {
	// Ensure the storage directory exists.
	if err := os.MkdirAll(filepath.Join(s.storageBaseDir, userAddr), os.ModePerm); err != nil {
		return err
	}

	if len(fileName) > 1000 {
		return fmt.Errorf("file name too long")
	}

	cleanName := filepath.Base(filepath.Clean(fileName))

	// Validate the fileName to prevent vulnerabilities like directory traversal.
	if cleanName == "." || cleanName == "" ||
		strings.Contains(cleanName, "..") ||
		strings.ContainsRune(cleanName, os.PathSeparator) {
		return fmt.Errorf("invalid file name: %s", fileName)
	}

	// Define the full path for the file.
	fullFilePath := filepath.Join(s.storageBaseDir, userAddr, cleanName)

	files, err := s.db.GetFilesByUser(userAddr)
	if err != nil {
		return fmt.Errorf("failed to retrieve files for user %s: %w", userAddr, err)
	}

	numPending := 0
	for _, file := range files {
		if file.Provider == nil || file.Provider.Status == "error" {
			numPending++
		}

		if numPending >= 3 {
			return fmt.Errorf("too many pending files")
		}
	}

	// Create and open the file on disk.
	file, err := os.Create(fullFilePath)
	if err != nil {
		return fmt.Errorf("failed to create file on disk: %w", err)
	}
	defer file.Close()

	// Write the content to the file from the io.Reader.
	if _, err := io.Copy(file, fileReader); err != nil {
		return fmt.Errorf("failed to write file content to disk: %w", err)
	}

	fileData := db.FileInfo{
		OwnerAddr: userAddr,
		FilePath:  cleanName,
		CreatedAt: time.Now(),
		State:     db.FileStateNew,
	}

	if err := s.db.StoreFileInfo(userAddr, fileData); err != nil {
		return fmt.Errorf("failed to store file metadata in database: %w", err)
	}

	return nil
}

func (s *Service) doStore() {
	storeList, err := s.db.GetPendingStoreTasks()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get pending tasks")
		return
	}

	for _, key := range storeList {
		fi, err := s.db.GetFileByKey(key)
		if err != nil {
			s.logger.Error().Err(err).Str("key", key).Msg("failed to get file data")
			continue
		}

		fullFilePath := filepath.Join(s.storageBaseDir, fi.OwnerAddr, fi.FilePath)

		id, err := s.stg.CreateBag(context.Background(), fullFilePath, fi.FilePath, nil)
		if err != nil {
			s.logger.Error().Err(err).Str("key", key).Msg("failed to create bag")
			continue
		}

		details, err := s.stg.GetBag(context.Background(), id)
		if err != nil {
			s.logger.Error().Err(err).Str("key", key).Msg("failed to get bag details")
			continue
		}

		b := db.Bag{
			RootHash:   mustHexDecode(details.BagID),
			MerkleHash: mustHexDecode(details.MerkleHash),
			FullSize:   details.Size + details.HeaderSize, // TODO: file size not full bag
			PieceSize:  details.PieceSize,
			CreatedAt:  time.Now(),
		}

		addr, err := s.calcContractAddr(&b, address.MustParseAddr(fi.OwnerAddr))
		if err != nil {
			s.logger.Error().Err(err).Str("key", key).Msg("failed to get contract deploy data")
			continue
		}

		remove, err := s.db.CompleteStoreTask(key, b, addr.String(), s.freeStore)
		if err != nil {
			s.logger.Error().Err(err).Str("key", key).Msg("failed to complete task")
		}

		if remove {
			if err = os.Remove(fullFilePath); err != nil {
				s.logger.Error().Err(err).Str("key", key).Msg("failed to remove file")
			}
		}
	}
}

func (s *Service) doCleanup() {
	list, err := s.db.GetPendingCleanupTasks()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get pending cleanup tasks")
		return
	}

	for _, t := range list {
		fi, err := s.db.GetFileByKey(t.Key)
		if err != nil {
			s.logger.Error().Err(err).Str("key", t.Key).Msg("failed to get file data")
			continue
		}

		rm := t.Force
		if fi != nil {
			if fi.State <= db.FileStateBag {
				rm = true
			}
		}

		del, err := s.db.CompleteCleanTask(t.Key, rm)
		if err != nil {
			s.logger.Error().Err(err).Str("key", t.Key).Msg("failed to complete task")
			continue
		}

		if del && fi != nil {
			// we remove after, because remove before is bad, and in case of our fail not so critical
			if err = s.stg.RemoveBag(context.Background(), fi.Bag.RootHash, true); err != nil {
				s.logger.Error().Err(err).Hex("id", fi.Bag.RootHash).Str("key", t.Key).Msg("failed to remove bag")
				continue
			}
		}
	}
}

func (s *Service) doUpdate() {
	list, err := s.db.GetPendingUpdateTasks()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get pending update tasks")
		return
	}

	var toUpd []db.UpdateTaskResult
	for _, task := range list {
		func() {
			nextAt := time.Now().Add(time.Second * 15)
			res := db.UpdateTaskResult{
				UpdateTask: task,
				NextExecAt: &nextAt,
			}

			defer func() {
				if task.ExecAt.Unix() == 0 {
					// not repeating immediate tasks
					res.NextExecAt = nil
				}

				toUpd = append(toUpd, res)
			}()

			fi, err := s.db.GetFileByKey(res.Key)
			if err != nil {
				s.logger.Error().Err(err).Str("key", res.Key).Msg("failed to get file data")
				return
			}

			if fi == nil {
				res.NextExecAt = nil
				s.logger.Debug().Str("key", res.Key).Msg("file not found")
				return
			}
			if fi.Bag == nil {
				s.logger.Debug().Str("key", res.Key).Msg("bag not found, try later")
				return
			}
			res.ProviderInfo = fi.Provider

			details, err := s.stg.GetBag(context.Background(), fi.Bag.RootHash)
			if err != nil && !errors.Is(err, storage.ErrNotFound) {
				s.logger.Error().Err(err).Str("key", res.Key).Msg("failed to get bag details")
				return
			}

			if details == nil {
				if err = s.db.CreateCleanTaskByKey(res.Key); err != nil {
					s.logger.Error().Err(err).Str("key", res.Key).Msg("failed to create clean task")
					return
				}

				res.NextExecAt = nil
				s.logger.Debug().Str("key", res.Key).Msg("bag not found anymore, removing")
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
			balance, toProof, perDay, left, err := s.fetchContractInfo(ctx, fi.Bag, address.MustParseAddr(fi.OwnerAddr), s.providerKey)
			cancel()
			if err != nil {
				if errors.Is(err, contract.ErrProviderNotFound) || errors.Is(err, contract.ErrNotDeployed) {
					s.logger.Debug().Str("key", res.Key).Msg("no contract for provider yet")

					if fi.State >= db.FileStateStored {
						// already had provider info, so provider or contract removed
						if err = s.db.CreateCleanTaskByKey(res.Key); err != nil {
							s.logger.Error().Err(err).Str("key", res.Key).Msg("failed to create clean task")
							return
						}

						res.NextExecAt = nil
						s.logger.Debug().Str("key", res.Key).Msg("provider contract not found anymore, removing")
						return
					}

					return
				}
				s.logger.Debug().Err(err).Str("key", res.Key).Msg("failed to get contract info")
				return
			}
			s.logger.Debug().Str("key", res.Key).Time("at", res.ExecAt).Msgf("contract fetched, balance: %s", balance.String())

			ctx, cancel = context.WithTimeout(context.Background(), 7*time.Second)
			info, err := s.provider.RequestStorageInfo(ctx, s.providerKey, address.MustParseAddr(fi.ContractAddr), toProof)
			cancel()
			if err != nil {
				s.logger.Warn().Err(err).Str("key", res.Key).Msg("failed to get storage info")
				return
			}

			var errorSince *time.Time
			if info.Status == "error" {
				if info.Reason != "internal provider error" {
					if fi.Provider != nil && fi.Provider.ErrorSince != nil && time.Since(*fi.Provider.ErrorSince) > s.freeStore {
						s.logger.Debug().Str("key", res.Key).Msg("provider is not agrees, removing")
						if err = s.db.CreateCleanTaskByKey(res.Key); err != nil {
							s.logger.Error().Err(err).Str("key", res.Key).Msg("failed to create clean task")
						}
						res.NextExecAt = nil
						return
					}

					if fi.Provider != nil && fi.Provider.ErrorSince != nil {
						errorSince = fi.Provider.ErrorSince
					} else {
						tm := time.Now()
						errorSince = &tm
					}
				}

				snc := time.Now()
				if errorSince != nil {
					snc = *errorSince
				}

				s.logger.Warn().Str("key", res.Key).Str("for", time.Since(snc).String()).Str("reason", info.Reason).Msg("provider error")
			}

			nextAt = time.Now().Add(time.Minute * 5)
			res.NextExecAt = &nextAt

			res.ProviderInfo = &db.ProviderInfo{
				PerDay:      perDay.String(),
				Balance:     balance.String(),
				Status:      info.Status,
				Reason:      info.Reason,
				LastUpdated: time.Now(),
				ErrorSince:  errorSince,
				Left:        left,
			}
		}()
	}

	if err = s.db.CompleteUpdateTasks(toUpd); err != nil {
		s.logger.Error().Err(err).Msg("failed to complete update tasks")
		return
	}
}

func (s *Service) worker() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.doStore()
			s.doCleanup()
			s.doUpdate()
		}
	}
}

func mustHexDecode(s string) []byte {
	v, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return v
}

func toSz(sz uint64) string {
	switch {
	case sz < 1024:
		return fmt.Sprintf("%d Bytes", sz)
	case sz < 1024*1024:
		return fmt.Sprintf("%.2f KB", float64(sz)/1024)
	case sz < 1024*1024*1024:
		return fmt.Sprintf("%.2f MB", float64(sz)/(1024*1024))
	default:
		return fmt.Sprintf("%.2f GB", float64(sz)/(1024*1024*1024))
	}
}
