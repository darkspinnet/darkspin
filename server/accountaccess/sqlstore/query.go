package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

type summaryRow struct {
	UserID           int64  `db:"user_id"`
	LoginName        string `db:"login_name"`
	DisplayName      string `db:"display_name"`
	Level            int64  `db:"level"`
	Status           int64  `db:"status"`
	IsBanned         bool   `db:"is_banned"`
	TimeoutUntilUnix int64  `db:"timeout_until_unix"`
}

func (e *Store) ListUsers(ctx context.Context, actor accountaccess.Actor, req accountaccess.UserListRequest) (accountaccess.UserList, error) {
	if ctx == nil {
		return accountaccess.UserList{}, errors.New("nil context")
	}
	err := accountaccess.ValidateActor(actor)
	if err != nil {
		return accountaccess.UserList{}, fmt.Errorf("usersActor: %w", err)
	}
	req, err = accountaccess.NormalizeUserListRequest(req)
	if err != nil {
		return accountaccess.UserList{}, fmt.Errorf("usersValidate: %w", err)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return accountaccess.UserList{}, fmt.Errorf("usersBegin: %w", err)
	}
	defer rollback(tx)
	err = e.authorizeActor(ctx, tx, actor)
	if err != nil {
		return accountaccess.UserList{}, fmt.Errorf("usersAuthorize: %w", err)
	}
	query := `SELECT u.id AS user_id, u.login_name, u.display_name, u.level, a.status, a.is_banned, a.timeout_until_unix
		FROM "user" u JOIN account_access a ON a.user_id = u.id
		WHERE u.trmt_id = 0 AND u.id > ?`
	arguments := []any{req.AfterUserID}
	if req.Search != "" {
		// A fixed escape character keeps % and _ literal on both SQL backends.
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(req.Search) + "%"
		query += ` AND (LOWER(u.login_name) LIKE LOWER(?) ESCAPE '!' OR LOWER(u.display_name) LIKE LOWER(?) ESCAPE '!')`
		arguments = append(arguments, pattern, pattern)
	}
	query += ` ORDER BY u.id LIMIT ?`
	arguments = append(arguments, req.Limit+1)
	rows := make([]summaryRow, 0, req.Limit+1)
	err = tx.SelectContext(ctx, &rows, tx.Rebind(query), arguments...)
	if err != nil {
		return accountaccess.UserList{}, fmt.Errorf("usersSelect: %w", err)
	}
	result := accountaccess.UserList{Users: make([]accountaccess.UserSummary, 0, req.Limit)}
	if len(rows) > req.Limit {
		rows = rows[:req.Limit]
		result.NextUserID = rows[len(rows)-1].UserID
	}
	for _, row := range rows {
		result.Users = append(result.Users, accountaccess.UserSummary{
			UserID: row.UserID, LoginName: row.LoginName, DisplayName: row.DisplayName,
			Level: row.Level, Status: row.Status, IsBanned: row.IsBanned,
			TimeoutUntilUnix: row.TimeoutUntilUnix,
			IsTimedOut:       (accountaccess.Access{TimeoutUntilUnix: row.TimeoutUntilUnix}).IsTimedOut(),
		})
	}
	err = tx.Commit()
	if err != nil {
		return accountaccess.UserList{}, fmt.Errorf("usersCommit: %w", err)
	}
	return result, nil
}

type eventRow struct {
	ID             int64  `db:"id"`
	ActorType      string `db:"actor_type"`
	ActorUserID    int64  `db:"actor_user_id"`
	TargetUserID   int64  `db:"target_user_id"`
	Action         string `db:"action"`
	OccurredAtUnix int64  `db:"occurred_at_unix"`
	BeforeDetail   string `db:"before_detail"`
	AfterDetail    string `db:"after_detail"`
	Reason         string `db:"reason"`
	OperationID    string `db:"operation_id"`
	Outcome        string `db:"outcome"`
}

func (e *Store) ListHistory(ctx context.Context, actor accountaccess.Actor, req accountaccess.HistoryRequest) (accountaccess.History, error) {
	if ctx == nil {
		return accountaccess.History{}, errors.New("nil context")
	}
	err := accountaccess.ValidateActor(actor)
	if err != nil {
		return accountaccess.History{}, fmt.Errorf("historyActor: %w", err)
	}
	req, err = accountaccess.NormalizeHistoryRequest(req)
	if err != nil {
		return accountaccess.History{}, fmt.Errorf("historyValidate: %w", err)
	}
	tx, err := e.db.BeginTxx(ctx, nil)
	if err != nil {
		return accountaccess.History{}, fmt.Errorf("historyBegin: %w", err)
	}
	defer rollback(tx)
	err = e.authorizeActor(ctx, tx, actor)
	if err != nil {
		return accountaccess.History{}, fmt.Errorf("historyAuthorize: %w", err)
	}
	query := `SELECT id, actor_type, COALESCE(actor_user_id, 0) AS actor_user_id,
		COALESCE(target_user_id, 0) AS target_user_id, action, occurred_at_unix,
		before_detail, after_detail, reason, operation_id, outcome FROM admin_event WHERE 1 = 1`
	arguments := make([]any, 0, 10)
	if req.BeforeEventID > 0 {
		query += ` AND id < ?`
		arguments = append(arguments, req.BeforeEventID)
	}
	if req.TargetUserID > 0 {
		query += ` AND target_user_id = ?`
		arguments = append(arguments, req.TargetUserID)
	}
	if req.ActorUserID > 0 {
		query += ` AND actor_user_id = ?`
		arguments = append(arguments, req.ActorUserID)
	}
	if req.ActorType != "" {
		query += ` AND actor_type = ?`
		arguments = append(arguments, req.ActorType)
	}
	if req.AfterUnix > 0 {
		query += ` AND occurred_at_unix >= ?`
		arguments = append(arguments, req.AfterUnix)
	}
	if req.BeforeUnix > 0 {
		query += ` AND occurred_at_unix < ?`
		arguments = append(arguments, req.BeforeUnix)
	}
	if req.Action != "" {
		query += ` AND action = ?`
		arguments = append(arguments, req.Action)
	}
	if req.Outcome != "" {
		query += ` AND outcome = ?`
		arguments = append(arguments, req.Outcome)
	}
	if req.OperationID != "" {
		query += ` AND operation_id = ?`
		arguments = append(arguments, req.OperationID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	arguments = append(arguments, req.Limit+1)
	rows := make([]eventRow, 0, req.Limit+1)
	err = tx.SelectContext(ctx, &rows, tx.Rebind(query), arguments...)
	if err != nil {
		return accountaccess.History{}, fmt.Errorf("historySelect: %w", err)
	}
	result := accountaccess.History{Events: make([]accountaccess.AdminEvent, 0, req.Limit)}
	if len(rows) > req.Limit {
		rows = rows[:req.Limit]
		result.NextEventID = rows[len(rows)-1].ID
	}
	for _, row := range rows {
		result.Events = append(result.Events, accountaccess.AdminEvent(row))
	}
	err = tx.Commit()
	if err != nil {
		return accountaccess.History{}, fmt.Errorf("historyCommit: %w", err)
	}
	return result, nil
}
