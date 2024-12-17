#!/bin/bash
set -e

cd shared || true

i=1
while [ $i -le 4 ]; do
    ## get correlation id from upload message
    MSG=$(
        curl -s -X POST \
            -H "content-type:application/json" \
            -u guest:guest http://rabbitmq:15672/api/queues/sda/verified/get \
            -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}'
    )

    corrid=$(jq -r '.[0].properties.correlation_id' <<< "$MSG")
    user=$(jq -r '.[0].payload|fromjson|.user' <<< "$MSG")
    filepath=$(jq -r '.[0].payload|fromjson|.filepath' <<< "$MSG")
    decrypted_checksums=$(jq -r '.[0].payload|fromjson|.decrypted_checksums|tostring' <<< "$MSG")

    ## publish message to trigger backup
    properties=$(
        jq -c -n \
            --argjson delivery_mode 2 \
            --arg correlation_id "$corrid" \
            --arg content_encoding UTF-8 \
            --arg content_type application/json \
            '$ARGS.named'
    )

    accession_id=EGAF7490000000$i
    if [[ "$filepath" == *.bai.c4gh ]]; then
        accession_id="SYNC-123-0000$i"
    fi

    accession_payload=$(
        jq -r -c -n \
            --arg type accession \
            --arg user "$user" \
            --arg filepath "$filepath" \
            --arg accession_id "$accession_id" \
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

    i=$(( i + 1 ))
done

echo "waiting for finalize to complete"

until [ "$(curl -su guest:guest http://rabbitmq:15672/api/queues/sda/completed/ | jq -r '.messages_ready')" -eq 4 ]; do
    echo "waiting for finalize to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for finalize to complete"
        exit 1
    fi
    sleep 2
done

echo "finalize completed successfully"

cat >/shared/direct <<EOD
[default]
access_key=access
secret_key=secretKey
check_ssl_certificate = False
check_ssl_hostname = False
encoding = UTF-8
encrypt = False
guess_mime_type = True
host_base = s3:9000
host_bucket = s3:9000
human_readable_sizes = false
multipart_chunk_size_mb = 50
use_https = False
socket_timeout = 30
EOD

# check DB for archive file names
for file in NA12878.bam.c4gh NA12878.bai.c4gh NA12878_20k_b37.bam.c4gh NA12878_20k_b37.bai.c4gh; do
    archiveName=$(psql -U postgres -h postgres -d sda -At -c "SELECT archive_file_path from sda.files where submission_file_path = '$file';")
    size=$(s3cmd -c direct ls s3://backup/"$archiveName" | tr -s ' ' | cut -d ' ' -f 3)
    if [ "$size" -eq 0 ]; then
        echo "Failed to get size of $file from backup site"
        exit 1
    fi
done

echo "backup and finalize test completed successfully"
