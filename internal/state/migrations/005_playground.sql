CREATE TABLE playground_admissions (session_id TEXT NOT NULL, accepted_at INTEGER NOT NULL);
CREATE INDEX playground_admission_time ON playground_admissions(accepted_at);
CREATE INDEX playground_admission_session ON playground_admissions(session_id, accepted_at);
CREATE TABLE playground_days (day TEXT PRIMARY KEY, count INTEGER NOT NULL);
