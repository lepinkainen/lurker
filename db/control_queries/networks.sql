-- name: GetNetwork :one
SELECT id, name, kind, host, port, tls, nick, COALESCE(realname,'') AS realname,
       sasl_user, sasl_pass, server_pass, sort_order, disabled
FROM networks WHERE id = ?;

-- name: GetNetworkIDByNameCI :one
SELECT id FROM networks WHERE name_ci = ?;

-- name: CreateNetwork :exec
INSERT INTO networks(id, name, name_ci, kind, host, port, tls, nick, realname, sasl_user, sasl_pass, server_pass, sort_order, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
        (SELECT COALESCE(MAX(sort_order) + 1, 0) FROM networks), ?);

-- name: UpsertNetwork :exec
INSERT INTO networks(id, name, name_ci, kind, host, port, tls, nick, realname, sasl_user, sasl_pass, server_pass, sort_order, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
        (SELECT COALESCE(MAX(sort_order) + 1, 0) FROM networks), ?)
ON CONFLICT(name_ci) DO UPDATE SET
  name=excluded.name, kind=excluded.kind, host=excluded.host, port=excluded.port,
  tls=excluded.tls, nick=excluded.nick, realname=excluded.realname,
  sasl_user=excluded.sasl_user, sasl_pass=excluded.sasl_pass, server_pass=excluded.server_pass,
  disabled=0;

-- name: UpdateNetwork :execrows
UPDATE networks
SET name = ?, name_ci = ?, host = ?, port = ?, tls = ?, nick = ?, realname = ?, sasl_user = ?, sasl_pass = ?, server_pass = ?
WHERE id = ?;

-- name: DeleteNetwork :execrows
DELETE FROM networks WHERE id = ?;

-- name: ListNetworkIDs :many
SELECT id FROM networks;

-- name: SetNetworkSortOrder :exec
UPDATE networks SET sort_order = ? WHERE id = ?;

-- name: ListNetworks :many
SELECT id, name, kind, host, port, tls, nick, COALESCE(realname,'') AS realname, sort_order, disabled
FROM networks ORDER BY sort_order, id;

-- name: ListNetworksWithSASL :many
SELECT id, name, kind, host, port, tls, nick, COALESCE(realname,'') AS realname,
       sasl_user, sasl_pass, server_pass, sort_order
FROM networks WHERE disabled=0 ORDER BY sort_order, id;

-- name: SetNetworkDisabled :execrows
UPDATE networks SET disabled=? WHERE id=?;
