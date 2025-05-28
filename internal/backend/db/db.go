package db

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	FileStateNew = iota
	FileStateBag
	FileStateStored
)

// FileInfo represents the structure of the JSON object to be stored
type FileInfo struct {
	State int

	Key       string
	OwnerAddr string
	Bag       *Bag
	FilePath  string
	CreatedAt time.Time
	Provider  *ProviderInfo

	ContractAddr string
}

type Bag struct {
	RootHash   []byte
	MerkleHash []byte
	FullSize   uint64
	PieceSize  uint32
	CreatedAt  time.Time
}

type ProviderInfo struct {
	Balance     string
	PerDay      string
	Status      string
	Reason      string
	Left        string
	LastUpdated time.Time
	ErrorSince  *time.Time
}

type BagInfo struct {
	Usages   int
	FilePath string
}

// Database struct encapsulates the leveldb instance
type Database struct {
	db     *leveldb.DB
	logger zerolog.Logger

	mx sync.Mutex
}

// NewDatabase initializes and returns a new Database instance
func NewDatabase(dbPath string, logger zerolog.Logger) (*Database, error) {
	db, err := leveldb.OpenFile(dbPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open LevelDB database: %w", err)
	}
	return &Database{db: db, logger: logger}, nil
}

// SetChainScannerLT stores a uint64 value with the key "chain-lt"
func (d *Database) SetChainScannerLT(value uint64) error {
	key := "chain-lt"
	data := []byte(fmt.Sprint(value))

	if err := d.db.Put([]byte(key), data, nil); err != nil {
		d.logger.Error().Err(err).Msg("failed to store chain scanner LT")
		return fmt.Errorf("failed to store chain scanner LT: %w", err)
	}
	return nil
}

// GetChainScannerLT retrieves the uint64 value stored with the key "chain-lt"
func (d *Database) GetChainScannerLT() (uint64, error) {
	key := "chain-lt"

	data, err := d.db.Get([]byte(key), nil)
	if err != nil {
		if errors.Is(err, leveldb.ErrNotFound) {
			return 0, nil // Return default value if not found
		}
		d.logger.Error().Err(err).Msg("failed to retrieve chain scanner LT")
		return 0, fmt.Errorf("failed to retrieve chain scanner LT: %w", err)
	}

	value, err := strconv.ParseUint(string(data), 10, 64)
	if err != nil {
		d.logger.Error().Err(err).Str("value", string(data)).Msg("failed to parse chain scanner LT")
		return 0, fmt.Errorf("failed to parse chain scanner LT: %w", err)
	}
	return value, nil
}

func (d *Database) RefreshUserIfNeeded(userID string, updateKeys []string, gapSec int64) error {
	key := fmt.Sprintf("upd-user:%s", userID)

	d.mx.Lock()
	defer d.mx.Unlock()

	// Check if the key exists in the database
	val, err := d.db.Get([]byte(key), nil)
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		d.logger.Error().Err(err).Str("userID", userID).Str("key", key).Msg("failed to get upd key")
		return fmt.Errorf("failed to get upd key: %w", err)
	}

	var timeVal int64 = 0
	if len(val) > 0 {
		parsedTime, err := strconv.ParseInt(string(val), 10, 64)
		if err != nil {
			d.logger.Error().Err(err).Str("value", string(val)).Msg("failed to parse unix time")
			return fmt.Errorf("failed to parse unix time: %w", err)
		}
		timeVal = parsedTime
	}

	if timeVal > time.Now().Unix()-gapSec {
		return nil
	}

	batch := new(leveldb.Batch)

	// Add the update tasks to the batch
	for _, file := range updateKeys {
		batch.Put([]byte(fmt.Sprintf("update-task:%d:%s", 0, fileKey(userID, file))), []byte{})
	}
	batch.Put([]byte(key), []byte(fmt.Sprint(time.Now().Unix())))

	// Write the batch to the database
	if err := d.db.Write(batch, &opt.WriteOptions{Sync: false}); err != nil {
		d.logger.Error().Err(err).Str("key", key).Msg("failed to write the refresh user task")
		return fmt.Errorf("failed to write the refresh user task: %w", err)
	}

	return nil
}

// StoreFileInfo stores a FileInfo object as JSON for a given user ID
func (d *Database) StoreFileInfo(userID string, fileData FileInfo) error {
	jsonData, err := json.Marshal(fileData)
	if err != nil {
		d.logger.Error().Err(err).Str("id", userID).Msg("failed to marshal file data")
		return fmt.Errorf("failed to marshal file data: %w", err)
	}

	d.mx.Lock() // not allow concurrency to bypass Has verification
	defer d.mx.Unlock()

	// Check if the file data for the userID and FilePath already exists in the database
	key := "file:" + userID + ":" + fileData.FilePath
	exists, err := d.db.Has([]byte(key), nil)
	if err != nil {
		d.logger.Error().Err(err).Str("id", userID).Str("filePath", fileData.FilePath).Msg("failed to check existing file data")
		return fmt.Errorf("failed to check existing file data: %w", err)
	}

	if exists {
		return fmt.Errorf("file data already exists for user %s with filePath %s, remove it first before upload new", userID, fileData.FilePath)
	}

	batch := new(leveldb.Batch)
	batch.Put([]byte(key), jsonData)
	batch.Put([]byte("store-task:"+userID+":"+fileData.FilePath), []byte{})
	if err := d.db.Write(batch, &opt.WriteOptions{Sync: false}); err != nil {
		d.logger.Error().Err(err).Str("id", userID).Msg("failed to store file data and task key")
		return fmt.Errorf("failed to store file data and task key: %w", err)
	}
	return nil
}

// CompleteStoreTask removes the task key associated with a stored file, indicating the task has been completed
func (d *Database) CompleteStoreTask(key string, bag Bag, contractAddr string, cleanAfter time.Duration) (bool, error) {
	batch := new(leveldb.Batch)
	// Retrieve the current FileInfo to check state
	data, err := d.db.Get([]byte("file:"+key), nil)
	if err != nil {
		return false, fmt.Errorf("failed to retrieve file data: %w", err)
	}

	var fileData FileInfo
	if err = json.Unmarshal(data, &fileData); err != nil {
		return false, fmt.Errorf("failed to unmarshal file data: %w", err)
	}

	removeOnDisk := false
	if fileData.State == FileStateNew {
		fileData.State = FileStateBag
		fileData.Bag = &bag
		fileData.ContractAddr = contractAddr

		// Check if the bag ID already exists in the database
		existingBagKey := "bag:" + hex.EncodeToString(bag.RootHash)
		existingBagData, err := d.db.Get([]byte(existingBagKey), nil)
		if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
			return false, fmt.Errorf("failed to check if bag exists: %w", err)
		}

		// If the bag already exists, process it (e.g., delete associated file on disk)
		if len(existingBagData) > 0 {
			var existingBag BagInfo
			if err = json.Unmarshal(existingBagData, &existingBag); err != nil {
				d.logger.Error().Err(err).Hex("bagID", bag.RootHash).Msg("failed to unmarshal existing bag data")
				return false, fmt.Errorf("failed to unmarshal existing bag data: %w", err)
			}

			existingBag.Usages += 1
			fileData.FilePath = existingBag.FilePath
			removeOnDisk = true

			updatedBagData, err := json.Marshal(existingBag)
			if err != nil {
				d.logger.Error().Err(err).Hex("bagID", bag.RootHash).Msg("failed to marshal updated bag data")
				return false, fmt.Errorf("failed to marshal updated bag data: %w", err)
			}
			batch.Put([]byte("bag:"+hex.EncodeToString(bag.RootHash)), updatedBagData)
		} else {
			newBag := BagInfo{Usages: 1, FilePath: fileData.FilePath}
			newBagData, err := json.Marshal(newBag)
			if err != nil {
				d.logger.Error().Err(err).Hex("bagID", bag.RootHash).Msg("failed to marshal new bag data")
				return false, fmt.Errorf("failed to marshal new bag data: %w", err)
			}
			batch.Put([]byte("bag:"+hex.EncodeToString(bag.RootHash)), newBagData)
		}

		updatedData, err := json.Marshal(fileData)
		if err != nil {
			return false, fmt.Errorf("failed to marshal file data: %w", err)
		}

		cleanupTask := CleanupTask{
			Key:    key,
			ExecAt: fileData.Bag.CreatedAt.Add(cleanAfter),
			Force:  false,
		}

		cleanupTaskData, err := json.Marshal(cleanupTask)
		if err != nil {
			return false, fmt.Errorf("failed to marshal cleanup task: %w", err)
		}

		batch.Put([]byte("clean-task:"+key), cleanupTaskData)
		batch.Put([]byte(fmt.Sprintf("update-task:%d:%s", time.Now().Unix(), key)), nil)
		batch.Put([]byte("file:"+key), updatedData)
	}

	// Delete the task key to mark completion
	batch.Delete([]byte("store-task:" + key))
	if err := d.db.Write(batch, &opt.WriteOptions{Sync: true}); err != nil {
		return false, fmt.Errorf("failed to complete task: %w", err)
	}

	return removeOnDisk, nil
}

type CleanupTask struct {
	Key    string
	ExecAt time.Time
	Force  bool
}

// GetPendingCleanupTasks retrieves the list of cleanup task keys that are pending completion
// and only includes tasks where the current time is after the stored timestamp.
func (d *Database) GetPendingCleanupTasks() ([]CleanupTask, error) {
	var tasks []CleanupTask

	// Use a prefix-based range for cleanup task keys
	prefix := "clean-task:"
	iter := d.db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
	defer iter.Release()

	for iter.Next() {
		// Parse the stored timestamp from the task value
		var task CleanupTask
		err := json.Unmarshal(iter.Value(), &task)
		if err != nil {
			d.logger.Error().Err(err).Str("key", string(iter.Key())).Msg("failed to unmarshal cleanup task")
			continue
		}

		// Check if the current time is past the stored timestamp
		if time.Now().After(task.ExecAt) {
			task.Key = string(iter.Key())[len(prefix):]
			tasks = append(tasks, task)
		}
	}

	if err := iter.Error(); err != nil {
		d.logger.Error().Err(err).Msg("Iterator error while retrieving pending cleanup tasks")
		return nil, err
	}
	return tasks, nil
}

// CreateCleanTask creates and stores a new cleanup task in the database.
func (d *Database) CreateCleanTask(user, file string) error {
	return d.CreateCleanTaskByKey(fileKey(user, file))
}

func (d *Database) CreateCleanTaskByKey(key string) error {
	cleanupTask := CleanupTask{
		Key:    key,
		ExecAt: time.Now(),
		Force:  true,
	}

	cleanupTaskData, err := json.Marshal(cleanupTask)
	if err != nil {
		return fmt.Errorf("failed to marshal cleanup task: %w", err)
	}

	if err = d.db.Put([]byte("clean-task:"+key), cleanupTaskData, &opt.WriteOptions{Sync: false}); err != nil {
		return fmt.Errorf("failed to store cleanup task: %w", err)
	}

	return nil
}

// CompleteCleanTask processes a cleanup task by checking the associated bag,
// decrementing its usage or removing it if no longer used, and determines whether
// the associated file should be removed. All database actions are performed in a batch.
func (d *Database) CompleteCleanTask(key string, remove bool) (bool, error) {
	batch := new(leveldb.Batch)

	// Retrieve the FileInfo for the given key
	data, err := d.db.Get([]byte("file:"+key), nil)
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
		return false, fmt.Errorf("failed to retrieve file data: %w", err)
	}

	removeFile := false

	if data != nil {
		var fileData FileInfo
		if err := json.Unmarshal(data, &fileData); err != nil {
			return false, fmt.Errorf("failed to unmarshal file data: %w", err)
		}

		if remove {
			// Check if there's an associated bag and if its usages need to be decremented
			if fileData.Bag != nil {
				bagKey := "bag:" + hex.EncodeToString(fileData.Bag.RootHash)
				bagData, err := d.db.Get([]byte(bagKey), nil)
				if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
					d.logger.Error().Err(err).Hex("bagID", fileData.Bag.RootHash).Msg("failed to retrieve bag data")
					return false, fmt.Errorf("failed to retrieve bag data: %w", err)
				}

				if bagData != nil {
					var bag BagInfo
					if err := json.Unmarshal(bagData, &bag); err != nil {
						d.logger.Error().Err(err).Hex("bagID", fileData.Bag.RootHash).Msg("failed to unmarshal bag data")
						return false, fmt.Errorf("failed to unmarshal bag data: %w", err)
					}

					// Decrement usages and check if the bag needs to be deleted
					bag.Usages--
					if bag.Usages <= 0 {
						// Remove the bag record from the database
						batch.Delete([]byte(bagKey))
						removeFile = true
					} else {
						// Update the bag with decremented usages
						updatedBagData, err := json.Marshal(bag)
						if err != nil {
							d.logger.Error().Err(err).Hex("bagID", fileData.Bag.RootHash).Msg("failed to marshal updated bag data")
							return false, fmt.Errorf("failed to marshal updated bag data: %w", err)
						}
						batch.Put([]byte(bagKey), updatedBagData)
					}
				}
			}
			batch.Delete([]byte("file:" + key))
		}
	}

	// Delete the clean task record
	batch.Delete([]byte("clean-task:" + key))

	// Write all batch operations to the database
	if err := d.db.Write(batch, &opt.WriteOptions{Sync: false}); err != nil {
		return false, fmt.Errorf("failed to complete batch operations: %w", err)
	}

	return removeFile, nil
}

type UpdateTask struct {
	Key    string
	ExecAt time.Time
	Repeat bool
}

type UpdateTaskResult struct {
	UpdateTask
	NextExecAt *time.Time

	ProviderInfo *ProviderInfo
}

// GetPendingUpdateTasks retrieves the list of update task keys that are pending execution
// and only includes tasks where the current time is after the stored execution time.
func (d *Database) GetPendingUpdateTasks() ([]UpdateTask, error) {
	var tasks []UpdateTask

	// Use a prefix-based range for update task keys
	prefix := "update-task:"
	iter := d.db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
	defer iter.Release()

	now := time.Now().Unix()
	for iter.Next() {
		// Extract the execution time from the task key
		keyParts := strings.SplitN(string(iter.Key()), ":", 3)
		if len(keyParts) != 3 {
			d.logger.Error().Str("key", string(iter.Key())).Msg("invalid update task key format")
			continue
		}

		execAt, err := strconv.ParseInt(keyParts[1], 10, 64)
		if err != nil {
			d.logger.Error().Err(err).Str("key", string(iter.Key())).Msg("failed to parse execution time")
			continue
		}

		// Check if the current time is past the execution time
		if now >= execAt {
			tasks = append(tasks, UpdateTask{
				Key:    keyParts[2],
				ExecAt: time.Unix(execAt, 0),
			})
		}
	}

	if err := iter.Error(); err != nil {
		d.logger.Error().Err(err).Msg("iterator error while retrieving pending update tasks")
		return nil, err
	}
	return tasks, nil
}

// CompleteUpdateTasks processes a batch of update tasks, replaces them with new tasks
// if `nextAfter` is specified, and deletes old tasks in a single operation.
func (d *Database) CompleteUpdateTasks(tasks []UpdateTaskResult) error {
	batch := new(leveldb.Batch)

	for _, r := range tasks {
		if r.ProviderInfo != nil {
			fi, err := d.GetFileByKey(r.Key)
			if err != nil {
				return fmt.Errorf("failed to retrieve file data: %w", err)
			}

			if fi != nil {
				fi.State = FileStateStored
				fi.Provider = r.ProviderInfo

				updatedData, err := json.Marshal(fi)
				if err != nil {
					return fmt.Errorf("failed to marshal file data: %w", err)
				}

				batch.Put([]byte("file:"+r.Key), updatedData)
			}
		}

		// Delete old task
		oldTaskKey := fmt.Sprintf("update-task:%d:%s", r.ExecAt.Unix(), r.Key)
		batch.Delete([]byte(oldTaskKey))

		// If nextAfter is provided, create a new task with the same key and updated execution time
		if r.NextExecAt != nil {
			newTaskKey := fmt.Sprintf("update-task:%d:%s", r.NextExecAt.Unix(), r.Key)
			batch.Put([]byte(newTaskKey), nil) // Value isn't used, so it's nil
		}
	}

	// Write all batch operations to the database
	if err := d.db.Write(batch, &opt.WriteOptions{Sync: false}); err != nil {
		d.logger.Error().Err(err).Msg("failed to complete update tasks batch")
		return fmt.Errorf("failed to complete update tasks: %w", err)
	}

	return nil
}

// GetPendingStoreTasks retrieves the list of task keys that are pending completion
func (d *Database) GetPendingStoreTasks() ([]string, error) {
	var tasks []string

	// Use a prefix-based range for task keys
	prefix := "store-task:"
	iter := d.db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
	defer iter.Release()

	for iter.Next() {
		tasks = append(tasks, string(iter.Key())[len(prefix):])
	}

	if err := iter.Error(); err != nil {
		d.logger.Error().Err(err).Msg("Iterator error while retrieving pending tasks")
		return nil, err
	}
	return tasks, nil
}

func (d *Database) GetFile(key, name string) (*FileInfo, error) {
	return d.GetFileByKey(fileKey(key, name))
}

// GetFileByKey retrieves a FileInfo object based on the provided key
func (d *Database) GetFileByKey(key string) (*FileInfo, error) {
	data, err := d.db.Get([]byte("file:"+key), nil)
	if err != nil {
		if errors.Is(err, leveldb.ErrNotFound) {
			return nil, nil
		}

		d.logger.Error().Err(err).Str("key", key).Msg("failed to retrieve file data")
		return nil, fmt.Errorf("failed to retrieve file data: %w", err)
	}

	var fileData FileInfo
	if err := json.Unmarshal(data, &fileData); err != nil {
		d.logger.Error().Err(err).Str("key", key).Msg("failed to unmarshal file data")
		return nil, fmt.Errorf("failed to unmarshal file data: %w", err)
	}

	return &fileData, nil
}

// GetFilesByUser retrieves the list of FileInfo objects for a given user ID
func (d *Database) GetFilesByUser(userID string) ([]FileInfo, error) {
	var fileDataList []FileInfo

	// Use a prefix-based range to optimize iteration
	prefix := "file:" + userID + ":"
	iter := d.db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
	defer iter.Release()

	for iter.Next() {
		var fileData FileInfo
		if err := json.Unmarshal(iter.Value(), &fileData); err != nil {
			d.logger.Error().Err(err).Str("id", userID).Msg("failed to unmarshal file data")
			continue
		}
		fileDataList = append(fileDataList, fileData)
	}

	if err := iter.Error(); err != nil {
		d.logger.Error().Err(err).Str("userID", userID).Msg("Iterator error")
		return nil, err
	}
	return fileDataList, nil
}

// Close closes the LevelDB database
func (d *Database) Close() error {
	if err := d.db.Close(); err != nil {
		d.logger.Error().Err(err).Msg("Failed to close LevelDB database")
		return err
	}
	return nil
}

func fileKey(user, name string) string {
	return user + ":" + name
}
