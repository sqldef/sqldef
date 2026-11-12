#!/bin/sh
# Regenerates catalog.tsv from the pg_catalog of a running PostgreSQL server, which psql reaches
# through the usual PG* environment variables. Use the newest PostgreSQL release.
set -eu
cd "$(dirname "$0")"
psql -X -q -At -F '	' -v ON_ERROR_STOP=1 > catalog.tsv <<'SQL'
SELECT 'T', t.typname, format_type(t.oid, NULL), t.typcategory, t.typispreferred::int, t.typtype,
       coalesce(e.typname, '')
FROM pg_type t
LEFT JOIN pg_type e ON e.oid = t.typelem AND t.typcategory = 'A'
WHERE t.typnamespace = 'pg_catalog'::regnamespace
ORDER BY 2;

SELECT 'C', s.typname, t.typname, c.castcontext
FROM pg_cast c
JOIN pg_type s ON s.oid = c.castsource
JOIN pg_type t ON t.oid = c.casttarget
WHERE s.typnamespace = 'pg_catalog'::regnamespace AND t.typnamespace = 'pg_catalog'::regnamespace
ORDER BY 2, 3;

SELECT 'F', p.proname,
       coalesce((SELECT string_agg(a.typname, ',' ORDER BY i)
                 FROM unnest(p.proargtypes::oid[]) WITH ORDINALITY AS u(oid, i)
                 JOIN pg_type a ON a.oid = u.oid), ''),
       r.typname, (p.provariadic <> 0)::int, p.pronargdefaults, p.proretset::int
FROM pg_proc p
JOIN pg_type r ON r.oid = p.prorettype
WHERE p.pronamespace = 'pg_catalog'::regnamespace
  AND p.prokind IN ('f', 'a', 'w')
  AND r.typname NOT IN ('internal', 'trigger', 'event_trigger', 'language_handler', 'fdw_handler',
                        'index_am_handler', 'table_am_handler', 'tsm_handler', 'cstring')
  AND NOT EXISTS (SELECT FROM unnest(p.proargtypes::oid[]) AS u(oid)
                  JOIN pg_type a ON a.oid = u.oid
                  WHERE a.typname IN ('internal', 'cstring'))
ORDER BY 2, 3;

SELECT 'O', o.oprname, coalesce(l.typname, ''), r.typname, res.typname
FROM pg_operator o
LEFT JOIN pg_type l ON l.oid = o.oprleft
JOIN pg_type r ON r.oid = o.oprright
JOIN pg_type res ON res.oid = o.oprresult
WHERE o.oprnamespace = 'pg_catalog'::regnamespace
ORDER BY 2, 3, 4;
SQL
