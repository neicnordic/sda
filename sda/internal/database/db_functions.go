// Package database provides functionalities for using the database,
// providing high level functions
package database

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// RegisterFile inserts a file in the database, along with a "registered" log
// event. If the file already exists in the database, the entry is updated, but
// a new file event is always inserted.
func (dbs *SDAdb) RegisterFile(uploadPath, uploadUser string) (string, error) {

	dbs.checkAndReconnectIfNeeded()

	if dbs.Version < 4 {
		return "", errors.New("database schema v4 required for RegisterFile()")
	}

	query := "SELECT sda.register_file($1, $2);"

	var fileID string

	err := dbs.DB.QueryRow(query, uploadPath, uploadUser).Scan(&fileID)

	return fileID, err
}

func (dbs *SDAdb) GetFileID(corrID string) (string, error) {
	var (
		err   error
		count int
		ID    string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		ID, err = dbs.getFileID(corrID)
		count++
	}

	return ID, err
}
func (dbs *SDAdb) getFileID(corrID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT DISTINCT file_id FROM sda.file_event_log where correlation_id = $1;"

	var fileID string
	err := db.QueryRow(getFileID, corrID).Scan(&fileID)
	if err != nil {
		return "", err
	}

	return fileID, nil
}

// UpdateFileEventLog updates the status in of the file in the database.
// The message parameter is the rabbitmq message sent on file upload.
func (dbs *SDAdb) UpdateFileEventLog(fileUUID, event, corrID, user, details, message string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.updateFileEventLog(fileUUID, event, corrID, user, details, message)
		count++
	}

	return err
}
func (dbs *SDAdb) updateFileEventLog(fileUUID, event, corrID, user, details, message string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "INSERT INTO sda.file_event_log(file_id, event, correlation_id, user_id, details, message) VALUES($1, $2, $3, $4, $5, $6);"

	result, err := db.Exec(query, fileUUID, event, corrID, user, details, message)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// StoreHeader stores the file header in the database
func (dbs *SDAdb) StoreHeader(header []byte, id string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.storeHeader(header, id)
		count++
	}

	return err
}
func (dbs *SDAdb) storeHeader(header []byte, id string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "UPDATE sda.files SET header = $1 WHERE id = $2;"
	result, err := db.Exec(query, hex.EncodeToString(header), id)
	if err != nil {
		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// SetArchived marks the file as 'ARCHIVED'
func (dbs *SDAdb) SetArchived(file FileInfo, fileID, corrID string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.setArchived(file, fileID, corrID)
		count++
	}

	return err
}
func (dbs *SDAdb) setArchived(file FileInfo, fileID, corrID string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT sda.set_archived($1, $2, $3, $4, $5, $6);"
	_, err := db.Exec(query,
		fileID,
		corrID,
		file.Path,
		file.Size,
		file.Checksum,
		"SHA256",
	)

	return err
}

func (dbs *SDAdb) GetFileStatus(corrID string) (string, error) {
	var (
		err    error
		count  int
		status string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		status, err = dbs.getFileStatus(corrID)
		count++
	}

	return status, err
}
func (dbs *SDAdb) getFileStatus(corrID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT event from sda.file_event_log WHERE correlation_id = $1 ORDER BY id DESC LIMIT 1;"

	var status string
	err := db.QueryRow(getFileID, corrID).Scan(&status)
	if err != nil {
		return "", err
	}

	return status, nil
}

// GetHeader retrieves the file header
func (dbs *SDAdb) GetHeader(fileID string) ([]byte, error) {
	var (
		r     []byte
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		r, err = dbs.getHeader(fileID)
		count++
	}

	return r, err
}
func (dbs *SDAdb) getHeader(fileID string) ([]byte, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT header from sda.files WHERE id = $1;"

	var hexString string
	if err := db.QueryRow(query, fileID).Scan(&hexString); err != nil {
		return nil, err
	}

	header, err := hex.DecodeString(hexString)
	if err != nil {
		return nil, err
	}

	return header, nil
}

// MarkCompleted marks the file as "COMPLETED"
func (dbs *SDAdb) SetVerified(file FileInfo, fileID, corrID string) error {
	var (
		err   error
		count int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		err = dbs.setVerified(file, fileID, corrID)
		count++
	}

	return err
}
func (dbs *SDAdb) setVerified(file FileInfo, fileID, corrID string) error {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const completed = "SELECT sda.set_verified($1, $2, $3, $4, $5, $6, $7);"
	_, err := db.Exec(completed,
		fileID,
		corrID,
		file.Checksum,
		"SHA256",
		file.DecryptedSize,
		file.DecryptedChecksum,
		"SHA256",
	)

	return err
}

// GetArchived retrieves the location and size of archive
func (dbs *SDAdb) GetArchived(corrID string) (string, int, error) {
	var (
		filePath string
		fileSize int
		err      error
		count    int
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		filePath, fileSize, err = dbs.getArchived(corrID)
		count++
	}

	return filePath, fileSize, err
}
func (dbs *SDAdb) getArchived(corrID string) (string, int, error) {
	dbs.checkAndReconnectIfNeeded()

	db := dbs.DB
	const query = "SELECT archive_file_path, archive_file_size from sda.files WHERE id = $1;"

	var filePath string
	var fileSize int
	if err := db.QueryRow(query, corrID).Scan(&filePath, &fileSize); err != nil {
		return "", 0, err
	}

	return filePath, fileSize, nil
}

// CheckAccessionIdExists validates if an accessionID exists in the db
func (dbs *SDAdb) CheckAccessionIDExists(accessionID, fileID string) (string, error) {
	var err error
	var exists string
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		exists, err = dbs.checkAccessionIDExists(accessionID, fileID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return exists, err
}
func (dbs *SDAdb) checkAccessionIDExists(accessionID, fileID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const sameID = "SELECT COUNT(id) FROM sda.files WHERE stable_id = $1 and id = $2;"
	var same int
	if err := db.QueryRow(sameID, accessionID, fileID).Scan(&same); err != nil {
		return "", err
	}

	if same > 0 {
		return "same", nil
	}

	const checkIDExist = "SELECT COUNT(id) FROM sda.files WHERE stable_id = $1;"
	var stableIDCount int
	if err := db.QueryRow(checkIDExist, accessionID).Scan(&stableIDCount); err != nil {
		return "", err
	}

	if stableIDCount > 0 {
		return "duplicate", nil
	}

	return "", nil
}

// SetAccessionID adds a stable id to a file
// identified by the user submitting it, inbox path and decrypted checksum
func (dbs *SDAdb) SetAccessionID(accessionID, fileID string) error {
	var err error
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		err = dbs.setAccessionID(accessionID, fileID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}
func (dbs *SDAdb) setAccessionID(accessionID, fileID string) error {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const setStableID = "UPDATE sda.files SET stable_id = $1 WHERE id = $2;"
	result, err := db.Exec(setStableID, accessionID, fileID)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil
}

// MapFilesToDataset maps a set of files to a dataset in the database
func (dbs *SDAdb) MapFilesToDataset(datasetID string, accessionIDs []string) error {
	var err error
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		err = dbs.mapFilesToDataset(datasetID, accessionIDs)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}
func (dbs *SDAdb) mapFilesToDataset(datasetID string, accessionIDs []string) error {
	dbs.checkAndReconnectIfNeeded()

	const getID = "SELECT id FROM sda.files WHERE stable_id = $1;"
	const dataset = "INSERT INTO sda.datasets (stable_id) VALUES ($1) ON CONFLICT DO NOTHING;"
	const mapping = "INSERT INTO sda.file_dataset (file_id, dataset_id) SELECT $1, id FROM sda.datasets WHERE stable_id = $2 ON CONFLICT DO NOTHING;"
	var fileID string

	db := dbs.DB
	_, err := db.Exec(dataset, datasetID)
	if err != nil {
		return err
	}

	transaction, _ := db.Begin()
	for _, accessionID := range accessionIDs {
		err := db.QueryRow(getID, accessionID).Scan(&fileID)
		if err != nil {
			log.Errorf("something went wrong with the DB query: %s", err.Error())
			if err := transaction.Rollback(); err != nil {
				log.Errorf("failed to rollback the transaction: %s", err.Error())
			}

			return err
		}
		_, err = transaction.Exec(mapping, fileID, datasetID)
		if err != nil {
			log.Errorf("something went wrong with the DB transaction: %s", err.Error())
			if err := transaction.Rollback(); err != nil {
				log.Errorf("failed to rollback the transaction: %s", err.Error())
			}

			return err
		}
	}

	return transaction.Commit()
}

// GetInboxPath retrieves the submission_fie_path for a file with a given accessionID
func (dbs *SDAdb) GetInboxPath(stableID string) (string, error) {
	var (
		err       error
		count     int
		inboxPath string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		inboxPath, err = dbs.getInboxPath(stableID)
		count++
	}

	return inboxPath, err
}
func (dbs *SDAdb) getInboxPath(stableID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT submission_file_path from sda.files WHERE stable_id = $1;"

	var inboxPath string
	err := db.QueryRow(getFileID, stableID).Scan(&inboxPath)
	if err != nil {
		return "", err
	}

	return inboxPath, nil
}

// UpdateDatasetEvent marks the files in a dataset as "registered","released" or "deprecated"
func (dbs *SDAdb) UpdateDatasetEvent(datasetID, status, message string) error {
	var err error
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		err = dbs.updateDatasetEvent(datasetID, status, message)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}
func (dbs *SDAdb) updateDatasetEvent(datasetID, status, message string) error {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const setStatus = "INSERT INTO sda.dataset_event_log(dataset_id, event, message) VALUES($1, $2, $3);"
	result, err := db.Exec(setStatus, datasetID, status, message)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query zero rows were changed")
	}

	return nil

}

// GetFileInfo returns info on a ingested file
func (dbs *SDAdb) GetFileInfo(id string) (FileInfo, error) {
	var (
		err   error
		count int
		info  FileInfo
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		info, err = dbs.getFileInfo(id)
		count++
	}

	return info, err
}
func (dbs *SDAdb) getFileInfo(id string) (FileInfo, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT archive_file_path, archive_file_size from sda.files where id = $1;"
	const checkSum = "SELECT MAX(checksum) FILTER(where source = 'ARCHIVED') as Archived, MAX(checksum) FILTER(where source = 'UNENCRYPTED') as Unencrypted from sda.checksums where file_id = $1;"
	var info FileInfo
	if err := db.QueryRow(getFileID, id).Scan(&info.Path, &info.Size); err != nil {
		return FileInfo{}, err
	}

	if err := db.QueryRow(checkSum, id).Scan(&info.Checksum, &info.DecryptedChecksum); err != nil {
		return FileInfo{}, err
	}

	return info, nil
}

// GetHeaderForStableID retrieves the file header by using stable id
func (dbs *SDAdb) GetHeaderForStableID(stableID string) ([]byte, error) {
	dbs.checkAndReconnectIfNeeded()
	const query = "SELECT header from sda.files WHERE stable_id = $1;"
	var hexString string
	if err := dbs.DB.QueryRow(query, stableID).Scan(&hexString); err != nil {
		return nil, err
	}

	header, err := hex.DecodeString(hexString)
	if err != nil {
		return nil, err
	}

	return header, nil
}

// GetSyncData retrieves the file information needed to sync a dataset
func (dbs *SDAdb) GetSyncData(accessionID string) (SyncData, error) {
	var (
		s   SyncData
		err error
	)

	for count := 1; count <= RetryTimes; count++ {
		s, err = dbs.getSyncData(accessionID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(3, float64(count))) * time.Second)
	}

	return s, err
}

// getSyncData is the actual function performing work for GetSyncData
func (dbs *SDAdb) getSyncData(accessionID string) (SyncData, error) {
	dbs.checkAndReconnectIfNeeded()

	const query = "SELECT submission_user, submission_file_path from sda.files WHERE stable_id = $1;"
	var data SyncData
	if err := dbs.DB.QueryRow(query, accessionID).Scan(&data.User, &data.FilePath); err != nil {
		return SyncData{}, err
	}

	const checksum = "SELECT checksum from sda.checksums WHERE source = 'UNENCRYPTED' and file_id = (SELECT id FROM sda.files WHERE stable_id = $1);"
	if err := dbs.DB.QueryRow(checksum, accessionID).Scan(&data.Checksum); err != nil {
		return SyncData{}, err
	}

	return data, nil
}

// CheckIfDatasetExists checks if a dataset already is registered
func (dbs *SDAdb) CheckIfDatasetExists(datasetID string) (bool, error) {
	var (
		ds  bool
		err error
	)

	for count := 1; count <= RetryTimes; count++ {
		ds, err = dbs.checkIfDatasetExists(datasetID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(3, float64(count))) * time.Second)
	}

	return ds, err
}

// getSyncData is the actual function performing work for GetSyncData
func (dbs *SDAdb) checkIfDatasetExists(datasetID string) (bool, error) {
	dbs.checkAndReconnectIfNeeded()

	const query = "SELECT EXISTS(SELECT id from sda.datasets WHERE stable_id = $1);"
	var yesNo bool
	if err := dbs.DB.QueryRow(query, datasetID).Scan(&yesNo); err != nil {
		return yesNo, err
	}

	return yesNo, nil
}

// GetInboxPath retrieves the submission_fie_path for a file with a given accessionID
func (dbs *SDAdb) GetArchivePath(stableID string) (string, error) {
	var (
		err         error
		count       int
		archivePath string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		archivePath, err = dbs.getArchivePath(stableID)
		count++
	}

	return archivePath, err
}
func (dbs *SDAdb) getArchivePath(stableID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getFileID = "SELECT archive_file_path from sda.files WHERE stable_id = $1;"

	var archivePath string
	err := db.QueryRow(getFileID, stableID).Scan(&archivePath)
	if err != nil {
		return "", err
	}

	return archivePath, nil
}

// GetUserFiles retrieves all the files a user submitted
func (dbs *SDAdb) GetUserFiles(userID string) ([]*SubmissionFileInfo, error) {
	var err error

	files := []*SubmissionFileInfo{}

	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		files, err = dbs.getUserFiles(userID)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return files, err
}

// getUserFiles is the actual function performing work for GetUserFiles
func (dbs *SDAdb) getUserFiles(userID string) ([]*SubmissionFileInfo, error) {
	dbs.checkAndReconnectIfNeeded()

	files := []*SubmissionFileInfo{}
	db := dbs.DB

	// select all files (that are not part of a dataset) of the user, each one annotated with its latest event
	const query = "SELECT f.submission_file_path, e.event, f.created_at FROM sda.files f " +
		"LEFT JOIN (SELECT DISTINCT ON (file_id) file_id, started_at, event FROM sda.file_event_log ORDER BY file_id, started_at DESC) e ON f.id = e.file_id WHERE f.submission_user = $1 " +
		"AND f.id NOT IN (SELECT f.id FROM sda.files f RIGHT JOIN sda.file_dataset d ON f.id = d.file_id); "

	// nolint:rowserrcheck
	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Iterate rows
	for rows.Next() {
		// Read rows into struct
		fi := &SubmissionFileInfo{}
		err := rows.Scan(&fi.InboxPath, &fi.Status, &fi.CreateAt)
		if err != nil {
			return nil, err
		}

		// Add instance of struct (file) to array
		files = append(files, fi)
	}

	return files, nil
}

// get the correlation ID for a user-inbox_path combination
func (dbs *SDAdb) GetCorrID(user, path string) (string, error) {
	var (
		corrID string
		err    error
	)
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		corrID, err = dbs.getCorrID(user, path)
		if err == nil || strings.Contains(err.Error(), "sql: no rows in result set") {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return corrID, err
}
func (dbs *SDAdb) getCorrID(user, path string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const query = "SELECT DISTINCT correlation_id FROM sda.file_event_log e RIGHT JOIN sda.files f ON e.file_id = f.id WHERE f.submission_file_path = $1 and f.submission_user = $2 AND NOT EXISTS (SELECT file_id FROM sda.file_dataset WHERE file_id = f.id)"

	var corrID string
	err := db.QueryRow(query, path, user).Scan(&corrID)
	if err != nil {
		return "", err
	}

	return corrID, nil
}

// list all users with files not yet assigned to a dataset
func (dbs *SDAdb) ListActiveUsers() ([]string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	var users []string
	rows, err := db.Query("SELECT DISTINCT submission_user FROM sda.files WHERE id NOT IN (SELECT f.id FROM sda.files f RIGHT JOIN sda.file_dataset d ON f.id = d.file_id) ORDER BY submission_user ASC;")
	if err != nil {
		return nil, err
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	defer rows.Close()

	for rows.Next() {
		var user string
		err := rows.Scan(&user)
		if err != nil {
			return nil, err
		}

		users = append(users, user)
	}

	return users, nil
}

func (dbs *SDAdb) GetDatasetStatus(datasetID string) (string, error) {
	var (
		err    error
		count  int
		status string
	)

	for count == 0 || (err != nil && count < RetryTimes) {
		status, err = dbs.getDatasetStatus(datasetID)
		count++
	}

	return status, err
}
func (dbs *SDAdb) getDatasetStatus(datasetID string) (string, error) {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB
	const getDatasetEvent = "SELECT event from sda.dataset_event_log WHERE dataset_id = $1 ORDER BY id DESC LIMIT 1;"

	var status string
	err := db.QueryRow(getDatasetEvent, datasetID).Scan(&status)
	if err != nil {
		return "", err
	}

	return status, nil
}

// AddKeyHash adds a key hash and key description in the encryption_keys table
func (dbs *SDAdb) AddKeyHash(keyHash, keyDescription string) error {
	var err error
	// 2, 4, 8, 16, 32 seconds between each retry event.
	for count := 1; count <= RetryTimes; count++ {
		err = dbs.addKeyHash(keyHash, keyDescription)
		if err == nil || strings.Contains(err.Error(), "key hash already exists") {
			break
		}
		time.Sleep(time.Duration(math.Pow(2, float64(count))) * time.Second)
	}

	return err
}

func (dbs *SDAdb) addKeyHash(keyHash, keyDescription string) error {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	const query = "INSERT INTO sda.encryption_keys(key_hash, description) VALUES($1, $2) ON CONFLICT DO NOTHING;"

	result, err := db.Exec(query, keyHash, keyDescription)
	if err != nil {
		return err
	}

	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("key hash already exists or no rows were updated")
	}

	return nil
}

func (dbs *SDAdb) SetKeyHash(keyHash, fileID string) error {
	dbs.checkAndReconnectIfNeeded()
	db := dbs.DB

	query := `SELECT key_hash FROM sda.encryption_keys WHERE key_hash = $1;`
	var hashID string
	err := db.QueryRow(query, keyHash).Scan(&hashID)
	if err != nil {
		return fmt.Errorf("keyhash not present in database: %v", err)
	}
	query = "UPDATE sda.files SET key_hash = $1 WHERE id = $2;"
	result, err := db.Exec(query, hashID, fileID)
	if err != nil {

		return err
	}
	if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
		return errors.New("something went wrong with the query, zero rows were changed")
	}
	log.Debugf("Successfully set key hash for file %v", fileID)

	return nil
}
