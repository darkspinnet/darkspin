package blaze

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/blaze/tdf"
)

const (
	AuthComponentID        uint16 = 0x01
	GameManagerComponentID uint16 = 0x04
	RedirectorComponentID  uint16 = 0x05
	PlaygroupsComponentID  uint16 = 0x06
	UtilComponentID        uint16 = 0x09
	MessagingComponentID   uint16 = 0x0f
	RoomsComponentID       uint16 = 0x15
	AssociationComponentID uint16 = 0x19
	UserSessionComponentID uint16 = 0x7802
)

const (
	redirectorGetServerInstance uint16 = 0x01
	utilFetchClientConfig       uint16 = 0x01
	utilPing                    uint16 = 0x02
	utilGetTelemetryServer      uint16 = 0x05
	utilPreAuth                 uint16 = 0x07
	utilPostAuth                uint16 = 0x08
	utilUserSettingsSave        uint16 = 0x0b
	utilUserSettingsLoadAll     uint16 = 0x0c
	utilSetClientMetrics        uint16 = 0x16
)

// Endpoints contains service addresses advertised by Blaze components.
type Endpoints struct {
	Host          string
	BlazePort     uint16
	PSSPort       uint16
	TickPort      uint16
	TelemetryPort uint16
	HTTPQoSPort   uint16
	GameVersion   string
}

// RegisterCoreComponents registers components that do not depend on SporeNet
// or active game instances. Stateful components are added by their packages.
func RegisterCoreComponents(registry *Registry, endpoints Endpoints) error {
	err := registry.Register(redirectorComponent(endpoints))
	if err != nil {
		return fmt.Errorf("redirectorRegister: %w", err)
	}
	err = registry.Register(utilComponent(endpoints))
	if err != nil {
		return fmt.Errorf("utilityRegister: %w", err)
	}
	return nil
}

func redirectorComponent(endpoints Endpoints) Component {
	return Component{
		ID:   RedirectorComponentID,
		Name: "Redirector",
		Commands: map[uint16]Handler{
			redirectorGetServerInstance: func(context.Context, *Request) (*Response, error) {
				address := tdf.UnionValue(0,
					tdf.FieldNamed("VALU", tdf.StructValue(
						tdf.FieldNamed("HOST", tdf.StringValue(endpoints.Host)),
						tdf.FieldNamed("IP", tdf.IntegerValue(0)),
						tdf.FieldNamed("PORT", tdf.IntegerValue(uint64(endpoints.BlazePort))),
					)),
				)
				return &Response{Fields: []tdf.Field{
					tdf.FieldNamed("ADDR", address),
					tdf.FieldNamed("SECU", tdf.IntegerValue(0)),
					tdf.FieldNamed("XDNS", tdf.IntegerValue(0)),
				}}, nil
			},
		},
	}
}

func utilComponent(endpoints Endpoints) Component {
	emptyReply := func(context.Context, *Request) (*Response, error) {
		return &Response{}, nil
	}
	return Component{
		ID:   UtilComponentID,
		Name: "Util",
		Commands: map[uint16]Handler{
			utilFetchClientConfig:   emptyReply,
			utilPing:                pingHandler,
			utilGetTelemetryServer:  telemetryHandler(endpoints, true),
			utilPreAuth:             preAuthHandler(endpoints),
			utilPostAuth:            postAuthHandler(endpoints),
			utilUserSettingsSave:    emptyReply,
			utilUserSettingsLoadAll: emptyReply,
			utilSetClientMetrics:    emptyReply,
		},
	}
}

func pingHandler(context.Context, *Request) (*Response, error) {
	return &Response{Fields: []tdf.Field{
		tdf.FieldNamed("STIM", tdf.IntegerValue(uint64(time.Now().Unix()))),
	}}, nil
}

func telemetryHandler(endpoints Endpoints, isAnonymous bool) Handler {
	return func(context.Context, *Request) (*Response, error) {
		return &Response{Fields: telemetryFields(endpoints, isAnonymous)}, nil
	}
}

func preAuthHandler(endpoints Endpoints) Handler {
	return func(_ context.Context, request *Request) (*Response, error) {
		serviceName := "game-pc"
		platform := "pc"
		clientData, isFound := tdf.Find(request.Fields, "CDAT")
		if isFound {
			service, serviceFound := tdf.Find(clientData.Fields, "SVCN")
			if serviceFound && service.Type == tdf.String {
				serviceName = service.String
			}
		}
		clientInfo, isFound := tdf.Find(request.Fields, "CINF")
		if isFound {
			platformValue, platformFound := tdf.Find(clientInfo.Fields, "PLAT")
			if platformFound && platformValue.Type == tdf.String {
				platform = platformValue.String
			}
		}

		componentValues := []ValueAlias{
			{AssociationComponentID}, {AuthComponentID}, {GameManagerComponentID},
			{MessagingComponentID}, {PlaygroupsComponentID}, {RedirectorComponentID},
			{RoomsComponentID}, {UserSessionComponentID}, {UtilComponentID},
		}
		components := make([]tdf.Value, 0, len(componentValues))
		for _, component := range componentValues {
			components = append(components, tdf.IntegerValue(uint64(component.value)))
		}
		configuration := tdf.MapValue(tdf.String, tdf.String, tdf.MapEntry{
			Key: tdf.StringValue("pingPeriod"), Value: tdf.StringValue("20000"),
		})
		pingSite := tdf.StructValue(
			tdf.FieldNamed("PSA", tdf.StringValue(endpoints.Host)),
			tdf.FieldNamed("PSP", tdf.IntegerValue(uint64(endpoints.HTTPQoSPort))),
			tdf.FieldNamed("SNA", tdf.StringValue("ams")),
		)
		qosConfig := tdf.StructValue(
			tdf.FieldNamed("BWPS", tdf.StructValue()),
			tdf.FieldNamed("LNP", tdf.IntegerValue(1)),
			tdf.FieldNamed("LTPS", tdf.MapValue(tdf.String, tdf.Struct, tdf.MapEntry{
				Key: tdf.StringValue("ams"), Value: pingSite,
			})),
			tdf.FieldNamed("SVID", tdf.IntegerValue(1161889797)),
		)
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("ASRC", tdf.StringValue("321915")),
			tdf.FieldNamed("CIDS", tdf.ListValue(tdf.Integer, components...)),
			tdf.FieldNamed("CONF", tdf.StructValue(tdf.FieldNamed("CONF", configuration))),
			tdf.FieldNamed("INST", tdf.StringValue(serviceName)),
			tdf.FieldNamed("NASP", tdf.StringValue("cem_ea_id")),
			tdf.FieldNamed("PILD", tdf.StringValue("")),
			tdf.FieldNamed("PLAT", tdf.StringValue(platform)),
			tdf.FieldNamed("QOSS", qosConfig),
			tdf.FieldNamed("RSRC", tdf.StringValue("321915")),
			tdf.FieldNamed("SVER", tdf.StringValue("Blaze 3.9.3.1")),
		}}, nil
	}
}

type ValueAlias struct{ value uint16 }

func postAuthHandler(endpoints Endpoints) Handler {
	return func(context.Context, *Request) (*Response, error) {
		pss := tdf.StructValue(
			tdf.FieldNamed("ADRS", tdf.StringValue(endpoints.Host)),
			tdf.FieldNamed("CSIG", tdf.BinaryValue(nil)),
			tdf.FieldNamed("OIDS", tdf.ListValue(tdf.Integer)),
			tdf.FieldNamed("PJID", tdf.StringValue("123071")),
			tdf.FieldNamed("PORT", tdf.IntegerValue(uint64(endpoints.PSSPort))),
			tdf.FieldNamed("RPRT", tdf.IntegerValue(9)),
			tdf.FieldNamed("TIID", tdf.IntegerValue(0)),
		)
		tickerKey := "0," + endpoints.Host + ":" + uint16String(endpoints.TickPort) + ",game-pc,10,50,50,50,50,0,0"
		ticker := tdf.StructValue(
			tdf.FieldNamed("ADRS", tdf.StringValue(endpoints.Host)),
			tdf.FieldNamed("PORT", tdf.IntegerValue(uint64(endpoints.TickPort))),
			tdf.FieldNamed("SKEY", tdf.StringValue(tickerKey)),
		)
		return &Response{Fields: []tdf.Field{
			tdf.FieldNamed("PSS", pss),
			tdf.FieldNamed("TELE", tdf.StructValue(telemetryFields(endpoints, false)...)),
			tdf.FieldNamed("TICK", ticker),
			tdf.FieldNamed("UROP", tdf.StructValue(tdf.FieldNamed("TMOP", tdf.IntegerValue(1)))),
		}}, nil
	}
}

func telemetryFields(endpoints Endpoints, isAnonymous bool) []tdf.Field {
	anonymousValue := uint64(0)
	if isAnonymous {
		anonymousValue = 1
	}
	return []tdf.Field{
		tdf.FieldNamed("ADRS", tdf.StringValue(endpoints.Host)),
		tdf.FieldNamed("ANON", tdf.IntegerValue(anonymousValue)),
		tdf.FieldNamed("DISA", tdf.StringValue("")),
		tdf.FieldNamed("FILT", tdf.StringValue("")),
		tdf.FieldNamed("LOC", tdf.IntegerValue(0)),
		tdf.FieldNamed("NOOK", tdf.StringValue("US,CA,MX")),
		tdf.FieldNamed("PORT", tdf.IntegerValue(uint64(endpoints.TelemetryPort))),
		tdf.FieldNamed("SDLY", tdf.IntegerValue(15000)),
		tdf.FieldNamed("SESS", tdf.StringValue("telemetry_session")),
		tdf.FieldNamed("SKEY", tdf.StringValue("telemetry_key")),
		tdf.FieldNamed("SPCT", tdf.IntegerValue(75)),
	}
}

func uint16String(value uint16) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	buffer := [5]byte{}
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = digits[value%10]
		value /= 10
	}
	return string(buffer[position:])
}
