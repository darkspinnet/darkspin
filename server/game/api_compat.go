package game

import (
	"encoding/xml"
	"fmt"
	"net/http"

	"github.com/darkspinnet/darkspin/server/buildinfo"
)

type bootstrapAPIResponse struct {
	XMLName   xml.Name            `xml:"response"`
	Stat      string              `xml:"stat"`
	Code      int                 `xml:"code"`
	Result    int                 `xml:"result"`
	Configs   bootstrapConfigs    `xml:"configs"`
	ToImage   struct{}            `xml:"to_image"`
	FromImage struct{}            `xml:"from_image"`
	Settings  *bootstrapSettings  `xml:"settings,omitempty"`
	Patches   *bootstrapPatchList `xml:"patches,omitempty"`
}

type bootstrapConfigs struct {
	Config []bootstrapConfig `xml:"config"`
}

type bootstrapConfig struct {
	BlazeServiceName string `xml:"blaze_service_name"`
	BlazeSecure      string `xml:"blaze_secure"`
	BlazeEnvironment string `xml:"blaze_env"`
	SporeNetCDNHost  string `xml:"sporenet_cdn_host"`
	SporeNetDBHost   string `xml:"sporenet_db_host"`
	SporeNetDBName   string `xml:"sporenet_db_name"`
	SporeNetHost     string `xml:"sporenet_host"`
	HTTPSecure       string `xml:"http_secure"`
	LiferayHost      string `xml:"liferay_host"`
	LauncherAction   int    `xml:"launcher_action"`
	LauncherURL      string `xml:"launcher_url"`
}

type bootstrapSettings struct {
	Open             bootstrapOpen `xml:"open"`
	TelemetryRate    int           `xml:"telemetry-rate"`
	TelemetrySetting int           `xml:"telemetry-setting"`
}

type bootstrapOpen struct {
	Test  string `xml:"test,attr"`
	Value string `xml:",chardata"`
}

type bootstrapPatchList struct{}

type gameStatusAPIResponse struct {
	XMLName    xml.Name           `xml:"response"`
	Stat       string             `xml:"stat"`
	Code       int                `xml:"code"`
	Result     int                `xml:"result"`
	Status     *gameServiceStatus `xml:"status,omitempty"`
	Broadcasts *gameBroadcastList `xml:"broadcasts,omitempty"`
}

type gameServiceStatus struct {
	API     gameAPIHealth     `xml:"api"`
	Blaze   gameHealth        `xml:"blaze"`
	GMS     gameHealth        `xml:"gms"`
	Nucleus gameHealth        `xml:"nucleus"`
	Game    gameRuntimeHealth `xml:"game"`
}

type gameAPIHealth struct {
	Health   int `xml:"health"`
	Revision int `xml:"revision"`
	Version  int `xml:"version"`
}

type gameHealth struct {
	Health int `xml:"health"`
}

type gameRuntimeHealth struct {
	Health    int `xml:"health"`
	Countdown int `xml:"countdown"`
	Open      int `xml:"open"`
	Throttle  int `xml:"throttle"`
	VIP       int `xml:"vip"`
}

type gameBroadcastList struct {
	Broadcast []gameBroadcast `xml:"broadcast"`
}

type gameBroadcast struct {
	ID      int    `xml:"id"`
	End     int    `xml:"end"`
	Start   int    `xml:"start"`
	Type    int    `xml:"type"`
	Message string `xml:"message"`
	Tokens  string `xml:"tokens"`
}

func newBootstrapAPIResponse(host string, port uint16, build string, areSettingsIncluded, arePatchesIncluded bool) bootstrapAPIResponse {
	response := bootstrapAPIResponse{Stat: "ok", Code: http.StatusOK, Result: 1}
	response.Configs.Config = []bootstrapConfig{{
		BlazeServiceName: "game",
		BlazeSecure:      "N",
		BlazeEnvironment: "prod",
		SporeNetCDNHost:  host,
		SporeNetDBHost:   host,
		SporeNetDBName:   "game",
		SporeNetHost:     host,
		HTTPSecure:       "N",
		LiferayHost:      host,
		LauncherAction:   2,
		LauncherURL:      fmt.Sprintf("http://%s:%d/bootstrap/launcher/?version=%s", host, port, build),
	}}
	if areSettingsIncluded {
		response.Settings = &bootstrapSettings{
			Open:             bootstrapOpen{Test: "true", Value: "true"},
			TelemetryRate:    256,
			TelemetrySetting: 0,
		}
	}
	if arePatchesIncluded {
		response.Patches = &bootstrapPatchList{}
	}
	return response
}

func newGameStatusAPIResponse(isStatusIncluded, areBroadcastsIncluded bool) gameStatusAPIResponse {
	response := gameStatusAPIResponse{Stat: "ok", Code: http.StatusOK, Result: 1}
	if isStatusIncluded {
		response.Status = &gameServiceStatus{
			API:     gameAPIHealth{Health: 1, Revision: 1, Version: 1},
			Blaze:   gameHealth{Health: 1},
			GMS:     gameHealth{Health: 1},
			Nucleus: gameHealth{Health: 1},
			Game:    gameRuntimeHealth{Health: 1, Countdown: 90, Open: 1, Throttle: 1, VIP: 1},
		}
	}
	if areBroadcastsIncluded {
		response.Broadcasts = &gameBroadcastList{Broadcast: []gameBroadcast{{
			ID:      0x10,
			End:     0x11,
			Start:   0x12,
			Type:    0x13,
			Message: "Dark Spinner v" + buildinfo.Version,
			Tokens:  "12345678",
		}}}
	}
	return response
}

func writeXMLDocument(writer http.ResponseWriter, status int, document any) {
	contents, err := xml.Marshal(document)
	if err != nil {
		http.Error(writer, "encode XML response", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/xml")
	writer.Header().Set("Content-Language", "en-us")
	writer.WriteHeader(status)
	_, _ = writer.Write([]byte(xml.Header))
	_, _ = writer.Write(contents)
}

func queryBool(values interface{ Get(string) string }, names ...string) bool {
	for _, name := range names {
		value := values.Get(name)
		if value == "1" || value == "true" || value == "TRUE" {
			return true
		}
	}
	return false
}
