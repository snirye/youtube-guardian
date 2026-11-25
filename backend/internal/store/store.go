// Package store owns the persistent channel decisions and approval workflow.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var (
	channelPattern = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	videoPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	ErrDecided     = errors.New("channel already has a decision")
	ErrNotPending  = errors.New("request is no longer pending")
)

func ValidChannel(id string) bool { return channelPattern.MatchString(id) }
func ValidVideo(id string) bool   { return videoPattern.MatchString(id) }

type Store struct{ DB *sql.DB }

type Channel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Decision string `json:"decision"`
}

type Snapshot struct {
	Version     int64     `json:"version"`
	Channels    []Channel `json:"channels"`
	BlockShorts bool      `json:"blockShorts"`
	Pending     []string  `json:"pending"`
}

type Request struct {
	ID          int64  `json:"id"`
	ChannelID   string `json:"channelId"`
	ChannelName string `json:"channelName"`
	VideoURL    string `json:"videoUrl"`
	RequestedAt int64  `json:"requestedAt"`
}

func (s Store) Snapshot(ctx context.Context) (Snapshot, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()
	snapshot := Snapshot{Pending: []string{}}
	if err := tx.QueryRowContext(ctx, "SELECT version, block_shorts FROM configuration WHERE id = 1").
		Scan(&snapshot.Version, &snapshot.BlockShorts); err != nil {
		return Snapshot{}, err
	}
	snapshot.Channels, err = channels(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT channel_id FROM approval_requests WHERE status = 'pending' ORDER BY channel_id")
	if err != nil {
		return Snapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return Snapshot{}, err
		}
		snapshot.Pending = append(snapshot.Pending, id)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, tx.Commit()
}

type channelReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func channels(ctx context.Context, db channelReader) ([]Channel, error) {
	rows, err := db.QueryContext(ctx, "SELECT channel_id, channel_name, decision FROM channels ORDER BY channel_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Channel{}
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.Name, &c.Decision); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (s Store) RequestApproval(ctx context.Context, channelID, name, videoID string) (Request, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Request{}, false, err
	}
	defer tx.Rollback()
	var decision string
	err = tx.QueryRowContext(ctx, "SELECT decision FROM channels WHERE channel_id = ?", channelID).Scan(&decision)
	switch {
	case err == nil:
		return Request{}, false, ErrDecided
	case !errors.Is(err, sql.ErrNoRows):
		return Request{}, false, err
	}
	now := time.Now().UnixMilli()
	url := "https://www.youtube.com/watch?v=" + videoID
	result, err := tx.ExecContext(ctx, `INSERT INTO approval_requests
		(channel_id, channel_name, video_url, requested_at_ms) VALUES (?, ?, ?, ?)
		ON CONFLICT(channel_id) WHERE status = 'pending' DO NOTHING`, channelID, name, url, now)
	if err != nil {
		return Request{}, false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Request{}, false, err
	}
	var r Request
	err = tx.QueryRowContext(ctx, `SELECT id, channel_id, channel_name, video_url, requested_at_ms
		FROM approval_requests WHERE channel_id = ? AND status = 'pending'`, channelID).
		Scan(&r.ID, &r.ChannelID, &r.ChannelName, &r.VideoURL, &r.RequestedAt)
	if err != nil {
		return Request{}, false, err
	}
	if n != 0 {
		if err := logDecision(ctx, tx, "approval_requested", channelID, name, videoID, "denied", "pending"); err != nil {
			return Request{}, false, err
		}
	}
	return r, n != 0, tx.Commit()
}

func (s Store) Pending(ctx context.Context, unsentOnly bool, limit int) ([]Request, error) {
	query := `SELECT id, channel_id, channel_name, video_url, requested_at_ms
		FROM approval_requests WHERE status = 'pending'`
	if unsentOnly {
		query += " AND telegram_notified_at_ms IS NULL"
	}
	rows, err := s.DB.QueryContext(ctx, query+" ORDER BY id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var requests []Request
	for rows.Next() {
		var r Request
		if err := rows.Scan(&r.ID, &r.ChannelID, &r.ChannelName, &r.VideoURL, &r.RequestedAt); err != nil {
			return nil, err
		}
		requests = append(requests, r)
	}
	return requests, rows.Err()
}

func (s Store) MarkNotified(ctx context.Context, requestID int64) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE approval_requests SET telegram_notified_at_ms = ? WHERE id = ?", time.Now().UnixMilli(), requestID)
	return err
}

func (s Store) Resolve(ctx context.Context, requestID int64, allow bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id, name string
	err = tx.QueryRowContext(ctx, `SELECT channel_id, channel_name FROM approval_requests
		WHERE id = ? AND status = 'pending'`, requestID).Scan(&id, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotPending
	}
	if err != nil {
		return err
	}
	if err := decide(ctx, tx, id, name, allow); err != nil {
		return err
	}
	return tx.Commit()
}

func (s Store) Decide(ctx context.Context, id, name string, allow bool) error {
	if !ValidChannel(id) {
		return fmt.Errorf("invalid channel ID")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := decide(ctx, tx, id, name, allow); err != nil {
		return err
	}
	return tx.Commit()
}

func decide(ctx context.Context, tx *sql.Tx, id, name string, allow bool) error {
	now := time.Now().UnixMilli()
	decision, eventDecision, requestStatus := "blocked", "denied", "rejected"
	if allow {
		decision, eventDecision, requestStatus = "approved", "approved", "approved"
	}
	change, err := tx.ExecContext(ctx, `INSERT INTO channels (channel_id, channel_name, decision, updated_at_ms)
		VALUES (?, COALESCE(NULLIF(?, ''), (SELECT channel_name FROM approval_requests
			WHERE channel_id = ? ORDER BY id DESC LIMIT 1), ?), ?, ?)
		ON CONFLICT(channel_id) DO UPDATE SET decision = excluded.decision, updated_at_ms = excluded.updated_at_ms,
		channel_name = COALESCE(NULLIF(?, ''), channels.channel_name)
		WHERE channels.decision <> excluded.decision OR (? <> '' AND channels.channel_name <> excluded.channel_name)`,
		id, name, id, id, decision, now, name, name)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE approval_requests SET status = ?, resolved_at_ms = ?
		WHERE channel_id = ? AND status = 'pending'`, requestStatus, now, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		requestStatus = ""
	}
	changed, err := change.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 && count == 0 {
		return nil
	}
	if changed != 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE configuration SET version = version + 1 WHERE id = 1"); err != nil {
			return err
		}
	}
	return logDecision(ctx, tx, "channel_"+decision, id, name, "", eventDecision, requestStatus)
}

func logDecision(ctx context.Context, tx *sql.Tx, event, channelID, name, videoID, decision, status string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO activity_events (occurred_at_ms, event_type, channel_id, channel_name,
		video_id, permission_decision, approval_request_status, configuration_version)
		SELECT ?, ?, ?, COALESCE(NULLIF(?, ''), (SELECT channel_name FROM channels WHERE channel_id = ?)), NULLIF(?, ''), ?, NULLIF(?, ''), version FROM configuration WHERE id = 1`,
		time.Now().UnixMilli(), event, channelID, name, channelID, videoID, decision, status)
	return err
}

func (s Store) SetShorts(ctx context.Context, enabled bool) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE configuration SET block_shorts = ?, version = version + 1 WHERE id = 1 AND block_shorts <> ?", enabled, enabled)
	return err
}
