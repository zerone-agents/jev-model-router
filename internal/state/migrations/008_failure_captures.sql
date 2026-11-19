CREATE TABLE failure_captures (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT NOT NULL UNIQUE,
    created_ns INTEGER NOT NULL,
    body TEXT NOT NULL,
    charge INTEGER NOT NULL CHECK (charge >= 1024)
);
CREATE INDEX failure_captures_expiry ON failure_captures(created_ns);
