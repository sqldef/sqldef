-- Desired schema for psqldef example
CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    name TEXT,
    email TEXT NOT NULL
);

CREATE INDEX idx_users_email ON users(email);
