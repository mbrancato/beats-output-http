package http

// Taken from https://github.com/elastic/beats/blob/master/libbeat/outputs/elasticsearch/url.go
// with minor changes

import (
  "net/url"
  "strings"
)

func makeURL(url, path, pipeline string, params map[string]string) string {
  if len(params) == 0 && pipeline == "" {
    return url + path
  }

  return strings.Join([]string{url, path, "?", urlEncode(pipeline, params),}, "")
}

// Encode parameters in url
func urlEncode(pipeline string, params map[string]string) string {
  values := url.Values{}

  for key, val := range params {
    values.Add(key, string(val))
  }

  if pipeline != "" {
    values.Add("pipeline", pipeline)
  }

  return values.Encode()
}
