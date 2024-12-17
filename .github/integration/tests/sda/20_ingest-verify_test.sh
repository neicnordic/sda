#!/bin/sh
set -e

cd shared || true

for file in NA12878.bam NA12878_20k_b37.bam NA12878.bai NA12878_20k_b37.bai; do
    ENC_SHA=$(sha256sum "$file.c4gh" | cut -d' ' -f 1)
    ENC_MD5=$(md5sum "$file.c4gh" | cut -d' ' -f 1)

    ## get correlation id from upload message
    CORRID=$(
        curl -s -X POST \
            -H "content-type:application/json" \
            -u guest:guest http://rabbitmq:15672/api/queues/sda/inbox/get \
            -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}' | jq -r .[0].properties.correlation_id
    )

    ## publish message to trigger ingestion
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

    ingest_payload=$(
        jq -r -c -n \
            --arg type ingest \
            --arg user test@dummy.org \
            --arg filepath "$file.c4gh" \
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
done

echo "waiting for verify to complete"
RETRY_TIMES=0
until [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/verified/ | jq -r '.messages_ready')" -eq 4 ]; do
    echo "waiting for verify to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for verify to complete"
        exit 1
    fi
    sleep 2
done

# check that the files have key hashes assigned
key_hashes="$(psql -U postgres -h postgres -d sda -At -c "select count(distinct key_hash) from sda.files")"
if [ "$key_hashes" -eq 0 ]; then
	echo "::error::Ingested files did not have any key hashes."
	exit 1
fi

echo "ingestion and verification test completed successfully"