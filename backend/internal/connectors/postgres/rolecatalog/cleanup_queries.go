package rolecatalog

// Every address is returned as separate identifiers, never executable SQL from
// the peer. Column-only grants are covered by table-wide REVOKE (Postgres 16).
// Final DROP ROLE rejects default ACL, policy, grantor or concurrent dependencies
// not removed by this plan; there is no destructive fallback.
const cleanupPrivilegesQuery = `
SELECT 'database'::text, ''::text, database.datname::text
FROM pg_catalog.pg_database AS database
WHERE database.oid = $2 AND EXISTS (
 SELECT 1 FROM pg_catalog.aclexplode(database.datacl) AS acl WHERE acl.grantee = $1)
UNION
SELECT 'schema', '', namespace.nspname::text
FROM pg_catalog.pg_namespace AS namespace
WHERE EXISTS (SELECT 1 FROM pg_catalog.aclexplode(namespace.nspacl) AS acl WHERE acl.grantee = $1)
UNION
SELECT CASE WHEN relation.relkind = 'S' THEN 'sequence' ELSE 'table' END,
 namespace.nspname::text, relation.relname::text
FROM pg_catalog.pg_class AS relation
JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = relation.relnamespace
WHERE relation.relkind IN ('r', 'p', 'v', 'm', 'f', 'S') AND (
 EXISTS (SELECT 1 FROM pg_catalog.aclexplode(relation.relacl) AS acl WHERE acl.grantee = $1)
 OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute AS attribute
  CROSS JOIN LATERAL pg_catalog.aclexplode(attribute.attacl) AS acl
  WHERE attribute.attrelid = relation.oid AND attribute.attnum > 0
   AND NOT attribute.attisdropped AND acl.grantee = $1))
UNION
SELECT 'routines', '', namespace.nspname::text
FROM pg_catalog.pg_proc AS routine
JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = routine.pronamespace
WHERE EXISTS (SELECT 1 FROM pg_catalog.aclexplode(routine.proacl) AS acl WHERE acl.grantee = $1)
UNION
SELECT 'type', namespace.nspname::text, type.typname::text
FROM pg_catalog.pg_type AS type
JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = type.typnamespace
WHERE EXISTS (SELECT 1 FROM pg_catalog.aclexplode(type.typacl) AS acl WHERE acl.grantee = $1)
UNION
SELECT 'language', '', language.lanname::text
FROM pg_catalog.pg_language AS language
WHERE EXISTS (SELECT 1 FROM pg_catalog.aclexplode(language.lanacl) AS acl WHERE acl.grantee = $1)
UNION
SELECT 'foreign data wrapper', '', wrapper.fdwname::text
FROM pg_catalog.pg_foreign_data_wrapper AS wrapper
WHERE EXISTS (SELECT 1 FROM pg_catalog.aclexplode(wrapper.fdwacl) AS acl WHERE acl.grantee = $1)
UNION
SELECT 'foreign server', '', server.srvname::text
FROM pg_catalog.pg_foreign_server AS server
WHERE EXISTS (SELECT 1 FROM pg_catalog.aclexplode(server.srvacl) AS acl WHERE acl.grantee = $1)
UNION
SELECT 'large object', '', object.oid::text
FROM pg_catalog.pg_largeobject_metadata AS object
WHERE EXISTS (SELECT 1 FROM pg_catalog.aclexplode(object.lomacl) AS acl WHERE acl.grantee = $1)
ORDER BY 1, 2, 3
LIMIT 10000`
