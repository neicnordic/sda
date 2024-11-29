CREATE TABLE IF NOT EXISTS access_token (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    token_value VARCHAR(4096),
    expiration TIMESTAMP NULL,
    token_type VARCHAR(256),
    refresh_token_id BIGINT,
    client_id VARCHAR(256) NOT NULL,
    auth_holder_id BIGINT,
    approved_site_id BIGINT
);

CREATE TABLE IF NOT EXISTS authorization_code (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    code VARCHAR(256),
    auth_holder_id BIGINT,
    expiration TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS approved_site (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(256),
    client_id VARCHAR(256),
    creation_date TIMESTAMP NULL,
    access_date TIMESTAMP NULL,
    timeout_date TIMESTAMP NULL,
    whitelisted_site_id BIGINT
);

CREATE TABLE IF NOT EXISTS approved_site_scope (
    owner_id BIGINT,
    scope VARCHAR(256)
);

CREATE TABLE IF NOT EXISTS authentication_holder (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_auth_id BIGINT,
    approved BOOLEAN,
    redirect_uri VARCHAR(2048),
    client_id VARCHAR(256)
);

CREATE TABLE IF NOT EXISTS authentication_holder_authority (
    owner_id BIGINT,
    authority VARCHAR(256)
);

CREATE TABLE IF NOT EXISTS authentication_holder_resource_id (
    owner_id BIGINT,
    resource_id VARCHAR(2048)
);

CREATE TABLE IF NOT EXISTS authentication_holder_response_type (
    owner_id BIGINT,
    response_type VARCHAR(2048)
);

CREATE TABLE IF NOT EXISTS authentication_holder_extension (
    owner_id BIGINT,
    extension VARCHAR(2048),
    val VARCHAR(2048)
);

CREATE TABLE IF NOT EXISTS authentication_holder_scope (
    owner_id BIGINT,
    scope VARCHAR(2048)
);

CREATE TABLE IF NOT EXISTS authentication_holder_request_parameter (
    owner_id BIGINT,
    param VARCHAR(2048),
    val TEXT
);

CREATE TABLE IF NOT EXISTS saved_user_auth (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    acr VARCHAR(1024),
    auth_time BIGINT DEFAULT NULL,
    name VARCHAR(1024),
    authenticated BOOLEAN,
    authentication_attributes TEXT
);

CREATE TABLE IF NOT EXISTS saved_user_auth_authority (
    owner_id BIGINT,
    authority VARCHAR(256)
);

CREATE TABLE IF NOT EXISTS refresh_token (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    token_value VARCHAR(4096),
    expiration TIMESTAMP NULL,
    auth_holder_id BIGINT,
    client_id VARCHAR(256) NOT NULL
);

CREATE TABLE IF NOT EXISTS token_scope (
    owner_id BIGINT,
    scope VARCHAR(2048)
);

CREATE TABLE IF NOT EXISTS device_code (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    device_code VARCHAR(1024),
    user_code VARCHAR(1024),
    expiration TIMESTAMP NULL,
    client_id VARCHAR(256),
    approved BOOLEAN,
    auth_holder_id BIGINT,
    recorded_error TEXT DEFAULT NULL
);

CREATE TABLE IF NOT EXISTS device_code_scope (
    owner_id BIGINT NOT NULL,
    scope VARCHAR(256) NOT NULL
);

CREATE TABLE IF NOT EXISTS device_code_request_parameter (
    owner_id BIGINT,
    param VARCHAR(2048),
    val VARCHAR(2048)
);

alter table access_token
    add constraint access_token_authentication_holder_id_fk
        foreign key (auth_holder_id) references authentication_holder (id)
            on update cascade on delete set null;

alter table access_token
    add constraint access_token_refresh_token_id_fk
        foreign key (refresh_token_id) references refresh_token (id)
            on update cascade on delete set null;

alter table approved_site_scope
    add constraint approved_site_scope_approved_site_id_fk
        foreign key (owner_id) references approved_site (id)
            on update cascade on delete cascade;

alter table authentication_holder_authority
    add constraint authentication_holder_authority_authentication_holder_id_fk
        foreign key (owner_id) references authentication_holder (id)
            on update cascade on delete cascade;

alter table authentication_holder_extension
    add constraint authentication_holder_extension_authentication_holder_id_fk
        foreign key (owner_id) references authentication_holder (id)
            on update cascade on delete cascade;

alter table authentication_holder_request_parameter
    add constraint auth_holder_request_parameter_authentication_holder_id_fk
        foreign key (owner_id) references authentication_holder (id)
            on update cascade on delete cascade;

alter table authentication_holder_resource_id
    add constraint authentication_holder_resource_id_authentication_holder_id_fk
        foreign key (owner_id) references authentication_holder (id)
            on update cascade on delete cascade;

alter table authentication_holder_response_type
    add constraint authentication_holder_response_type_authentication_holder_id_fk
        foreign key (owner_id) references authentication_holder (id)
            on update cascade on delete cascade;

alter table authentication_holder
    add constraint authentication_holder_saved_user_auth_id_fk
        foreign key (user_auth_id) references saved_user_auth (id)
            on update cascade on delete cascade;

alter table authentication_holder_scope
    add constraint authentication_holder_scope_authentication_holder_id_fk
        foreign key (owner_id) references authentication_holder (id)
            on update cascade on delete cascade;

alter table authorization_code
    add constraint authorization_code_authentication_holder_id_fk
        foreign key (auth_holder_id) references authentication_holder (id)
            on update cascade on delete cascade;

alter table device_code
    add constraint device_code_authentication_holder_id_fk
        foreign key (auth_holder_id) references authentication_holder (id)
            on update cascade on delete set null;

alter table device_code_request_parameter
    add constraint device_code_request_parameter_device_code_id_fk
        foreign key (owner_id) references device_code (id)
            on update cascade on delete cascade;

alter table device_code_scope
    add constraint device_code_scope_device_code_id_fk
        foreign key (owner_id) references device_code (id)
            on update cascade on delete cascade;

alter table refresh_token
    add constraint refresh_token_authentication_holder_id_fk
        foreign key (auth_holder_id) references authentication_holder (id)
            on update cascade on delete set null;

alter table saved_user_auth_authority
    add constraint saved_user_auth_authority_saved_user_auth_id_fk
        foreign key (owner_id) references saved_user_auth (id)
            on update cascade on delete cascade;

alter table token_scope
    add constraint token_scope_refresh_token_id_fk
        foreign key (owner_id) references access_token (id)
            on update cascade on delete cascade;