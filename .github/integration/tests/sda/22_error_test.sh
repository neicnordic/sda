#!/bin/sh
set -e

cd shared || true
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

s3cmd -c direct rm s3://inbox/test_dummy.org/NB12878.bam.c4gh

ENC_SHA=$(sha256sum NA12878.bam.c4gh | cut -d' ' -f 1)

URI=http://rabbitmq:15672
if [ -n "$PGSSLCERT" ]; then
    URI=https://rabbitmq:15671
fi

## get correlation id from message
CORRID=$(psql -U postgres -h postgres -d sda -At -c "select id from sda.files where submission_file_path = 'NB12878.bam.c4gh';")

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
        '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
)

ingest_payload=$(
    jq -r -c -n \
        --arg type ingest \
        --arg user test@dummy.org \
        --arg filepath NB12878.bam.c4gh \
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

curl -k -u guest:guest "$URI/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$ingest_body" | jq

# check database to verify file status
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE correlation_id = '$CORRID' ORDER BY ID DESC LIMIT 1;")" = "error" ]; do
    echo "waiting for file error to be logged by ingest"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for ingest to set file error"
        exit 1
    fi
    sleep 2
done

## give the file a non existing archive path
psql -U postgres -h postgres -d sda -Atq -c "UPDATE sda.files SET archive_file_path = '$CORRID', header = '637279707434676801000000010000006c00000000000000' WHERE id = '$CORRID';"
psql -U postgres -h postgres -d sda -Atq -c "INSERT INTO sda.file_event_log(file_id, correlation_id, event) VALUES('$CORRID', '$CORRID', 'archived');"

encrypted_checksums=$(
    jq -c -n \
        --arg sha256 "$ENC_SHA" \
        '$ARGS.named|to_entries|map(with_entries(select(.key=="key").key="type"))'
)

verify_payload=$(
    jq -r -c -n \
        --arg user test@dummy.com \
        --arg archive_path "$CORRID" \
        --arg file_id "$CORRID" \
        --arg filepath NB12878.bam.c4gh \
        --argjson encrypted_checksums "$encrypted_checksums" \
        --argjson re_verify false \
        '$ARGS.named|@base64'
)

verify_body=$(
    jq -c -n \
        --arg vhost sda \
        --arg name sda \
        --argjson properties "$properties" \
        --arg routing_key "archived" \
        --arg payload_encoding base64 \
        --arg payload "$verify_payload" \
        '$ARGS.named'
)

curl -k -u guest:guest "$URI/api/exchanges/sda/sda/publish" \
    -H 'Content-Type: application/json;charset=UTF-8' \
    -d "$verify_body" | jq

# check database to verify file status
RETRY_TIMES=0
until [ "$(psql -U postgres -h postgres -d sda -At -c "SELECT event FROM sda.file_event_log WHERE correlation_id = '$CORRID' ORDER BY ID DESC LIMIT 1;")" = "error" ]; do
    echo "waiting for file error to be logged by verify"
    date
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for verify to set file error"
        exit 1
    fi
    sleep 2
done

echo ""
echo "file error test completed successfully"
