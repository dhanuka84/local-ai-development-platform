package sourceadapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/mcpclient"
)

func localPlainDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, domain.ErrForbidden
	}
	for _, ip := range ips {
		if !ip.IP.IsLoopback() && !ip.IP.IsPrivate() {
			return nil, domain.ErrForbidden
		}
	}
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

func (a *Adapter) get(ctx context.Context, v View, path string, params url.Values) ([]byte, error) {
	u, err := url.Parse(v.Backend.Endpoint)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if v.Backend.CredentialFile != "" {
		token, err := mcpclient.ReadToken(v.Backend.CredentialFile)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := a.client
	if u.Scheme == "http" {
		client = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{DialContext: localPlainDial, DisableKeepAlives: true}, CheckRedirect: a.client.CheckRedirect}
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("native HTTP query failed")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, v.Contract.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > v.Contract.MaxBytes {
		return nil, domain.ErrBudgetExhausted
	}
	return raw, nil
}

func (a *Adapter) readLoki(ctx context.Context, v View, q domain.SourceQuery) (out domain.SourceEnvelope, err error) {
	params := url.Values{"query": {v.Backend.Query}, "start": {strconv.FormatInt(q.Start.UnixNano(), 10)}, "end": {strconv.FormatInt(q.End.UnixNano(), 10)}, "limit": {strconv.Itoa(v.Backend.MaxScanRecords)}, "direction": {"forward"}}
	raw, err := a.get(ctx, v, "/loki/api/v1/query_range", params)
	if err != nil {
		return out, err
	}
	var response struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
		Data     struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Stream map[string]string `json:"stream"`
				Values [][]string        `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Status != "success" || response.Data.ResultType != "streams" {
		return out, errors.New("invalid log response")
	}
	out = envelope(v, q)
	out.Revision = "loki:" + domain.Digest(raw)
	watermarks := map[string]time.Time{}
	count := 0
	for _, stream := range response.Data.Result {
		for _, entry := range stream.Values {
			count++
			if len(entry) != 2 {
				return out, errors.New("invalid log entry")
			}
			var row map[string]json.RawMessage
			if json.Unmarshal([]byte(entry[1]), &row) != nil {
				return out, errors.New("log view requires structured records")
			}
			if watermark, ok := row["_watermark"]; ok {
				var when time.Time
				if json.Unmarshal(watermark, &when) != nil {
					return out, errors.New("invalid log producer watermark")
				}
				id := stream.Stream["stream"]
				if when.After(watermarks[id]) {
					watermarks[id] = when
				}
				continue
			}
			if err = appendRow(&out, q, row); err != nil {
				return out, err
			}
		}
	}
	if count >= v.Backend.MaxScanRecords || len(response.Warnings) > 0 {
		out.Complete = false
	}
	watermark := time.Now().UTC()
	for _, id := range v.Backend.ExpectedStreams {
		when := watermarks[id]
		if when.IsZero() {
			out.Complete = false
			when = time.Unix(0, 0).UTC()
		}
		if when.Before(watermark) {
			watermark = when
		}
	}
	out.Watermark = watermark
	sort.Slice(out.Rows, func(i, j int) bool { return string(out.Rows[i]["event_id"]) < string(out.Rows[j]["event_id"]) })
	return out, nil
}

type prometheusResponse struct {
	Status   string   `json:"status"`
	Warnings []string `json:"warnings"`
	Data     struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string   `json:"metric"`
			Values [][]json.RawMessage `json:"values"`
			Value  []json.RawMessage   `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func metricSample(sample []json.RawMessage) (time.Time, float64, error) {
	if len(sample) != 2 {
		return time.Time{}, 0, errors.New("invalid metric sample")
	}
	var seconds float64
	var value string
	if json.Unmarshal(sample[0], &seconds) != nil || json.Unmarshal(sample[1], &value) != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return time.Time{}, 0, errors.New("invalid metric time")
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return time.Time{}, 0, errors.New("missing or non-finite metric value")
	}
	return time.Unix(0, int64(seconds*1e9)).UTC(), number, nil
}
func (a *Adapter) readPrometheus(ctx context.Context, v View, q domain.SourceQuery) (out domain.SourceEnvelope, err error) {
	seconds := func(t time.Time) string { return strconv.FormatFloat(float64(t.UnixNano())/1e9, 'f', 9, 64) }
	params := url.Values{"query": {v.Backend.Query}, "start": {seconds(q.Start)}, "end": {seconds(q.End.Add(-time.Nanosecond))}, "step": {"1"}, "limit": {strconv.Itoa(v.Backend.MaxScanRecords)}}
	raw, err := a.get(ctx, v, "/api/v1/query_range", params)
	if err != nil {
		return out, err
	}
	var response prometheusResponse
	if json.Unmarshal(raw, &response) != nil || response.Status != "success" || response.Data.ResultType != "matrix" {
		return out, errors.New("invalid metric response")
	}
	out = envelope(v, q)
	out.Revision = "prometheus:" + domain.Digest(raw)
	count := 0
	for _, series := range response.Data.Result {
		labels, _ := json.Marshal(series.Metric)
		for _, sample := range series.Values {
			count++
			if count > v.Backend.MaxScanRecords {
				out.Complete = false
				break
			}
			when, value, e := metricSample(sample)
			if e != nil {
				return out, e
			}
			row := map[string]json.RawMessage{"event_id": jsonValue(domain.Digest(append(labels, []byte(when.Format(time.RFC3339Nano))...))), "event_time": jsonValue(when), "value": jsonValue(value)}
			for name, value := range series.Metric {
				if name != "event_id" && name != "event_time" && name != "value" {
					row[name] = jsonValue(value)
				}
			}
			if err = appendRow(&out, q, row); err != nil {
				return out, err
			}
		}
	}
	if len(response.Warnings) > 0 || len(out.Rows) == 0 {
		out.Complete = false
	}
	watermarkRaw, err := a.get(ctx, v, "/api/v1/query", url.Values{"query": {v.Backend.WatermarkQuery}})
	if err != nil {
		return out, err
	}
	var watermarks prometheusResponse
	if json.Unmarshal(watermarkRaw, &watermarks) != nil || watermarks.Status != "success" || watermarks.Data.ResultType != "vector" {
		return out, errors.New("metric producer watermark unavailable")
	}
	watermark := time.Now().UTC()
	if len(watermarks.Data.Result) == 0 || len(watermarks.Warnings) > 0 {
		out.Complete = false
		watermark = time.Unix(0, 0).UTC()
	}
	for _, series := range watermarks.Data.Result {
		_, value, e := metricSample(series.Value)
		if e != nil {
			return out, e
		}
		when := time.Unix(0, int64(value*1e9)).UTC()
		if when.Before(watermark) {
			watermark = when
		}
	}
	out.Watermark = watermark
	return out, nil
}

func readMCP(ctx context.Context, v View, q domain.SourceQuery) (out domain.SourceEnvelope, err error) {
	token, err := mcpclient.ReadToken(v.Backend.CredentialFile)
	if err != nil {
		return out, err
	}
	client, err := mcpclient.Connect(ctx, v.Backend.Endpoint, token)
	if err != nil {
		return out, err
	}
	defer client.Close()
	if err = client.RequireReadOnly(ctx, v.Backend.Tool); err != nil {
		return out, err
	}
	err = client.Call(ctx, v.Backend.Tool, q, &out)
	return out, err
}
