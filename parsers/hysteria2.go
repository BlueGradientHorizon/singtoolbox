package parsers

import (
	"singboxtest/utils"

	"github.com/sagernet/sing-box/option"
)

type Hysteria2Parser struct{}

func (p Hysteria2Parser) ParseProfile(connURI string) (*ProxyProfile, error) {
	connURI, err := utils.TryFixURI(connURI)
	if err != nil {
		return nil, err
	}

	uri, addr, port, err := extractCommonURIData(connURI, "vless")
	if err != nil {
		return nil, err
	}

	params := uri.Query()

	sni := params.Get("sni")
	insecure := params.Get("insecure") == "1"
	// pinSHA256 := params.Get("pinSHA256")
	obfsType := params.Get("obfs")
	salamanderPassword := params.Get("obfs-password")
	password := uri.User.Username()

	var obfs *option.Hysteria2Obfs
	if obfsType != "" && salamanderPassword != "" {
		obfs = &option.Hysteria2Obfs{
			Type:     obfsType,
			Password: salamanderPassword,
		}
	}

	TLSOptions := &option.OutboundTLSOptions{
		Enabled:    true,
		ServerName: sni,
		Insecure:   insecure,
	}

	if sni == "" {
		TLSOptions.Insecure = true
	}

	o := &option.Outbound{
		Type: "hysteria2",
		Options: &option.Hysteria2OutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     addr,
				ServerPort: port,
			},
			Obfs: obfs,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
				TLS: TLSOptions,
			},
			Password: password,
		},
	}

	return &ProxyProfile{
		Outbound: o,
		ConnURI:  connURI,
	}, nil
}
