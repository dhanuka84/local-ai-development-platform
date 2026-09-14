package sourceadapter

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
	"github.com/dhanuka84/hybrid-ai-platform/internal/mcpclient"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

type KafkaCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func kafkaOffsets(ctx context.Context, client *kgo.Client, b Backend, timestamp int64) (map[int32]int64, error) {
	req := kmsg.NewPtrListOffsetsRequest()
	topic := kmsg.NewListOffsetsRequestTopic()
	topic.Topic = b.Topic
	for _, id := range b.Partitions {
		p := kmsg.NewListOffsetsRequestTopicPartition()
		p.Partition = id
		p.Timestamp = timestamp
		topic.Partitions = append(topic.Partitions, p)
	}
	req.Topics = []kmsg.ListOffsetsRequestTopic{topic}
	out := map[int32]int64{}
	for _, shard := range client.RequestSharded(ctx, req) {
		if shard.Err != nil {
			return nil, shard.Err
		}
		response, ok := shard.Resp.(*kmsg.ListOffsetsResponse)
		if !ok {
			return nil, errors.New("invalid Kafka offset response")
		}
		for _, topic := range response.Topics {
			if topic.Topic != b.Topic {
				return nil, errors.New("unexpected Kafka topic")
			}
			for _, p := range topic.Partitions {
				if p.ErrorCode != 0 {
					return nil, kerr.ErrorForCode(p.ErrorCode)
				}
				if p.Offset < 0 {
					return nil, errors.New("Kafka offset unavailable")
				}
				out[p.Partition] = p.Offset
			}
		}
	}
	if len(out) != len(b.Partitions) {
		return nil, errors.New("Kafka partition coverage missing")
	}
	return out, nil
}

func readKafka(ctx context.Context, v View, q domain.SourceQuery) (out domain.SourceEnvelope, err error) {
	b := v.Backend
	// No ConsumerGroup option, producer call, offset commit or reset exists in
	// this reader. Explicit partition cursors are private to this one query.
	opts := []kgo.Opt{kgo.SeedBrokers(b.Brokers...), kgo.ClientID("hybrid-ai-read-only-observer"), kgo.FetchMaxBytes(int32(v.Contract.MaxBytes)), kgo.FetchMaxPartitionBytes(int32(v.Contract.MaxBytes)), kgo.FetchMaxWait(100 * time.Millisecond), kgo.FetchIsolationLevel(kgo.ReadCommitted()), kgo.MaxConcurrentFetches(1)}
	if !b.AllowPlaintextLocal {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	} else {
		opts = append(opts, kgo.Dialer(localPlainDial))
	}
	if b.CredentialFile != "" {
		raw, e := mcpclient.ReadToken(b.CredentialFile)
		if e != nil {
			return out, e
		}
		var c KafkaCredential
		if execution.DecodeProposal([]byte(raw), &c) != nil || c.Username == "" || c.Password == "" {
			return out, errors.New("invalid Kafka credential file")
		}
		opts = append(opts, kgo.SASL(scram.Auth{User: c.Username, Pass: c.Password}.AsSha256Mechanism()))
	}
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return out, err
	}
	defer client.Close()
	metadata := kmsg.NewPtrMetadataRequest()
	metadata.Topics = []kmsg.MetadataRequestTopic{{Topic: &b.Topic}}
	response, err := client.Request(ctx, metadata)
	if err != nil {
		return out, err
	}
	topology, ok := response.(*kmsg.MetadataResponse)
	if !ok || len(topology.Topics) != 1 || len(topology.Topics[0].Partitions) != len(b.Partitions) {
		return out, errors.New("Kafka topic partition contract changed")
	}
	for _, partition := range topology.Topics[0].Partitions {
		found := false
		for _, expected := range b.Partitions {
			found = found || partition.Partition == expected
		}
		if !found || partition.ErrorCode != 0 {
			return out, errors.New("Kafka topic partition contract unavailable")
		}
	}
	start, err := kafkaOffsets(ctx, client, b, -2)
	if err != nil {
		return out, err
	}
	end, err := kafkaOffsets(ctx, client, b, -1)
	if err != nil {
		return out, err
	}
	out = envelope(v, q)
	out.Offsets = map[string]int64{}
	watermarks := map[int32]time.Time{}
	pending := map[int32]bool{}
	partitions := map[int32]kgo.Offset{}
	for _, id := range b.Partitions {
		out.Offsets[fmt.Sprintf("%s:%d:start", b.Topic, id)] = start[id]
		out.Offsets[fmt.Sprintf("%s:%d:end", b.Topic, id)] = end[id]
		if end[id] > start[id] {
			pending[id] = true
			partitions[id] = kgo.NewOffset().At(start[id])
		}
	}
	client.AddConsumePartitions(map[string]map[int32]kgo.Offset{b.Topic: partitions})
	scanned := 0
	for len(pending) > 0 && scanned < b.MaxScanRecords {
		fetches := client.PollRecords(ctx, min(1000, b.MaxScanRecords-scanned))
		if errs := fetches.Errors(); len(errs) > 0 {
			return out, errors.New("Kafka partition fetch failed")
		}
		for _, record := range fetches.Records() {
			if !pending[record.Partition] || record.Offset >= end[record.Partition] {
				continue
			}
			scanned++
			barrier := false
			for _, header := range record.Headers {
				if header.Key == "hybrid-ai-watermark" {
					when, e := time.Parse(time.RFC3339Nano, string(header.Value))
					if e != nil || when.After(time.Now().Add(time.Minute)) {
						return out, errors.New("invalid producer watermark")
					}
					if when.After(watermarks[record.Partition]) {
						watermarks[record.Partition] = when
					}
					barrier = true
				}
			}
			if !barrier {
				var row map[string]json.RawMessage
				if json.Unmarshal(record.Value, &row) != nil {
					return out, errors.New("Kafka event schema invalid")
				}
				if err = appendRow(&out, q, row); err != nil {
					return out, err
				}
			}
			if record.Offset+1 >= end[record.Partition] {
				delete(pending, record.Partition)
				client.RemoveConsumePartitions(map[string][]int32{b.Topic: {record.Partition}})
			}
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
	}
	if len(pending) > 0 {
		out.Complete = false
	}
	watermark := time.Now().UTC()
	for _, id := range b.Partitions {
		when := watermarks[id]
		if when.IsZero() {
			when = time.Unix(0, 0).UTC()
			out.Complete = false
		}
		if when.Before(watermark) {
			watermark = when
		}
	}
	out.Watermark = watermark
	raw, _ := json.Marshal(out.Offsets)
	out.Revision = "kafka-prefix:" + domain.Digest(raw)
	return out, nil
}
