package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Playback struct {
	ChannelID   string `json:"channelId"`
	ChannelName string `json:"channelName"`
	VideoID     string `json:"videoId"`
	Decision    string `json:"decision"`
	Version     *int64 `json:"version"`
}

func (s Store) Record(ctx context.Context, p Playback) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO activity_events (occurred_at_ms, event_type, channel_id,
		channel_name, video_id, permission_decision, configuration_version)
		VALUES (?, 'playback', NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?)`,
		time.Now().UnixMilli(), p.ChannelID, p.ChannelName, p.VideoID, p.Decision, p.Version)
	return err
}

type Activity struct {
	At            time.Time
	Type          string
	ChannelID     string
	ChannelName   string
	VideoID       string
	Decision      string
	RequestStatus string
	Version       sql.NullInt64
}

func (s Store) Recent(ctx context.Context, limit int) ([]Activity, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT occurred_at_ms, event_type, COALESCE(channel_id, ''),
		COALESCE(channel_name, ''), COALESCE(video_id, ''), COALESCE(permission_decision, ''),
		COALESCE(approval_request_status, ''), configuration_version
		FROM activity_events ORDER BY occurred_at_ms DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []Activity
	for rows.Next() {
		var e Activity
		var ms int64
		if err := rows.Scan(&ms, &e.Type, &e.ChannelID, &e.ChannelName, &e.VideoID, &e.Decision, &e.RequestStatus, &e.Version); err != nil {
			return nil, err
		}
		e.At = time.UnixMilli(ms)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s Store) Prune(ctx context.Context, before time.Time) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM activity_events WHERE occurred_at_ms < ?", before.UnixMilli())
	return err
}

type Heartbeat struct {
	ExtensionVersion     string `json:"extensionVersion"`
	ConfigurationVersion *int64 `json:"configurationVersion"`
	ActiveTabs           *int   `json:"activeTabs"`
}

func (s Store) Beat(ctx context.Context, h Heartbeat) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO extension_heartbeat
		(id, received_at_ms, extension_version, configuration_version, active_tabs)
		VALUES (1, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET received_at_ms = excluded.received_at_ms,
		extension_version = excluded.extension_version, configuration_version = excluded.configuration_version, active_tabs = excluded.active_tabs`,
		time.Now().UnixMilli(), h.ExtensionVersion, h.ConfigurationVersion, h.ActiveTabs)
	return err
}

type Observation struct {
	At               time.Time
	ExtensionVersion string
	Version          sql.NullInt64
	Tabs             sql.NullInt64
}

func (s Store) LastHeartbeat(ctx context.Context) (*Observation, error) {
	var o Observation
	var ms int64
	err := s.DB.QueryRowContext(ctx, `SELECT received_at_ms, extension_version, configuration_version, active_tabs
		FROM extension_heartbeat WHERE id = 1`).Scan(&ms, &o.ExtensionVersion, &o.Version, &o.Tabs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	o.At = time.UnixMilli(ms)
	return &o, nil
}

func (s Store) TelegramOffset(ctx context.Context) (int64, error) {
	var offset int64
	err := s.DB.QueryRowContext(ctx, "SELECT next_update FROM telegram_state WHERE id = 1").Scan(&offset)
	return offset, err
}

func (s Store) SetTelegramOffset(ctx context.Context, offset int64) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE telegram_state SET next_update = MAX(next_update, ?) WHERE id = 1", offset)
	return err
}
