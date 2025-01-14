# mapper Service

The mapper service registers mapping of accessionIDs (stable ids for files) to datasetIDs.
Once the file accession ID has been mapped to a dataset ID, the file is removed from the inbox.

## Service Description

The `mapper` service maps file `accessionIDs` to `datasetIDs`.

When running, `mapper` reads messages from the configured RabbitMQ queue (commonly: `mappings`).  
For each message, these steps are taken (if not otherwise noted, errors halt progress and the service moves on to the next message):

1. The message is validated as valid JSON that matches the `dataset-mapping` schema.  
    - If the message can’t be validated it is discarded with an error message is logged.
2. AccessionIDs from the message are mapped to a datasetID (also in the message) in the database.  
    - On error the service sleeps for up to 5 minutes to allow for database recovery, after 5 minutes the message is Nacked, re-queued and an error message is written to the logs.
3. The uploaded files related to each AccessionID is removed from the inbox  
    - If this fails an error will be written to the logs.
4. The RabbitMQ message is Ack'ed.

## Communication

- `Mapper` reads messages from one RabbitMQ queue (commonly: `mappings`).
- `Mapper` maps files to datasets in the database using the `MapFilesToDataset` function.
- `Mapper` retrieves the inbox filepath from the database for each file using the `GetInboxPath` function.
- `Mapper` sets the status of a dataset in the database using the `UpdateDatasetEvent` function.
- `Mapper` removes data from inbox storage.

## Configuration

There are a number of options that can be set for the `mapper` service.
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

### RabbitMQ broker settings

These settings control how `mapper` connects to the RabbitMQ message broker.

- `BROKER_HOST`: hostname of the RabbitMQ server
- `BROKER_PORT`: RabbitMQ broker port (commonly: `5671` with TLS and `5672` without)
- `BROKER_QUEUE`: message queue to read messages from (commonly: `mappings`)
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

  More information is available in the [postgresql documentation](https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-PROTECTION)  
  Note that if `DB_SSLMODE` is set to anything but `disable`, then `DB_CACERT` needs to be set, and if set to `verify-full`, then `DB_CLIENTCERT`, and `DB_CLIENTKEY` must also be set.

- `DB_CLIENTKEY`: key-file for the database client certificate
- `DB_CLIENTCERT`: database client certificate file
- `DB_CACERT`: Certificate Authority (CA) certificate for the database to use

### Storage settings

Storage backend is defined by the `INBOX_TYPE` variable.
Valid values for these options are `S3` or `POSIX`
(Defaults to `POSIX` on unknown values).

The value of these variables define what other variables are read.
The same variables are available for all storage types, differing by prefix (`INBOX_`)

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

- `BROKER_HOST`=
- `BROKER_PORT`=
- `BROKER_USER`=
- `BROKER_PASSWORD`=
- `BROKER_VHOST`=
- `BROKER_QUEUE`=
- `BROKER_EXCHANGE`=
- `BROKER_ROUTINGERROR`=
- `BROKER_SSL`=
- `BROKER_VERIFYPEER`=
- `BROKER_CACERT`=
- `BROKER_CLIENTCERT`=
- `BROKER_CLIENTKEY`=
- `DB_HOST`=
- `DB_PORT`=
- `DB_USER`=
- `DB_PASSWORD`=
- `DB_DATABASE`=
- `DB_SSLMODE`=
- `DB_CLIENTCERT`=
- `DB_CLIENTKEY`=
- `LOG_LEVEL`=
- `INBOX_LOCATION`=
