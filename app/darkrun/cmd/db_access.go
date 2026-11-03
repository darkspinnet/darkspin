package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	server "github.com/darkspinnet/darkspin/server/runtime"
)

type databaseAccessResult struct {
	Database        string `json:"database"`
	Table           string `json:"table"`
	UserID          int64  `json:"id"`
	LoginName       string `json:"login_name"`
	PreviousStatus  int64  `json:"previous_status"`
	Status          int64  `json:"status"`
	PreviousIPLimit string `json:"previous_ip_limit"`
	IPLimit         string `json:"ip_limit"`
	IsAdmin         bool   `json:"is_admin"`
	IsBanned        bool   `json:"is_banned"`
}

func databaseSetAccess(ctx context.Context, output io.Writer, configPath string, parameters []string) error {
	if (len(parameters) != 4 && len(parameters) != 6) || !strings.EqualFold(parameters[len(parameters)-2], "where") {
		return errors.New("access usage: db user set status <integer> [ip_limit <IP|CIDR|none>] where <id|login_name>=<exact-account>; or db user set ip_limit <IP|CIDR|none> where <id|login_name>=<exact-account>")
	}
	var status *int64
	var ipLimit *string
	for index := 0; index < len(parameters)-2; index += 2 {
		switch strings.ToLower(parameters[index]) {
		case "status":
			if status != nil {
				return errors.New("status may only be supplied once")
			}
			parsedStatus, err := strconv.ParseInt(parameters[index+1], 10, 64)
			if err != nil || parsedStatus < 0 {
				return errors.New("status must be a nonnegative integer within the signed 64-bit range")
			}
			status = &parsedStatus
		case "ip_limit":
			if ipLimit != nil {
				return errors.New("ip_limit may only be supplied once")
			}
			limit := parameters[index+1]
			if strings.EqualFold(limit, "none") {
				limit = ""
			} else if strings.TrimSpace(limit) == "" {
				return errors.New("use ip_limit none to explicitly clear the restriction")
			}
			normalized, err := accountaccess.NormalizeIPLimit(limit)
			if err != nil {
				return fmt.Errorf("limitParse: %w", err)
			}
			ipLimit = &normalized
		default:
			return errors.New("access updates accept only status and ip_limit")
		}
	}
	matches := databasePredicatePattern.FindStringSubmatch(parameters[len(parameters)-1])
	if matches == nil || matches[2] != "=" || matches[3] == "" {
		return errors.New("access requires one exact id= or login_name= predicate")
	}
	var userID int64
	var loginName string
	switch strings.ToLower(matches[1]) {
	case "id":
		parsedID, err := strconv.ParseInt(matches[3], 10, 64)
		if err != nil || parsedID <= 0 {
			return errors.New("access account ID must be a positive signed 64-bit integer")
		}
		userID = parsedID
	case "login_name":
		loginName = matches[3]
	default:
		return errors.New("access requires one exact id= or login_name= predicate")
	}
	var before accountaccess.Access
	var after accountaccess.Access
	if status != nil {
		change, err := server.SetAccountStatusWithIPLimit(ctx, configPath, userID, loginName, *status, ipLimit)
		if err != nil {
			return fmt.Errorf("statusSet: %w", err)
		}
		before, after = change.Before, change.After
	} else {
		change, err := server.SetAccountIPLimit(ctx, configPath, userID, loginName, *ipLimit)
		if err != nil {
			return fmt.Errorf("limitSet: %w", err)
		}
		before, after = change.Before, change.After
	}
	err := writeDatabaseJSON(output, databaseAccessResult{
		Database: "user", Table: "user", UserID: after.UserID, LoginName: after.LoginName,
		PreviousStatus: before.Status, Status: after.Status,
		PreviousIPLimit: before.IPLimit, IPLimit: after.IPLimit,
		IsAdmin: after.IsAdmin(), IsBanned: after.IsBanned,
	})
	if err != nil {
		return fmt.Errorf("accessWrite: %w", err)
	}
	return nil
}

// The local operator projection remains usable on older databases before their
// first schema migration. Missing access is never interpreted as admin access.
func databaseUserSelect(ctx context.Context, target *databaseTarget) (string, error) {
	query := `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'account_access'`
	if target.database.DriverName() == "pgx" {
		query = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = CURRENT_SCHEMA() AND table_name = 'account_access'`
	}
	var count int
	err := target.database.GetContext(ctx, &count, query)
	if err != nil {
		return "", fmt.Errorf("accessTable: %w", err)
	}
	if count == 0 {
		return `SELECT *, NULL AS status, NULL AS ip_limit, FALSE AS is_admin FROM "user"`, nil
	}
	columns, err := databaseColumns(ctx, target.database, "account_access")
	if err != nil {
		return "", fmt.Errorf("accessColumns: %w", err)
	}
	limitProjection := "NULL"
	if _, isFound := columns["ip_limit"]; isFound {
		limitProjection = `(SELECT ip_limit FROM account_access WHERE user_id = "user".id)`
	}
	return `SELECT *,
		(SELECT status FROM account_access WHERE user_id = "user".id) AS status,
		` + limitProjection + ` AS ip_limit,
		EXISTS (SELECT 1 FROM account_access WHERE user_id = "user".id
			AND status > 250 AND is_banned = 0 AND "user".trmt_id = 0) AS is_admin
		FROM "user"`, nil
}
