package http

import (
  "errors"
  "net/url"
  "strings"
  "time"

  "github.com/elastic/beats/libbeat/beat"
  "github.com/elastic/beats/libbeat/common"
  "github.com/elastic/beats/libbeat/logp"
  "github.com/elastic/beats/libbeat/outputs"
)

func init() {
  // outputs.RegisterOutputPlugin("http", New)
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

  headers := make(map[string]string)
  fields := strings.Split(config.Headers, "|")
  if len(fields) % 2 == 0 {
    for index := 0; index < len(fields); index+=2  {
      logp.Debug("header field: %s %s", fields[index], fields[index+1])
      headers[fields[index]] = fields[index+1]
    }
  } else {
    logp.Info("No header fields found")
  }

  clients := make([]outputs.NetworkClient, len(hosts))

  for i, host := range hosts {
    logp.Info("Making client for host: " + host)
    hostURL, err := getURL(config.Protocol, 80, config.Path, host)
    if err != nil {
      logp.Err("Invalid host param set: %s, Error: %v", host, err)
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
      headers:          headers,
    })

    if err != nil {
      return outputs.Fail(err)
    }

    // TODO: Backoff to be implemented
    client = outputs.WithBackoff(client, 1*time.Second, 60*time.Second)
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
