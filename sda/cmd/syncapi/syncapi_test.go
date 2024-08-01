package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	log "github.com/sirupsen/logrus"
)

var mqPort int

type SyncAPITest struct {
	suite.Suite
}

func TestSyncAPITestSuite(t *testing.T) {
	suite.Run(t, new(SyncAPITest))
}

func TestMain(m *testing.M) {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		m.Run()
	}

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
	rabbitmq, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "rabbitmq",
		Tag:        "3-management-alpine",
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

	mqPort, _ = strconv.Atoi(rabbitmq.GetPort("5672/tcp"))
	mqHostAndPort := rabbitmq.GetHostPort("15672/tcp")

	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://"+mqHostAndPort+"/api/users", http.NoBody)
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
		if err := pool.Purge(rabbitmq); err != nil {
			log.Fatalf("Could not purge resource: %s", err)
		}
		log.Fatalf("Could not connect to rabbitmq: %s", err)
	}

	log.Println("starting tests")
	_ = m.Run()

	log.Println("tests completed")
	if err := pool.Purge(rabbitmq); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}
	pvo := docker.PruneVolumesOptions{Filters: make(map[string][]string), Context: context.Background()}
	if _, err := pool.Client.PruneVolumes(pvo); err != nil {
		log.Fatalf("could not prune docker volumes: %s", err.Error())
	}
}

func (suite *SyncAPITest) SetupTest() {
	viper.Set("log.level", "debug")
	viper.Set("log.format", "json")

	viper.Set("bpPrefix", "PFX")

	viper.Set("broker.host", "127.0.0.1")
	viper.Set("broker.port", mqPort)
	viper.Set("broker.user", "guest")
	viper.Set("broker.password", "guest")
	viper.Set("broker.exchange", "amq.direct")
	viper.Set("broker.vhost", "/")

	viper.Set("schema.type", "bigpicture")

	viper.Set("sync.api.user", "dummy")
	viper.Set("sync.api.password", "admin")
}

func (suite *SyncAPITest) TestConfigSyncAPI() {
	conf, err := config.NewConfig("sync-api")
	assert.NoError(suite.T(), err, "Failed to setup config")
	assert.Equal(suite.T(), mqPort, conf.Broker.Port)
	assert.Equal(suite.T(), mqPort, viper.GetInt("broker.port"))

	server := setup(conf)
	assert.Equal(suite.T(), "0.0.0.0:8080", server.Addr)
}

func (suite *SyncAPITest) TestShutdown() {
	Conf, err = config.NewConfig("sync-api")
	assert.NoError(suite.T(), err)

	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "127.0.0.1", Conf.API.MQ.Conf.Host)

	// make sure all conections are alive
	assert.Equal(suite.T(), false, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(suite.T(), false, Conf.API.MQ.Connection.IsClosed())

	shutdown()
	assert.Equal(suite.T(), true, Conf.API.MQ.Channel.IsClosed())
	assert.Equal(suite.T(), true, Conf.API.MQ.Connection.IsClosed())
}

func (suite *SyncAPITest) TestReadinessResponse() {
	Conf, err = config.NewConfig("sync-api")
	assert.NoError(suite.T(), err)

	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	r := mux.NewRouter()
	r.HandleFunc("/ready", readinessResponse)
	ts := httptest.NewServer(r)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/ready")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, res.StatusCode)
	defer res.Body.Close()

	// close the connection to force a reconneciton
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
}

func (suite *SyncAPITest) TestDatasetRoute() {
	Conf, err = config.NewConfig("sync-api")
	assert.NoError(suite.T(), err)

	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	Conf.Broker.SchemasPath = "../../schemas/bigpicture/"

	r := mux.NewRouter()
	r.HandleFunc("/dataset", dataset)
	ts := httptest.NewServer(r)
	defer ts.Close()

	goodJSON := []byte(`{"type": "mapping", "dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e6", "accession_ids": ["5fe7b660-afea-4c3a-88a9-3daabf055ebb", "ed6af454-d910-49e3-8cda-488a6f246e76"]}`)
	good, err := http.Post(ts.URL+"/dataset", "application/json", bytes.NewBuffer(goodJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, good.StatusCode)
	defer good.Body.Close()

	badJSON := []byte(`{"dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "dataset_files": []}`)
	bad, err := http.Post(ts.URL+"/dataset", "application/json", bytes.NewBuffer(badJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusBadRequest, bad.StatusCode)
	defer bad.Body.Close()

	goodJSON2 := []byte(`{"type": "release", "dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e6"}`)
	good2, err := http.Post(ts.URL+"/dataset", "application/json", bytes.NewBuffer(goodJSON2))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, good2.StatusCode)
	defer good2.Body.Close()
}

func (suite *SyncAPITest) TestMetadataRoute() {
	Conf = &config.Config{}
	Conf.Broker.SchemasPath = "../../schemas"

	r := mux.NewRouter()
	r.HandleFunc("/metadata", metadata)
	ts := httptest.NewServer(r)
	defer ts.Close()

	goodJSON := []byte(`{"dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "metadata": {"dummy":"data"}}`)
	good, err := http.Post(ts.URL+"/metadata", "application/json", bytes.NewBuffer(goodJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, good.StatusCode)
	defer good.Body.Close()

	badJSON := []byte(`{"dataset_id": "phail", "metadata": {}}`)
	bad, err := http.Post(ts.URL+"/metadata", "application/json", bytes.NewBuffer(badJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusBadRequest, bad.StatusCode)
	defer bad.Body.Close()
}

func (suite *SyncAPITest) TestBasicAuth() {
	Conf = &config.Config{}
	Conf.Broker.SchemasPath = "../../schemas"
	Conf.SyncAPI = config.SyncAPIConf{
		APIUser:     "dummy",
		APIPassword: "test",
	}

	r := mux.NewRouter()
	r.HandleFunc("/metadata", basicAuth(metadata))
	ts := httptest.NewServer(r)
	defer ts.Close()

	goodJSON := []byte(`{"dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "metadata": {"dummy":"data"}}`)
	req, err := http.NewRequest("POST", ts.URL+"/metadata", bytes.NewBuffer(goodJSON))
	assert.NoError(suite.T(), err)
	req.SetBasicAuth(Conf.SyncAPI.APIUser, Conf.SyncAPI.APIPassword)
	good, err := ts.Client().Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, good.StatusCode)
	defer good.Body.Close()

	req.SetBasicAuth(Conf.SyncAPI.APIUser, "wrongpass")
	bad, err := ts.Client().Do(req)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusUnauthorized, bad.StatusCode)
	defer bad.Body.Close()
}

func (suite *SyncAPITest) TestIngestRoute() {
	Conf, err = config.NewConfig("sync-api")
	assert.NoError(suite.T(), err)

	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	Conf.Broker.SchemasPath = "../../schemas/bigpicture"

	r := mux.NewRouter()
	r.HandleFunc("/ingest", ingest)
	ts := httptest.NewServer(r)
	defer ts.Close()

	goodJSON := []byte(`{"type": "ingest", "user": "test.user@example.com", "filepath": "inbox/user/file-1.c4gh"}`)
	good, err := http.Post(ts.URL+"/ingest", "application/json", bytes.NewBuffer(goodJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, good.StatusCode)
	defer good.Body.Close()

	badJSON := []byte(`{"dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "dataset_files": []}`)
	bad, err := http.Post(ts.URL+"/ingest", "application/json", bytes.NewBuffer(badJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusBadRequest, bad.StatusCode)
	defer bad.Body.Close()
}

func (suite *SyncAPITest) TestAccessionRoute() {
	Conf, err = config.NewConfig("sync-api")
	assert.NoError(suite.T(), err)

	Conf.API.MQ, err = broker.NewMQ(Conf.Broker)
	assert.NoError(suite.T(), err)

	Conf.Broker.SchemasPath = "../../schemas/bigpicture"

	r := mux.NewRouter()
	r.HandleFunc("/accession", accession)
	ts := httptest.NewServer(r)
	defer ts.Close()

	goodJSON := []byte(`{"type": "accession", "accession_id": "5fe7b660-afea-4c3a-88a9-3daabf055ebb", "user": "test.user@example.com", "filepath": "inbox/user/file-1.c4gh", "decrypted_checksums": [{"type": "sha256", "value": "82E4e60e7beb3db2e06A00a079788F7d71f75b61a4b75f28c4c942703dabb6d6"}]}`)
	good, err := http.Post(ts.URL+"/accession", "application/json", bytes.NewBuffer(goodJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusOK, good.StatusCode)
	defer good.Body.Close()

	badJSON := []byte(`{"dataset_id": "cd532362-e06e-4460-8490-b9ce64b8d9e7", "dataset_files": []}`)
	bad, err := http.Post(ts.URL+"/accession", "application/json", bytes.NewBuffer(badJSON))
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), http.StatusBadRequest, bad.StatusCode)
	defer bad.Body.Close()
}
