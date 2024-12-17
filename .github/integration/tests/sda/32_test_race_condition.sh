#!/bin/sh
set -e

cd shared || true

head -c 1M </dev/urandom >race_file
yes | /shared/crypt4gh encrypt -p c4gh.pub.pem -f race_file
s3cmd -c s3cfg put race_file.c4gh s3://test_dummy.org/

DEC_SHA=$(sha256sum race_file | cut -d' ' -f 1)
DEC_MD5=$(md5sum race_file | cut -d' ' -f 1)

ENC_SHA=$(sha256sum race_file.c4gh | cut -d' ' -f 1)
ENC_MD5=$(md5sum race_file.c4gh | cut -d' ' -f 1)

## get correlation id from message
CORRID=$(psql -U postgres -h postgres -d sda -At -c "select id from sda.files where submission_file_path = 'race_file.c4gh';")

properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg correlation_id "$CORRID" \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

decrypted_checksums=$(
    jq -c -n \
        --arg sha256 "$DEC_SHA" \
        --arg md5 "$DEC_MD5" \
        '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
)

accession_payload=$(
    jq -r -c -n \
        --arg type accession \
        --arg user test@dummy.org \
        --arg filepath race_file.c4gh \
        --arg accession_id EGAF74900000099 \
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

curl -sq -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$accession_body" | jq

sleep 10
if [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/accession/ | jq -r '.messages_unacknowledged')" -ne 1 ]; then
    echo "::error::Finalize processed message out of order"
    exit 1
fi

encrypted_checksums=$(
    jq -c -n \
        --arg sha256 "$ENC_SHA" \
        --arg md5 "$ENC_MD5" \
        '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
)

ingest_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath race_file.c4gh \
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

curl -s -u guest:guest "http://rabbitmq:15672/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$ingest_body" | jq

RETRY_TIMES=0
until [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/verified/ | jq -r '.messages_ready')" -eq 2 ]; do
    echo "waiting for verify to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for verify to complete"
        exit 1
    fi
    sleep 2
done

RETRY_TIMES=0
until [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/completed/ | jq -r '.messages_ready')" -eq 6 ]; do
    echo "waiting for finalize to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for finalize to complete"
        exit 1
    fi
    sleep 2
done

echo "race condition test completed successfully"
