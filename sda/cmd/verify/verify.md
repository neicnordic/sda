# verify Service

Uses a crypt4gh secret key, this service can decrypt the stored files and checksum them against the embedded checksum for the unencrypted file.

## Service Description

The `verify` service ensures that ingested files are encrypted with the correct key, and that the provided checksums match those of the ingested files.

When running, `verify` reads messages from the configured RabbitMQ queue (commonly: `archived`).
For each message, these steps are taken (if not otherwise noted, errors halt progress and the service moves on to the next message.
Unless explicitly stated, error messages are *not* written to the RabbitMQ error queue, and messages are not NACK or ACKed.):

1. The message is validated as valid JSON that matches the `ingestion-verification` schema.
    - If the message can’t be validated it is discarded with an error message in the logs.
2. The service attempts to fetch the header for the file id in the message from the database.
    - If this fails a NACK will be sent for the RabbitMQ message, the error will be written to the logs, and sent to the RabbitMQ error queue.
3. The file size of the encrypted file is fetched from the archive storage system.
    - If this fails an error will be written to the logs.
4. The archive file is then opened for reading.
    - If this fails an error will be written to the logs and to the RabbitMQ error queue.
5. A decryptor is opened with the archive file.
    - If this fails an error will be written to the logs.
6. The file size, md5 and sha256 checksum will be read from the decryptor.
    - If this fails an error will be written to the logs.
7. If the `re_verify` boolean is not set in the RabbitMQ message, the message processing ends here, and continues with the next message.

    - Otherwise the processing continues with verification:
      1. A verification message is created, and validated against the `ingestion-accession-request` schema.
          - If this fails an error will be written to the logs.
      2. The file is marked as *verified* in the database (*COMPLETED* if you are using database schema <= `3`).
          - If this fails an error will be written to the logs.
      3. The verification message created in step 7.1 is sent to the `verified` queue.
          - If this fails an error will be written to the logs.
      4. The original RabbitMQ message is ACKed.
          - If this fails an error is written to the logs, but processing continues to the next step.

## Communication

- `Verify` reads messages from one RabbitMQ queue (commonly: `archived`).
- `Verify` publishes messages to one RabbitMQ queue (commonly: `verified`).
- `Verify` gets the file encryption header from the database using `GetHeader`,
and marks the files as `verified` (`COMPLETED` in db version <= `2.0`) using `MarkCompleted`.

## Configuration

There are a number of options that can be set for the `verify` service.
These settings can be set by mounting a yaml-file at `/config.yaml` with settings.

ex.

```yaml
log:
  level: "debug"
  format: "json"
```

They may also be set using environment variables like:

```bash
export LOG_LEVEL="debug"
export LOG_FORMAT="json"
```

### Keyfile settings

These settings control which crypt4gh keyfile is loaded.

- `C4GH_FILEPATH`: filepath to the crypt4gh keyfile
- `C4GH_PASSPHRASE`: pass phrase to unlock the keyfile

### RabbitMQ broker settings

These settings control how `verify` connects to the RabbitMQ message broker.

- `BROKER_HOST`: hostname of the RabbitMQ server
- `BROKER_PORT`: RabbitMQ broker port (commonly: `5671` with TLS and `5672` without)
- `BROKER_QUEUE`: message queue to read messages from (commonly: `archived`)
- `BROKER_ROUTINGKEY`: Routing key for publishing messages (commonly: `verified`)
- `BROKER_USER`: username to connect to RabbitMQ
- `BROKER_PASSWORD`: password to connect to RabbitMQ
- `BROKER_PREFETCHCOUNT`: Number of messages to pull from the message server at the time (default to `2`)
- `BROKER_EXCHANGE`= the exchange name (i.e., `sda`)

### PostgreSQL Database settings

- `DB_HOST`: hostname for the postgresql database
- `DB_PORT`: database port (commonly: `5432`)
- `DB_USER`: username for the database
- `DB_PASSWORD`: password for the database
- `DB_DATABASE`: database name
- `DB_SSLMODE`: The TLS encryption policy to use for database connections, valid options are:
    - `disable`
    - `allow`
    - `prefer`
    - `require`
    - `verify-ca`
    - `verify-full`

  More information is available
  [in the postgresql documentation](https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-PROTECTION)

  Note that if `DB_SSLMODE` is set to anything but `disable`, then `DB_CACERT` needs to be set,
  and if set to `verify-full`, then `DB_CLIENTCERT`, and `DB_CLIENTKEY` must also be set.

- `DB_CLIENTKEY`: key-file for the database client certificate
- `DB_CLIENTCERT`: database client certificate file
- `DB_CACERT`: Certificate Authority (CA) certificate for the database to use

### Storage settings

Storage backend is defined by the `ARCHIVE_TYPE` variable.
Valid values for these options are `S3` or `POSIX`
(Defaults to `POSIX` on unknown values).

The value of these variables define what other variables are read.
The same variables are available for all storage types, differing by prefix (`ARCHIVE_`)

if `*_TYPE` is `S3` then the following variables are available:

- `*_URL`: URL to the S3 system
- `*_ACCESSKEY`: The S3 access and secret key are used to authenticate to S3,
 [more info at AWS](https://docs.aws.amazon.com/general/latest/gr/aws-sec-cred-types.html#access-keys-and-secret-access-keys)
- `*_SECRETKEY`: The S3 access and secret key are used to authenticate to S3,
 [more info at AWS](https://docs.aws.amazon.com/general/latest/gr/aws-sec-cred-types.html#access-keys-and-secret-access-keys)
- `*_BUCKET`: The S3 bucket to use as the storage root
- `*_PORT`: S3 connection port (default: `443`)
- `*_REGION`: S3 region (default: `us-east-1`)
- `*_CHUNKSIZE`: S3 chunk size for multipart uploads.
- `*_CACERT`: Certificate Authority (CA) certificate for the storage system, this is only needed if the S3 server has a certificate signed by a private entity

and if `*_TYPE` is `POSIX`:

- `*_LOCATION`: POSIX path to use as storage root

### Logging settings

- `LOG_FORMAT` can be set to `json` to get logs in JSON format. All other values result in text logging.
- `LOG_LEVEL` can be set to one of the following, in increasing order of severity:
    - `trace`
    - `debug`
    - `info`
    - `warn` (or `warning`)
    - `error`
    - `fatal`
    - `panic`

### Required settings (Example)

The following configuration variables are essential for a successful setup.

- `ARCHIVE_TYPE`=
- `ARCHIVE_LOCATION`=
- `BROKER_HOST`=
- `BROKER_PORT`=
- `BROKER_USER`=
- `BROKER_PASSWORD`=
- `BROKER_VHOST`=
- `BROKER_QUEUE`=
- `BROKER_EXCHANGE`=
- `BROKER_ROUTINGKEY`=
- `BROKER_ROUTINGERROR`=
- `BROKER_SSL`=
- `BROKER_VERIFYPEER`=
- `BROKER_CACERT`=
- `BROKER_CLIENTCERT`=
- `BROKER_CLIENTKEY`=
- `C4GH_PASSPHRASE`=
- `C4GH_FILEPATH`=
- `DB_HOST`=
- `DB_PORT`=
- `DB_USER`=
- `DB_PASSWORD`=
- `DB_DATABASE`=
- `DB_SSLMODE`=
- `DB_CLIENTCERT`=
- `DB_CLIENTKEY`=
- `INBOX_LOCATION`=
- `LOG_LEVEL`=
