CREATE TABLE health_meta(id INTEGER PRIMARY KEY CHECK(id=1), fingerprint_key BLOB, version INTEGER NOT NULL);
CREATE TABLE health_records(id TEXT PRIMARY KEY, body TEXT NOT NULL);
CREATE TABLE health_resources(id TEXT PRIMARY KEY, revision TEXT NOT NULL);
INSERT INTO health_resources(id,revision) SELECT id,lower(hex(randomblob(16))) FROM models;
CREATE TABLE health_initialized(id INTEGER PRIMARY KEY CHECK(id=1), key_hash BLOB NOT NULL);
