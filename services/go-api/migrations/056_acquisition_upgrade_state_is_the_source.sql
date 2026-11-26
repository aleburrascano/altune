SET LOCAL lock_timeout = '5s';

DROP TRIGGER IF EXISTS tracks_mirror_acquisition_upgrade_state ON tracks;
DROP FUNCTION IF EXISTS mirror_acquisition_upgrade_state();

CREATE OR REPLACE FUNCTION mirror_acquisition_upgrade_state_back() RETURNS trigger AS $$
DECLARE
    keys CONSTANT text[] := ARRAY[
        'upgrade_tried_at', 'upgrade_tries',
        'best_available_generation', 'best_available_reason', 'best_available_at',
        'in_place_generation', 'in_place_at', 'in_place_basis', 'in_place_bit_error_rate', 'in_place_failures'
    ];
BEGIN
    UPDATE tracks SET
        acquisition_evidence = (COALESCE(acquisition_evidence, '{}'::jsonb) - keys) || COALESCE(NEW.evidence, '{}'::jsonb),
        rejected_source_keys = NEW.rejected_source_keys
    WHERE id = NEW.track_id
        AND (
            (COALESCE(acquisition_evidence, '{}'::jsonb) - keys) || COALESCE(NEW.evidence, '{}'::jsonb)
                IS DISTINCT FROM COALESCE(acquisition_evidence, '{}'::jsonb)
            OR rejected_source_keys IS DISTINCT FROM NEW.rejected_source_keys
        );
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS acquisition_upgrade_state_mirror_back ON acquisition_upgrade_state;
CREATE TRIGGER acquisition_upgrade_state_mirror_back
    AFTER INSERT OR UPDATE ON acquisition_upgrade_state
    FOR EACH ROW EXECUTE FUNCTION mirror_acquisition_upgrade_state_back();
