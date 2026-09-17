package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	serverauth "github.com/darkspinnet/darkspin/server/auth"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/spf13/cobra"
)

const localAuthAddress = "0.0.0.0:8090"

func newAuthCommand() *cobra.Command {
	isLocal := false
	configPath := game.DefaultConfigFilename
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Run the desktop authentication broker",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isLocal {
				return errors.New("auth requires --local until an OAuth provider is configured")
			}
			err := runLocalAuth(cmd.Context(), cmd, localAuthAddress, configPath)
			if err != nil {
				return fmt.Errorf("localAuth: %w", err)
			}
			return nil
		},
	}
	authCmd.Flags().BoolVar(&isLocal, "local", false, "approve requested identities from loopback development launchers")
	authCmd.Flags().StringVar(&configPath, "config", game.DefaultConfigFilename, "path to the server configuration")
	return authCmd
}

func runLocalAuth(ctx context.Context, cmd *cobra.Command, address, configPath string) error {
	config, _, err := game.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("configLoad: %w", err)
	}
	secret := config.String(game.ConfigAuthJWTSecret)
	issuerName := config.String(game.ConfigAuthJWTIssuer)
	audience := config.String(game.ConfigAuthJWTAudience)
	issuer, err := serverauth.NewJWTIssuer([]byte(secret), issuerName, audience)
	if err != nil {
		return fmt.Errorf("issuerCreate: %w", err)
	}
	logger := log.New(cmd.ErrOrStderr(), "", log.LstdFlags)
	broker, err := serverauth.NewLocalBroker(issuer, address, logger)
	if err != nil {
		return fmt.Errorf("brokerCreate: %w", err)
	}
	server := &http.Server{Addr: address, Handler: broker.Handler(), ReadHeaderTimeout: 5 * time.Second}
	shutdownComplete := make(chan struct{})
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		close(shutdownComplete)
	}()
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "LOCAL AUTH DEVELOPMENT MODE: http://%s\n", address)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownComplete
		return nil
	}
	if err != nil {
		return fmt.Errorf("listenServe: %w", err)
	}
	return nil
}
