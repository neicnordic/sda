#!/bin/bash
set -e

cd shared || true

# clean ingest queue to simplify what is to come.
curl -s -u guest:guest -XDELETE http://rabbitmq:15672/api/queues/sda/inbox/contents

# encrypt file with wrong public key and upload to inbox
yes | /shared/crypt4gh encrypt -p sync.pub.pem -f NA12878.bam
s3cmd -c s3cfg put NA12878.bam.c4gh s3://test_dummy.org/bad.file.c4gh

# truncate file to simulate storage issues
dd if=NA12878_20k_b37.bam.c4gh bs=128 count=1024 of=truncated.c4gh
s3cmd -c s3cfg put truncated.c4gh s3://test_dummy.org/

## test starts here

CORRID=$(
    curl -s -X POST \
        -H "content-type:application/json" \
        -u guest:guest http://rabbitmq:15672/api/queues/sda/inbox/get \
        -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}' | jq -r .[0].properties.correlation_id
)

stream_size=$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')

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
        --arg sha256 "$(echo "aa" | sha256sum | cut -d ' ' -f1)" \
        --arg md5 "$(echo "aa" | md5sum | cut -d ' ' -f1)" \
        '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
)
    
bad_file_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath bad.file.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

bad_file=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$bad_file_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest 'http://rabbitmq:15672/api/exchanges/sda/sda/publish' \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$bad_file" | jq

sleep 10

if [ $((stream_size++)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')" ]; then
    echo "Bad file not moved to error"
    exit 1
fi


missing_file_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath missing.file.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

missing_file=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$missing_file_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest 'http://rabbitmq:15672/api/exchanges/sda/sda/publish' \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$missing_file" | jq

sleep 10


if [ $((stream_size++)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')" ]; then
    echo "missing file not moved to error"
    exit 1
fi

CORRID=$(
    curl -s -X POST \
        -H "content-type:application/json" \
        -u guest:guest http://rabbitmq:15672/api/queues/sda/inbox/get \
        -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}' | jq -r .[0].properties.correlation_id
)

properties=$(
    jq -c -n \
        --argjson delivery_mode 2 \
        --arg correlation_id "$CORRID" \
        --arg content_encoding UTF-8 \
        --arg content_type application/json \
        '$ARGS.named'
)

truncated_file_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath truncated.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        '$ARGS.named|@base64'
)

truncated_file=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "ingest" \
        --arg payload_encoding base64 \
        --arg payload "$truncated_file_payload" \
        '$ARGS.named'
)

curl -s -u guest:guest 'http://rabbitmq:15672/api/exchanges/sda/sda/publish' \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$truncated_file" | jq

sleep 10

if [ $((stream_size++)) -eq "$(curl -s -u guest:guest http://rabbitmq:15672/api/queues/sda/error_stream | jq '.messages_ready')" ]; then
    echo "truncated file not moved to error"
    exit 1
fi

echo "file errors test completed successfully"
