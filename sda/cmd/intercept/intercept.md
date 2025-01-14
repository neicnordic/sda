# intercept Service

The `intercept` service relays messages between `CentralEGA` and Federated EGA nodes.

## Service Description

When running, `intercept` reads messages from the configured RabbitMQ queue (commonly: `from_cega`).
For each message, these steps are taken:

1. The message type is read from the message `type` field.
   1. If the message `type` is not known, an error is logged and the message is Ack'ed.
2. The correct queue for the message is decided based on message type.
3. The message is sent to the queue. 
   - This has no error handling as the resend-mechanism hasn't been finished.
4. The message is Ack'ed.
5. If the message type is of unknown type, we acknowledge it and send it to `catch_all.dead` (needs to exist)

## Communication

- `Intercept` reads messages from one queue (commonly: `from_cega`).
- `Intercept` publishes messages to three queues, `accession`, `ingest`, and `mappings`.

## Configuration

There are a number of options that can be set for the `intercept` service.
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

These settings control how `intercept` connects to the RabbitMQ message broker.

- `BROKER_HOST`: hostname of the RabbitMQ server
- `BROKER_PORT`: RabbitMQ broker port (commonly: `5671` with TLS and `5672` without)
- `BROKER_QUEUE`: message queue to read messages from (commonly: `from_cega`)
- `BROKER_USER`: username to connect to RabbitMQ
- `BROKER_PASSWORD`: password to connect to RabbitMQ
- `BROKER_VHOST`= the virtual host of the exchange (i.e., `test`)
- `BROKER_EXCHANGE`= the exchange name (i.e., `sda`)

### PostgreSQL Database settings:

- `DB_HOST`: hostname for the postgresql database
- `DB_PORT`: database port (commonly: `5432`)
- `DB_USER`: username for the database
- `DB_PASSWORD`: password for the database
- `DB_DATABASE`: database name (i.e., `sda`)
- `DB_SSLMODE`: The TLS encryption policy to use for database connections ([See Docs](https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-PROTECTION)), valid options are:
    - `disable`
    - `allow`
    - `prefer`
    - `require`
    - `verify-ca`
    - `verify-full`

### Logging settings

- `LOG_FORMAT` can be set to “json” to get logs in json format, all other values result in text logging.
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
- `BROKER_QUEUE`=
- `BROKER_VHOST`=
- `BROKER_EXCHANGE`=
- `DB_HOST`=
- `DB_PORT`=
- `DB_USER`=
- `DB_PASSWORD`=
- `DB_DATABASE`=
- `DB_SSLMODE`=
- `LOG_LEVEL`=

