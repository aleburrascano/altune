DO $$
DECLARE
    t text;
    web_role text;
BEGIN
    FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', t);
    END LOOP;

    FOREACH web_role IN ARRAY ARRAY['anon', 'authenticated'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = web_role) THEN
            EXECUTE format('REVOKE ALL ON ALL TABLES IN SCHEMA public FROM %I', web_role);
            EXECUTE format('REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM %I', web_role);
            EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM %I', web_role);
            EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM %I', web_role);
        END IF;
    END LOOP;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'altune_app') THEN
        GRANT USAGE ON SCHEMA public TO altune_app;
        GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO altune_app;
        GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO altune_app;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO altune_app;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO altune_app;
        REVOKE ALL ON public.schema_migrations FROM altune_app;
    END IF;

    IF to_regclass('auth.users') IS NOT NULL THEN
        CREATE OR REPLACE VIEW public.auth_user_ids AS SELECT id FROM auth.users;
        REVOKE ALL ON public.auth_user_ids FROM PUBLIC;
        FOREACH web_role IN ARRAY ARRAY['anon', 'authenticated', 'altune_app'] LOOP
            IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = web_role) THEN
                EXECUTE format('REVOKE ALL ON public.auth_user_ids FROM %I', web_role);
            END IF;
        END LOOP;
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'altune_app') THEN
            GRANT SELECT ON public.auth_user_ids TO altune_app;
        END IF;
    END IF;
END $$;
