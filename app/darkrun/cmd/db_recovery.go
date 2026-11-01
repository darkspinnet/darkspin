package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	server "github.com/darkspinnet/darkspin/server/runtime"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func databaseUndelete(
	ctx context.Context, output io.Writer, configPath, table string,
	parameters []string,
) error {
	if table != "user" || len(parameters) < 1 || len(parameters) > 2 {
		return errors.New("undelete usage: db user undelete <id> [new-login-name]")
	}
	userID, err := strconv.ParseInt(parameters[0], 10, 64)
	if err != nil || userID <= 0 {
		return errors.New("undelete requires a positive account ID")
	}
	loginName := ""
	if len(parameters) == 2 {
		loginName = strings.TrimSpace(parameters[1])
		if loginName == "" {
			return errors.New("replacement login name is empty")
		}
	}
	restoredLogin, err := server.RestoreUserProfile(ctx, configPath, userID, loginName)
	if err != nil {
		return fmt.Errorf("profileRestore: %w", err)
	}
	result := struct {
		ID        int64  `json:"id"`
		LoginName string `json:"login_name"`
	}{ID: userID, LoginName: restoredLogin}
	err = writeDatabaseJSON(output, result)
	if err != nil {
		return fmt.Errorf("restoreOutput: %w", err)
	}
	return nil
}

func databaseSwapProfiles(
	ctx context.Context, output io.Writer, configPath, table string, parameters []string,
) error {
	if table != "user" || len(parameters) != 2 {
		return errors.New("swap usage: db user swap <deleted-id> <active-id>")
	}
	deletedID, err := strconv.ParseInt(parameters[0], 10, 64)
	if err != nil || deletedID <= 0 {
		return errors.New("first account ID must be a positive deleted account ID")
	}
	activeID, err := strconv.ParseInt(parameters[1], 10, 64)
	if err != nil || activeID <= 0 || activeID == deletedID {
		return errors.New("second account ID must be a different positive active account ID")
	}
	swap, err := server.SwapUserProfileNames(ctx, configPath, sporenet.ProfileNameSwapRequest{
		DeletedUserID: deletedID, ActiveUserID: activeID,
	})
	if err != nil {
		return fmt.Errorf("profileSwap: %w", err)
	}
	result := struct {
		DeletedID        int64  `json:"deleted_id"`
		DeletedLoginName string `json:"deleted_login_name"`
		ActiveID         int64  `json:"active_id"`
		ActiveLoginName  string `json:"active_login_name"`
	}{
		DeletedID: swap.DeletedUserID, DeletedLoginName: swap.DeletedLoginName,
		ActiveID: swap.ActiveUserID, ActiveLoginName: swap.ActiveLoginName,
	}
	err = writeDatabaseJSON(output, result)
	if err != nil {
		return fmt.Errorf("swapOutput: %w", err)
	}
	return nil
}
