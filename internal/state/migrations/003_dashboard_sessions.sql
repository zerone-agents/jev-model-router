CREATE TABLE session_auth (
 id INTEGER PRIMARY KEY CHECK (id=1),
 salt BLOB NOT NULL, verifier BLOB NOT NULL,
 generation TEXT NOT NULL, origin_policy TEXT NOT NULL
);
CREATE TABLE dashboard_sessions (
 hash TEXT PRIMARY KEY, generation TEXT NOT NULL, origin TEXT NOT NULL,
 created_ns INTEGER NOT NULL, expires_ns INTEGER NOT NULL
);
CREATE INDEX dashboard_sessions_expiry ON dashboard_sessions(expires_ns);
