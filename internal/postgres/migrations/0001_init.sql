CREATE TABLE users (
    id            text  PRIMARY KEY,
    email         text  NOT NULL UNIQUE,
    password_hash text  NOT NULL,
    record        jsonb NOT NULL
);

-- One row per enrolled key; the history a signature's key_id resolves against.
CREATE TABLE user_keys (
    key_id     text PRIMARY KEY,
    user_id    text NOT NULL REFERENCES users,
    public_key text NOT NULL
);

CREATE TABLE topics (
    id     text  PRIMARY KEY,
    record jsonb NOT NULL
);

CREATE TABLE props (
    topic_id text  NOT NULL REFERENCES topics,
    id       text  NOT NULL,
    record   jsonb NOT NULL,
    PRIMARY KEY (topic_id, id)
);

CREATE TABLE votes (
    topic_id text  NOT NULL,
    prop_id  text  NOT NULL,
    user_id  text  NOT NULL REFERENCES users,
    record   jsonb NOT NULL,
    PRIMARY KEY (topic_id, prop_id, user_id),
    FOREIGN KEY (topic_id, prop_id) REFERENCES props
);
