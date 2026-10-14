ALTER TABLE track_metadata_changes DROP CONSTRAINT IF EXISTS track_metadata_changes_field_check;
ALTER TABLE track_metadata_changes ADD CONSTRAINT track_metadata_changes_field_check
    CHECK (field IN ('year', 'album_artist', 'isrc', 'track_number', 'genre', 'catalog_duration_seconds', 'artwork_url',
                     'title', 'artist', 'album', 'dedup_key'));
ALTER TABLE track_metadata_changes ADD COLUMN IF NOT EXISTS change_group TEXT;
