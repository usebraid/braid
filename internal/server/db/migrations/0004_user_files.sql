-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors
-- Store raw data once per user (01 section 3.4 amendment). Nothing is deleted except exact duplicates.

ALTER TABLE source_files ADD COLUMN scope_device_id uuid;
ALTER TABLE raw_lines ADD COLUMN device_id uuid;
ALTER TABLE raw_snapshots ADD COLUMN device_id uuid;

UPDATE raw_lines l SET device_id = f.device_id FROM source_files f WHERE f.id = l.source_file_id;
UPDATE raw_snapshots s SET device_id = f.device_id FROM source_files f WHERE f.id = s.source_file_id;
UPDATE source_files SET scope_device_id = device_id;

ALTER TABLE raw_lines ALTER COLUMN device_id SET NOT NULL;
ALTER TABLE raw_snapshots ALTER COLUMN device_id SET NOT NULL;
ALTER TABLE raw_lines ADD FOREIGN KEY (device_id, user_id) REFERENCES devices (id, user_id) ON DELETE CASCADE;
ALTER TABLE raw_snapshots ADD FOREIGN KEY (device_id, user_id) REFERENCES devices (id, user_id) ON DELETE CASCADE;
ALTER TABLE source_files ADD FOREIGN KEY (scope_device_id, user_id) REFERENCES devices (id, user_id) ON DELETE CASCADE;

-- Merge rows of one (user, path, head) into the primary: highest end_offset, then lowest id.
DO $$
DECLARE
  r record;
  conflicted bigint[] := '{}';
  clash boolean;
BEGIN
  FOR r IN
    SELECT id, kind, primary_id FROM (
      SELECT id, kind, first_value(id) OVER w AS primary_id, row_number() OVER w AS rn
        FROM source_files WINDOW w AS (PARTITION BY user_id, rel_path, head_hash ORDER BY end_offset DESC, id)
    ) x WHERE rn > 1 ORDER BY id
  LOOP
    IF r.kind = 'snapshot' THEN
      UPDATE raw_snapshots SET source_file_id = r.primary_id WHERE source_file_id = r.id;
      DELETE FROM source_files WHERE id = r.id;
      -- keep the "newest differs from its predecessor" invariant
      DELETE FROM raw_snapshots WHERE id IN (
        SELECT id FROM (SELECT id, content_hash, lag(content_hash) OVER (ORDER BY received_at, id) AS prev
                          FROM raw_snapshots WHERE source_file_id = r.primary_id) q WHERE content_hash = prev);
      CONTINUE;
    END IF;
    SELECT EXISTS (
      SELECT 1 FROM raw_lines a JOIN raw_lines b ON b."offset" = a."offset"
       WHERE a.source_file_id = r.id AND b.source_file_id = r.primary_id
         AND NOT EXISTS (SELECT 1 FROM raw_lines c WHERE c.source_file_id = r.primary_id
                            AND c."offset" = a."offset" AND c.line_hash = a.line_hash)
    ) INTO clash;
    IF clash THEN
      conflicted := conflicted || r.id;
      CONTINUE;
    END IF;
    DELETE FROM raw_lines x WHERE x.source_file_id IN (r.id, r.primary_id) AND EXISTS (
      SELECT 1 FROM raw_lines y WHERE y.source_file_id IN (r.id, r.primary_id) AND y.source_file_id <> x.source_file_id
         AND y."offset" = x."offset" AND y.line_hash = x.line_hash AND y.id < x.id);
    UPDATE raw_lines SET source_file_id = r.primary_id WHERE source_file_id = r.id;
    UPDATE source_files SET end_offset = greatest(end_offset, (SELECT end_offset FROM source_files WHERE id = r.id)) WHERE id = r.primary_id;
    DELETE FROM source_files WHERE id = r.id;
  END LOOP;
  UPDATE source_files SET scope_device_id = NULL WHERE NOT (id = ANY(conflicted));
END $$;

ALTER TABLE source_files DROP COLUMN device_id; -- drops the old unique and composite FK
ALTER TABLE source_files ADD CONSTRAINT source_files_identity
  UNIQUE NULLS NOT DISTINCT (user_id, rel_path, head_hash, scope_device_id);
