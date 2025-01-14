package sda

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/chacha20poly1305"
	"google.golang.org/protobuf/proto"

	"github.com/neicnordic/crypt4gh/keys"
	"github.com/neicnordic/crypt4gh/model/headers"
	"github.com/neicnordic/crypt4gh/streaming"
	"github.com/neicnordic/sda-download/api/middleware"
	"github.com/neicnordic/sda-download/internal/config"
	"github.com/neicnordic/sda-download/internal/database"
	"github.com/neicnordic/sda-download/internal/reencrypt"
	"github.com/neicnordic/sda-download/internal/session"
	"github.com/neicnordic/sda-download/internal/storage"
)

func TestDatasets(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	gin.SetMode(gin.ReleaseMode)
	c.Set("datasets", session.Cache{Datasets: []string{"dataset1", "dataset2"}})

	// Test the outcomes of the handler
	Datasets(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 200
	expectedBody := []byte(`["dataset1","dataset2"]`)

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDatasets failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDatasets failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext

}

func TestFind_Found(t *testing.T) {

	// Test case
	datasets := []string{"dataset1", "dataset2", "dataset3"}

	// Run test target
	found := find("dataset2", datasets)

	// Expected results
	expectedFound := true

	if found != expectedFound {
		t.Errorf("TestFind_Found failed, got %t expected %t", found, expectedFound)
	}

}

func TestFind_NotFound(t *testing.T) {

	// Test case
	datasets := []string{"dataset1", "dataset2", "dataset3"}

	// Run test target
	found := find("dataset4", datasets)

	// Expected results
	expectedFound := false

	if found != expectedFound {
		t.Errorf("TestFind_Found failed, got %t expected %t", found, expectedFound)
	}

}

func TestGetFiles_Fail_Database(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFilesDB := database.GetFiles

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}
	database.GetFiles = func(_ string) ([]*database.FileInfo, error) {
		return nil, errors.New("something went wrong")
	}

	// Run test target
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	fileInfo, statusCode, err := getFiles("dataset1", c)

	// Expected results
	expectedStatusCode := 500
	expectedError := "database error"

	if fileInfo != nil {
		t.Errorf("TestGetFiles_Fail_Database failed, got %v expected nil", fileInfo)
	}
	if statusCode != expectedStatusCode {
		t.Errorf("TestGetFiles_Fail_Database failed, got %d expected %d", statusCode, expectedStatusCode)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetFiles_Fail_Database failed, got %v expected %s", err, expectedError)
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFiles = originalGetFilesDB

}

func TestGetFiles_Fail_NotFound(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}

	// Run test target
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	fileInfo, statusCode, err := getFiles("dataset3", c)

	// Expected results
	expectedStatusCode := 404
	expectedError := "dataset not found"

	if fileInfo != nil {
		t.Errorf("TestGetFiles_Fail_NotFound failed, got %v expected nil", fileInfo)
	}
	if statusCode != expectedStatusCode {
		t.Errorf("TestGetFiles_Fail_NotFound failed, got %d expected %d", statusCode, expectedStatusCode)
	}
	if err.Error() != expectedError {
		t.Errorf("TestGetFiles_Fail_NotFound failed, got %v expected %s", err, expectedError)
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext
}

func TestGetFiles_Success(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFilesDB := database.GetFiles

	// Substitute mock functions
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1", "dataset2"},
		}
	}
	database.GetFiles = func(_ string) ([]*database.FileInfo, error) {
		fileInfo := database.FileInfo{
			FileID: "file1",
		}
		files := []*database.FileInfo{}
		files = append(files, &fileInfo)

		return files, nil
	}

	// Run test target
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	fileInfo, statusCode, err := getFiles("dataset1", c)

	// Expected results
	expectedStatusCode := 200
	expectedFileID := "file1"

	if fileInfo[0].FileID != expectedFileID {
		t.Errorf("TestGetFiles_Success failed, got %v expected nil", fileInfo)
	}
	if statusCode != expectedStatusCode {
		t.Errorf("TestGetFiles_Success failed, got %d expected %d", statusCode, expectedStatusCode)
	}
	if err != nil {
		t.Errorf("TestGetFiles_Success failed, got %v expected nil", err)
	}

	// Return mock functions to originals
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFiles = originalGetFilesDB

}

func TestFiles_Fail(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetFiles := getFiles

	// Substitute mock functions
	getFiles = func(_ string, _ *gin.Context) ([]*database.FileInfo, int, error) {
		return nil, 404, errors.New("dataset not found")
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = []gin.Param{
		{
			Key:   "dataset",
			Value: "dataset1/files",
		},
	}

	// Test the outcomes of the handler
	Files(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 404
	expectedBody := []byte("dataset not found")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDatasets failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDatasets failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	getFiles = originalGetFiles

}

func TestFiles_Success(t *testing.T) {

	// Save original to-be-mocked functions
	originalGetFiles := getFiles

	// Substitute mock functions
	getFiles = func(_ string, _ *gin.Context) ([]*database.FileInfo, int, error) {
		fileInfo := database.FileInfo{
			FileID:                    "file1",
			DatasetID:                 "dataset1",
			DisplayFileName:           "file1.txt",
			FilePath:                  "dir/file1.txt",
			EncryptedFileSize:         200,
			EncryptedFileChecksum:     "hash",
			EncryptedFileChecksumType: "sha256",
			DecryptedFileSize:         100,
			DecryptedFileChecksum:     "hash",
			DecryptedFileChecksumType: "sha256",
		}
		files := []*database.FileInfo{}
		files = append(files, &fileInfo)

		return files, 200, nil
	}

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = []gin.Param{
		{
			Key:   "dataset",
			Value: "dataset1/files",
		},
	}

	// Test the outcomes of the handler
	Files(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 200
	expectedBody := []byte(
		`[{"fileId":"file1","datasetId":"dataset1","displayFileName":"file1.txt","filePath":` +
			`"dir/file1.txt","encryptedFileSize":200,` +
			`"encryptedFileChecksum":"hash","encryptedFileChecksumType":"sha256",` +
			`"decryptedFileSize":100,` +
			`"decryptedFileChecksum":"hash","decryptedFileChecksumType":"sha256"}]`)

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDatasets failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDatasets failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	getFiles = originalGetFiles

}

func TestDownload_Fail_UnencryptedDownloadNotAllowed(t *testing.T) {

	// Save original to-be-mocked config
	originalServeUnencryptedDataTrigger := config.Config.C4GH.PublicKeyB64
	config.Config.C4GH.PublicKeyB64 = ""

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Test downloading of unencrypted file, should fail
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 400
	expectedBody := []byte("downloading unencrypted data is not supported")

	assert.Equal(t, expectedStatusCode, response.StatusCode, "Unexpected status code from download")
	assert.Equal(t, expectedBody, body, "Unexpected body from download")

	// Test downloading of unencrypted file from the s3 endpoint, should fail
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/mocks3/somepath", RawQuery: "filename=somepath"}}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)
	expectedStatusCode = 400
	expectedBody = []byte("downloading unencrypted data is not supported")

	assert.Equal(t, expectedStatusCode, response.StatusCode, "Unexpected status code from download")
	assert.Equal(t, expectedBody, body, "Unexpected body from download")

	// Test downloading from unencrypted file serving /s3 when passing a c4gh pubkey, should fail
	config.Config.C4GH.PublicKeyB64 = "somepubkeyBase64"
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET"}
	c.Request.Header = http.Header{"Client-Public-Key": []string{"somepubkey"}}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)
	expectedStatusCode = 400
	expectedBody = []byte("downloading encrypted data is not supported")

	assert.Equal(t, expectedStatusCode, response.StatusCode, "Unexpected status code from download")
	assert.Equal(t, expectedBody, body, "Unexpected body from download")

	// Return mock config to originals
	config.Config.C4GH.PublicKeyB64 = originalServeUnencryptedDataTrigger
}

func TestDownload_Fail_FileNotFound(t *testing.T) {

	privateKeyFilePath, err := GenerateTestC4ghKey(t)
	assert.NoError(t, err)

	// Save original to-be-mocked functions and config
	originalCheckFilePermission := database.CheckFilePermission
	originalServeUnencryptedDataTrigger := config.Config.C4GH.PublicKeyB64
	originalC4ghPrivateKeyFilepath := config.Config.C4GH.PrivateKey

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		return "", errors.New("file not found")
	}

	viper.Set("c4gh.transientKeyPath", privateKeyFilePath)
	viper.Set("c4gh.transientPassphrase", "password")
	config.Config.C4GH.PrivateKey, config.Config.C4GH.PublicKeyB64, err = config.GetC4GHKeys()
	assert.NoError(t, err, "Could not load c4gh keys")

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET"}
	c.Request.Header = http.Header{"Client-Public-Key": []string{""}}

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 404
	expectedBody := []byte("file not found")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_FileNotFound failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_FileNotFound failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	config.Config.C4GH.PublicKeyB64 = originalServeUnencryptedDataTrigger
	config.Config.C4GH.PrivateKey = originalC4ghPrivateKeyFilepath
	viper.Set("c4gh.transientKeyPath", "")
	viper.Set("c4gh.transientPassphrase", "")

}

func TestDownload_Fail_NoPermissions(t *testing.T) {

	privateKeyFilePath, err := GenerateTestC4ghKey(t)
	assert.NoError(t, err)

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalServeUnencryptedDataTrigger := config.Config.C4GH.PublicKeyB64
	originalC4ghPrivateKeyFilepath := config.Config.C4GH.PrivateKey

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		// nolint:goconst
		return "dataset1", nil
	}
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{}
	}

	viper.Set("c4gh.transientKeyPath", privateKeyFilePath)
	viper.Set("c4gh.transientPassphrase", "password")
	config.Config.C4GH.PrivateKey, config.Config.C4GH.PublicKeyB64, err = config.GetC4GHKeys()
	assert.NoError(t, err, "Could not load c4gh keys")

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET"}
	c.Request.Header = http.Header{"Client-Public-Key": []string{""}}

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 401
	expectedBody := []byte("unauthorised")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_NoPermissions failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_NoPermissions failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	middleware.GetCacheFromContext = originalGetCacheFromContext
	config.Config.C4GH.PublicKeyB64 = originalServeUnencryptedDataTrigger
	config.Config.C4GH.PrivateKey = originalC4ghPrivateKeyFilepath
	viper.Set("c4gh.transientKeyPath", "")
	viper.Set("c4gh.transientPassphrase", "")

}

func TestDownload_Fail_GetFile(t *testing.T) {

	privateKeyFilePath, err := GenerateTestC4ghKey(t)
	assert.NoError(t, err)

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFile := database.GetFile
	originalServeUnencryptedDataTrigger := config.Config.C4GH.PublicKeyB64
	originalC4ghPrivateKeyFilepath := config.Config.C4GH.PrivateKey

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		return "dataset1", nil
	}
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1"},
		}
	}
	database.GetFile = func(_ string) (*database.FileDownload, error) {
		return nil, errors.New("database error")
	}

	viper.Set("c4gh.transientKeyPath", privateKeyFilePath)
	viper.Set("c4gh.transientPassphrase", "password")
	config.Config.C4GH.PrivateKey, config.Config.C4GH.PublicKeyB64, err = config.GetC4GHKeys()
	assert.NoError(t, err, "Could not load c4gh keys")

	// Mock request and response holders
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET"}
	c.Request.Header = http.Header{"Client-Public-Key": []string{""}}

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 500
	expectedBody := []byte("database error")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_GetFile failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_GetFile failed, got %s expected %s", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFile = originalGetFile
	config.Config.C4GH.PublicKeyB64 = originalServeUnencryptedDataTrigger
	config.Config.C4GH.PrivateKey = originalC4ghPrivateKeyFilepath
	viper.Set("c4gh.transientKeyPath", "")
	viper.Set("c4gh.transientPassphrase", "")

}

func TestDownload_Fail_OpenFile(t *testing.T) {

	privateKeyFilePath, err := GenerateTestC4ghKey(t)
	assert.NoError(t, err)

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFile := database.GetFile
	originalServeUnencryptedDataTrigger := config.Config.C4GH.PublicKeyB64
	originalC4ghPrivateKeyFilepath := config.Config.C4GH.PrivateKey
	Backend, _ = storage.NewBackend(config.Config.Archive)

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		return "dataset1", nil
	}
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1"},
		}
	}
	database.GetFile = func(_ string) (*database.FileDownload, error) {
		fileDetails := &database.FileDownload{
			ArchivePath: "non-existant-file.txt",
			ArchiveSize: 0,
			Header:      []byte{},
		}

		return fileDetails, nil
	}

	viper.Set("c4gh.transientKeyPath", privateKeyFilePath)
	viper.Set("c4gh.transientPassphrase", "password")
	config.Config.C4GH.PrivateKey, config.Config.C4GH.PublicKeyB64, err = config.GetC4GHKeys()
	assert.NoError(t, err, "Could not load c4gh keys")

	// Mock request and response holders and initialize headers
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := &http.Request{
		URL:    &url.URL{},
		Header: make(http.Header),
	}
	c.Request = req

	// Test the outcomes of the handler
	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	expectedStatusCode := 500
	expectedBody := []byte("archive error")

	if response.StatusCode != expectedStatusCode {
		t.Errorf("TestDownload_Fail_OpenFile failed, got %d expected %d", response.StatusCode, expectedStatusCode)
	}
	if !bytes.Equal(body, []byte(expectedBody)) {
		// visual byte comparison in terminal (easier to find string differences)
		t.Error(body)
		t.Error([]byte(expectedBody))
		t.Errorf("TestDownload_Fail_OpenFile failed, got '%s' expected '%s'", string(body), string(expectedBody))
	}

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFile = originalGetFile
	config.Config.C4GH.PublicKeyB64 = originalServeUnencryptedDataTrigger
	config.Config.C4GH.PrivateKey = originalC4ghPrivateKeyFilepath
	viper.Set("c4gh.transientKeyPath", "")
	viper.Set("c4gh.transientPassphrase", "")
}

func Test_CalucalateCoords(t *testing.T) {
	var to, from int64
	from, to = 0, 1000
	fileDetails := &database.FileDownload{
		ArchivePath: "non-existant-file.txt",
		ArchiveSize: 2000,
		Header:      make([]byte, 124),
	}

	//	htsget_range should be used first and its end position should be increased by one
	headerSize := bytes.NewReader(fileDetails.Header).Size()
	fullSize := headerSize + int64(fileDetails.ArchiveSize)
	var endPos int64
	endPos = 20
	start, end, err := calculateCoords(from, to, "bytes=10-"+strconv.FormatInt(endPos, 10), fileDetails, "default")
	assert.Equal(t, start, int64(10))
	assert.Equal(t, end, endPos+1)
	assert.NoError(t, err)

	// end should be greater than or equal to inputted end
	_, end, err = calculateCoords(from, to, "", fileDetails, "encrypted")
	assert.GreaterOrEqual(t, end, from)
	assert.NoError(t, err)

	// no "padding" if requesting part of the header only
	_, end, err = calculateCoords(from, headerSize-10, "", fileDetails, "encrypted")
	assert.GreaterOrEqual(t, end, headerSize-10)
	assert.NoError(t, err)

	// end should not be larger than file length + header
	_, end, err = calculateCoords(from, fullSize+1900, "", fileDetails, "encrypted")
	assert.Equal(t, fullSize, end)
	assert.NoError(t, err)

	// param range 0-0 should give whole file
	start, end, err = calculateCoords(0, 0, "", fileDetails, "encrypted")
	assert.Equal(t, int64(0), start)
	assert.Equal(t, int64(0), end)
	assert.NoError(t, err)

	// byte range 0-1000 should return the range size, end coord inclusive
	endPos = 1000
	_, end, err = calculateCoords(0, 0, "bytes=0-"+strconv.FormatInt(endPos, 10), fileDetails, "encrypted")
	assert.Equal(t, end, endPos+1)
	assert.NoError(t, err)

	// range in the header should return error if values are not numbers
	_, _, err = calculateCoords(0, 0, "bytes=start-end", fileDetails, "encrypted")
	assert.Error(t, err)
}

type fakeGRPC struct {
	t      *testing.T
	pubkey [32]byte
}

func (f *fakeGRPC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Poor person's grpc server

	w.Header().Add("Content-type", "application/grpc+proto")
	w.Header().Add("Grpc-encoding", "identity")
	w.Header().Add("Trailer", "Grpc-status")

	var inLength int32
	compressed := make([]byte, 1)
	_, err := r.Body.Read(compressed)
	assert.NoError(f.t, err, "Could not read compressed flag")
	assert.Equal(f.t, compressed[0], byte(0), "Unexpected compressed flag")

	err = binary.Read(r.Body, binary.BigEndian, &inLength)
	assert.NoError(f.t, err, "Could not read length")

	body, err := io.ReadAll(r.Body)
	assert.NoError(f.t, err, "Could not read body")

	re := reencrypt.ReencryptRequest{}
	err = proto.Unmarshal(body, &re)
	assert.NoError(f.t, err, "Could not unmarshal request")

	rr := reencrypt.ReencryptResponse{Header: re.GetOldheader()}
	response, err := proto.Marshal(&rr)
	assert.NoError(f.t, err, "Could not marshal response")

	w.WriteHeader(200)
	_, err = w.Write([]byte{0})
	assert.NoError(f.t, err, "Could not write response flag")

	assert.LessOrEqual(f.t, len(response), math.MaxInt32, "Response too long")
	err = binary.Write(w, binary.BigEndian, int32(len(response))) //nolint:gosec // we're checking the length above
	assert.NoError(f.t, err, "Could not write response length")

	_, err = w.Write(response)
	assert.NoError(f.t, err, "Could not write response")
	w.Header().Add("Grpc-status", "0")
}

func TestDownload_Whole_Range_Encrypted(t *testing.T) {

	privateKeyFilePath, err := GenerateTestC4ghKey(t)
	assert.NoError(t, err)

	// Save original to-be-mocked functions
	originalCheckFilePermission := database.CheckFilePermission
	originalGetCacheFromContext := middleware.GetCacheFromContext
	originalGetFile := database.GetFile
	originalServeUnencryptedDataTrigger := config.Config.C4GH.PublicKeyB64
	originalC4ghPrivateKeyFilepath := config.Config.C4GH.PrivateKey
	archive := config.Config.Archive
	archive.Posix.Location = "."
	Backend, _ = storage.NewBackend(archive)

	// Fix to run fake GRPC server
	faker := fakeGRPC{t: t}
	server := httptest.NewUnstartedServer(&faker)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	// Figure out IP, port
	serverdetails := strings.Split(server.Listener.Addr().String(), ":")

	// Set up TLS for fake server
	keyfile, err := os.CreateTemp("", "key")
	assert.NoError(t, err, "Could not create temp file for key")
	defer os.Remove(keyfile.Name())
	privdata, err := x509.MarshalPKCS8PrivateKey(server.TLS.Certificates[0].PrivateKey)
	assert.NoError(t, err, "Could not marshal private key")

	privPEM := "-----BEGIN PRIVATE KEY-----\n" + base64.StdEncoding.EncodeToString(privdata) + "\n-----END PRIVATE KEY-----\n"
	_, err = keyfile.Write([]byte(privPEM))
	assert.NoError(t, err, "Could not write private key")
	keyfile.Close()

	certfile, err := os.CreateTemp("", "cert")
	assert.NoError(t, err, "Could not create temp file for cert")
	defer os.Remove(certfile.Name())
	pubPEM := "-----BEGIN CERTIFICATE-----\n" + base64.StdEncoding.EncodeToString(server.Certificate().Raw) + "\n-----END CERTIFICATE-----\n"
	_, err = certfile.Write([]byte(pubPEM))
	assert.NoError(t, err, "Could not write public key")
	certfile.Close()

	// Configure Reencrypt to use fake server
	config.Config.Reencrypt.Host = serverdetails[0]
	port, _ := strconv.ParseInt(serverdetails[1], 10, 32)
	config.Config.Reencrypt.Port = int(port)
	config.Config.Reencrypt.CACert = certfile.Name()
	config.Config.Reencrypt.ClientCert = certfile.Name()
	config.Config.Reencrypt.ClientKey = keyfile.Name()
	config.Config.Reencrypt.Timeout = 10

	viper.Set("c4gh.transientKeyPath", privateKeyFilePath)
	viper.Set("c4gh.transientPassphrase", "password")
	config.Config.C4GH.PrivateKey, config.Config.C4GH.PublicKeyB64, err = config.GetC4GHKeys()
	assert.NoError(t, err, "Could not load c4gh keys")

	// Make a file to hold the archive file
	datafile, err := os.CreateTemp(".", "datafile.")
	assert.NoError(t, err, "Could not create datafile for test")
	datafileName := datafile.Name()
	defer os.Remove(datafileName)

	tempKey, err := base64.StdEncoding.DecodeString(config.Config.C4GH.PublicKeyB64)
	assert.NoError(t, err, "Could not decode public key envelope")

	// Decode public key
	pubKeyReader := bytes.NewReader(tempKey)
	publicKey, err := keys.ReadPublicKey(pubKeyReader)
	assert.NoError(t, err, "Could not decode public key")

	// Reader list for archive file
	readerPublicKeyList := [][chacha20poly1305.KeySize]byte{}
	readerPublicKeyList = append(readerPublicKeyList, [32]byte(publicKey))
	faker.pubkey = [32]byte(publicKey)

	bufferWriter := bytes.Buffer{}
	dataWriter, err := streaming.NewCrypt4GHWriter(&bufferWriter, config.Config.C4GH.PrivateKey, readerPublicKeyList, nil)
	assert.NoError(t, err, "Could not make crypt4gh writer for test")

	// Write some data to the file
	for i := 0; i < 40000; i++ {
		_, err = dataWriter.Write([]byte("data"))
		assert.NoError(t, err, "Could not write to crypt4gh writer for test")
	}
	dataWriter.Close()

	// We have now written a crypt4gh to our buffer, prepare it for use
	// by separating out the header and write the rest to the file

	headerBytes, err := headers.ReadHeader(&bufferWriter)
	assert.NoError(t, err, "Could not get header")

	_, err = io.Copy(datafile, &bufferWriter)
	assert.NoError(t, err, "Could not write temporary file")
	datafile.Close()

	// Substitute mock functions
	database.CheckFilePermission = func(_ string) (string, error) {
		return "dataset1", nil
	}
	middleware.GetCacheFromContext = func(_ *gin.Context) session.Cache {
		return session.Cache{
			Datasets: []string{"dataset1"},
		}
	}
	database.GetFile = func(_ string) (*database.FileDownload, error) {
		fileDetails := &database.FileDownload{
			ArchivePath: datafileName,
			ArchiveSize: 4 * 40000,
			Header:      headerBytes,
		}

		return fileDetails, nil
	}

	// Test download, should work and return the whole file
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{}}

	Download(c)
	response := w.Result()
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)

	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download")
	// We only check
	assert.Equal(t, []byte(strings.Repeat("data", 40000)), body, "Unexpected body from download")

	// Test download with specified coordinates, should return a small bit of the file
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/somepath", RawQuery: "startCoordinate=5&endCoordinate=10"}}
	// Test the outcomes of the handler
	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download")
	assert.Equal(t, []byte("atada"), body, "Unexpected body from download")

	// Test encrypted download, should work and give output that is crypt4gh
	// encrypted
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/mocks3/somepath", RawQuery: "filename=somepath"}}
	c.Request.Header = http.Header{"Client-Public-Key": []string{config.Config.C4GH.PublicKeyB64},
		"Range": []string{"bytes=0-10"}}

	c.Params = make(gin.Params, 1)
	c.Params[0] = gin.Param{Key: "type", Value: "encrypted"}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download")
	assert.Equal(t, []byte("crypt4gh"), body[:8], "Unexpected body from download")
	assert.Equal(t, 11, len(body), "Unexpected body from download")

	// Test encrypted download not from the start, should work and give output
	// that is crypt4gh encrypted
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/mocks3/somepath", RawQuery: "filename=somepath"}}
	c.Request.Header = http.Header{"Client-Public-Key": []string{config.Config.C4GH.PublicKeyB64},
		"Range": []string{"bytes=4-1404"}}

	c.Params = make(gin.Params, 1)
	c.Params[0] = gin.Param{Key: "type", Value: "encrypted"}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download")
	assert.Equal(t, []byte("t4gh"), body[:4], "Unexpected body from download")
	assert.Equal(t, 1401, len(body), "Unexpected body from download")

	// Test encrypted download of a part late in the file, should work and give
	// output that is crypt4gh encrypted (but those bits)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/mocks3/somepath", RawQuery: "filename=somepath&startCoordinate=70000&endCoordinate=70200"}}
	c.Request.Header = http.Header{"Client-Public-Key": []string{config.Config.C4GH.PublicKeyB64}}

	c.Params = make(gin.Params, 1)
	c.Params[0] = gin.Param{Key: "type", Value: "encrypted"}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download")
	assert.Equal(t, 65536+28, len(body), "Unexpected body from download")
	// TODO: is it worth it to grab a header and construct a crypt4gh stream
	// to verify that we see expected content?

	// Test encrypted download, should work even when AllowedUnencryptedDownload is false
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/mocks3/somepath", RawQuery: "filename=somepath"}}
	c.Request.Header = http.Header{"Client-Public-Key": []string{config.Config.C4GH.PublicKeyB64},
		"Range": []string{"bytes=0-10"}}

	c.Params = make(gin.Params, 1)
	c.Params[0] = gin.Param{Key: "type", Value: "encrypted"}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	assert.Equal(t, 200, response.StatusCode, "Unexpected status code from download")
	assert.Equal(t, []byte("crypt4gh"), body[:8], "Unexpected body from download")
	assert.Equal(t, 11, len(body), "Unexpected body from download")

	// Test encrypted download without passing the key, should fail
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = &http.Request{Method: "GET", URL: &url.URL{Path: "/mocks3/somepath", RawQuery: "filename=somepath"}}
	c.Params = make(gin.Params, 1)
	c.Params[0] = gin.Param{Key: "type", Value: "encrypted"}

	Download(c)
	response = w.Result()
	defer response.Body.Close()
	body, _ = io.ReadAll(response.Body)

	assert.Equal(t, 400, response.StatusCode, "Unexpected status code from download")
	assert.Equal(t, []byte("c4gh pub"), body[:8], "Unexpected body from download")

	// Return mock functions to originals
	database.CheckFilePermission = originalCheckFilePermission
	middleware.GetCacheFromContext = originalGetCacheFromContext
	database.GetFile = originalGetFile
	config.Config.C4GH.PublicKeyB64 = originalServeUnencryptedDataTrigger
	config.Config.C4GH.PrivateKey = originalC4ghPrivateKeyFilepath
	viper.Set("c4gh.transientKeyPath", "")
	viper.Set("c4gh.transientPassphrase", "")
}

func GenerateTestC4ghKey(t *testing.T) (string, error) {
	t.Helper()
	// Generate a crypth4gh private key file
	_, privateKey, err := keys.GenerateKeyPair()
	if err != nil {
		return "", err
	}

	tempDir := t.TempDir()
	privateKeyFile, err := os.Create(fmt.Sprintf("%s/c4fg.key", tempDir))
	if err != nil {
		return "", err
	}

	err = keys.WriteCrypt4GHX25519PrivateKey(privateKeyFile, privateKey, []byte("password"))
	if err != nil {
		return "", err
	}

	return privateKeyFile.Name(), nil
}
