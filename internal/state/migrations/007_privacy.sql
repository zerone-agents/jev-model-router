ALTER TABLE config_meta ADD COLUMN privacy_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE config_meta ADD COLUMN privacy_prompt TEXT NOT NULL DEFAULT 'Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.';
