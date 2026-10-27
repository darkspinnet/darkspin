package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/darkspinnet/darkspin/server/buildinfo"
	"github.com/darkspinnet/darkspin/server/game"
)

func checkMaintenanceReadiness(ctx context.Context, configPath string) error {
	fi, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("configStat: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return errors.New("readiness requires an existing configuration file")
	}
	config, isCreated, err := game.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("configLoad: %w", err)
	}
	if isCreated {
		return errors.New("readiness configuration was missing")
	}
	bindHost, advertisedHost, err := config.NetworkHosts()
	if err != nil {
		return fmt.Errorf("networkHost: %w", err)
	}
	if advertisedHost == "" {
		return errors.New("server advertised host is missing")
	}
	if bindHost == "0.0.0.0" {
		bindHost = "127.0.0.1"
	}
	port, err := config.Uint16(game.ConfigServerPort)
	if err != nil {
		return fmt.Errorf("portParse: %w", err)
	}
	endpoint := "http://" + net.JoinHostPort(bindHost, strconv.Itoa(int(port))) + "/recap/server"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("probeRequest: %w", err)
	}
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: rejectReadinessRedirect}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("probeSend: %w", err)
	}
	var identity struct {
		BuildID       string `json:"build_id"`
		ServerVersion string `json:"server_version"`
		GameVersion   string `json:"game_version"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&identity)
	closeErr := response.Body.Close()
	if decodeErr != nil || closeErr != nil {
		return fmt.Errorf("probeDecode: %w", errors.Join(decodeErr, closeErr))
	}
	if response.StatusCode != http.StatusOK || identity.BuildID != buildinfo.ID ||
		identity.ServerVersion != buildinfo.Version || identity.GameVersion == "" {
		return errors.New("main server endpoint does not identify this running image")
	}
	return nil
}

func rejectReadinessRedirect(req *http.Request, previousRequests []*http.Request) error {
	return http.ErrUseLastResponse
}
