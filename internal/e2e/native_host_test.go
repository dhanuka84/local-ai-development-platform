//go:build sdlc_host

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/sourceadapter"
	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"
)

// These are native service protocol fixtures. No model output or HTTP source
// stub stands in for Kafka, S3, PostgreSQL, Loki or Prometheus.
type nativeFixture struct {
	verify        func()
	client        *kgo.Client
	lake          *minio.Client
	bucket, topic string
	contracts     []domain.SourceDescriptor
	config        sourceadapter.Config
}

func TestSDLCNativeSourcesE2E(t *testing.T) {
	f := startRuntime(t)
	setupNativeSources(t, f).verify()
}
func setupNativeSources(t *testing.T, f *runtime, dynamic ...bool) nativeFixture {
	for _, name := range []string{"TEST_KAFKA_ADDRESS", "TEST_S3_ENDPOINT", "TEST_LOKI_ENDPOINT", "TEST_PROMETHEUS_ENDPOINT"} {
		if os.Getenv(name) == "" {
			t.Fatal("missing disposable native service", name)
		}
	}
	for _, entry := range []struct{ env, path string }{{"TEST_LOKI_ENDPOINT", "/ready"}, {"TEST_PROMETHEUS_ENDPOINT", "/-/ready"}} {
		f.wait(entry.env, func() bool {
			r, e := http.Get(os.Getenv(entry.env) + entry.path)
			if e != nil {
				return false
			}
			defer r.Body.Close()
			return r.StatusCode == 200
		})
	}
	end := time.Now().UTC().Add(-2 * time.Second).Truncate(time.Second)
	start := end.Add(-4 * time.Second)
	when := start.Add(time.Second)
	row := map[string]any{"event_id": "order-1", "event_time": when, "status": "processed"}
	raw, _ := json.Marshal(row)
	client, err := kgo.NewClient(kgo.SeedBrokers(os.Getenv("TEST_KAFKA_ADDRESS")), kgo.MaxVersions(kversion.V3_8_0()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	topic := "orders-" + strings.TrimPrefix(f.project, "pilot-e2e-")
	create := kmsg.NewPtrCreateTopicsRequest()
	create.Topics = []kmsg.CreateTopicsRequestTopic{{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}}
	created, err := create.RequestWith(f.ctx, client)
	if err != nil || len(created.Topics) != 1 || created.Topics[0].ErrorCode != 0 {
		t.Fatal("native topic creation", err, created)
	}
	if err = client.ProduceSync(f.ctx, &kgo.Record{Topic: topic, Value: raw}, &kgo.Record{Topic: topic, Headers: []kgo.RecordHeader{{Key: "hybrid-ai-watermark", Value: []byte(end.Format(time.RFC3339Nano))}}}).FirstErr(); err != nil {
		t.Fatal(err)
	}
	// An unrelated application group has a committed position. The observer
	// must not join it or move that position, including on repeated reads.
	group := "application-" + topic
	commit := kmsg.NewPtrOffsetCommitRequest()
	commit.Group = group
	partition := kmsg.NewOffsetCommitRequestTopicPartition()
	partition.Partition = 0
	partition.Offset = 1
	commit.Topics = []kmsg.OffsetCommitRequestTopic{{Topic: topic, Partitions: []kmsg.OffsetCommitRequestTopicPartition{partition}}}
	committed, err := commit.RequestWith(f.ctx, client)
	if err != nil || len(committed.Topics) != 1 || committed.Topics[0].Partitions[0].ErrorCode != 0 {
		t.Fatal("native sentinel offset commit", err, committed)
	}
	offset := func() int64 {
		t.Helper()
		fetch := kmsg.NewPtrOffsetFetchRequest()
		fetch.Group = group
		fetch.Topics = []kmsg.OffsetFetchRequestTopic{{Topic: topic, Partitions: []int32{0}}}
		fetch.Groups = []kmsg.OffsetFetchRequestGroup{{Group: group, Topics: []kmsg.OffsetFetchRequestGroupTopic{{Topic: topic, Partitions: []int32{0}}}}}
		r, e := fetch.RequestWith(f.ctx, client)
		if e != nil {
			t.Fatal(e)
		}
		if len(r.Groups) > 0 && len(r.Groups[0].Topics) > 0 {
			return r.Groups[0].Topics[0].Partitions[0].Offset
		}
		if len(r.Topics) > 0 {
			return r.Topics[0].Partitions[0].Offset
		}
		t.Fatal("missing native committed offset")
		return -1
	}
	before := offset()
	if before != 1 {
		t.Fatal("sentinel offset not established", before)
	}

	s3URL, _ := url.Parse(os.Getenv("TEST_S3_ENDPOINT"))
	lake, err := minio.New(s3URL.Host, &minio.Options{Creds: credentials.NewStaticV4("synthetic-acceptance", "synthetic-acceptance-only", ""), Secure: false})
	if err != nil {
		t.Fatal(err)
	}
	bucket := "orders-" + strings.TrimPrefix(f.project, "pilot-e2e-")
	if err = lake.MakeBucket(f.ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = lake.SetBucketVersioning(f.ctx, bucket, minio.BucketVersioningConfiguration{Status: "Enabled"}); err != nil {
		t.Fatal(err)
	}
	var typed map[string]json.RawMessage
	_ = json.Unmarshal(raw, &typed)
	snapshot := domain.SourceEnvelope{SchemaVersion: "orders/v1", Revision: "producer-1", Start: start, End: end, Watermark: end, Complete: true, Rows: []map[string]json.RawMessage{typed}}
	putLake := func(s domain.SourceEnvelope) minio.UploadInfo {
		t.Helper()
		data, _ := json.Marshal(s)
		info, e := lake.PutObject(f.ctx, bucket, "orders.json", bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: "application/json"})
		if e != nil {
			t.Fatal(e)
		}
		return info
	}
	lakeInfo := putLake(snapshot)

	role := "reader_" + strings.ReplaceAll(strings.TrimPrefix(f.project, "pilot-e2e-"), "-", "")
	for _, sql := range []string{`CREATE TABLE native_orders(event_id text PRIMARY KEY,event_time timestamptz NOT NULL,status text NOT NULL)`, `CREATE TABLE native_coverage(watermark timestamptz NOT NULL)`, `CREATE VIEW native_orders_view AS SELECT * FROM native_orders`, `CREATE VIEW native_watermark_view AS SELECT * FROM native_coverage`, "CREATE ROLE " + pgx.Identifier{role}.Sanitize() + " LOGIN PASSWORD 'synthetic-reader-only'", "GRANT SELECT ON native_orders_view,native_watermark_view TO " + pgx.Identifier{role}.Sanitize()} {
		if _, err = f.repo.Pool().Exec(f.ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = f.repo.Pool().Exec(ctx, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize())
		_, _ = f.repo.Pool().Exec(ctx, "DROP ROLE "+pgx.Identifier{role}.Sanitize())
	})
	if _, err = f.repo.Pool().Exec(f.ctx, `INSERT INTO native_orders VALUES('order-1',$1,'processed')`, when); err != nil {
		t.Fatal(err)
	}
	if _, err = f.repo.Pool().Exec(f.ctx, `INSERT INTO native_coverage VALUES($1)`, end); err != nil {
		t.Fatal(err)
	}
	dsn, _ := url.Parse(f.env["DATABASE_URL"])
	dsn.User = url.UserPassword(role, "synthetic-reader-only")
	readOnly, err := pgx.Connect(f.ctx, dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = readOnly.Exec(f.ctx, `DELETE FROM native_orders`); err == nil {
		t.Fatal("native audit reader has write privileges")
	}
	_ = readOnly.Close(f.ctx)

	watermark, _ := json.Marshal(map[string]any{"_watermark": end})
	push, _ := json.Marshal(map[string]any{"streams": []any{map[string]any{"stream": map[string]string{"stream": topic}, "values": [][]string{{strconv.FormatInt(when.UnixNano(), 10), string(raw)}, {strconv.FormatInt(end.Add(-time.Nanosecond).UnixNano(), 10), string(watermark)}}}}})
	response, err := http.Post(os.Getenv("TEST_LOKI_ENDPOINT")+"/loki/api/v1/push", "application/json", bytes.NewReader(push))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatal("native log push", response.StatusCode)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	tokenPath := f.write("native-adapter.token", []byte("synthetic-native-view-token"))
	backends := map[string]sourceadapter.Backend{
		"event":  {Kind: "kafka", Brokers: []string{os.Getenv("TEST_KAFKA_ADDRESS")}, Topic: topic, Partitions: []int32{0}},
		"lake":   {Kind: "s3", Endpoint: os.Getenv("TEST_S3_ENDPOINT"), Bucket: bucket, Object: "orders.json", CredentialFile: f.writeJSON("lake-credentials.json", sourceadapter.S3Credential{AccessKey: "synthetic-acceptance", SecretKey: "synthetic-acceptance-only"})},
		"audit":  {Kind: "postgres", DatabaseURLFile: f.write("audit-dsn", []byte(dsn.String())), View: "public.native_orders_view", WatermarkView: "public.native_watermark_view"},
		"log":    {Kind: "loki", Endpoint: os.Getenv("TEST_LOKI_ENDPOINT"), Query: `{stream="` + topic + `"}`, ExpectedStreams: []string{topic}},
		"metric": {Kind: "prometheus", Endpoint: os.Getenv("TEST_PROMETHEUS_ENDPOINT"), Query: "vector(1)", WatermarkQuery: fmt.Sprintf("vector(%d)", end.Unix())},
	}
	if len(dynamic) > 0 && dynamic[0] {
		b := backends["metric"]
		b.Query = `synthetic_processed{project="` + f.project + `"}`
		b.WatermarkQuery = `synthetic_watermark{project="` + f.project + `"}`
		backends["metric"] = b
	}
	views := []sourceadapter.View{}
	contracts := []domain.SourceDescriptor{}
	for _, kind := range []string{"event", "lake", "audit", "log", "metric"} {
		backend := backends[kind]
		backend.MaxScanRecords = 100
		backend.AllowPlaintextLocal = true
		fields := map[string]string{"event_id": "string", "event_time": "timestamp", "status": "string"}
		if kind == "metric" {
			fields = map[string]string{"event_id": "string", "event_time": "timestamp", "value": "number"}
		}
		contract := domain.SourceDescriptor{ID: kind, ProjectID: f.project, ProductID: "labels", Kind: kind, Endpoint: "http://" + address + "/query", TokenFile: tokenPath, AdapterSHA256: backend.Digest(), AllowLoopbackHTTP: true, SchemaVersion: "orders/v1", Classification: "internal", Owner: "human:local-developer", Roles: []string{"operations", "sdlc_diagnosis", "sdlc_evaluator"}, Purposes: []string{"diagnosis", "recovery"}, Fields: fields, MaxRows: 20, MaxBytes: 65536, TimeoutSeconds: 20, MaxWindowSeconds: 60, RetentionSeconds: 3600}
		views = append(views, sourceadapter.View{Contract: contract, Backend: backend})
		contracts = append(contracts, contract)
	}
	config := sourceadapter.Config{Schema: "hybrid-ai/source-adapter/v1", Address: address, TokenFile: tokenPath, Views: views}
	command := f.command("source-adapter", "", "--config", f.writeJSON("native-views.json", config))
	command.Env = []string{"PATH=" + os.Getenv("PATH")}
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Signal(os.Interrupt)
		_ = command.Wait()
		if t.Failed() {
			t.Log(diagnostics.String())
		}
	})
	f.wait("native source adapter", func() bool {
		r, e := http.Get("http://" + address + "/query")
		if e != nil {
			return false
		}
		defer r.Body.Close()
		return r.StatusCode == 404
	})
	f.stopGateway()
	f.env["PRODUCT_SOURCE_REGISTRY"] = f.writeJSON("native-sources.json", contracts)
	f.stopGateway = f.start("gateway")
	waitExecutionGateway(f)
	verify := func() {
		receipts := []domain.SourceReceipt{}
		for _, contract := range contracts {
			fields := []string{"event_id", "event_time", "status"}
			if contract.Kind == "metric" {
				fields[2] = "value"
			}
			q := domain.SourceQuery{ProjectID: f.project, ProductID: "labels", SourceID: contract.ID, Purpose: "diagnosis", Start: start, End: end, Fields: fields, Limit: 20, IdempotencyKey: "native-read"}
			var receipt, again domain.SourceReceipt
			f.call(f.operator, "product_source_query", q, true, &receipt)
			if receipt.Status != "complete" || !receipt.Source.Complete || receipt.RecordID == "" {
				t.Fatalf("native %s lacks complete validated receipt: %+v", contract.Kind, receipt)
			}
			f.call(f.operator, "product_source_query", q, true, &again)
			if again.ID != receipt.ID {
				t.Fatal("native read retry duplicated observation")
			}
			var record domain.ProductRecord
			f.call(f.operator, "product_record_get", map[string]any{"project_id": f.project, "record_id": receipt.RecordID, "current_only": true}, true, &record)
			if record.Origin != "source_adapter" || record.Status != "observed" {
				t.Fatal("native observed evidence lost its origin")
			}
			receipts = append(receipts, receipt)
			q.Fields = append(q.Fields, "password")
			q.IdempotencyKey = "forbidden-field"
			f.call(f.operator, "product_source_query", q, false, nil)
		}
		if after := offset(); after != before {
			t.Fatalf("observer moved application group: %d -> %d", before, after)
		}
		if !strings.Contains(receipts[1].Source.Snapshot, lakeInfo.VersionID) {
			t.Fatal("lake native version not retained")
		}
		q := domain.SourceQuery{ProjectID: f.project, ProductID: "labels", SourceID: "lake", Purpose: "diagnosis", Start: start, End: end, Fields: []string{"event_id", "event_time", "status"}, Limit: 20, IdempotencyKey: "late-lake"}
		snapshot.Watermark = start
		_ = putLake(snapshot)
		var late domain.SourceReceipt
		f.call(f.operator, "product_source_query", q, true, &late)
		if late.Status != "partial" || late.Source.Complete {
			t.Fatal("delayed lake claimed complete")
		}
		receipts = append(receipts, late)
		snapshot.SchemaVersion = "unreviewed/v9"
		_ = putLake(snapshot)
		time.Sleep(25 * time.Millisecond)
		q.IdempotencyKey = "bad-schema"
		var invalid domain.SourceReceipt
		f.call(f.operator, "product_source_query", q, false, nil)
		invalid, err = f.repo.GetSourceReceipt(f.ctx, f.project, q.SourceID, "human:local-developer", q.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		if invalid.Status != "failed" || invalid.RecordID != "" {
			t.Fatal("invalid native data entered KB")
		}
		receipts = append(receipts, invalid)
		root := filepath.Join(os.Getenv("AGENT_READY_REPORT_DIR"), t.Name())
		if err = os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		evidence, _ := json.MarshalIndent(map[string]any{"kind": "native_protocol_fixture", "receipts": receipts, "application_offset_before": before, "application_offset_after": offset()}, "", "  ")
		if err = os.WriteFile(filepath.Join(root, "sources.json"), evidence, 0400); err != nil {
			t.Fatal(err)
		}
	}
	return nativeFixture{verify: verify, client: client, lake: lake, bucket: bucket, topic: topic, contracts: contracts, config: config}
}
