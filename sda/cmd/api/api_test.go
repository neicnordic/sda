package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

var dbPort, mqPort, OIDCport int
var BrokerAPI string

func TestMain(m *testing.M) {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		m.Run()
	}

	_, b, _, _ := runtime.Caller(0)
	rootDir := path.Join(path.Dir(b), "../../../")

	// uses a sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		log.Fatalf("Could not connect to Docker: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	postgres, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "15.2-alpine3.17",
		Env: []string{
			"POSTGRES_PASSWORD=rootpasswd",
			"POSTGRES_DB=sda",
		},
		Mounts: []string{
			fmt.Sprintf("%s/postgresql/initdb.d:/docker-entrypoint-initdb.d", rootDir),
		},
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}

	dbHostAndPort := postgres.GetHostPort("5432/tcp")
	dbPort, _ = strconv.Atoi(postgres.GetPort("5432/tcp"))
	databaseURL := fmt.Sprintf("postgres://postgres:rootpasswd@%s/sda?sslmode=disable", dbHostAndPort)

	pool.MaxWait = 120 * time.Second
	if err = pool.Retry(func() error {
		db, err := sql.Open("postgres", databaseURL)
		if err != nil {
			log.Println(err)

			return err
		}

		return db.Ping()
	}); err != nil {
		log.Fatalf("Could not connect to postgres: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	rabbitmq, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "ghcr.io/neicnordic/sensitive-data-archive",
		Tag:        "v0.3.89-rabbitmq",
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		if err := pool.Purge(postgres); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		log.Fatalf("Could not start resource: %s", err)
	}

	mqPort, _ = strconv.Atoi(rabbitmq.GetPort("5672/tcp"))
	BrokerAPI = rabbitmq.GetHostPort("15672/tcp")

	client := http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/", http.NoBody)
	if err != nil {
		log.Fatal(err)
	}
	req.SetBasicAuth("guest", "guest")

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()

		return nil
	}); err != nil {
		if err := pool.Purge(postgres); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		if err := pool.Purge(rabbitmq); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		log.Fatalf("Could not connect to rabbitmq: %s", err)
	}

	RSAPath, _ := os.MkdirTemp("", "RSA")
	if err := helper.CreateRSAkeys(RSAPath, RSAPath); err != nil {
		log.Panic("Failed to create RSA keys")
	}
	ECPath, _ := os.MkdirTemp("", "EC")
	if err := helper.CreateECkeys(ECPath, ECPath); err != nil {
		log.Panic("Failed to create EC keys")
	}

	// OIDC container
	oidc, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "python",
		Tag:        "3.10-slim",
		Cmd: []string{
			"/bin/sh",
			"-c",
			"pip install --upgrade pip && pip install aiohttp Authlib joserfc requests && python -u /oidc.py",
		},
		ExposedPorts: []string{"8080"},
		Mounts: []string{
			fmt.Sprintf("%s/.github/integration/sda/oidc.py:/oidc.py", rootDir),
		},
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}

	OIDCport, _ = strconv.Atoi(oidc.GetPort("8080/tcp"))
	OIDCHostAndPort := oidc.GetHostPort("8080/tcp")

	client = http.Client{Timeout: 5 * time.Second}
	req, err = http.NewRequest(http.MethodGet, "http://"+OIDCHostAndPort+"/jwk", http.NoBody)
	if err != nil {
		log.Panic(err)
	}

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()

		return nil
	}); err != nil {
		if err := pool.Purge(oidc); err != nil {
			log.Panicf("Could not purge oidc resource: %s", err)
		}
		if err := pool.Purge(postgres); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		if err := pool.Purge(rabbitmq); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		log.Panicf("Could not connect to oidc: %s", err)
	}

	log.Println("starting tests")
	_ = m.Run()

	log.Println("tests completed")
	if err := pool.Purge(postgres); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
	if err := pool.Purge(rabbitmq); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
	if err := pool.Purge(oidc); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
}

type TestSuite struct {
	suite.Suite
	Path        string
	PublicPath  string
	PrivatePath string
	KeyName     string
	Token       string
	User        string
}

func (suite *TestSuite) TestShutdown() {
	Conf = &config.Config{}
	Conf.Broker = broker.MQConf{
		Host:     "localhost",
		Port:     mqPort,
		User:     "guest",
		Password: "guest",
		Exchange: "sda",
		Vhost:    "/sda",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     dbPort,
		User:     "postgres",
		Password: "rootpasswd",
		Database: "sda",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)

	// make sure all conections are alive
	assert.Equal(suite.T(), false, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(suite.T(), false, Conf.API.MQ.Connection.IsClosed())
	assert.Equal(suite.T(), nil, Conf.API.DB.DB.Ping())

	shutdown()
	assert.Equal(suite.T(), true, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(suite.T(), true, Conf.API.MQ.Connection.IsClosed())
	assert.Equal(suite.T(), "sql: database is closed", Conf.API.DB.DB.Ping().Error())
}

func (suite *TestSuite) TestReadinessResponse() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.GET("/ready", readinessResponse)
	ts := httptest.NewServer(r)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close the connection to force a reconnection
	Conf.API.MQ.Connection.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close the channel to force a reconneciton
	Conf.API.MQ.Channel.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close DB connection to force a reconnection
	Conf.API.DB.Close()
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusServiceUnavailable, res.StatusCode)
	defer res.Body.Close()

	// reconnect should be fast so now this should pass
	res, err = http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()
}

// Initialise configuration and create jwt keys
func (suite *TestSuite) SetupSuite() {
	log.SetLevel(log.DebugLevel)
	suite.Path = "/tmp/keys/"
	suite.KeyName = "example.demo"

	log.Print("Creating JWT keys for testing")
	privpath, pubpath, err := helper.MakeFolder(suite.Path)
	assert.NoError(suite.T(), err)
	suite.PrivatePath = privpath
	suite.PublicPath = pubpath
	err = helper.CreateRSAkeys(privpath, pubpath)
	assert.NoError(suite.T(), err)

	// Create a valid token for queries to the API
	prKeyParsed, err := helper.ParsePrivateRSAKey(suite.PrivatePath, "/rsa")
	assert.NoError(suite.T(), err)
	token, err := helper.CreateRSAToken(prKeyParsed, "RS256", helper.DefaultTokenClaims)
	assert.NoError(suite.T(), err)
	suite.Token = token
	user, ok := helper.DefaultTokenClaims["sub"].(string)
	assert.True(suite.T(), ok)
	suite.User = user

	c := &config.Config{}
	ServerConf := config.ServerConfig{}
	ServerConf.Jwtpubkeypath = suite.PublicPath
	c.Server = ServerConf

	Conf = c

	log.Print("Setup DB for my test")
	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     dbPort,
		User:     "postgres",
		Password: "rootpasswd",
		Database: "sda",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)

	Conf.Broker = broker.MQConf{
		Host:     "localhost",
		Port:     mqPort,
		User:     "guest",
		Password: "guest",
		Exchange: "sda",
		Vhost:    "/sda",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

}

func (suite *TestSuite) SetupTest() {
	Conf.Database = database.DBConf{
		Host:     "localhost",
		Port:     dbPort,
		User:     "postgres",
		Password: "rootpasswd",
		Database: "sda",
		SslMode:  "disable",
	}
	Conf.API.DB, err = database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)

	_, err = Conf.API.DB.DB.Exec("TRUNCATE sda.files CASCADE")
	assert.NoError(suite.T(), err)

	Conf.Broker = broker.MQConf{
		Host:     "localhost",
		Port:     mqPort,
		User:     "guest",
		Password: "guest",
		Exchange: "sda",
		Vhost:    "/sda",
	}
	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)
}

func (suite *TestSuite) TestDatabasePingCheck() {
	emptyDB := database.SDAdb{}
	assert.Error(suite.T(), checkDB(&emptyDB, 1*time.Second), "nil DB should fail")

	db, err := database.NewSDAdb(Conf.Database)
	assert.NoError(suite.T(), err)
	assert.NoError(suite.T(), checkDB(db, 1*time.Second), "ping should succeed")
}

func (suite *TestSuite) TestAPIAuthenticate() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.GET("/files", func(c *gin.Context) {
		getFiles(c)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()
	filesURL := ts.URL + "/files"
	client := &http.Client{}

	assert.NoError(suite.T(), setupJwtAuth())

	requestURL, err := url.Parse(filesURL)
	assert.NoError(suite.T(), err)

	// No credentials
	resp, err := http.Get(requestURL.String())
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusUnauthorized, resp.StatusCode)
	defer resp.Body.Close()

	// Valid credentials

	req, err := http.NewRequest("GET", filesURL, nil)
	assert.NoError(suite.T(), err)
	req.Header.Add("Authorization", "Bearer "+suite.Token)
	resp, err = client.Do(req)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	assert.NoError(suite.T(), err)
	defer resp.Body.Close()
}

func (suite *TestSuite) TestAPIGetFiles() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.GET("/files", func(c *gin.Context) {
		getFiles(c)
	})
	ts := httptest.NewServer(r)
	defer ts.Close()
	filesURL := ts.URL + "/files"
	client := &http.Client{}

	assert.NoError(suite.T(), setupJwtAuth())

	req, err := http.NewRequest("GET", filesURL, nil)
	assert.NoError(suite.T(), err)
	req.Header.Add("Authorization", "Bearer "+suite.Token)

	// Test query when no files is in db
	resp, err := client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()
	filesData := []database.SubmissionFileInfo{}
	err = json.NewDecoder(resp.Body).Decode(&filesData)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), len(filesData), 0)
	assert.NoError(suite.T(), err)

	// Insert a file and make sure it is listed
	file1 := fmt.Sprintf("/%v/TestAPIGetFiles.c4gh", suite.User)
	fileID, err := Conf.API.DB.RegisterFile(file1, suite.User)
	assert.NoError(suite.T(), err, "failed to register file in database")
	corrID := uuid.New().String()

	latestStatus := "uploaded"
	err = Conf.API.DB.UpdateFileEventLog(fileID, latestStatus, corrID, suite.User, "{}", "{}")
	assert.NoError(suite.T(), err, "got (%v) when trying to update file status")

	resp, err = client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&filesData)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), len(filesData), 1)
	assert.Equal(suite.T(), filesData[0].Status, latestStatus)
	assert.NoError(suite.T(), err)

	// Update the file's status and make sure only the lastest status is listed
	latestStatus = "ready"
	err = Conf.API.DB.UpdateFileEventLog(fileID, latestStatus, corrID, suite.User, "{}", "{}")
	assert.NoError(suite.T(), err, "got (%v) when trying to update file status")

	resp, err = client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&filesData)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), len(filesData), 1)
	assert.Equal(suite.T(), filesData[0].Status, latestStatus)

	assert.NoError(suite.T(), err)

	// Insert a second file and make sure it is listed
	file2 := fmt.Sprintf("/%v/TestAPIGetFiles2.c4gh", suite.User)
	_, err = Conf.API.DB.RegisterFile(file2, suite.User)
	assert.NoError(suite.T(), err, "failed to register file in database")

	resp, err = client.Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)

	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&filesData)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), len(filesData), 2)
	for _, fileInfo := range filesData {
		switch fileInfo.InboxPath {
		case file1:
			assert.Equal(suite.T(), fileInfo.Status, latestStatus)
		case file2:
			assert.Equal(suite.T(), fileInfo.Status, "registered")
		}
	}
	assert.NoError(suite.T(), err)
}

func TestApiTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func testEndpoint(c *gin.Context) {
	c.JSON(200, gin.H{"ok": true})
}

func (suite *TestSuite) TestIsAdmin_NoToken() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	_, router := gin.CreateTestContext(w)
	router.GET("/", isAdmin(), testEndpoint)

	// no token should not be allowed
	router.ServeHTTP(w, r)
	badResponse := w.Result()
	defer badResponse.Body.Close()
	b, _ := io.ReadAll(badResponse.Body)
	assert.Equal(suite.T(), http.StatusUnauthorized, badResponse.StatusCode)
	assert.Contains(suite.T(), string(b), "no access token supplied")
}
func (suite *TestSuite) TestIsAdmin_BadUser() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"foo", "bar"}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	_, router := gin.CreateTestContext(w)
	router.GET("/", isAdmin(), testEndpoint)

	// non admin user should not be allowed
	r.Header.Add("Authorization", "Bearer "+suite.Token)
	router.ServeHTTP(w, r)
	notAdmin := w.Result()
	defer notAdmin.Body.Close()
	b, _ := io.ReadAll(notAdmin.Body)
	assert.Equal(suite.T(), http.StatusUnauthorized, notAdmin.StatusCode)
	assert.Contains(suite.T(), string(b), "not authorized")
}
func (suite *TestSuite) TestIsAdmin() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"foo", "bar", "dummy"}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/", isAdmin(), testEndpoint)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	b, _ := io.ReadAll(okResponse.Body)
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)
	assert.Contains(suite.T(), string(b), "ok")
}

func (suite *TestSuite) TestIngestFile() {
	user := "dummy"
	filePath := "/inbox/dummy/file10.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type ingest struct {
		FilePath string `json:"filepath"`
		User     string `json:"user"`
	}
	ingestMsg, _ := json.Marshal(ingest{User: user, FilePath: filePath})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/ingest", bytes.NewBuffer(ingestMsg))

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", ingestFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/ingest", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(suite.T(), err, "failed to unmarshal response")
	assert.Equal(suite.T(), 1, data.MessagesReady)
}

func (suite *TestSuite) TestIngestFile_NoUser() {
	user := "dummy"
	filePath := "/inbox/dummy/file10.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type ingest struct {
		FilePath string `json:"filepath"`
		User     string `json:"user"`
	}
	ingestMsg, _ := json.Marshal(ingest{User: "", FilePath: filePath})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/ingest", bytes.NewBuffer(ingestMsg))

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", ingestFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusBadRequest, okResponse.StatusCode)
}
func (suite *TestSuite) TestIngestFile_WrongUser() {
	user := "dummy"
	filePath := "/inbox/dummy/file10.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type ingest struct {
		FilePath string `json:"filepath"`
		User     string `json:"user"`
	}
	ingestMsg, _ := json.Marshal(ingest{User: "foo", FilePath: filePath})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/ingest", bytes.NewBuffer(ingestMsg))

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", ingestFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	b, _ := io.ReadAll(okResponse.Body)
	assert.Equal(suite.T(), http.StatusBadRequest, okResponse.StatusCode)
	assert.Contains(suite.T(), string(b), "sql: no rows in result set")
}

func (suite *TestSuite) TestIngestFile_WrongFilePath() {
	user := "dummy"
	filePath := "/inbox/dummy/file10.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type ingest struct {
		FilePath string `json:"filepath"`
		User     string `json:"user"`
	}
	ingestMsg, _ := json.Marshal(ingest{User: "dummy", FilePath: "bad/path"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/file/ingest", bytes.NewBuffer(ingestMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/ingest", isAdmin(), ingestFile)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	b, _ := io.ReadAll(okResponse.Body)
	assert.Equal(suite.T(), http.StatusBadRequest, okResponse.StatusCode)
	assert.Contains(suite.T(), string(b), "sql: no rows in result set")
}

func (suite *TestSuite) TestSetAccession() {
	user := "dummy"
	filePath := "/inbox/dummy/file11.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(suite.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(suite.T(), err)

	fileInfo := database.FileInfo{
		Checksum:          fmt.Sprintf("%x", encSha.Sum(nil)),
		Size:              1000,
		Path:              filePath,
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     948,
	}
	err = Conf.API.DB.SetArchived(fileInfo, fileID, fileID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = Conf.API.DB.SetVerified(fileInfo, fileID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type accession struct {
		AccessionID string `json:"accession_id"`
		FilePath    string `json:"filepath"`
		User        string `json:"user"`
	}
	aID := "API:accession-id-01"
	accessionMsg, _ := json.Marshal(accession{AccessionID: aID, FilePath: filePath, User: user})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/accession", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/accession", isAdmin(), setAccession)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/accession", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(suite.T(), err, "failed to unmarshal response")
	assert.Equal(suite.T(), 1, data.MessagesReady)
}

func (suite *TestSuite) TestSetAccession_WrongUser() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type accession struct {
		AccessionID string `json:"accession_id"`
		FilePath    string `json:"filepath"`
		User        string `json:"user"`
	}
	aID := "API:accession-id-01"
	accessionMsg, _ := json.Marshal(accession{AccessionID: aID, FilePath: "/inbox/dummy/file11.c4gh", User: "fooBar"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/accession", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/accession", isAdmin(), setAccession)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusBadRequest, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/accession", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(suite.T(), err, "failed to unmarshal response")
	assert.Equal(suite.T(), 1, data.MessagesReady)
}

func (suite *TestSuite) TestSetAccession_WrongFormat() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}
	Conf.Broker.SchemasPath = "../../schemas/federated"

	type accession struct {
		AccessionID string `json:"accession_id"`
		FilePath    string `json:"filepath"`
		User        string `json:"user"`
	}
	aID := "API:accession-id-01"
	accessionMsg, _ := json.Marshal(accession{AccessionID: aID, FilePath: "/inbox/dummy/file11.c4gh", User: "dummy"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/file/accession", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/file/accession", isAdmin(), setAccession)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusBadRequest, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/accession", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(suite.T(), err, "failed to unmarshal response")
	assert.Equal(suite.T(), 1, data.MessagesReady)
}

func (suite *TestSuite) TestCreateDataset() {
	user := "dummy"
	filePath := "/inbox/dummy/file12.c4gh"

	fileID, err := Conf.API.DB.RegisterFile(filePath, user)
	assert.NoError(suite.T(), err, "failed to register file in database")
	err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
	assert.NoError(suite.T(), err, "failed to update satus of file in database")

	encSha := sha256.New()
	_, err = encSha.Write([]byte("Checksum"))
	assert.NoError(suite.T(), err)

	decSha := sha256.New()
	_, err = decSha.Write([]byte("DecryptedChecksum"))
	assert.NoError(suite.T(), err)

	fileInfo := database.FileInfo{
		Checksum:          fmt.Sprintf("%x", encSha.Sum(nil)),
		Size:              1000,
		Path:              filePath,
		DecryptedChecksum: fmt.Sprintf("%x", decSha.Sum(nil)),
		DecryptedSize:     948,
	}
	err = Conf.API.DB.SetArchived(fileInfo, fileID, fileID)
	assert.NoError(suite.T(), err, "failed to mark file as Archived")

	err = Conf.API.DB.SetVerified(fileInfo, fileID, fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	err = Conf.API.DB.SetAccessionID("API:accession-id-11", fileID)
	assert.NoError(suite.T(), err, "got (%v) when marking file as verified", err)

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	type dataset struct {
		AccessionIDs []string `json:"accession_ids"`
		DatasetID    string   `json:"dataset_id"`
	}
	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{"API:accession-id-11", "API:accession-id-12", "API:accession-id-13"}, DatasetID: "API:dataset-01"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", isAdmin(), createDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/mappings", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	assert.NoError(suite.T(), json.Unmarshal(body, &data), "failed to unmarshal response")
	assert.Equal(suite.T(), 1, data.MessagesReady)
}

func (suite *TestSuite) TestCreateDataset_BadFormat() {
	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}
	Conf.Broker.SchemasPath = "../../schemas/federated"

	type dataset struct {
		AccessionIDs []string `json:"accession_ids"`
		DatasetID    string   `json:"dataset_id"`
	}
	accessionMsg, _ := json.Marshal(dataset{AccessionIDs: []string{"API:accession-id-11", "API:accession-id-12", "API:accession-id-13"}, DatasetID: "API:dataset-01"})
	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/create", bytes.NewBuffer(accessionMsg))
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/create", isAdmin(), createDataset)

	router.ServeHTTP(w, r)
	response := w.Result()
	defer response.Body.Close()

	assert.Equal(suite.T(), http.StatusBadRequest, response.StatusCode)
}

func (suite *TestSuite) TestReleaseDataset() {
	// purge the queue so that the test passes when all tests are run as well as when run standalone.
	client := http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodDelete, "http://"+BrokerAPI+"/api/queues/sda/mappings/contents", http.NoBody)
	assert.NoError(suite.T(), err, "failed to generate query")
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	res.Body.Close()

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/release/API:dataset-01", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/release/*dataset", isAdmin(), releaseDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	// verify that the message shows up in the queue
	time.Sleep(10 * time.Second) // this is needed to ensure we don't get any false negatives
	req, _ = http.NewRequest(http.MethodGet, "http://"+BrokerAPI+"/api/queues/sda/mappings", http.NoBody)
	req.SetBasicAuth("guest", "guest")
	res, err = client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	var data struct {
		MessagesReady int `json:"messages_ready"`
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	assert.NoError(suite.T(), err, "failed to read response from broker")
	err = json.Unmarshal(body, &data)
	assert.NoError(suite.T(), err, "failed to unmarshal response")
	assert.Equal(suite.T(), 1, data.MessagesReady)
}

func (suite *TestSuite) TestReleaseDataset_NoDataset() {
	// purge the queue so that the test passes when all tests are run as well as when run standalone.
	client := http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodDelete, "http://"+BrokerAPI+"/api/queues/sda/mappings/contents", http.NoBody)
	assert.NoError(suite.T(), err, "failed to generate query")
	req.SetBasicAuth("guest", "guest")
	res, err := client.Do(req)
	assert.NoError(suite.T(), err, "failed to query broker")
	res.Body.Close()

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}
	Conf.Broker.SchemasPath = "../../schemas/isolated"

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dataset/release/", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.POST("/dataset/release/*dataset", isAdmin(), releaseDataset)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusBadRequest, okResponse.StatusCode)
}

func (suite *TestSuite) TestListActiveUsers() {
	testUsers := []string{"User-A", "User-B", "User-C"}
	for _, user := range testUsers {
		for i := 0; i < 3; i++ {
			fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i), user)
			if err != nil {
				suite.FailNow("failed to register file in database")
			}

			err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
			if err != nil {
				suite.FailNow("failed to update satus of file in database")
			}

			stableID := fmt.Sprintf("accession_%s_0%d", user, i)
			err = Conf.API.DB.SetAccessionID(stableID, fileID)
			if err != nil {
				suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
			}
		}
	}

	err = Conf.API.DB.MapFilesToDataset("test-dataset-01", []string{"accession_User-A_00", "accession_User-A_01", "accession_User-A_02"})
	if err != nil {
		suite.FailNow("failed to map files to dataset")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/users", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/users", isAdmin(), listActiveUsers)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	var users []string
	err = json.NewDecoder(okResponse.Body).Decode(&users)
	assert.NoError(suite.T(), err, "failed to list users from DB")
	assert.Equal(suite.T(), []string{"User-B", "User-C"}, users)
}

func (suite *TestSuite) TestListUserFiles() {
	testUsers := []string{"user_example.org", "User-B", "User-C"}
	for _, user := range testUsers {
		for i := 0; i < 5; i++ {
			fileID, err := Conf.API.DB.RegisterFile(fmt.Sprintf("/%v/TestGetUserFiles-00%d.c4gh", user, i), user)
			if err != nil {
				suite.FailNow("failed to register file in database")
			}

			err = Conf.API.DB.UpdateFileEventLog(fileID, "uploaded", fileID, user, "{}", "{}")
			if err != nil {
				suite.FailNow("failed to update satus of file in database")
			}

			stableID := fmt.Sprintf("accession_%s_0%d", user, i)
			err = Conf.API.DB.SetAccessionID(stableID, fileID)
			if err != nil {
				suite.FailNowf("got (%s) when setting stable ID: %s, %s", err.Error(), stableID, fileID)
			}
		}
	}

	err = Conf.API.DB.MapFilesToDataset("test-dataset-01", []string{"accession_user_example.org_00", "accession_user_example.org_01", "accession_user_example.org_02"})
	if err != nil {
		suite.FailNow("failed to map files to dataset")
	}

	gin.SetMode(gin.ReleaseMode)
	assert.NoError(suite.T(), setupJwtAuth())
	Conf.API.Admins = []string{"dummy"}

	// Mock request and response holders
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/users/user@example.org/files", http.NoBody)
	r.Header.Add("Authorization", "Bearer "+suite.Token)

	_, router := gin.CreateTestContext(w)
	router.GET("/users/:username/files", isAdmin(), listUserFiles)

	router.ServeHTTP(w, r)
	okResponse := w.Result()
	defer okResponse.Body.Close()
	assert.Equal(suite.T(), http.StatusOK, okResponse.StatusCode)

	files := []database.SubmissionFileInfo{}
	err = json.NewDecoder(okResponse.Body).Decode(&files)
	assert.NoError(suite.T(), err, "failed to list users from DB")
	assert.Equal(suite.T(), 2, len(files))
}
