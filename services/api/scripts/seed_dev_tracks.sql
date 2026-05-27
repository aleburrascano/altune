-- Dev seed for the view-library feature.
--
-- Inserts 15 sample tracks for the hardcoded dev user id (per ADR-0004:
-- 00000000-0000-0000-0000-000000000001). Idempotent via ON CONFLICT DO
-- NOTHING so re-running is safe.
--
-- Usage:
--   docker exec -i altune-postgres-dev psql -U altune -d altune \
--     < services/api/scripts/seed_dev_tracks.sql

INSERT INTO tracks (id, user_id, title, artist, album, duration_seconds, added_at) VALUES
    ('11111111-1111-1111-1111-000000000001', '00000000-0000-0000-0000-000000000001',
     'Blinding Lights', 'The Weeknd', 'After Hours', 200, '2026-05-01 12:00:00+00'),
    ('11111111-1111-1111-1111-000000000002', '00000000-0000-0000-0000-000000000001',
     'Save Your Tears', 'The Weeknd', 'After Hours', 215, '2026-05-01 12:01:00+00'),
    ('11111111-1111-1111-1111-000000000003', '00000000-0000-0000-0000-000000000001',
     'Take On Me', 'a-ha', 'Hunting High and Low', 225, '2026-05-02 09:30:00+00'),
    ('11111111-1111-1111-1111-000000000004', '00000000-0000-0000-0000-000000000001',
     'Sunflower', 'Post Malone', 'Spider-Man: Into the Spider-Verse', 158, '2026-05-02 17:45:00+00'),
    ('11111111-1111-1111-1111-000000000005', '00000000-0000-0000-0000-000000000001',
     'Levitating', 'Dua Lipa', 'Future Nostalgia', 203, '2026-05-03 08:15:00+00'),
    ('11111111-1111-1111-1111-000000000006', '00000000-0000-0000-0000-000000000001',
     'Watermelon Sugar', 'Harry Styles', 'Fine Line', 174, '2026-05-03 19:20:00+00'),
    ('11111111-1111-1111-1111-000000000007', '00000000-0000-0000-0000-000000000001',
     'Heat Waves', 'Glass Animals', 'Dreamland', 238, '2026-05-04 11:00:00+00'),
    ('11111111-1111-1111-1111-000000000008', '00000000-0000-0000-0000-000000000001',
     'Bad Guy', 'Billie Eilish', 'WHEN WE ALL FALL ASLEEP, WHERE DO WE GO?', 194, '2026-05-04 22:30:00+00'),
    ('11111111-1111-1111-1111-000000000009', '00000000-0000-0000-0000-000000000001',
     'Lose Yourself', 'Eminem', '8 Mile', 326, '2026-05-05 07:00:00+00'),
    ('11111111-1111-1111-1111-00000000000a', '00000000-0000-0000-0000-000000000001',
     'Bohemian Rhapsody', 'Queen', 'A Night at the Opera', 354, '2026-05-06 15:40:00+00'),
    ('11111111-1111-1111-1111-00000000000b', '00000000-0000-0000-0000-000000000001',
     'Africa', 'Toto', 'Toto IV', 295, '2026-05-07 10:00:00+00'),
    ('11111111-1111-1111-1111-00000000000c', '00000000-0000-0000-0000-000000000001',
     'Smells Like Teen Spirit', 'Nirvana', 'Nevermind', 301, '2026-05-08 18:25:00+00'),
    ('11111111-1111-1111-1111-00000000000d', '00000000-0000-0000-0000-000000000001',
     'Mr. Brightside', 'The Killers', 'Hot Fuss', 222, '2026-05-09 13:10:00+00'),
    ('11111111-1111-1111-1111-00000000000e', '00000000-0000-0000-0000-000000000001',
     'Sweet Caroline', 'Neil Diamond', NULL, 201, '2026-05-10 20:00:00+00'),
    ('11111111-1111-1111-1111-00000000000f', '00000000-0000-0000-0000-000000000001',
     'Hotel California', 'Eagles', 'Hotel California', 391, '2026-05-11 14:50:00+00')
ON CONFLICT (id) DO NOTHING;
