-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Copyright (C) 2026 The Braid Authors

-- Chunk series are per uploading device, so one device's new series never replaces another's.
-- Pending parts are scratch space (clients resend them), so existing rows are dropped.
DELETE FROM raw_line_parts;
ALTER TABLE raw_line_parts ADD COLUMN device_id uuid NOT NULL;
ALTER TABLE raw_line_parts DROP CONSTRAINT raw_line_parts_pkey;
ALTER TABLE raw_line_parts ADD PRIMARY KEY (user_id, source_file_id, "offset", device_id, idx);
