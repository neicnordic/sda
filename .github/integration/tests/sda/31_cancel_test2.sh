#!/bin/sh
set -e

cd shared || true

ENC_SHA=$(sha256sum NA12878.bam.c4gh | cut -d' ' -f 1)
ENC_MD5=$(md5sum NA12878.bam.c4gh | cut -d' ' -f 1)

## get correlation id from message
CORRID=$(psql -U postgres -h postgres -d sda -At -c "select id from sda.files where submission_file_path = 'NA12878.bam.c4gh';")


properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg correlation_id "$CORRID" \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

encrypted_checksums=$(
    jq -c -n \
        --arg sha256 "$ENC_SHA" \
        --arg md5 "$ENC_MD5" \
        '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
)

cancel_payload=$(
    jq -r -c -n \
        --arg type cancel \
        --arg user test@dummy.org \
        --arg filepath NA12878.bam.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

cancel_body=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$cancel_payload" \
        '$ARGS.named'
)

curl -k -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$cancel_body" | jq

# check database to verify file status
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "select event from sda.file_event_log where correlation_id = '$CORRID' order by id DESC LIMIT 1;")" = "disabled" ]; do
    echo "canceling file failed"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting file to be cancelled"
        exit 1
    fi
    sleep 2
done

# re-ingest cancelled file
ingest_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath NA12878.bam.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

ingest_body=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$ingest_payload" \
        '$ARGS.named'
)

curl -k -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$ingest_body" | jq

RETRY_TIMES=0
until [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/verified/ | jq -r '.messages_ready')" -eq 2 ]; do
    echo "waiting for verify to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for re-ingestion to complete"
        exit 1
    fi
    sleep 2
done

decrypted_checksums=$(
    curl -s -X POST \
        -H "content-type:application/json" \
        -u guest:guest http://rabbitmq:15672/api/queues/sda/verified/get \
        -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}' |
        jq -r '.[0].payload|fromjson|.decrypted_checksums|tostring'
)

accession_payload=$(
    jq -r -c -n \
        --arg type accession \
        --arg user test@dummy.org \
        --arg filepath NA12878.bam.c4gh \
        --arg accession_id EGAF74900000001 \
        --argjson decrypted_checksums "$decrypted_checksums" \
        '$ARGS.named|@base64'
)

accession_body=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "accession" \
        --arg payload_encoding base64 \
        --arg payload "$accession_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$accession_body" | jq

RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "select event from sda.file_event_log where correlation_id = '$CORRID' order by id DESC LIMIT 1")" = "ready" ]; do
    echo "waiting for re-ingested file to become ready"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for re-ingested file to become ready"
        echo "re-ingestion after finalize failed"
        exit 1
    fi
    sleep 2
done

echo "re-ingestion after finalize test completed successfully"
