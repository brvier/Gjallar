-- Role gjallar sur le pg16 prod (db-par2-pg16-prod-master01, 10.2.70.131:15432)
-- pour les monitors reco.
--
-- La base reco_orchestrator a ete migree de 10.2.70.100 (pg14) vers ce cluster
-- le 2026-09-24. Le dump/restore ne recree pas les roles du cluster : sans ce
-- script, les monitors "Reco results", "Reco subgroups muets" et "Reco configs
-- en retard" echouent en "password authentication failed".
--
-- Meme mot de passe que dans gjallar.yaml, passe en variable pour ne pas le
-- versionner. Idempotent :
--   sudo -u postgres psql -X -p 15432 -v ON_ERROR_STOP=1 -v pw='<mot de passe>' \
--        -f create-gjallar-user-reco-pg16.sql

SELECT format('CREATE ROLE gjallar LOGIN PASSWORD %L', :'pw')
 WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'gjallar') \gexec
ALTER ROLE gjallar LOGIN PASSWORD :'pw';
COMMENT ON ROLE gjallar IS 'Monitoring (Gjallar) - read-only';
GRANT CONNECT ON DATABASE reco_orchestrator TO gjallar;

\c reco_orchestrator
GRANT USAGE ON SCHEMA public TO gjallar;
-- results : "Reco results (1h)"
-- reco_recognition_configs, tasks_v2 : "Reco subgroups muets", "Reco configs en retard"
-- processors : "Reco workers sans stats (10 min)"
GRANT SELECT ON public.results, public.tasks_v2, public.reco_recognition_configs,
                public.processors TO gjallar;
