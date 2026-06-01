-- name: ListNetworkConnectCommands :many
SELECT command FROM network_connect_commands WHERE network_id = ? ORDER BY position;

-- name: DeleteNetworkConnectCommands :exec
DELETE FROM network_connect_commands WHERE network_id = ?;

-- name: InsertNetworkConnectCommand :exec
INSERT INTO network_connect_commands(network_id, position, command) VALUES (?, ?, ?);
