CREATE TABLE users (
    id            BIGSERIAL   PRIMARY KEY,
    username      TEXT        NOT NULL,
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- usernames are unique regardless of case
CREATE UNIQUE INDEX users_username_lower_idx ON users (lower(username));

CREATE TABLE rooms (
    name       TEXT        PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO rooms (name) VALUES ('general');

CREATE TABLE messages (
    id         BIGSERIAL   PRIMARY KEY,
    room       TEXT        NOT NULL REFERENCES rooms (name),
    user_id    BIGINT      NOT NULL REFERENCES users (id),
    text       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT messages_text_length CHECK (char_length(text) BETWEEN 1 AND 2000)
);

-- history queries: newest messages of one room, paginated by id
CREATE INDEX messages_room_id_idx ON messages (room, id DESC);
