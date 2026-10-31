package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type userLeaderboardRepository struct{ db sqlExecutor }

func NewUserLeaderboardRepository(db *sql.DB) service.UserLeaderboardRepository {
	return &userLeaderboardRepository{db: db}
}

func (r *userLeaderboardRepository) Top(ctx context.Context, model string) ([]service.UserLeaderboardEntry, error) {
	where := ""
	args := make([]any, 0, 1)
	if model != "" {
		where = " AND " + resolveModelDimensionExpressionWithAlias(usagestats.ModelSourceRequested, "ul") + "=$1"
		args = append(args, model)
	}
	// Cast each token column before addition to avoid int32 overflow. Failed
	// zero-usage placeholders are excluded, while free token calls still count.
	rows, err := r.db.QueryContext(ctx, `SELECT COALESCE(u.username,''),
        SUM(ul.input_tokens::bigint+ul.output_tokens::bigint+ul.cache_creation_tokens::bigint+ul.cache_read_tokens::bigint)::bigint AS tokens,
        SUM(ul.actual_cost)::numeric(30,8)::text AS quota
        FROM usage_logs ul JOIN users u ON u.id=ul.user_id
        WHERE u.deleted_at IS NULL AND u.status='active'
        AND (ul.actual_cost>0 OR ul.input_tokens>0 OR ul.output_tokens>0 OR ul.cache_creation_tokens>0 OR ul.cache_read_tokens>0)`+where+`
        GROUP BY u.id,u.username
        ORDER BY tokens DESC,SUM(ul.actual_cost) DESC,u.id ASC LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.UserLeaderboardEntry, 0, service.UserLeaderboardLimit)
	for rows.Next() {
		var item service.UserLeaderboardEntry
		if err := rows.Scan(&item.Username, &item.TotalTokens, &item.TotalQuota); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *userLeaderboardRepository) Models(ctx context.Context) ([]string, error) {
	expr := resolveModelDimensionExpressionWithAlias(usagestats.ModelSourceRequested, "ul")
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT `+expr+` AS name
        FROM usage_logs ul JOIN users u ON u.id=ul.user_id
        WHERE u.deleted_at IS NULL AND u.status='active'
        AND (ul.actual_cost>0 OR ul.input_tokens>0 OR ul.output_tokens>0 OR ul.cache_creation_tokens>0 OR ul.cache_read_tokens>0)
        AND `+expr+`<>'' ORDER BY name LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	models := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		models = append(models, name)
	}
	return models, rows.Err()
}
