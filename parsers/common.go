package parsers

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
)

func buildOutboundTLSOptions(query url.Values, protocol string) (*option.OutboundTLSOptions, error) {
	options := &option.OutboundTLSOptions{}

	securityKey := "security"

	if protocol == "vmess" {
		securityKey = "tls"
	}

	security := query.Get(securityKey) // "none"
	sni := query.Get("sni")
	alpn := query.Get("alpn")
	fp := query.Get("fp")
	pbk := query.Get("pbk")
	sid := query.Get("sid")
	// pqv := query.Get("pqv")
	ech := query.Get("ech")
	allowInsecure := query.Get("allowInsecure") == "1"
	insecure := query.Get("insecure") == "1"

	if security != "" {
		if (security != "tls") && (security != "reality") && (security != "none") {
			return nil, fmt.Errorf("unsupported security parameter %s", security)
		}

		if security != "none" {
			options.Enabled = true
		}

		options.ServerName = sni

		options.UTLS = &option.OutboundUTLSOptions{}
		if fp != "" {
			options.UTLS.Enabled = true
			options.UTLS.Fingerprint = fp
		}

		if alpn != "" {
			options.ALPN = badoption.Listable[string]{}
			parts := strings.Split(alpn, ",")
			for _, val := range parts {
				options.ALPN = append(options.ALPN, val)
			}
		}

		if ech != "" {
			options.ECH = &option.OutboundECHOptions{
				Enabled: true,
				Config:  []string{ech},
			}
		}

		if insecure || allowInsecure {
			options.Insecure = true
		}
	}

	if security == "reality" {
		options.Reality = &option.OutboundRealityOptions{
			Enabled:   true,
			PublicKey: pbk,
			ShortID:   sid,
		}

		// uTLS is required by reality client
		options.UTLS.Enabled = true
		if fp != "" {
			options.UTLS.Fingerprint = fp
		} else {
			options.UTLS.Fingerprint = "chrome"
		}
	}

	return options, nil
}

func buildV2RayTransportOptions(query url.Values, protocol string) (*option.V2RayTransportOptions, error) {
	options := &option.V2RayTransportOptions{}

	typeKey := "type"
	serviceNameKey := "serviceName"
	// modeKey := "mode"

	if protocol == "vmess" {
		typeKey = "net"
		serviceNameKey = "path"
		// modeKey = "type"
	}

	// spx := query.Get("spx")
	path := query.Get("path")
	host := query.Get("host")
	// headerType := query.Get("headerType")    // "none"
	serviceName := query.Get(serviceNameKey) // sni or host
	// authority := query.Get("authority")
	// seed := query.Get("seed")
	// mode := query.Get(modeKey)

	type_ := query.Get(typeKey) // "raw"

	switch type_ {
	case "", "raw", "tcp":
		// Transport not needed
	case "http", "h2":
		options.Type = C.V2RayTransportTypeHTTP
		options.HTTPOptions = option.V2RayHTTPOptions{
			Host:   []string{host},
			Path:   path,
			Method: "GET",
			// Headers: , // TODO ??
		}
	case "ws", "websocket":
		options.Type = C.V2RayTransportTypeWebsocket
		if path == "" {
			path = "/"
		}
		options.WebsocketOptions = option.V2RayWebsocketOptions{
			Path: path,
			// Headers: , // TODO ??
		}
	case "quic":
		options.Type = C.V2RayTransportTypeQUIC
		options.QUICOptions = option.V2RayQUICOptions{}
	case "grpc":
		options.Type = C.V2RayTransportTypeGRPC
		options.GRPCOptions = option.V2RayGRPCOptions{
			ServiceName: serviceName,
		}
	case "httpupgrade":
		options.Type = C.V2RayTransportTypeHTTPUpgrade
		options.HTTPUpgradeOptions = option.V2RayHTTPUpgradeOptions{
			Host: host,
			Path: path,
			// Headers: , // TODO ??
		}
	case "kcp":
		return nil, errors.New("transport kcp unsupported")
	case "mkcp":
		return nil, errors.New("transport mkcp unsupported")
	case "xhttp":
		return nil, errors.New("transport xhttp unsupported")
	case "splithttp":
		return nil, errors.New("transport splithttp unsupported")
	default:
		return nil, fmt.Errorf("unknown transport %s", type_)
	}

	return options, nil
}

func fixTrojanURI(uri string) (*url.URL, error) {
	remarkSplitLastIndex := strings.LastIndex(uri, "#")

	var beforeRemark string
	if remarkSplitLastIndex == -1 {
		beforeRemark = uri
	} else {
		beforeRemark = uri[:remarkSplitLastIndex]
	}

	var remark string
	if remarkSplitLastIndex < len(uri) {
		remark = uri[remarkSplitLastIndex+1:]
	} else {
		remark = ""
	}

	lastAt := strings.LastIndex(beforeRemark, "@")
	if lastAt == -1 {
		return nil, errors.New("malformed URI: symbol '@' not found")
	}

	beforeAt := beforeRemark[:lastAt]
	afterAt := beforeRemark[lastAt+1:]

	schemeSplit := strings.SplitN(beforeAt, "://", 2)
	if len(schemeSplit) < 2 {
		return nil, errors.New("malformed URI: split by '://' failed")
	}
	scheme := schemeSplit[0]
	userInfo := schemeSplit[1]

	querySplit := strings.SplitN(afterAt, "?", 2)
	hostPort := querySplit[0]

	tempURI := scheme + "://placeholder@" + afterAt
	u, err := url.Parse(tempURI)
	if err != nil {
		return nil, err
	}

	u.User = url.User(userInfo)
	u.Host = strings.ReplaceAll(hostPort, "/", "")
	u.Fragment = remark
	return u, nil
}

func parseConfigURI(uri string, scheme string) (*url.URL, error) {
	if scheme == "trojan" {
		return fixTrojanURI(uri)
	}

	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	if u.Scheme == "" {
		u.Scheme = scheme
	}

	return u, nil
}

func parseNetlocForEndpoint(u *url.URL) (string, uint16, bool) {
	netloc := u.Host
	ipv6Regexp := regexp.MustCompile(`^\[([a-fA-F0-9:]+)\]:(\d+)$`)
	match := ipv6Regexp.FindStringSubmatch(netloc)

	var address string
	var port int

	if len(match) > 0 {
		address = match[1]
		p, _ := strconv.Atoi(match[2])
		port = p
	} else {
		host, portStr, err := net.SplitHostPort(netloc)
		if err != nil {
			address = netloc
			pStr := u.Port()
			if pStr != "" {
				p, err := strconv.Atoi(pStr)
				if err != nil {
					return "", 0, false
				}
				port = p
			} else {
				port = 0
			}
		} else {
			address = host
			p, _ := strconv.Atoi(portStr)
			port = p
		}
	}

	address = strings.TrimPrefix(address, "[")
	address = strings.TrimSuffix(address, "]")

	if port < 0 || port > math.MaxUint16 {
		return "", 0, false
	}

	return address, uint16(port), true
}

func extractCommonURIData(uri string, scheme string) (*url.URL, string, uint16, error) {
	parsedURI, err := parseConfigURI(uri, scheme)
	if err != nil {
		return nil, "", 0, err
	}

	address, port, ok := parseNetlocForEndpoint(parsedURI)
	if !ok {
		return nil, "", 0, errors.New("cannot parse netloc for endpoint")
	}

	return parsedURI, address, port, nil
}
