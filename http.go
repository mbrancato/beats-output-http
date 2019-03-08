package http

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/outputs"
)

func init() {
	outputs.RegisterType("http", makeHTTP)
}

var (
	debugf = logp.MakeDebug("http")
)

var (
	// ErrNotConnected indicates failure due to client having no valid connection
	ErrNotConnected = errors.New("not connected")

	// ErrJSONEncodeFailed indicates encoding failures
	ErrJSONEncodeFailed = errors.New("json encode failed")
)

func makeHTTP(
	beat beat.Info,
	observer outputs.Observer,
	cfg *common.Config,
) (outputs.Group, error) {
	config := defaultConfig
	logp.Info("Initializing HTTP output")

	if err := cfg.Unpack(&config); err != nil {
		return outputs.Fail(err)
	}

	tlsConfig, err := outputs.LoadTLSConfig(config.TLS)
	if err != nil {
		return outputs.Fail(err)
	}

	hosts, err := outputs.ReadHostList(cfg)
	if err != nil {
		return outputs.Fail(err)
	}

	proxyURL, err := parseProxyURL(config.ProxyURL)
	if err != nil {
		return outputs.Fail(err)
	}
	if proxyURL != nil {
		logp.Info("Using proxy URL: %s", proxyURL)
	}

	params := config.Params
	if len(params) == 0 {
		params = nil
	}

	clients := make([]outputs.NetworkClient, len(hosts))

	for i, host := range hosts {
		logp.Info("Making client for host: " + host)
		var name string
		var port int
		if config.Protocol == "https" {
			port = 443
		} else if config.Protocol == "http" {
			port = 80
		} else {
			logp.Warn("Unknown protocol: %s", config.Protocol)
		}
		hostparts := strings.Split(host, ":")

		if len(hostparts) == 1 {
			name = hostparts[0]
		} else if len(hostparts) == 2 {
			name = hostparts[0]
			port, err = strconv.Atoi(hostparts[1])
			if err != nil {
				logp.Err("Invalid port number: %v, Error: %v", hostparts[1], err)
				return outputs.Fail(err)
			}
		} else {
			logp.Err("Unable to parse host:port from: %v", host)
		}

		//hostURL, err := getURL(config.Protocol, port, config.Path, host)
		hostURL := fmt.Sprintf("%v://%v:%v/%v", config.Protocol, name, port, config.Path)
		if err != nil {
			logp.Err("Invalid host param set: %s, Error: %v", name, err)
			return outputs.Fail(err)
		}

		logp.Info("Final host URL: " + hostURL)

		var client outputs.NetworkClient
		client, err = NewClient(ClientSettings{
			URL:              hostURL,
			Proxy:            proxyURL,
			TLS:              tlsConfig,
			Username:         config.Username,
			Password:         config.Password,
			Parameters:       params,
			Timeout:          config.Timeout,
			CompressionLevel: config.CompressionLevel,
			BatchPublish:     config.BatchPublish,
			headers:          config.Headers,
			ContentType:      config.ContentType,
		})

		if err != nil {
			return outputs.Fail(err)
		}

		client = outputs.WithBackoff(client, config.Backoff.Init, config.Backoff.Max)
		clients[i] = client
	}

	return outputs.SuccessNet(config.LoadBalance, config.BatchSize, config.MaxRetries, clients)
}

func parseProxyURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}

	url, err := url.Parse(raw)
	if err == nil && strings.HasPrefix(url.Scheme, "http") {
		return url, err
	}

	// Proxy was bogus. Try prepending "http://" to it and
	// see if that parses correctly.
	return url.Parse("http://" + raw)
}
