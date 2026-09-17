package recaphttp

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// URI is the parsed form used by the original HTTP router.
type URI struct {
	protocol        string
	domain          string
	resource        string
	fragment        string
	port            uint16
	queryParameters map[string]string
}

// ParseURI parses absolute URLs and HTTP request targets.
func ParseURI(raw string) (*URI, error) {
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		return nil, fmt.Errorf("uriDecode: %w", err)
	}
	parsed, err := url.Parse(decoded)
	if err != nil {
		return nil, fmt.Errorf("uriParse: %w", err)
	}

	protocol := parsed.Scheme
	if protocol == "" {
		protocol = "http"
	}
	port := defaultPort(protocol)
	if parsed.Port() != "" {
		parsedPort, parseErr := strconv.ParseUint(parsed.Port(), 10, 16)
		if parseErr != nil {
			return nil, fmt.Errorf("portParse: %w", parseErr)
		}
		port = uint16(parsedPort)
	}
	resource := parsed.EscapedPath()
	if resource == "" {
		resource = parsed.Path
	}
	if resource == "" {
		resource = "/"
	}

	queryParameters := make(map[string]string)
	for key, values := range parsed.Query() {
		if len(values) == 0 {
			queryParameters[key] = ""
			continue
		}
		queryParameters[key] = values[0]
	}

	return &URI{
		protocol:        protocol,
		domain:          parsed.Hostname(),
		resource:        resource,
		fragment:        parsed.Fragment,
		port:            port,
		queryParameters: queryParameters,
	}, nil
}

func (u *URI) Protocol() string { return u.protocol }
func (u *URI) Domain() string   { return u.domain }
func (u *URI) Resource() string { return u.resource }
func (u *URI) Fragment() string { return u.fragment }
func (u *URI) Port() uint16     { return u.port }

// Parameter returns a decoded queryParameters value or an empty string.
func (u *URI) Parameter(name string) string {
	return u.queryParameters[name]
}

// SetParameter updates an in-memory queryParameters parameter.
func (u *URI) SetParameter(name, value string) {
	u.queryParameters[name] = value
}

// Parameters returns an independent copy of all queryParameters parameters.
func (u *URI) Parameters() map[string]string {
	parameters := make(map[string]string, len(u.queryParameters))
	for key, value := range u.queryParameters {
		parameters[key] = value
	}
	return parameters
}

func defaultPort(protocol string) uint16 {
	if strings.EqualFold(protocol, "https") || strings.EqualFold(protocol, "wss") {
		return 443
	}
	return 80
}
