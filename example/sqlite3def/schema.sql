-- Desired schema for sqlite3def example
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT,
    email TEXT NOT NULL
);

CREATE INDEX idx_users_email ON users(email);
