# API

The API service provides data submitters with functionality to control
their submissions. Users are authenticated with a JWT.

## Service Description

Endpoints:

- `/files`
  1. Parses and validates the JWT token against the public keys, either locally provisioned or from OIDC JWK endpoints.
  2. The `sub` field from the token is extracted and used as the user's identifier
  3. All files belonging to this user are extracted from the database, together with their latest status and creation date

    Example:

    ```bash
    $ curl 'https://server/files' -H "Authorization: Bearer $token"
  [{"inboxPath":"requester_demo.org/data/file1.c4gh","fileStatus":"uploaded","createAt":"2023-11-13T10:12:43.144242Z"}] 
  ```

  If the `token` is invalid, 401 is returned.

### Admin endpoints

Admin endpoints are only available to a set of whitelisted users specified in the application config.

- `/file/ingest`
  - accepts `POST` requests with JSON data with the format: `{"filepath": "</PATH/TO/FILE/IN/INBOX>", "user": "<USERNAME>"}`
  - triggers the ingestion of the file.

- Error codes
  - `200` Query execute ok.
  - `400` Error due to bad payload i.e. wrong `user` + `filepath` combination.
  - `401` User is not in the list of admins.
  - `500` Internal error due to DB or MQ failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d '{"filepath": "/uploads/file.c4gh", "user": "testuser"}' https://HOSTNAME/file/ingest
    ```

- `/file/accession`
  - accepts `POST` requests with JSON data with the format: `{"accession_id": "<FILE_ACCESSION>", "filepath": "</PATH/TO/FILE/IN/INBOX>", "user": "<USERNAME>"}`
  - assigns accession ID to the file.

- Error codes
  - `200` Query execute ok.
  - `400` Error due to bad payload i.e. wrong `user` + `filepath` combination.
  - `401` User is not in the list of admins.
  - `500` Internal error due to DB or MQ failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d '{"accession_id": "my-id-01", "filepath": "/uploads/file.c4gh", "user": "testuser"}' https://HOSTNAME/file/accession
    ```

- `/dataset/create`
  - accepts `POST` requests with JSON data with the format: `{"accession_ids": ["<FILE_ACCESSION_01>", "<FILE_ACCESSION_02>"], "dataset_id": "<DATASET_01>"}`
  - creates a datset from the list of accession IDs and the dataset ID.

- Error codes
  - `200` Query execute ok.
  - `400` Error due to bad payload.
  - `401` User is not in the list of admins.
  - `500` Internal error due to DB or MQ failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -H "Content-Type: application/json" -X POST -d '{"accession_idd": ["my-id-01", "my-id-02"], "dataset_id": "my-dataset-01"}' https://HOSTNAME/dataset/create
    ```

- `/dataset/release/*dataset`
  - accepts `POST` requests with the dataset name as last part of the path`
  - releases a dataset so that it can be downloaded.

- Error codes
  - `200` Query execute ok.
  - `400` Error due to bad payload.
  - `401` User is not in the list of admins.
  - `500` Internal error due to DB or MQ failures.

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -X POST  https://HOSTNAME/dataset/release/my-dataset-01
    ```

- `/users`
  - accepts `GET` requests`
  - Returns all users with active uploads as a JSON array

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -X GET  https://HOSTNAME/users
    ```

- Error codes
  - `200` Query execute ok.
  - `401` User is not in the list of admins.
  - `500` Internal error due to DB failure.

- `/users/:username/files`
  - accepts `GET` requests`
  - Returns all files for a user with active uploads as a JSON array

    Example:

    ```bash
    curl -H "Authorization: Bearer $token" -X GET  https://HOSTNAME/users
    ```

- Error codes
  - `200` Query execute ok.
  - `401` User is not in the list of admins.
  - `500` Internal error due to DB failure.
