#!/bin/sh
set -e

if [ -z "$STORAGETYPE" ]; then
    echo "STORAGETYPE not set, exiting"
    exit 1
fi

cd shared || true

## verify that messages exists in MQ
URI=http://rabbitmq:15672
if [ -n "$PGSSLCERT" ]; then
    URI=https://rabbitmq:15671
fi
## empty all queues ##
for q in accession archived backup completed inbox ingest mappings verified; do
    curl -s -k -u guest:guest -X DELETE "$URI/api/queues/sda/$q/contents"
done
## truncate database
psql -U postgres -h postgres -d sda -At -c "TRUNCATE TABLE sda.files, sda.encryption_keys CASCADE;"

for file in NA12878.bam NA12878_20k_b37.bam NA12878.bai NA12878_20k_b37.bai; do
    curl --retry 100 -s -L -o /shared/$file "https://github.com/ga4gh/htsget-refserver/raw/main/data/gcp/gatk-test-data/wgs_bam/$file"
    if [ ! -f "$file.c4gh" ]; then
        yes | /shared/crypt4gh encrypt -p c4gh.pub.pem -f "$file"
    fi
    s3cmd -c s3cfg put "$file.c4gh" s3://test_dummy.org/
done

## reupload a file under a different name
s3cmd -c s3cfg put NA12878.bam.c4gh s3://test_dummy.org/NB12878.bam.c4gh

## reupload a file with the same name
s3cmd -c s3cfg put NA12878.bam.c4gh s3://test_dummy.org/


echo "waiting for upload to complete"
RETRY_TIMES=0
until [ "$(curl -s -k -u guest:guest $URI/api/queues/sda/inbox | jq -r '."messages_ready"')" -eq 6 ]; do
    echo "waiting for upload to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for upload to complete"
        exit 1
    fi
    sleep 2
done

num_rows=$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) from sda.files;")
if [ "$num_rows" -ne 5 ]; then
    echo "database queries for register_files failed, expected 5 got $num_rows"
    exit 1
fi

num_log_rows=$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) from sda.file_event_log;")
if [ "$num_log_rows" -ne 12 ]; then
    echo "database queries for file_event_logs failed, expected 12 got $num_log_rows"
    exit 1
fi

## test with token from OIDC service
echo "testing with OIDC token"
newToken=$(curl http://oidc:8080/tokens | jq '.[0]')
cp s3cfg oidc_s3cfg
sed -i "s/access_token=.*/access_token=$newToken/" oidc_s3cfg

s3cmd -c oidc_s3cfg put NA12878.bam.c4gh s3://requester_demo.org/data/file1.c4gh

## verify that messages exists in MQ
echo "waiting for upload to complete"
RETRY_TIMES=0
until [ "$(curl -s -k -u guest:guest $URI/api/queues/sda/inbox | jq -r '."messages_ready"')" -eq 7 ]; do
    echo "waiting for upload to complete"
    RETRY_TIMES=$((RETRY_TIMES + 1))
    if [ "$RETRY_TIMES" -eq 30 ]; then
        echo "::error::Time out while waiting for upload to complete"
        exit 1
    fi
    sleep 2
done

num_rows=$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) from sda.files;")
if [ "$num_rows" -ne 6 ]; then
    echo "database queries for register_files failed, expected 6 got $num_rows"
    exit 1
fi

num_log_rows=$(psql -U postgres -h postgres -d sda -At -c "SELECT COUNT(*) from sda.file_event_log;")
if [ "$num_log_rows" -ne 14 ]; then
    echo "database queries for file_event_logs failed, expected 14 got $num_log_rows"
    exit 1
fi


echo "files uploaded successfully"
