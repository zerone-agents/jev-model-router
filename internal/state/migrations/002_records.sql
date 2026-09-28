CREATE TABLE routing_records (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 request_id TEXT NOT NULL UNIQUE,
 created_ns INTEGER NOT NULL,
 body TEXT NOT NULL
);
CREATE INDEX routing_records_created ON routing_records(created_ns);
