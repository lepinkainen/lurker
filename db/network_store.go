package db

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/lepinkainen/lurker/db/internal/controldb"
)

// UpsertNetwork inserts the network if missing, or updates its config fields
// from the provided values if it already exists (keyed by case-insensitive
// name). sort_order and created_at are never modified.
func UpsertNetwork(ctx context.Context, d *sql.DB, n Network) (Network, error) {
	if err := ValidateNetworkName(n.Name); err != nil {
		return Network{}, err
	}
	nameCI := NormalizeNetworkName(n.Name)
	kind := n.Kind
	if kind == "" {
		kind = NetworkKindIRC
	}
	tls := int64(0)
	if n.TLS {
		tls = 1
	}
	newId := newID()
	q := controldb.New(d)
	if err := q.UpsertNetwork(ctx, controldb.UpsertNetworkParams{
		ID:         newId[:],
		Name:       n.Name,
		NameCi:     nameCI,
		Kind:       kind,
		Host:       n.Host,
		Port:       int64(n.Port),
		Tls:        tls,
		Nick:       n.Nick,
		Realname:   nullStr(n.Realname),
		SaslUser:   nullStr(n.SASLUser),
		SaslPass:   nullStr(n.SASLPass),
		ServerPass: nullStr(n.ServerPass),
		CreatedAt:  Now(),
	}); err != nil {
		return Network{}, err
	}
	idBytes, err := q.GetNetworkIDByNameCI(ctx, nameCI)
	if err != nil {
		return Network{}, err
	}
	id, err := parseUUID(idBytes)
	if err != nil {
		return Network{}, err
	}
	return GetNetwork(ctx, d, id)
}

// ListNetworks returns every network row for API state responses.
func ListNetworks(ctx context.Context, d *sql.DB) ([]Network, error) {
	rows, err := controldb.New(d).ListNetworks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Network, 0, len(rows))
	for _, r := range rows {
		id, err := parseUUID(r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, Network{
			ID:        id,
			Name:      r.Name,
			Kind:      r.Kind,
			Host:      r.Host,
			Port:      int(r.Port),
			TLS:       r.Tls == 1,
			Nick:      r.Nick,
			Realname:  r.Realname,
			SortOrder: int(r.SortOrder),
			Disabled:  r.Disabled == 1,
		})
	}
	return out, nil
}

// MarkNonYAMLNetworksDisabled sets disabled=1 on all networks whose name is
// not in the provided list. Used at startup to disable networks removed from
// config.yaml without deleting their log data. An empty list disables every
// network: config.yaml is the source of truth at boot, and a config that
// declares no networks means none should run. (A missing/broken config never
// reaches this point — loadConfig fails hard first.)
//
// Kept on raw database/sql because the IN-clause arity is dynamic; sqlc cannot
// codegen variable-length placeholder lists for SQLite.
func MarkNonYAMLNetworksDisabled(ctx context.Context, d *sql.DB, yamlNames []string) error {
	if len(yamlNames) == 0 {
		_, err := d.ExecContext(ctx, `UPDATE networks SET disabled=1 WHERE disabled=0`)
		return err
	}
	normalized := make([]any, len(yamlNames))
	for i, name := range yamlNames {
		normalized[i] = NormalizeNetworkName(name)
	}
	placeholders := make([]byte, 0, len(yamlNames)*2)
	for i := range yamlNames {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
	}
	_, err := d.ExecContext(ctx,
		`UPDATE networks SET disabled=1 WHERE disabled=0 AND name_ci NOT IN (`+string(placeholders)+`)`,
		normalized...)
	return err
}

// SetNetworkDisabled sets the disabled flag on a single network.
func SetNetworkDisabled(ctx context.Context, d *sql.DB, id uuid.UUID, disabled bool) error {
	v := int64(0)
	if disabled {
		v = 1
	}
	affected, err := controldb.New(d).SetNetworkDisabled(ctx, controldb.SetNetworkDisabledParams{
		Disabled: v,
		ID:       id[:],
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNetworkNotFound
	}
	return nil
}

// ListNetworksWithSASL returns every non-disabled network row including SASL
// credentials, ordered by sort_order. Used for config export only.
func ListNetworksWithSASL(ctx context.Context, d *sql.DB) ([]Network, error) {
	rows, err := controldb.New(d).ListNetworksWithSASL(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Network, 0, len(rows))
	for _, r := range rows {
		id, err := parseUUID(r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, Network{
			ID:         id,
			Name:       r.Name,
			Kind:       r.Kind,
			Host:       r.Host,
			Port:       int(r.Port),
			TLS:        r.Tls == 1,
			Nick:       r.Nick,
			Realname:   r.Realname,
			SASLUser:   r.SaslUser.String,
			SASLPass:   r.SaslPass.String,
			ServerPass: r.ServerPass.String,
			SortOrder:  int(r.SortOrder),
		})
	}
	for i := range out {
		commands, err := ListNetworkConnectCommands(ctx, d, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].ConnectCommands = commands
	}
	return out, nil
}
