package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type channelMonitorCandyRepository struct{ db *sql.DB }

func NewChannelMonitorCandyRepository(db *sql.DB) service.ChannelMonitorCandyRepository {
	return &channelMonitorCandyRepository{db: db}
}

func (r *channelMonitorCandyRepository) Claim(ctx context.Context, m *service.ChannelMonitor, fingerprint, attemptID string, now time.Time) (bool, error) {
	const query = `INSERT INTO channel_monitor_candy_states AS state
        (monitor_id, fingerprint, attempt_id, last_started_at, running_until)
        SELECT $1, $2, $3, $4, $5 FROM channel_monitors
        WHERE id = $1 AND enabled AND candy_enabled AND provider = 'openai'
          AND check_mode IN ('probe', 'quota_probe') AND updated_at = $6
        ON CONFLICT (monitor_id) DO UPDATE SET
            fingerprint = EXCLUDED.fingerprint, attempt_id = EXCLUDED.attempt_id,
            last_started_at = EXCLUDED.last_started_at, running_until = EXCLUDED.running_until,
            history = CASE WHEN state.fingerprint = EXCLUDED.fingerprint THEN state.history ELSE '[]'::jsonb END
        WHERE (state.running_until IS NULL OR state.running_until <= $4)
          AND (state.last_started_at <= $7 OR state.fingerprint <> EXCLUDED.fingerprint)`
	result, err := r.db.ExecContext(ctx, query, m.ID, fingerprint, attemptID, now,
		now.Add(service.MonitorCandyTimeout+2*time.Minute), m.UpdatedAt, now.Add(-service.MonitorCandyInterval))
	if err != nil {
		return false, fmt.Errorf("claim candy check: %w", err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *channelMonitorCandyRepository) Complete(ctx context.Context, id int64, fingerprint, attemptID string, point service.MonitorCandyPoint) error {
	data, err := json.Marshal(point)
	if err != nil {
		return err
	}
	const query = `UPDATE channel_monitor_candy_states AS state SET
        running_until = NULL,
        history = (SELECT COALESCE(jsonb_agg(value ORDER BY ordinal), '[]'::jsonb)
            FROM jsonb_array_elements(state.history || jsonb_build_array($4::jsonb)) WITH ORDINALITY AS h(value, ordinal)
            WHERE ordinal > GREATEST(jsonb_array_length(state.history) + 1 - $5, 0))
        WHERE monitor_id = $1 AND fingerprint = $2 AND attempt_id = $3 AND running_until IS NOT NULL`
	_, err = r.db.ExecContext(ctx, query, id, fingerprint, attemptID, string(data), service.MonitorCandyHistoryLimit)
	return err
}

func (r *channelMonitorCandyRepository) ListStates(ctx context.Context, ids []int64) (map[int64]*service.MonitorCandyState, error) {
	states := make(map[int64]*service.MonitorCandyState)
	if len(ids) == 0 {
		return states, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT monitor_id, fingerprint, last_started_at, running_until, history
        FROM channel_monitor_candy_states WHERE monitor_id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		state := &service.MonitorCandyState{}
		var history []byte
		if err := rows.Scan(&state.MonitorID, &state.Fingerprint, &state.LastStartedAt, &state.RunningUntil, &history); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(history, &state.History); err != nil {
			return nil, fmt.Errorf("decode candy history: %w", err)
		}
		states[state.MonitorID] = state
	}
	return states, rows.Err()
}
