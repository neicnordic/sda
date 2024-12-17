// The mapper service register mapping of accessionIDs
// (IDs for files) to datasetIDs.
package main

import (
	"encoding/json"
	"fmt"

	"github.com/neicnordic/sensitive-data-archive/internal/broker"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/helper"
	"github.com/neicnordic/sensitive-data-archive/internal/schema"
	"github.com/neicnordic/sensitive-data-archive/internal/storage"

	log "github.com/sirupsen/logrus"
)

func main() {
	forever := make(chan bool)
	conf, err := config.NewConfig("mapper")
	if err != nil {
		log.Fatal(err)
	}
	mq, err := broker.NewMQ(conf.Broker)
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.NewSDAdb(conf.Database)
	if err != nil {
		log.Fatal(err)
	}
	inbox, err := storage.NewBackend(conf.Inbox)
	if err != nil {
		log.Fatal(err)
	}

	defer mq.Channel.Close()
	defer mq.Connection.Close()
	defer db.Close()

	go func() {
		connError := mq.ConnectionWatcher()
		log.Error(connError)
		forever <- false
	}()

	go func() {
		connError := mq.ChannelWatcher()
		log.Error(connError)
		forever <- false
	}()

	log.Info("Starting mapper service")
	var mappings schema.DatasetMapping

	go func() {
		messages, err := mq.GetMessages(conf.Broker.Queue)
		if err != nil {
			log.Fatalf("Failed to get message from mq (error: %v)", err)
		}

		for delivered := range messages {
			log.Debugf("received a message: %s", delivered.Body)
			schemaType, err := schemaFromDatasetOperation(delivered.Body)
			if err != nil {
				log.Errorf("%s", err.Error())
				if err := delivered.Ack(false); err != nil {
					log.Errorf("failed to ack message: %v", err)
				}
				if err := mq.SendMessage(delivered.CorrelationId, mq.Conf.Exchange, "error", delivered.Body); err != nil {
					log.Errorf("failed to send error message: %v", err)
				}

				continue
			}

			err = schema.ValidateJSON(fmt.Sprintf("%s/%s.json", conf.Broker.SchemasPath, schemaType), delivered.Body)
			if err != nil {
				log.Errorf("validation of incoming message (%s) failed, reason: %v ", schemaType, err)
				if err := delivered.Ack(false); err != nil {
					log.Errorf("Failed acking canceled work, reason: %v", err)
				}

				continue
			}

			// we unmarshal the message in the validation step so this is safe to do
			_ = json.Unmarshal(delivered.Body, &mappings)

			switch mappings.Type {
			case "mapping":
				log.Debug("Mapping type operation, mapping files to dataset")
				if err := db.MapFilesToDataset(mappings.DatasetID, mappings.AccessionIDs); err != nil {
					log.Errorf("failed to map files to dataset, reason: %v", err)

					// Nack message so the server gets notified that something is wrong and requeue the message
					if err := delivered.Nack(false, true); err != nil {
						log.Errorf("failed to Nack message, reason: (%v)", err)
					}

					continue
				}

				for _, aID := range mappings.AccessionIDs {
					log.Debugf("Mapped file to dataset (corr-id: %s, datasetid: %s, accessionid: %s)", delivered.CorrelationId, mappings.DatasetID, aID)
					fileInfo, err := db.GetFileInfoFromAccessionID(aID)
					if err != nil {
						log.Errorf("failed to get file info for file with stable ID: %s", aID)
					}

					err = inbox.RemoveFile(helper.UnanonymizeFilepath(fileInfo.FilePath, fileInfo.User))
					if err != nil {
						log.Errorf("Remove file from inbox %s failed, reason: %v", fileInfo.FilePath, err)
					}
				}

				if err := db.UpdateDatasetEvent(mappings.DatasetID, "registered", string(delivered.Body)); err != nil {
					log.Errorf("failed to set dataset status for dataset: %s", mappings.DatasetID)
					if err = delivered.Nack(false, false); err != nil {
						log.Errorf("Failed to Nack message, reason: (%s)", err.Error())
					}

					continue
				}
			case "release":
				log.Debug("Release type operation, marking dataset as released")
				if err := db.UpdateDatasetEvent(mappings.DatasetID, "released", string(delivered.Body)); err != nil {
					log.Errorf("failed to set dataset status for dataset: %s", mappings.DatasetID)
					if err = delivered.Nack(false, false); err != nil {
						log.Errorf("Failed to Nack message, reason: (%s)", err.Error())
					}

					continue
				}
			case "deprecate":
				log.Debug("Deprecate type operation, marking dataset as deprecated")
				if err := db.UpdateDatasetEvent(mappings.DatasetID, "deprecated", string(delivered.Body)); err != nil {
					log.Errorf("failed to set dataset status for dataset: %s", mappings.DatasetID)
					if err = delivered.Nack(false, false); err != nil {
						log.Errorf("Failed to Nack message, reason: (%s)", err.Error())
					}

					continue
				}
			}

			if err := delivered.Ack(false); err != nil {
				log.Errorf("failed to Ack message, reason: (%v)", err)
			}
		}
	}()

	<-forever
}

// schemaFromDatasetOperation returns the operation done with dataset supplied in body of the message
func schemaFromDatasetOperation(body []byte) (string, error) {
	message := make(map[string]interface{})
	err := json.Unmarshal(body, &message)
	if err != nil {
		return "", err
	}

	datasetMessageType, ok := message["type"]
	if !ok {
		return "", fmt.Errorf("malformed message, dataset message type is missing")
	}

	datasetOpsType, ok := datasetMessageType.(string)
	if !ok {
		return "", fmt.Errorf("could not cast operation attribute to string")
	}

	switch datasetOpsType {
	case "mapping":
		return "dataset-mapping", nil
	case "release":
		return "dataset-release", nil
	case "deprecate":
		return "dataset-deprecate", nil
	default:
		return "", fmt.Errorf("could not recognize mapping operation")
	}

}
