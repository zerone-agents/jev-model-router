-- Keep retention checks and pagination totals constant-time on the write path.
CREATE TABLE routing_record_count (id INTEGER PRIMARY KEY CHECK (id=1), total INTEGER NOT NULL);
INSERT INTO routing_record_count SELECT 1, COUNT(*) FROM routing_records;
CREATE TRIGGER routing_record_added AFTER INSERT ON routing_records BEGIN
  UPDATE routing_record_count SET total=total+1 WHERE id=1;
END;
CREATE TRIGGER routing_record_removed AFTER DELETE ON routing_records BEGIN
  UPDATE routing_record_count SET total=total-1 WHERE id=1;
END;
