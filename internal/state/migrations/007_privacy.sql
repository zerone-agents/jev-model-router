ALTER TABLE config_meta ADD COLUMN privacy_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE config_meta ADD COLUMN privacy_prompt TEXT NOT NULL DEFAULT 'Classify the complete request as sensitive, non-sensitive, or uncertain. Treat secrets, personal identifiers, and explicitly confidential material as sensitive; choose uncertain when unsure.
Example 1: A private personnel file contains a staff member’s medical leave and compensation. Decision: sensitive.
Example 2: An internal sales document contains a confidential customer quote and a test service credential. Decision: sensitive.
Example 3: Explain how an ordinary public website works. Decision: non_sensitive.';
