package parsers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/option"
)

type VMessParser struct{}

func (p VMessParser) ParseProfile(connURI string) (*ProxyProfile, error) {
	base64Part := strings.ReplaceAll(connURI, "vmess://", "")

	var enc *base64.Encoding
	isURL := strings.ContainsAny(base64Part, "-_")
	isRaw := !strings.HasSuffix(base64Part, "=")

	switch {
	case isURL && isRaw:
		enc = base64.RawURLEncoding
	case isURL && !isRaw:
		enc = base64.URLEncoding
	case !isURL && isRaw:
		enc = base64.RawStdEncoding
	default:
		enc = base64.StdEncoding
	}

	decodedBytes, err := enc.DecodeString(base64Part)
	if err != nil {
		return nil, errors.New("VMessParser.ParseProfile: " + err.Error())
	}

	var tempMap map[string]any
	if err := json.Unmarshal(decodedBytes, &tempMap); err != nil {
		return nil, errors.New("VMessParser.ParseProfile: " + err.Error())
	}

	query := map[string]string{}
	for k, v := range tempMap {
		if v == nil {
			query[k] = ""
			continue
		}
		query[k] = fmt.Sprintf("%v", v)
	}

	params := make(url.Values)
	for k, v := range query {
		params[k] = []string{v}
	}

	addr := params.Get("add")
	portUnchecked, err := strconv.ParseUint(params.Get("port"), 10, 16)
	if err != nil {
		return nil, errors.New("VMessParser.ParseProfile: " + err.Error())
	}
	port := uint16(portUnchecked)
	// remark := params.Get("ps")
	id := params.Get("id")
	security := params.Get("scy")

	TLSOptions, err := buildOutboundTLSOptions(params, "vmess")
	if err != nil {
		return nil, errors.New("VMessParser.ParseProfile: " + err.Error())
	}

	transportOptions, err := buildV2RayTransportOptions(params, "vmess")
	if err != nil {
		return nil, errors.New("VMessParser.ParseProfile: " + err.Error())
	}

	o := &option.Outbound{
		Type: "vmess",
		Options: &option.VMessOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     addr,
				ServerPort: port,
			},
			UUID:     id,
			Security: security,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
				TLS: TLSOptions,
			},
			Transport: transportOptions,
		},
	}

	return &ProxyProfile{
		Outbound: o,
		ConnURI:  connURI,
	}, nil
}
